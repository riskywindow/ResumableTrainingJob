# Phase 7 Troubleshooting

Common issues with capacity-guaranteed launch and their solutions.

---

## Built-in AdmissionCheck inactive

**Symptom:** The ProvisioningRequest AdmissionCheck exists but is not
being evaluated. The Workload's `admissionChecks` list is empty or the
check stays in `Pending` state indefinitely. No ProvisioningRequest
object is created.

**Possible causes:**

1. **ProvisioningACC feature gate not enabled.** Kueue's built-in
   ProvisioningRequest AdmissionCheck controller requires the
   `ProvisioningACC` feature gate (default-on in Kueue v0.15.1, but may
   be disabled):

   ```bash
   # Check the Kueue config
   kubectl -n kueue-system get configmap kueue-manager-config \
     -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -A5 featureGates
   ```

   If `ProvisioningACC: false` is set explicitly, the controller is disabled.

2. **AdmissionCheck controllerName mismatch.** The check must use
   `kueue.x-k8s.io/provisioning`:

   ```bash
   kubectl get admissionchecks.kueue.x-k8s.io dev-provisioning \
     -o jsonpath='controllerName={.spec.controllerName}'
   ```

   If the controllerName is wrong, Kueue will not process the check.

3. **AdmissionCheck not wired to ClusterQueue.** The check must be
   referenced in the ClusterQueue's `admissionChecksStrategy`:

   ```bash
   kubectl get clusterqueues.kueue.x-k8s.io phase7-cq \
     -o jsonpath='{.spec.admissionChecksStrategy.admissionChecks}'
   ```

4. **ProvisioningRequestConfig missing.** The AdmissionCheck references
   a ProvisioningRequestConfig via `spec.parameters`:

   ```bash
   kubectl get admissionchecks.kueue.x-k8s.io dev-provisioning \
     -o jsonpath='{.spec.parameters}'
   ```

   Check that the referenced config exists:

   ```bash
   kubectl get provisioningrequestconfigs.kueue.x-k8s.io
   ```

5. **Kueue controller not running.** Verify Kueue is healthy:

   ```bash
   kubectl -n kueue-system get pods
   kubectl -n kueue-system logs deployment/kueue-controller-manager --tail=20
   ```

**Resolution:** Re-apply the Phase 7 profile to ensure all resources are
correctly configured:

```bash
make phase7-profile
make phase7-smoke
```

---

## ProvisioningRequest not created

**Symptom:** The RTJ is admitted (Workload has quota reserved), the
AdmissionCheck is on the Workload in `Pending` state, but no
ProvisioningRequest object appears in the namespace.

**Possible causes:**

1. **Kueue has not reconciled yet.** Kueue's ProvisioningRequest AC
   controller creates the PR asynchronously. Wait a few seconds:

   ```bash
   sleep 10
   kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io
   ```

2. **ProvisioningRequest CRD not installed.** The dev-only CRD must be
   present:

   ```bash
   kubectl get crd provisioningrequests.autoscaling.x-k8s.io
   ```

   If missing, re-apply:

   ```bash
   kubectl apply --server-side -f deploy/dev/phase7/provisioning/00-provisioning-request-crd.yaml
   ```

3. **Workload not yet admitted.** Kueue creates PRs only after quota is
   reserved. Check Workload status:

   ```bash
   WORKLOAD=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
     -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}")
   kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
     -o jsonpath='{.status.admission.clusterQueue}'
   ```

   If empty, the Workload is not yet admitted. Check queue capacity.

4. **Kueue RBAC issue.** Kueue needs permission to create PRs:

   ```bash
   kubectl -n kueue-system logs deployment/kueue-controller-manager --tail=50 | grep -i "provisioning\|forbidden"
   ```

**Resolution:** Verify the CRD is installed and the Workload is admitted.
The smoke test covers all prerequisites:

```bash
make phase7-smoke
```

---

## ProvisioningRequest stuck pending

**Symptom:** The ProvisioningRequest exists but never transitions to
`Provisioned=True` or `Failed=True`. It stays without conditions or with
only internal conditions.

**Possible causes:**

1. **Fake provisioner not running.** In the dev environment, the fake
   backend must be deployed:

   ```bash
   kubectl -n checkpoint-dev get deployment fake-provisioner
   kubectl -n checkpoint-dev get pods -l app=fake-provisioner
   ```

   If not running:

   ```bash
   make phase7-profile
   ```

2. **Fake provisioner crash-looping.** Check pod status and logs:

   ```bash
   kubectl -n checkpoint-dev describe pod -l app=fake-provisioner
   kubectl -n checkpoint-dev logs -l app=fake-provisioner --tail=30
   ```

3. **Unknown provisioningClassName.** The fake backend only handles
   three classes: `check-capacity.fake.dev`, `failed.fake.dev`,
   `booking-expiry.fake.dev`. If the PR uses a different class, it is
   ignored:

   ```bash
   kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io <pr-name> \
     -o jsonpath='class={.spec.provisioningClassName}'
   ```

4. **Fake provisioner RBAC issue.** The fake backend needs permission to
   get/update ProvisioningRequest status:

   ```bash
   kubectl -n checkpoint-dev logs -l app=fake-provisioner | grep -i "forbidden\|rbac"
   ```

   Verify RBAC:

   ```bash
   kubectl get clusterrole fake-provisioner -o yaml
   kubectl get clusterrolebinding fake-provisioner -o yaml
   ```

5. **In production (not dev):**, the real provisioning backend
   (cluster-autoscaler or similar) must be configured and healthy.

**Resolution:** For the dev environment, ensure the fake provisioner is
running and the ProvisioningRequest uses a recognized class:

```bash
kubectl -n checkpoint-dev rollout restart deployment/fake-provisioner
kubectl -n checkpoint-dev rollout status deployment/fake-provisioner
```

---

## Conflicting podSetUpdates

**Symptom:** The RTJ transitions to `PhaseFailed` with a condition
`LaunchBlockedByConflictingPodSetUpdate`. The child JobSet is not created.

**Possible causes:**

1. **AdmissionCheck podSetUpdates conflict with existing template.**
   The ProvisioningRequest AC (or any AC) may inject nodeSelector,
   labels, annotations, or tolerations that conflict with existing values
   in the RTJ's JobSet template.

   Check the error message:

   ```bash
   kubectl -n checkpoint-dev get rtj <rtj-name> \
     -o jsonpath='{range .status.conditions[?(@.type=="LaunchBlockedByConflictingPodSetUpdate")]}{.message}{"\n"}{end}'
   ```

   The message identifies the conflicting field, key, existing value,
   and update value.

2. **Multiple ACs injecting conflicting values.** If multiple ACs
   (e.g., ProvisioningRequest + ResumeReadiness) both inject
   nodeSelector for the same key with different values, the merge fails.

   Check podSetUpdates from each AC:

   ```bash
   make phase7-inspect-workload PHASE7_RTJ_NAME=<rtj-name>
   ```

   Look at the `admissionCheck podSetUpdates` section.

3. **RTJ template already has topology nodeSelector.** If topology-aware
   placement injects `topology.example.io/rack=rack-1` and the
   provisioning AC also tries to set a different value for the same key,
   this is a conflict.

**Resolution:**

- If the conflict is between topology and provisioning, ensure they use
  non-overlapping nodeSelector keys.
- If the conflict is between the RTJ template and an AC, remove the
  conflicting key from the template (let the AC provide it).
- Same-value updates are NOT conflicts. Only different values for the
  same key cause failures.

To recover after fixing the cause:

```bash
kubectl -n checkpoint-dev delete rtj <rtj-name>
# Re-submit with fixed template
```

---

## Topology second pass never completing

**Symptom:** The RTJ has `launchGateState: Blocked` with reason
`TopologyPendingSecondPass`. Provisioning has succeeded but the topology
assignment never appears.

**Possible causes:**

1. **Topology not configured on ClusterQueue.** Check the queue:

   ```bash
   kubectl get clusterqueues.kueue.x-k8s.io phase7-cq -o yaml | grep -A10 topology
   ```

2. **Kueue TAS not assigning topology.** The Topology Aware Scheduling
   feature may not be enabled in Kueue:

   ```bash
   kubectl -n kueue-system get configmap kueue-manager-config \
     -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -i topology
   ```

3. **Delayed topology request stuck.** Check the PodSetAssignment:

   ```bash
   WORKLOAD=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
     -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}")
   kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
     -o jsonpath='{range .status.admission.podSetAssignments[*]}  name={.name}  delayed={.delayedTopologyRequest}{"\n"}{end}'
   ```

4. **Combined provisioning + topology not supported in Kueue v0.15.1.**
   This is an open question (OQ3 in the session handoff). If Kueue
   v0.15.1 does not handle topology assignment after provisioning
   completion, this is a known limitation.

**Resolution:**

- If topology is not needed, ensure the RTJ does not request topology
  (`spec.topologyRequest` is nil or disabled).
- If topology IS needed, verify the Kueue TAS feature is properly
  configured. Check the Kueue logs for topology assignment errors.
- If this is a Kueue limitation, consider upgrading Kueue or using
  provisioning without topology.

---

## waitForPodsReady timeout confusion

**Symptom:** The RTJ's `status.startupRecovery.startupState` shows
`StartupTimedOut` or `RecoveryTimedOut`, but the operator believes this
is a manual pause or preemption. Or conversely, a manual pause is
being confused with a timeout eviction.

**Possible causes:**

1. **Understanding the distinction.** These are different eviction paths:

   | Source | RTJ behavior |
   |--------|-------------|
   | Manual pause | User sets `spec.desiredState: Paused` → operator drains gracefully |
   | Kueue preemption | Kueue sets `spec.suspend: true` → operator observes suspension |
   | waitForPodsReady timeout | Kueue sets `Evicted` condition with `reason: PodsReadyTimeout` |

   The RTJ operator distinguishes them via the Workload's condition
   reason. Manual pause is completely decoupled (enters via
   `stopSourceManual`).

2. **Startup vs recovery timeout confusion.** Both use the same Kueue
   eviction reason (`PodsReadyTimeout`). The RTJ operator distinguishes
   them based on prior running state:

   - **Startup timeout**: pods never reached Running after launch
   - **Recovery timeout**: pods were Running but lost readiness

   Check:

   ```bash
   kubectl -n checkpoint-dev get rtj <rtj-name> \
     -o jsonpath=$'startupState={.status.startupRecovery.startupState}\nevictionReason={.status.startupRecovery.evictionReason}\n'
   ```

3. **Operator restart during eviction.** The eviction classification is
   idempotent and re-derived on restart. After an operator restart, the
   same classification should be reached from the Workload condition +
   prior RTJ status.

4. **waitForPodsReady not enabled.** If the Kueue config does not have
   `waitForPodsReady.enable: true`, evictions will not occur:

   ```bash
   kubectl -n kueue-system get configmap kueue-manager-config \
     -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -A5 waitForPodsReady
   ```

5. **Timeout too short or too long.** The default dev profile uses 120s.
   Adjust via the Kueue config:

   ```bash
   # Check current timeout
   kubectl -n kueue-system get configmap kueue-manager-config \
     -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -A10 waitForPodsReady
   ```

**Resolution:**

- Check `status.startupRecovery.evictionReason` to confirm the eviction
  source is `PodsReadyTimeout` (not `Preempted` or `InactiveWorkload`).
- If there is no eviction, verify waitForPodsReady is enabled in Kueue.
- If the timeout is too aggressive, increase `timeout` in the Kueue
  config and restart Kueue:

  ```bash
  make phase7-profile  # re-applies Kueue config
  ```

---

## Fake backend misconfiguration

**Symptom:** The fake provisioner is deployed but ProvisioningRequests
are not being processed, or they behave unexpectedly (wrong delay,
wrong failure message, etc.).

**Possible causes:**

1. **Wrong provisioningClassName.** The fake backend recognizes three
   classes:

   | Class | Behavior |
   |-------|----------|
   | `check-capacity.fake.dev` | Delayed success (default 10s) |
   | `failed.fake.dev` | Permanent failure |
   | `booking-expiry.fake.dev` | Success then capacity revoked |

   Check the ProvisioningRequestConfig:

   ```bash
   kubectl get provisioningrequestconfigs.kueue.x-k8s.io -o yaml
   ```

   Verify the `provisioningClassName` in the config matches one of the
   above.

2. **Custom parameters not applied.** The fake backend reads parameters
   from the ProvisioningRequest's `spec.parameters`:

   ```bash
   kubectl -n checkpoint-dev get provisioningrequests.autoscaling.x-k8s.io <pr-name> \
     -o jsonpath='{.spec.parameters}'
   ```

   Supported parameters:

   | Parameter | Default | Description |
   |-----------|---------|-------------|
   | `fake.dev/delay` | `10s` | Time before success |
   | `fake.dev/expiry` | `60s` | Time after success before revocation |
   | `fake.dev/failure-message` | `"capacity unavailable"` | Custom failure message |

3. **Fake provisioner image not loaded.** The image must be built and
   loaded into the kind cluster:

   ```bash
   make phase7-build-fake-provisioner
   kind load docker-image fake-provisioner:dev --name checkpoint-phase1
   kubectl -n checkpoint-dev rollout restart deployment/fake-provisioner
   ```

4. **Fake provisioner using stale image.** After code changes, rebuild
   and reload:

   ```bash
   make phase7-build-fake-provisioner
   make phase7-load-images
   kubectl -n checkpoint-dev rollout restart deployment/fake-provisioner
   ```

5. **RBAC insufficient.** The fake provisioner needs cluster-wide access
   to ProvisioningRequest resources:

   ```bash
   kubectl get clusterrole fake-provisioner -o yaml
   ```

   Required verbs: `get`, `list`, `watch`, `update`, `patch`.

**Resolution:**

```bash
# Full rebuild and redeploy
make phase7-build-fake-provisioner
make phase7-load-images
kubectl -n checkpoint-dev rollout restart deployment/fake-provisioner
kubectl -n checkpoint-dev rollout status deployment/fake-provisioner

# Verify logs
kubectl -n checkpoint-dev logs -l app=fake-provisioner --tail=20
```

---

## Quick diagnostic checklist

When investigating Phase 7 issues, run through this checklist:

```bash
# 1. Is the operator running with Phase 7 support?
# Look for phase7Metrics=true in startup log
kubectl logs <operator-pod> | head -10

# 2. Is the Phase 7 infrastructure healthy?
make phase7-smoke

# 3. What is the launch gate state?
make phase7-inspect-launchgate PHASE7_RTJ_NAME=<rtj-name>

# 4. What is the Workload admission state?
make phase7-inspect-workload PHASE7_RTJ_NAME=<rtj-name>

# 5. Is there a ProvisioningRequest?
make phase7-inspect-provisioningrequest PHASE7_RTJ_NAME=<rtj-name>

# 6. What are the startup/recovery details?
make phase7-inspect-checkpoints PHASE7_RTJ_NAME=<rtj-name>

# 7. Is the fake provisioner running?
kubectl -n checkpoint-dev get deployment fake-provisioner
kubectl -n checkpoint-dev logs -l app=fake-provisioner --tail=10

# 8. What do the metrics say?
curl -s http://localhost:8080/metrics | grep -E 'launch_gate|provisioning|capacity_guaranteed'
```
