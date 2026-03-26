# Phase 4 Troubleshooting

Common problems and diagnostic steps for the Phase 4 admission pipeline.

---

## AdmissionCheck Inactive or Misconfigured

### Symptom

The RTJ stays in `Queued` phase indefinitely. The Workload never gets
admitted despite available capacity.

### Diagnosis

```bash
make phase4-inspect-admissioncheck
```

Check the `Active` condition:

```bash
kubectl get admissionchecks.kueue.x-k8s.io resume-readiness \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} reason={.reason}{"\n"}{end}'
```

### Common causes and fixes

**Active=False, reason=PolicyNotFound**

The `spec.parameters` on the AdmissionCheck references a
`ResumeReadinessPolicy` that does not exist.

```bash
# Check what policy is referenced.
kubectl get admissionchecks.kueue.x-k8s.io resume-readiness \
  -o jsonpath='{.spec.parameters}'

# Verify the policy exists.
kubectl get resumereadinesspolicies.training.checkpoint.example.io

# Fix: create the missing policy.
kubectl apply -f deploy/dev/admissionchecks/resume-readiness-policy.yaml
```

**Active=False, reason=ParametersMissing**

The AdmissionCheck has no `spec.parameters` or references the wrong kind.

```bash
# Inspect the AdmissionCheck.
kubectl get admissionchecks.kueue.x-k8s.io resume-readiness -o yaml

# Fix: ensure parameters references the correct kind.
# spec.parameters.apiGroup: training.checkpoint.example.io
# spec.parameters.kind: ResumeReadinessPolicy
# spec.parameters.name: <policy-name>
```

**controllerName mismatch**

The `spec.controllerName` does not match `training.checkpoint.example.io/resume-readiness`.

```bash
kubectl get admissionchecks.kueue.x-k8s.io resume-readiness \
  -o jsonpath='{.spec.controllerName}'
```

This field is immutable — delete and recreate the AdmissionCheck.

**ClusterQueue does not reference the check**

The AdmissionCheck may be healthy but not wired into the queue.

```bash
kubectl get clusterqueues.kueue.x-k8s.io phase4-cq -o yaml | grep -A5 admissionChecks
```

Fix: add the check to `spec.admissionChecksStrategy.admissionChecks`.

---

## Readiness Gate Stuck

### Symptom

The RTJ shows `status.launchReadiness.gateState=Pending` and never
transitions to `Ready`. The child JobSet is never created.

### Diagnosis

```bash
make phase4-inspect-workload PHASE4_RTJ_NAME=<rtj-name>
make phase4-inspect-admissioncheck
```

Check the Workload's admission check state:

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .status.admissionChecks[*]}name={.name} state={.state} message={.message}{"\n"}{end}'
```

### Common causes and fixes

**Check state=Pending, no message**

The resume-readiness controller has not reconciled this Workload yet.
Possible causes:
- Operator is not running or has crashed
- RBAC is missing for AdmissionCheck or Workload resources

```bash
# Check operator logs.
kubectl -n checkpoint-dev logs deploy/checkpoint-operator | grep -i "resume\|admission\|readiness"

# Check RBAC.
kubectl auth can-i update workloads.kueue.x-k8s.io/status --as=system:serviceaccount:checkpoint-dev:checkpoint-operator -n checkpoint-dev
```

**Check state=Retry, message=PolicyResolutionFailed**

The Workload reconciler could not resolve the policy. The AdmissionCheck
parameters reference is broken or the policy was deleted.

```bash
make phase4-inspect-admissioncheck
# Fix: recreate the policy.
kubectl apply -f deploy/dev/admissionchecks/resume-readiness-policy.yaml
```

**Check state=Retry, message=StorageUnavailable**

The checkpoint catalog/store is unreachable and `failurePolicy=FailClosed`.

```bash
# Check MinIO is running.
kubectl -n checkpoint-dev get pods -l app=minio

# Check operator logs for catalog errors.
kubectl -n checkpoint-dev logs deploy/checkpoint-operator | grep -i "catalog\|storage\|minio"
```

Fix: restore storage access, or temporarily set `failurePolicy: FailOpen`.

**Check state=Rejected**

The gate permanently rejected the launch. Check the reason:
- `NoCheckpointAvailable` — no checkpoint + `allowInitial=false`
- `CheckpointTooOld` — checkpoint exceeds `maxCheckpointAge`
- `CheckpointIncompatible` — no compatible checkpoint

The RTJ will not retry until re-queued. Fix the underlying issue
(create a checkpoint, update the policy) and delete the Workload
to trigger re-admission.

---

## Topology Request Emitted but No Topology Assignment Surfaced

### Symptom

The RTJ shows `status.launchReadiness.reason=WaitingForTopologyAssignment`
and never progresses. The Workload has a `TopologyRequest` but no
`TopologyAssignment`.

### Diagnosis

```bash
make phase4-inspect-topology PHASE4_RTJ_NAME=<rtj-name>
```

Check the Workload's PodSet spec vs admission:

```bash
# Verify TopologyRequest is on the PodSet spec.
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .spec.podSets[*]}name={.name} topologyRequest={.topologyRequest}{"\n"}{end}'

# Check if admission has topology data.
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .status.admission.podSetAssignments[*]}name={.name} topology={.topologyAssignment}{"\n"}{end}'
```

### Common causes and fixes

**Kueue TAS feature gate not enabled**

```bash
# Check Kueue config.
kubectl -n kueue-system get configmap kueue-manager-config \
  -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -i topology
```

Fix: enable `TopologyAwareScheduling` feature gate. The Phase 4 profile
does this automatically:

```bash
./hack/dev/install-phase4-profile.sh
```

**Topology object does not exist**

```bash
kubectl get topologies.kueue.x-k8s.io
```

Fix: apply the Topology object:

```bash
kubectl apply -f deploy/dev/topology/00-dev-topology.yaml
```

**ResourceFlavor has no topologyName**

```bash
kubectl get resourceflavors.kueue.x-k8s.io phase4-topology \
  -o jsonpath='{.spec.topologyName}'
```

Fix: ensure `spec.topologyName` references the Topology object.

**Nodes missing topology labels**

```bash
kubectl get nodes -L topology.example.io/block -L topology.example.io/rack
```

If labels are missing, re-run:

```bash
./hack/dev/label-kind-nodes.sh
```

**Insufficient capacity in a single topology domain**

With `Required` mode, all pods must fit in one domain. If the domain
is too small, admission blocks.

```bash
# Check domain capacity.
kubectl get nodes -L topology.example.io/rack --no-headers | \
  awk '{print $NF}' | sort | uniq -c
```

Fix: reduce replicas or switch to `Preferred` mode.

---

## Child JobSet Launched Without Expected Topology Patches

### Symptom

The child JobSet exists but pods are not constrained to the expected
topology domain. The pod template has no `nodeSelector` for topology
labels.

### Diagnosis

```bash
make phase4-inspect-topology PHASE4_RTJ_NAME=<rtj-name>
```

Check the child JobSet's nodeSelector:

```bash
active=$(kubectl -n checkpoint-dev get \
  resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')

kubectl -n checkpoint-dev get jobset "$active" \
  -o jsonpath='{range .spec.replicatedJobs[*]}{.name}: {.template.spec.template.spec.template.spec.nodeSelector}{"\n"}{end}'
```

### Common causes and fixes

**RTJ has topology disabled**

```bash
kubectl -n checkpoint-dev get \
  resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.spec.topology}'
```

If `spec.topology` is nil or `mode=Disabled`, the Phase 3 path is used
and no topology injection occurs. Set `spec.topology.mode` to `Required`
or `Preferred`.

**Workload was admitted without TAS data**

If Kueue TAS is not active (feature gate off, Topology object missing),
the Workload may be admitted without a TopologyAssignment. The operator's
gate evaluation notes this and proceeds with a fallback launch.

Check operator logs:

```bash
kubectl -n checkpoint-dev logs deploy/checkpoint-operator | grep "topology"
```

**Phase 3 code path was taken**

When `spec.topology` is nil and `status.workloadReference` is nil, the
controller skips gate evaluation entirely. Ensure the RTJ has topology
configured and is targeting a Kueue-managed queue.

---

## Unsupported Topology Shapes

### Symptom

The RTJ fails with `status.launchReadiness.reason=TopologyNotRepresentable`
or the operator logs show a topology parse error.

### Background

The operator injects topology constraints as `nodeSelector` labels on
the child JobSet pod template. This means all pods in a replicatedJob
share one pod template and therefore one nodeSelector. The operator
supports:

- **Single-domain assignments** — all pods in one domain (trivially representable)
- **Multi-domain assignments with uniform higher levels** — common labels
  (e.g., same zone) extracted as nodeSelector
- **Multi-domain assignments with heterogeneous higher levels** — **not supported**

### Diagnosis

```bash
make phase4-inspect-workload PHASE4_RTJ_NAME=<rtj-name>
```

Check the `launchReadiness.message` for details:

```bash
kubectl -n checkpoint-dev get \
  resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.launchReadiness.message}'
```

Check the operator metrics:

```bash
curl -s http://localhost:8080/metrics | grep unsupported_topology_shape
```

### Common causes and fixes

**Heterogeneous multi-domain assignment**

Kueue assigned pods across domains with different parent levels (e.g.,
some pods in zone-a and some in zone-b). The operator cannot express
this as a single nodeSelector.

This typically happens when:
- The topology has 3+ levels and Kueue spreads across sub-domains
- Cluster capacity forces spreading across different top-level domains

Fixes:
1. Reduce replica count to fit in a single domain
2. Use `Preferred` mode instead of `Required`
3. Wait for future scheduling gate support (per-pod placement)

**Topology assignment has empty levels**

A malformed TopologyAssignment from Kueue. Check the Workload:

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{.status.admission.podSetAssignments[0].topologyAssignment}'
```

This is typically a Kueue bug — file an issue with the Workload YAML.

---

## Quick Diagnostic Checklist

| Check | Command |
|-------|---------|
| Operator running | `kubectl -n checkpoint-dev get pods -l app=checkpoint-operator` |
| AdmissionCheck active | `make phase4-inspect-admissioncheck` |
| RTJ phase and gate | `make phase4-inspect-workload PHASE4_RTJ_NAME=<name>` |
| Topology chain | `make phase4-inspect-topology PHASE4_RTJ_NAME=<name>` |
| Checkpoint evidence | `make phase4-inspect-checkpoints PHASE4_RTJ_NAME=<name>` |
| Kueue TAS enabled | `make phase4-smoke` |
| Node labels | `kubectl get nodes -L topology.example.io/block -L topology.example.io/rack` |
| Operator logs | `kubectl -n checkpoint-dev logs deploy/checkpoint-operator --tail=50` |
| Metrics | `curl -s http://localhost:8080/metrics \| grep phase4` |
