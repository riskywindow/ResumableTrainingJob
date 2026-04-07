# Phase 9 Troubleshooting Guide: Elastic Resize for ResumableTrainingJob

This guide covers common failure modes for the checkpoint-native preemption controller's elastic resize feature (Phase 9). Phase 9 introduces the `spec.elasticity` API on `ResumableTrainingJob` (RTJ) and integrates with Kueue's `reclaimablePods` mechanism for quota release during shrink operations.

**Environment reference:**

- RTJ CRD group: `training.checkpoint.example.io`
- Kueue CRD group: `kueue.x-k8s.io`
- Namespace: `checkpoint-dev`
- Kind cluster context: `kind-checkpoint-phase1`
- Operator mode flag: `--mode=worker` (default) or `--mode=manager`
- SSA field manager: `rtj-elastic-reclaim`

---

## Table of Contents

1. [reclaimablePods not updating](#1-reclaimablepods-not-updating)
2. [Workload status patch conflicts](#2-workload-status-patch-conflicts)
3. [RTJ stays stuck in ResizePending](#3-rtj-stays-stuck-in-resizepending)
4. [Shrink requested but fallback/relaunch not happening](#4-shrink-requested-but-fallbackrelaunch-not-happening)
5. [Grow requested but relaunch blocked](#5-grow-requested-but-relaunch-blocked)
6. [Fixture/runtime not honoring the shrink protocol](#6-fixtureruntime-not-honoring-the-shrink-protocol)

---

## 1. reclaimablePods not updating

### Symptoms

- `status.elasticity.reclaimablePodsPublished` remains `false` after a shrink resize is initiated.
- The Kueue `Workload` object at `kueue.x-k8s.io/v1beta1` does not show an updated `status.reclaimablePods` entry for the worker pod group.
- `status.elasticity.resizeState` transitions to `ShrinkReclaimPublished` never occurs; the RTJ stays in `Shrinking` or `Idle`.
- Quota is not released in Kueue despite the RTJ having fewer admitted workers than the original count.
- No `ShrinkReclaimPublished` condition is set on the RTJ.

### Diagnostic Commands

Check the RTJ's elasticity status and conditions:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> -o jsonpath='{.status.elasticity}' | jq .

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> -o jsonpath='{.status.conditions}' | jq \
  '[.[] | select(.type | test("Resize|Shrink|Reclaim"))]'
```

Inspect the Kueue Workload object's `reclaimablePods` field:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> -o jsonpath='{.status.reclaimablePods}' | jq .
```

Check for SSA field manager ownership on the workload status:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  --show-managed-fields \
  -o json | jq '.metadata.managedFields[] | select(.manager == "rtj-elastic-reclaim")'
```

Look for conflicts or errors in the controller logs (manager mode):

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller --since=10m \
  | grep -E "reclaimable|SSA|field.manager|workload.*patch"
```

Verify the workload admission state:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o jsonpath='{.status.admission}' | jq .
```

Check that the workload reference is populated on the RTJ:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.workloadRef}' | jq .
```

### Root Causes

- **Workload not admitted:** The Kueue `Workload` has not been admitted (`.status.admission` is absent or empty). Kueue ignores `reclaimablePods` patches on non-admitted workloads, so the field silently has no effect on quota.
- **SSA field manager mismatch:** The patch is being applied with a field manager name other than `rtj-elastic-reclaim`. Kubernetes Server-Side Apply (SSA) will reject or silently drop the field if the declared manager does not own the path `status.reclaimablePods`.
- **SSA conflict with Kueue:** Kueue's own controller also writes to `Workload` status. If Kueue has taken ownership of `status.reclaimablePods` under a different manager, the RTJ controller's SSA patch will be rejected with a `409 Conflict` unless `--force-conflicts` is set.
- **Workload reference not set on RTJ:** If `status.workloadRef` is empty or points to the wrong object, the controller cannot locate the target `Workload` and skips the patch entirely.
- **Controller running in wrong mode:** The `reclaimablePods` patch logic executes only when the operator is running with `--mode=manager`. A pod running in the default `--mode=worker` will not attempt this patch.

### Resolution Steps

1. Confirm the `Workload` is admitted before expecting quota release. If it is not admitted, investigate the Kueue admission queue separately; `reclaimablePods` cannot be effective on an unadmitted workload.

2. Verify the SSA manager name used in the patch call matches `rtj-elastic-reclaim` exactly. Search the controller source for the `ManagedFields` manager string and correct any drift.

3. If Kueue has taken conflicting ownership, apply the patch with `--force-conflicts` (SSA option) or coordinate with the Kueue maintainer to allow the RTJ manager to co-own the field. Example patch call with force:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch workload <workload-name> \
     --subresource=status \
     --type=merge \
     --field-manager=rtj-elastic-reclaim \
     -p '{"status":{"reclaimablePods":[{"name":"workers","count":<target>}]}}'
   ```

4. If `status.workloadRef` is missing, check that the RTJ reconciler's workload-lookup step completed successfully. Inspect logs for `workloadRef not set` or similar messages. Triggering a manual annotation bump on the RTJ can force a reconcile cycle:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     annotate rtj <rtj-name> elastic.training.checkpoint.example.io/reconcile-trigger="$(date +%s)" --overwrite
   ```

5. Ensure the manager-mode pod is running and healthy. Confirm with:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get pods -l app=rtj-controller \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[0].args}{"\n"}{end}'
   ```

   Look for `--mode=manager` in the args. If absent, update the Deployment or Helm values and redeploy.

---

## 2. Workload status patch conflicts

### Symptoms

- Controller logs show `409 Conflict` or `the object has been modified; please apply your changes to the latest version` errors when patching `Workload` status.
- `status.elasticity.reclaimablePodsPublished` flickers between `true` and `false`.
- The `ShrinkReclaimPublished` condition is set and then cleared repeatedly.
- High reconcile loop rate visible in controller metrics without forward progress.

### Diagnostic Commands

Stream controller logs and filter for conflict errors:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller -f \
  | grep -E "Conflict|resourceVersion|managedFields|409"
```

Check the current `resourceVersion` and `managedFields` on the workload:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o json | jq '{resourceVersion: .metadata.resourceVersion, managedFields: .metadata.managedFields}'
```

Identify all field managers writing to the workload status subresource:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o json | jq '.metadata.managedFields[] | {manager, operation, time, fields: (.fieldsV1 | keys)}'
```

Check Kueue controller pod logs for competing writes:

```bash
kubectl --context kind-checkpoint-phase1 -n kueue-system \
  logs -l control-plane=controller-manager --since=5m \
  | grep -E "workload.*status|reclaimable"
```

Inspect RTJ controller metrics for reconcile error rate:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  port-forward svc/rtj-controller-metrics 8080:8080 &
curl -s http://localhost:8080/metrics \
  | grep -E "controller_runtime_reconcile_errors_total|workload_patch"
```

### Root Causes

- **Concurrent Kueue writes:** Kueue's own workload controller continuously reconciles the same `Workload` object. Both Kueue and the RTJ controller may write to `status` within the same short window, causing `resourceVersion` drift.
- **Stale resourceVersion in update path:** If the controller uses a non-SSA `Update` or `Patch` (merge patch) with an embedded `resourceVersion`, a conflict arises whenever the object is modified between the controller's Get and its Patch call.
- **SSA field ownership conflicts:** Two managers declaring ownership of overlapping fields under SSA will generate a conflict. If `rtj-elastic-reclaim` and `kueue-manager` both claim `status.reclaimablePods`, SSA returns a conflict error unless `--force-conflicts` is used by one party.
- **Controller replicas > 1 without leader election:** Running multiple manager-mode replicas without leader election causes split-brain writes where both pods race to patch the same workload status.

### Resolution Steps

1. Switch all `Workload` status patches in the RTJ controller to Server-Side Apply (SSA) instead of merge patch or strategic merge patch. SSA is conflict-aware and will only fail on explicit field ownership collisions, not on `resourceVersion` races.

2. For SSA field ownership collisions, apply the patch with `Force: true` in the SSA options. This instructs the API server to transfer ownership of the conflicting field to `rtj-elastic-reclaim`:

   ```go
   // In controller Go code
   patchOpts := []client.PatchOption{
       client.ForceOwnership,
       client.FieldOwner("rtj-elastic-reclaim"),
   }
   err = r.Patch(ctx, workload, client.Apply, patchOpts...)
   ```

3. Ensure the RTJ controller deployment has leader election enabled if running with multiple replicas:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get deployment rtj-controller \
     -o jsonpath='{.spec.template.spec.containers[0].args}' | jq .
   ```

   The `--leader-elect=true` flag must be present. If not, patch the deployment to add it.

4. If the RTJ controller must use a non-SSA patch, implement a retry loop with exponential backoff that re-fetches the object after a `409` before retrying. Most controller-runtime reconcilers handle this via `controller-runtime`'s built-in requeue with error, but ensure the error is not swallowed.

5. Coordinate with the Kueue team to ensure `status.reclaimablePods` is designated as a field the RTJ manager owns. Document this in the CRD's field-level ownership table so future Kueue upgrades do not re-claim the field.

---

## 3. RTJ stays stuck in ResizePending

### Symptoms

- `status.elasticity.resizeState` is `Pending` and does not transition to `InProgress`.
- The `ResizePending` condition is set with `status: "True"` for longer than expected (typically more than a few reconcile cycles).
- No `ShrinkingInPlace`, `ResizeCheckpointing`, or `RelaunchingForResize` condition appears.
- `status.elasticity.targetWorkerCount` does not match `status.elasticity.admittedWorkerCount`.

### Diagnostic Commands

Check the resize state and conditions on the RTJ:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.elasticity}' | jq .

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.conditions}' | jq \
  '[.[] | select(.type | test("Resize"))]'
```

Verify the workload admission state and preemption status:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o jsonpath='{.status}' | jq '{admission: .admission, conditions: .conditions}'
```

Check for active preemption events on the workload or related pods:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get events --field-selector reason=Preempted \
  | grep <rtj-name>

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  describe workload <workload-name> \
  | grep -A5 "Preempt"
```

Inspect DRA (Dynamic Resource Allocation) constraints if applicable:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get resourceclaim -l training.checkpoint.example.io/rtj=<rtj-name> \
  -o json | jq '.items[] | {name: .metadata.name, status: .status}'
```

Validate that `targetWorkerCount` is within the bounds specified in `spec.elasticity`:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.spec.elasticity}' | jq .
```

Check controller logs for the reason the transition is blocked:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller --since=10m \
  | grep -E "ResizePending|transition|blocked|preemption|DRA|bounds"
```

### Root Causes

- **Workload not admitted:** The resize state machine waits for the Kueue `Workload` to be in admitted state before transitioning out of `Pending`. If the workload lost admission (e.g., cluster queue capacity changed), the resize is effectively frozen.
- **Preemption in progress:** If a preemption event is active (the workload is being evicted or is mid-preemption), the controller intentionally holds in `Pending` to avoid conflicting with the eviction flow.
- **DRA constraints not satisfiable:** If the target worker count requires new `ResourceClaim` allocations and no suitable devices are available, the controller cannot proceed and remains in `Pending`.
- **Target out of bounds:** If `spec.elasticity.targetWorkerCount` is set below the minimum or above the maximum allowed worker count (if bounds are enforced by the controller), the state machine halts in `Pending` with a `ResizeBlocked` or validation error.
- **Elasticity mode set to Disabled:** If `spec.elasticity.mode` is `Disabled`, no resize transitions will occur regardless of `targetWorkerCount`.

### Resolution Steps

1. If the workload is not admitted, investigate the Kueue cluster queue to understand why admission was revoked or blocked. Check queue capacity and pending workloads:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get clusterqueue -o json | jq '.items[] | {name: .metadata.name, usage: .status.flavorsUsage}'
   ```

2. If preemption is in progress, wait for the preemption cycle to complete. The RTJ controller will automatically retry after the preemption concludes. Do not manually force-transition the state.

3. For DRA constraint failures, check whether the `ResourceClaim` objects associated with the RTJ are in `Pending` or `Failed` state and resolve device availability or claim binding issues separately.

4. Verify that `spec.elasticity.targetWorkerCount` is within valid bounds:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get rtj <rtj-name> \
     -o jsonpath='{.spec.elasticity.targetWorkerCount}'
   ```

   If out of bounds, patch it to a valid value:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch rtj <rtj-name> \
     --type=merge \
     -p '{"spec":{"elasticity":{"targetWorkerCount":<valid-count>}}}'
   ```

5. Confirm `spec.elasticity.mode` is set to `Manual` (not `Disabled`) to allow resize transitions:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get rtj <rtj-name> \
     -o jsonpath='{.spec.elasticity.mode}'
   ```

---

## 4. Shrink requested but fallback/relaunch not happening

### Symptoms

- `spec.elasticity.targetWorkerCount` is less than `status.elasticity.admittedWorkerCount` (a shrink request).
- `status.elasticity.resizeState` is `InProgress` or `Pending` but does not progress to `Completed`.
- `status.elasticity.resizePath` shows `CheckpointAndRelaunch` but no checkpoint or relaunch activity is observed.
- `ShrinkingInPlace` condition is set, but the pod count does not decrease.
- `ResizeCheckpointing` or `RelaunchingForResize` conditions never appear after `ShrinkingInPlace`.
- The drain flow (graceful pod termination sequence) is not triggering.

### Diagnostic Commands

Check the resize path and current shrink conditions:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.elasticity}' | jq '{resizeState, resizePath, inPlaceShrinkSupported, admittedWorkerCount, targetWorkerCount}'

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.conditions}' | jq \
  '[.[] | select(.type | test("Shrink|Resize|Checkpoint|Relaunch"))]'
```

Check whether `inPlaceShrinkSupported` is correctly reflected:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.elasticity.inPlaceShrinkSupported}'
```

Inspect the `inPlaceShrinkPolicy` in the spec:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.spec.elasticity.inPlaceShrinkPolicy}'
```

Check worker pod states for drain/termination signals:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get pods -l training.checkpoint.example.io/rtj=<rtj-name>,role=worker \
  -o wide

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  describe pod <worker-pod-name> \
  | grep -E "State|Reason|Signal|drain|resize"
```

Review controller logs for shrink decision logic:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller --since=10m \
  | grep -E "shrink|InPlace|CheckpointAndRelaunch|drain|fallback|relaunch"
```

### Root Causes

- **`inPlaceShrinkSupported` incorrectly reported as true:** If the status field `inPlaceShrinkSupported` is `true` but the runtime does not actually support in-place shrink, the controller will attempt `InPlace` path and get stuck when the pods do not cooperate. This mismatch typically arises from a stale capability detection or a bug in the runtime capability probe.
- **Resize state stuck in intermediate condition:** If the `ShrinkingInPlace` condition is set but the controller never sees confirmation from the fixture that the shrink completed, the state machine will not progress to `ShrinkReclaimPublished` or `CheckpointAndRelaunch`.
- **Condition not progressing due to missing status update from worker:** The controller may be waiting for a worker-side status signal (e.g., a pod annotation or a volume file written by the fixture) that never arrives.
- **Drain flow not triggering:** If the `inPlaceShrinkPolicy` is `IfSupported` and the runtime capability check returns `true` erroneously, the controller will not issue delete calls for excess worker pods, so the drain flow never starts.
- **`reclaimMode` not set to `ReclaimablePods`:** If `spec.elasticity.reclaimMode` is absent or set incorrectly, the quota release step is skipped and the shrink appears stalled from a quota perspective.

### Resolution Steps

1. If `inPlaceShrinkSupported` is incorrectly `true`, override the policy by setting `spec.elasticity.inPlaceShrinkPolicy: Never`. This forces the controller to use the `CheckpointAndRelaunch` path regardless of the runtime capability:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch rtj <rtj-name> \
     --type=merge \
     -p '{"spec":{"elasticity":{"inPlaceShrinkPolicy":"Never"}}}'
   ```

2. If the `ShrinkingInPlace` condition is stuck, check whether the fixture wrote the expected completion signal to the resize-signal volume. See [Section 6](#6-fixtureruntime-not-honoring-the-shrink-protocol) for fixture-level diagnostics.

3. If the drain flow is not triggering because the controller believes in-place is supported, manually verify capability by checking the node feature or runtime probe output:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get rtj <rtj-name> \
     -o jsonpath='{.status.elasticity.executionMode}'
   ```

   Compare this against what the fixture actually supports and file a bug if there is a mismatch.

4. Ensure `spec.elasticity.reclaimMode` is set to `ReclaimablePods`:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get rtj <rtj-name> \
     -o jsonpath='{.spec.elasticity.reclaimMode}'
   ```

   Patch if missing:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch rtj <rtj-name> \
     --type=merge \
     -p '{"spec":{"elasticity":{"reclaimMode":"ReclaimablePods"}}}'
   ```

5. If conditions are stuck for more than several reconcile intervals, force a reconcile by adding a trigger annotation and then check logs for the next cycle's decision:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     annotate rtj <rtj-name> \
     elastic.training.checkpoint.example.io/reconcile-trigger="$(date +%s)" --overwrite
   ```

---

## 5. Grow requested but relaunch blocked

### Symptoms

- `spec.elasticity.targetWorkerCount` is greater than `status.elasticity.admittedWorkerCount` (a grow request).
- `status.elasticity.resizeState` is `Blocked` or stuck in `Pending`.
- The `ResizeBlocked` condition is set on the RTJ.
- No new worker pods are created.
- `RelaunchingForResize` condition does not appear.
- The checkpoint was written successfully (visible in storage) but the relaunched job does not start.

### Diagnostic Commands

Check the RTJ grow state and `ResizeBlocked` condition:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.elasticity}' | jq .

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.conditions}' | jq \
  '[.[] | select(.type | test("ResizeBlocked|ResizePending|Relaunch|Checkpoint"))]'
```

Check Kueue queue capacity for the grow request:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get clusterqueue -o json | jq \
  '.items[] | {name: .metadata.name, pendingWorkloads: .status.pendingWorkloads, flavorsUsage: .status.flavorsUsage}'

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get localqueue -o json | jq \
  '.items[] | {name: .metadata.name, pendingWorkloads: .status.pendingWorkloads}'
```

Check preemption status on the associated workload:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o jsonpath='{.status.conditions}' | jq \
  '[.[] | select(.type | test("Preempted|Evicted|QuotaReserved"))]'
```

Verify checkpoint availability in the checkpoint store:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> \
  -o jsonpath='{.status.checkpointRef}' | jq .

# If checkpoint is stored as a PVC or object, verify it exists and is accessible:
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get pvc -l training.checkpoint.example.io/rtj=<rtj-name>
```

Check admission check gate status on the workload:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o jsonpath='{.status.admissionChecks}' | jq .
```

Review controller logs for grow/relaunch blocking reason:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller --since=10m \
  | grep -E "grow|relaunch|quota|preempt|checkpoint|admission.check|gate"
```

### Root Causes

- **Quota insufficient for new size:** The cluster queue does not have enough headroom to admit a workload at the larger `targetWorkerCount`. Kueue will not admit the resized workload until sufficient quota is available or preemption of lower-priority workloads frees space.
- **Preemption in progress:** A preemption cycle is ongoing on this or a competing workload. The RTJ controller holds the grow in `Pending` or `Blocked` to avoid conflicting with the active eviction.
- **Checkpoint not available or incomplete:** The `CheckpointAndRelaunch` path requires a valid, readable checkpoint before relaunch. If the checkpoint write failed, was partial, or the checkpoint reference in `status.checkpointRef` is stale, the controller will block relaunch to prevent starting from an undefined state.
- **Admission check gate not ready:** If the workload has an `admissionCheck` gate (e.g., a provisioning request or external admission webhook) that has not cleared, Kueue will not admit the workload even if quota is available.
- **Target count exceeds hard limits:** The new `targetWorkerCount` may exceed a hard cap defined in the RTJ spec or cluster-level policy, causing an immediate `ResizeBlocked` transition.

### Resolution Steps

1. If quota is insufficient, check whether lower-priority workloads can be preempted. If the cluster queue has preemption configured, Kueue will handle this automatically. Otherwise, manually scale down or delete lower-priority workloads to free quota.

2. If preemption is in progress, wait for the cycle to complete. Monitor the workload conditions:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get workload <workload-name> -w \
     -o jsonpath='{.status.conditions}'
   ```

3. If the checkpoint is unavailable, investigate the checkpoint write step:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     logs -l training.checkpoint.example.io/rtj=<rtj-name>,role=worker \
     | grep -E "checkpoint|write|flush|error"
   ```

   If the checkpoint is confirmed lost, consider resetting `status.elasticity.resizeState` to `Idle` (via a controller annotation or admin action) to allow the job to continue at the current admitted count rather than remain blocked.

4. For admission check gate failures, identify the failing check:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get workload <workload-name> \
     -o jsonpath='{.status.admissionChecks}' | jq '.[] | select(.state != "Ready")'
   ```

   Investigate and resolve the specific admission check (e.g., provisioning request, external webhook approval).

5. If `targetWorkerCount` exceeds a hard limit, reduce it to a valid value:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch rtj <rtj-name> \
     --type=merge \
     -p '{"spec":{"elasticity":{"targetWorkerCount":<valid-count>}}}'
   ```

---

## 6. Fixture/runtime not honoring the shrink protocol

### Symptoms

- Worker pods are signaled to shrink (the resize-signal volume file is written or a signal is sent) but the fixture does not respond.
- `ShrinkingInPlace` condition is set but worker count does not decrease and no acknowledgment is written back.
- `status.elasticity.resizeState` remains `InProgress` indefinitely during an in-place shrink attempt.
- The barrier/rendezvous times out, causing the entire job to fail rather than shrink cleanly.
- Environment variables expected by the YIELD_SDK are absent from worker pod specs.
- The resize-signal volume is not mounted in worker containers.

### Diagnostic Commands

Check that YIELD_SDK environment variables are present in the worker pod spec:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get pod <worker-pod-name> \
  -o jsonpath='{.spec.containers[0].env}' | jq \
  '[.[] | select(.name | test("YIELD|RESIZE|ELASTIC"))]'
```

Verify the resize-signal volume is mounted in the worker container:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get pod <worker-pod-name> \
  -o jsonpath='{.spec.containers[0].volumeMounts}' | jq \
  '[.[] | select(.name | test("resize|signal|elastic"))]'

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get pod <worker-pod-name> \
  -o jsonpath='{.spec.volumes}' | jq \
  '[.[] | select(.name | test("resize|signal|elastic"))]'
```

Exec into a worker pod to inspect the resize-signal volume contents:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  exec -it <worker-pod-name> -- ls -la /var/run/resize-signal/

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  exec -it <worker-pod-name> -- cat /var/run/resize-signal/target-worker-count
```

Check whether the fixture is polling for the resize signal:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs <worker-pod-name> \
  | grep -E "resize|shrink|signal|barrier|yield|rendezvous"
```

Inspect the worker pod events for OOMKill, barrier timeout, or abnormal exits:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  describe pod <worker-pod-name> \
  | grep -A10 "Events:"

kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get events --field-selector involvedObject.name=<worker-pod-name>
```

Check RTJ controller logs for barrier timeout detection:

```bash
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller --since=10m \
  | grep -E "barrier|timeout|fixture|ack|shrink.protocol"
```

### Root Causes

- **YIELD_SDK environment variables not injected:** The controller or admission webhook responsible for injecting YIELD_SDK env vars (e.g., `YIELD_RESIZE_SIGNAL_PATH`, `YIELD_TARGET_WORKER_COUNT`) did not run, or the pod spec was created before the webhook was active. The fixture relies on these variables to locate the signal file and understand the resize protocol.
- **Resize-signal volume not mounted:** The mutating admission webhook or pod template that should add the `resize-signal` volume and its corresponding `volumeMount` was bypassed or the pod template was missing the volume definition entirely. Without the mount, the fixture cannot read the resize signal regardless of what is written.
- **Fixture not checking for resize:** The fixture binary or training script does not include a resize-awareness check loop. This may occur if an older fixture image is deployed that predates Phase 9 changes, or if the fixture version was not bumped after the shrink protocol was added.
- **Barrier timeout too short:** The rendezvous barrier used during in-place shrink has a timeout that is shorter than the time required for all surviving workers to reach the barrier after some workers exit. The barrier fires a timeout error and the job fails before the shrink completes.
- **Signal file written in wrong format or path:** The controller writes the resize signal to a path that differs from the path the fixture is polling. This can happen due to a configuration drift between the controller's `RESIZE_SIGNAL_PATH` setting and the fixture's `YIELD_RESIZE_SIGNAL_PATH` environment variable.

### Resolution Steps

1. Confirm the YIELD_SDK environment variables are injected. If they are missing, check the mutating admission webhook:

   ```bash
   kubectl --context kind-checkpoint-phase1 \
     get mutatingwebhookconfiguration \
     | grep -i rtj

   kubectl --context kind-checkpoint-phase1 \
     describe mutatingwebhookconfiguration <webhook-name> \
     | grep -E "Rules|NamespaceSelector|Failure"
   ```

   If the webhook is not matching the worker pods (wrong namespace selector, wrong resource rules), update the webhook configuration to include the `checkpoint-dev` namespace and the `pods` resource.

2. If the resize-signal volume is missing, check the pod template in the RTJ spec or the controller's pod-builder logic. Ensure the volume definition and mount are added for all worker pods during resize-aware runs. A temporary workaround is to manually exec the signal file write, but this does not fix the root cause:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     exec -it <worker-pod-name> -- \
     sh -c 'mkdir -p /var/run/resize-signal && echo <target-count> > /var/run/resize-signal/target-worker-count'
   ```

3. If the fixture is an older image that does not include resize-awareness, update the image tag in the RTJ spec or the job template to a version that includes the Phase 9 shrink protocol handler. Confirm by checking the fixture image digest:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get pod <worker-pod-name> \
     -o jsonpath='{.spec.containers[0].image}'
   ```

4. If the barrier is timing out, increase the barrier timeout in the fixture configuration. This is typically controlled by an environment variable (e.g., `YIELD_BARRIER_TIMEOUT_SECONDS`). Patch the RTJ pod template or job spec to increase the value:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch rtj <rtj-name> \
     --type=json \
     -p '[{"op":"add","path":"/spec/workerTemplate/spec/containers/0/env/-","value":{"name":"YIELD_BARRIER_TIMEOUT_SECONDS","value":"300"}}]'
   ```

5. Verify the signal file path agreement between the controller and the fixture. The controller's configured path and the fixture's `YIELD_RESIZE_SIGNAL_PATH` must be identical. Check both:

   ```bash
   # Controller config:
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get configmap rtj-controller-config \
     -o jsonpath='{.data.resizeSignalPath}'

   # Fixture env var:
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     get pod <worker-pod-name> \
     -o jsonpath='{.spec.containers[0].env}' | jq \
     '.[] | select(.name == "YIELD_RESIZE_SIGNAL_PATH") | .value'
   ```

   If they differ, align both to the same path (default: `/var/run/resize-signal/target-worker-count`) and redeploy.

6. After resolving fixture issues, if the RTJ is stuck in `InProgress` due to a previous failed shrink attempt, set the policy to `Never` (forcing `CheckpointAndRelaunch`) to escape the stuck state and allow a clean relaunch at the target size:

   ```bash
   kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
     patch rtj <rtj-name> \
     --type=merge \
     -p '{"spec":{"elasticity":{"inPlaceShrinkPolicy":"Never"}}}'
   ```

---

## Quick Reference: Status Fields and Conditions

| Field / Condition | Normal Value During Resize | Notes |
|---|---|---|
| `status.elasticity.resizeState` | `Idle` -> `Pending` -> `InProgress` -> `Completed` | `Blocked` or `Failed` indicate error states |
| `status.elasticity.resizePath` | `InPlace` or `CheckpointAndRelaunch` | Set when resize begins |
| `status.elasticity.admittedWorkerCount` | Matches actual running workers | Updated after admission |
| `status.elasticity.targetWorkerCount` | Matches `spec.elasticity.targetWorkerCount` | Copied from spec at resize initiation |
| `status.elasticity.reclaimablePodsPublished` | `false` -> `true` during shrink | Set when Kueue workload is patched |
| `status.elasticity.inPlaceShrinkSupported` | `true` or `false` | Detected at runtime |
| `ResizePending` condition | `True` while waiting to start | Clears when `InProgress` begins |
| `ShrinkingInPlace` condition | `True` during in-place shrink | Clears when shrink completes or falls back |
| `ShrinkReclaimPublished` condition | `True` after quota release patch | Should appear during every shrink |
| `ResizeCheckpointing` condition | `True` during checkpoint write | Only on `CheckpointAndRelaunch` path |
| `RelaunchingForResize` condition | `True` during pod relaunch | Only on `CheckpointAndRelaunch` path |
| `ResizeBlocked` condition | `True` when blocked | Check message for reason |
| `ResizeFailed` condition | `True` on failure | Check message for reason |

## Quick Reference: Key Commands

```bash
# Watch RTJ elasticity status live
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> -w \
  -o jsonpath='{.status.elasticity}'

# Watch all resize-related conditions
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get rtj <rtj-name> -w \
  -o jsonpath='{range .status.conditions[*]}{.type}{"\t"}{.status}{"\t"}{.message}{"\n"}{end}'

# Check Kueue workload status and reclaimablePods
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  get workload <workload-name> \
  -o jsonpath='{.status}' | jq .

# Tail controller logs for all resize activity
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  logs -l app=rtj-controller -f \
  | grep -E "resize|elastic|shrink|grow|relaunch|checkpoint|reclaimable|SSA"

# Force a reconcile cycle on the RTJ
kubectl --context kind-checkpoint-phase1 -n checkpoint-dev \
  annotate rtj <rtj-name> \
  elastic.training.checkpoint.example.io/reconcile-trigger="$(date +%s)" --overwrite
```
