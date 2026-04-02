# Phase 7 Operations Guide

This document explains how to inspect and operate Phase 7 capacity-guaranteed
launch resources.

## Inspect RTJ launch-gate status

The RTJ's `status.launchGate` sub-object contains the aggregate gate state
and per-AdmissionCheck summary. Use the inspect-launchgate script or direct
kubectl:

```bash
# Via Makefile
make phase7-inspect-launchgate PHASE7_RTJ_NAME=<rtj-name>

# Direct kubectl — launch gate state
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.status.launchGate.launchGateState}'

# Direct kubectl — full launch gate object
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.status.launchGate}' | python3 -m json.tool
```

Key fields:

| Field | Description |
|-------|-------------|
| `launchGateState` | Aggregate state: Open, Blocked, or Unknown |
| `message` | Human-readable explanation when Blocked |
| `admissionCheckSummary` | Per-AC name/state pairs from the Workload |

The launch gate is `Open` when ALL of the following are true:
1. Quota is reserved (Workload has admission)
2. All AdmissionChecks are in `Ready` state
3. Topology second-pass is complete (when topology is configured)

When the launch gate is `Blocked`, no child JobSet will be created.

## Inspect Workload admissionChecks

AdmissionChecks are Kueue's mechanism for gating admission beyond quota.
Phase 7 uses the built-in ProvisioningRequest AdmissionCheck.

```bash
# Via Makefile
make phase7-inspect-workload PHASE7_RTJ_NAME=<rtj-name>

# Direct kubectl — find the Workload owned by the RTJ
WORKLOAD=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}")

# List admission checks on the Workload
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='{range .status.admissionChecks[*]}  name={.name}  state={.state}  message={.message}{"\n"}{end}'
```

AdmissionCheck states:

| State | Meaning |
|-------|---------|
| Pending | Check not yet evaluated or still in progress |
| Ready | Check passed; this AC is satisfied |
| Retry | Temporary failure; Kueue will retry |
| Rejected | Permanent failure; Kueue may evict the Workload |

To check the AdmissionCheck configuration on the ClusterQueue:

```bash
kubectl get clusterqueues.kueue.x-k8s.io phase7-cq \
  -o jsonpath='{.spec.admissionChecksStrategy.admissionChecks}'
```

To check the AdmissionCheck controller registration:

```bash
kubectl get admissionchecks.kueue.x-k8s.io dev-provisioning \
  -o jsonpath='controllerName={.spec.controllerName}  parameters={.spec.parameters}'
```

Expected: `controllerName=kueue.x-k8s.io/provisioning`.

### podSetUpdates from AdmissionChecks

AdmissionChecks can inject podSetUpdates (labels, nodeSelector, annotations,
tolerations) into the Workload. The RTJ operator merges these into the child
JobSet.

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='{range .status.admissionChecks[*]}  check={.name}:{"\n"}{range .podSetUpdates[*]}    podSet={.name} nodeSelector={.nodeSelector}{"\n"}{end}{end}'
```

## Inspect ProvisioningRequest objects

ProvisioningRequests are created by Kueue's built-in ProvisioningRequest
AdmissionCheck controller. They represent requests for physical capacity.

```bash
# Via Makefile
make phase7-inspect-provisioningrequest PHASE7_RTJ_NAME=<rtj-name>

# List all ProvisioningRequests in the dev namespace
kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io

# Inspect a specific ProvisioningRequest
kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io <pr-name> -o yaml
```

ProvisioningRequest naming convention (Kueue v0.15.1):
`{workload-name}-{check-name}-{attempt}`.

Key fields to check:

```bash
# Provisioning class
kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io <pr-name> \
  -o jsonpath='class={.spec.provisioningClassName}'

# Conditions (Provisioned, Failed, CapacityRevoked)
kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io <pr-name> \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}): {.message}{"\n"}{end}'

# Creation timestamp (used by fake backend for delay computation)
kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io <pr-name> \
  -o jsonpath='created={.metadata.creationTimestamp}'
```

### ProvisioningRequestConfig

The ProvisioningRequestConfig links an AdmissionCheck to the provisioning
backend parameters:

```bash
# List configs
kubectl get provisioningrequestconfigs.kueue.x-k8s.io

# Inspect the default config
kubectl get provisioningrequestconfigs.kueue.x-k8s.io dev-provisioning-config -o yaml
```

## Inspect delayed topology vs topology assignment

When topology-aware scheduling is configured alongside provisioning, the
launch gate has two sequential requirements:

1. All AdmissionChecks must be Ready (including ProvisioningRequest AC)
2. Topology assignment must be present on PodSetAssignments

### Check topology assignment on the Workload

```bash
WORKLOAD=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}")

# PodSetAssignment topology
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='{range .status.admission.podSetAssignments[*]}  name={.name}  topologyAssignment={.topologyAssignment}{"\n"}{end}'

# Delayed topology request state
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='{range .status.admission.podSetAssignments[*]}  name={.name}  delayedTopologyRequest={.delayedTopologyRequest}{"\n"}{end}'
```

### Check RTJ topology gate state

The RTJ status surfaces the topology gate observation:

```bash
# Via launch gate inspection
make phase7-inspect-launchgate PHASE7_RTJ_NAME=<rtj-name>

# Direct kubectl — look for TopologyPendingSecondPass condition
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{range .status.conditions[?(@.type=="TopologyPendingSecondPass")]}{.status} {.reason}: {.message}{"\n"}{end}'
```

Topology gate states:

| State | Meaning |
|-------|---------|
| NotConfigured | Topology is not enabled on this RTJ |
| Pending | Topology second-pass not yet complete |
| Assigned | TopologyAssignment present on all PodSetAssignments |

## Confirm no child JobSet launched too early

The critical Phase 7 invariant: **no child JobSet before capacity guarantee**.

### Quick check

```bash
# RTJ phase + active JobSet in one command
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath=$'phase={.status.phase}\nactiveJobSet={.status.activeJobSetName}\nlaunchGate={.status.launchGate.launchGateState}\ncapacityGuaranteed={.status.capacity.capacityGuaranteed}\n'
```

If `launchGateState=Blocked` and `activeJobSetName` is non-empty, there is
a bug. This should never happen.

### Systematic check

```bash
# 1. Verify launch gate is Blocked
make phase7-inspect-launchgate PHASE7_RTJ_NAME=<rtj-name>

# 2. Verify no child JobSets exist for this RTJ
kubectl -n checkpoint-dev get jobsets.jobset.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=<rtj-name>

# 3. Verify no pods from this RTJ
kubectl -n checkpoint-dev get pods \
  -l training.checkpoint.example.io/rtj-name=<rtj-name>
```

### Verify the full lifecycle invariant

For the delayed-success flow, the expected sequence is:

1. RTJ submitted → Kueue creates Workload → quota reserved
2. ProvisioningRequest created → `provisioningState: Pending`
3. `launchGateState: Blocked`, **no child JobSet**
4. Fake backend sets `Provisioned=True` after delay
5. AC transitions to Ready → `launchGateState: Open`
6. Child JobSet created → pods scheduled

Validate each step:

```bash
# Watch the full lifecycle
watch -n 3 "kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{\"phase=\"}{.status.phase}{\" gate=\"}{.status.launchGate.launchGateState}{\" prov=\"}{.status.provisioning.provisioningState}{\" cap=\"}{.status.capacity.capacityGuaranteed}{\" jobset=\"}{.status.activeJobSetName}'"
```

## Prometheus metrics

Query Phase 7 metrics from the operator's metrics endpoint:

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_rtjs_by_launch_gate
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_provisioning_states
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_launches_blocked
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_startup_timeout
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_recovery_timeout
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_capacity_guaranteed
```

## List all Phase 7 RTJs

```bash
kubectl -n checkpoint-dev get rtj \
  -o custom-columns='NAME:.metadata.name,PHASE:.status.phase,GATE:.status.launchGate.launchGateState,PROV:.status.provisioning.provisioningState,STARTUP:.status.startupRecovery.startupState,CAP:.status.capacity.capacityGuaranteed,JOBSET:.status.activeJobSetName'
```
