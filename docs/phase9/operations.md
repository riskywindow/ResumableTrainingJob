# Phase 9 Operations Guide

This document explains how to inspect, monitor, and operate ResumableTrainingJobs
(RTJ) that use the Phase 9 elastic resize feature. Phase 9 adds `spec.elasticity`
and `status.elasticity` to the RTJ API, enabling manual target-based worker-count
resize without restarting the job from scratch.

All commands assume:

- Cluster context: `kind-checkpoint-phase1`
- Namespace: `checkpoint-dev`
- RTJ CRD group: `training.checkpoint.example.io`
- Kueue CRD group: `kueue.x-k8s.io`

---

## 1. Inspecting RTJ Resize State

The `status.elasticity` section is the authoritative source for all resize
lifecycle state. The controller writes to it exclusively; users must not modify
it directly.

### Full elasticity status dump

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity}' | jq .
```

### Individual field queries

```bash
# Current resize state machine state (Idle|Pending|InProgress|Blocked|Completed|Failed)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.resizeState}'

# Resize execution path chosen (InPlace|CheckpointAndRelaunch)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.resizePath}'

# Machine-readable reason for the current resize state
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.resizeReason}'

# Human-readable description of the most recent resize event
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.lastResizeEvent}'

# When the elasticity state last changed
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.lastElasticTransitionTime}'

# When the most recent resize completed
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.lastResizeCompletedTime}'

# Whether the runtime supports in-place shrink (from child JobSet annotation)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.inPlaceShrinkSupported}'

# Current execution mode (Fixed|Elastic)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.currentExecutionMode}'
```

### Resize state field reference

| Field | Type | Description |
|---|---|---|
| `resizeState` | `Idle\|Pending\|InProgress\|Blocked\|Completed\|Failed` | Current state in the resize lifecycle |
| `resizePath` | `InPlace\|CheckpointAndRelaunch` | Resize execution path for the current or most recent resize |
| `resizeReason` | `string` | Machine-readable reason for the current state |
| `currentExecutionMode` | `Fixed\|Elastic` | Whether the RTJ is in fixed or elastic mode |
| `lastResizeEvent` | `string` | Human-readable description of the most recent resize event |
| `lastResizeFailureReason` | `string` | Reason for the most recent resize failure |
| `lastElasticTransitionTime` | `*metav1.Time` | When the elasticity state last changed |
| `lastResizeCompletedTime` | `*metav1.Time` | When the most recent resize completed |
| `inPlaceShrinkSupported` | `bool` | Whether the runtime advertises in-place shrink support |

### Resize state machine transitions

```
Idle
  -> Pending       (target changed, plan computed, execution not yet started)
  -> InProgress    (execution underway)
     -> Completed  (resize finished successfully)
     -> Blocked    (waiting for quota, preemption in progress, DRA constraint)
     -> Failed     (SSA patch failed or other execution error)
```

For in-place shrink, `Pending` is brief: the controller moves to `InProgress`
when it writes `reclaimablePods` to the Workload.

For checkpoint-and-relaunch paths (grow, or shrink fallback), `InProgress`
spans the entire drain → checkpoint → re-admit → relaunch cycle.

### Inspect conditions for detailed resize status

Conditions carry per-resize-step detail including `observedGeneration` for
staleness detection:

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.conditions}' | jq '.[] | select(.type | startswith("Resize") or . == "ShrinkingInPlace" or . == "ShrinkReclaimPublished" or . == "RelaunchingForResize")'
```

See [Section 6](#6-condition-types-specific-to-phase-9-resize) for a complete
reference of Phase 9 condition types.

---

## 2. Inspecting Workload reclaimablePods

For in-place shrink, the RTJ controller writes `status.reclaimablePods` on
the Kueue Workload using server-side apply (field manager: `rtj-elastic-reclaim`).
Kueue reads this field and releases the corresponding quota from the
ClusterQueue. The Workload itself remains admitted; only the surplus quota is
freed.

### Find the Workload for an RTJ

The Kueue Workload is named `<rtj-name>` and lives in the same namespace as
the RTJ:

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <rtj-name> -o yaml
```

### Inspect reclaimablePods directly

```bash
kubectl -n checkpoint-dev get workload.kueue.x-k8s.io <rtj-name> \
  -o jsonpath='{.status.reclaimablePods}' | jq .
```

Example output when an in-place shrink of 2 workers is in progress:

```json
[
  {
    "name": "workers",
    "count": 2
  }
]
```

`name` matches the PodSet name (`workers` for the default worker PodSet).
`count` is the number of workers being released. After the reclaimed pods
terminate, the controller clears this list.

### Check whether reclaimablePods has been published

The RTJ status records this as a boolean:

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.reclaimablePodsPublished}'
```

`true` means the SSA patch has been written to the Workload. The controller
will not re-patch while this flag is set, preventing duplicate writes.

### Inspect reclaimable worker count

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.reclaimableWorkerCount}'
```

This is the number of workers declared reclaimable (the delta being released).

---

## 3. Inspecting Active vs Target Worker Count

### Via RTJ status

```bash
# Desired worker count (from spec.parallelism.preferredCount or spec.identity.worldSize)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.desiredWorkerCount}'

# Current target from spec.elasticity.targetWorkerCount (mirrored for observability)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.targetWorkerCount}'

# Number of worker pods admitted by Kueue
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.admittedWorkerCount}'

# Number of worker pods currently running
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.activeWorkerCount}'
```

### All four counts together

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{
    "desired": .status.elasticity.desiredWorkerCount,
    "target":  .status.elasticity.targetWorkerCount,
    "admitted":.status.elasticity.admittedWorkerCount,
    "active":  .status.elasticity.activeWorkerCount
  }' | jq .
```

### Worker count field semantics

| Field | Source | Role |
|---|---|---|
| `desiredWorkerCount` | `spec.parallelism.preferredCount` or `spec.identity.worldSize` | Upper bound; the "ideal" size |
| `targetWorkerCount` | `spec.elasticity.targetWorkerCount` | User-requested target; what the resize is aiming for |
| `admittedWorkerCount` | Kueue Workload admission | Baseline from which resize delta is computed |
| `activeWorkerCount` | Running pod count | Observed pods; lags `admittedWorkerCount` during transitions |

### Via the spec (request intent)

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.spec.elasticity.targetWorkerCount}'
```

A nil value means no explicit target is set (the job runs at the admitted
count).

### Via metrics

Port-forward the operator and query worker-count gauges:

```bash
kubectl -n checkpoint-dev port-forward deploy/rtj-operator 8080:8080 &

# Active workers per RTJ
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_elastic_active_workers'

# Target workers per RTJ
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_elastic_target_workers'

# Reclaimable workers per RTJ (in-place shrink in progress)
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_elastic_reclaimable_workers'

# Jobs by resize state
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_rtjs_by_resize_state'
```

---

## 4. Inspecting the Checkpoint Selected for a Resize Relaunch

For checkpoint-and-relaunch resize operations (grow, or shrink fallback), the
controller selects a checkpoint from the catalog before recreating the child
JobSet. This is the same checkpoint selection path used for preemption
recovery, extended with resize metadata.

### Most recent checkpoint reference in RTJ status

```bash
# Checkpoint used by the most recent resize-triggered relaunch
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.lastResizeCheckpoint}' | jq .

# General last completed checkpoint (used as the relaunch source)
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.lastCompletedCheckpoint}' | jq .
```

The `lastResizeCheckpoint` field is a `CheckpointReference` containing the
checkpoint ID and the storage URI. It is populated when the controller records
the checkpoint at the end of the resize drain flow.

### Checkpoint resize metadata in the manifest

Checkpoints produced during a resize event include additional fields in their
manifest. Inspect via the checkpoint catalog using the storage URI from the
RTJ:

```bash
STORAGE_URI=$(kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.spec.checkpoint.storageURI}')

LATEST_MANIFEST=$(kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}')

# Fetch and inspect (adjust for your storage backend)
# For S3-compatible stores:
aws s3 cp "$LATEST_MANIFEST" - | jq '{
  resizeActiveWorkerCount,
  resizeTargetWorkerCount,
  resizeDirection,
  resizeReason,
  resizeInPlaceShrinkSupported
}'
```

Resize manifest fields (present only when the checkpoint was produced during a
resize event):

| JSON Key | Description |
|---|---|
| `resizeActiveWorkerCount` | Worker count at the time of checkpointing |
| `resizeTargetWorkerCount` | Target worker count that triggered the resize |
| `resizeDirection` | `"Shrink"` or `"Grow"` |
| `resizeReason` | Human-readable reason for the resize checkpoint |
| `resizeInPlaceShrinkSupported` | Whether the runtime claimed in-place shrink support |

Non-resize checkpoints have these fields set to `null`. The Phase 3-8 resume
path is unaffected by their absence.

### RTJ conditions indicating resize checkpoint/relaunch progress

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.conditions}' | \
  jq '.[] | select(.type == "ResizeCheckpointing" or .type == "RelaunchingForResize")'
```

`ResizeCheckpointing` is set when the drain flow is active (runtime is writing
the checkpoint). `RelaunchingForResize` is set after the drain completes and
the controller is waiting for re-admission and relaunch to complete.

### Checkpoint compatibility for resize relaunch

The selected checkpoint must satisfy `spec.resume.allowWorldSizeChange: true`
because any resize changes the world size. The DCP resharding path handles the
size mismatch at restore time. If DRA is also enabled, the device profile
fingerprint must match (see [Section 5](#5-inspecting-dra-related-state-if-dra-is-enabled)).

---

## 5. Inspecting DRA-Related State if DRA is Enabled

Phase 8 DRA and Phase 9 elasticity are independently optional and fully
composable. When both are active, device profile consistency is enforced at
resize relaunch time.

### Check whether DRA is active

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.devices.mode}'
```

Returns `DRA` when Phase 8 DRA is active, `Disabled` otherwise. When the
mode is `Disabled`, the remainder of this section does not apply.

### Inspect the full DRA status

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.devices}' | jq .
```

### Key DRA fields relevant to resize

| Field | Description |
|---|---|
| `status.devices.mode` | `DRA` or `Disabled` |
| `status.devices.currentDeviceProfileFingerprint` | SHA256 of current device classes and selectors |
| `status.devices.lastCheckpointDeviceProfileFingerprint` | Fingerprint from the last committed checkpoint |
| `status.devices.claimAllocationState` | `Pending`, `Allocated`, `Failed`, or `Unknown` |

### Verify device profile fingerprint consistency

During a resize relaunch, the controller requires the checkpoint's device
profile fingerprint to match the current RTJ fingerprint (fail-closed):

```bash
# Current fingerprint
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.devices.currentDeviceProfileFingerprint}'

# Fingerprint from the checkpoint that will be used for relaunch
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.devices.lastCheckpointDeviceProfileFingerprint}'
```

If these differ, the planner returns `ResizeBlocked` with reason
`ResizeBlockedByDRA`. A mismatch indicates the device profile changed between
the last checkpoint and the current RTJ spec, which is not supported.

### DRA compatibility rules during resize

| Current RTJ | Checkpoint | Result |
|---|---|---|
| DRA active, fingerprint X | fingerprint X | Compatible — resize relaunch proceeds |
| DRA active, fingerprint X | fingerprint Y | Incompatible — resize blocked |
| DRA active, fingerprint X | no fingerprint | Incompatible — blocked (fail-closed) |
| DRA disabled | fingerprint X | Compatible — downgrade allowed |
| DRA disabled | no fingerprint | Compatible — Phase 7 path |

### Inspect ResourceClaims during resize

For in-place shrink, DRA ResourceClaimTemplates and ResourceClaims are not
modified. Reclaimed worker pods release their claims naturally when deleted.

For checkpoint-and-relaunch resize, the full DRA reconciliation runs on the
new launch. The old templates are cleaned up by owner reference GC:

```bash
# List claim templates (survive child JobSet deletion)
kubectl -n checkpoint-dev get resourceclaimtemplates \
  -l training.checkpoint.example.io/rtj-name=<name>

# List active claims (created by scheduler when pods are bound)
kubectl -n checkpoint-dev get resourceclaims \
  -l training.checkpoint.example.io/rtj-name=<name>

# Show claim allocation details
kubectl -n checkpoint-dev get resourceclaim <claim-name> \
  -o jsonpath='{.status.allocation}' | jq .
```

### Check DRA-related block on resize

If a resize is stuck in `ResizeBlocked` state with a DRA reason:

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.conditions}' | \
  jq '.[] | select(.type == "ResizeBlocked")'
```

The condition message specifies whether the block is due to a fingerprint
mismatch, a claim not yet allocated, or another DRA constraint.

---

## 6. Condition Types Specific to Phase 9 Resize

At most one resize execution condition is active at any time (except the
`ShrinkingInPlace` + `ShrinkReclaimPublished` pair which appear together for
the in-place path).

### List all Phase 9 conditions on an RTJ

```bash
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.conditions}' | jq '[.[] | select(
    .type == "ResizePending" or
    .type == "ShrinkingInPlace" or
    .type == "ShrinkReclaimPublished" or
    .type == "ResizeCheckpointing" or
    .type == "RelaunchingForResize" or
    .type == "ResizeBlocked" or
    .type == "ResizeFailed"
  )]'
```

### Condition reference

| Condition Type | Reason Values | When Set | Cleared When |
|---|---|---|---|
| `ResizePending` | `ShrinkInPlacePending`, `ShrinkViaRelaunchPending`, `GrowViaRelaunchPending` | Plan computed, execution not started | Execution begins or resize is blocked |
| `ShrinkingInPlace` | `ShrinkInPlaceExecuting` | SSA patch to Workload is being applied | In-place shrink completes or fails |
| `ShrinkReclaimPublished` | `ShrinkReclaimPublished` | `reclaimablePods` successfully written to Workload | Reclaimed pods terminate; controller clears reclaimablePods |
| `ResizeCheckpointing` | `ResizeCheckpointing` | Drain flow active (runtime is checkpointing for relaunch) | Drain completes, child JobSet deleted |
| `RelaunchingForResize` | `RelaunchingForResize` | Post-drain, controller waiting for re-admission and relaunch | Relaunch reaches Running state |
| `ResizeBlocked` | `ResizeBlockedByWorkload`, `ResizeBlockedByPreemption`, `ResizeBlockedByBounds`, `ResizeBlockedByDRA` | Resize cannot proceed for a specific reason | Blocking condition resolves; planner re-evaluates |
| `ResizeFailed` | `ResizeFailed` | Execution error (e.g., SSA patch to Workload failed) | Next successful resize attempt |

All conditions carry `observedGeneration` to distinguish stale state from
active state. When a resize completes (reaches `Idle` or `Completed`), the
execution conditions are cleared from the RTJ.

### Condition lifecycle diagram

```
ResizePending
  -> ShrinkingInPlace + ShrinkReclaimPublished  (in-place path, both set together)
  -> ResizeCheckpointing -> RelaunchingForResize (relaunch path)
  -> (all cleared on completion or NoResize plan)

ResizeBlocked  (mutually exclusive with execution conditions)
ResizeFailed   (set when SSA patch or other mutation fails)
```

---

## 7. Metrics Quick Reference

All Phase 9 metrics are registered under the `checkpoint_native_operator`
namespace. The full metric name is `checkpoint_native_operator_<name>`.

### Phase 9 metrics

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `rtjs_by_resize_state` | GaugeVec | `state` | Current RTJs by resize state (Idle, Pending, InProgress, Blocked, Completed, Failed) |
| `elastic_active_workers` | GaugeVec | `rtj` | Current active (admitted) worker count per RTJ |
| `elastic_target_workers` | GaugeVec | `rtj` | Current target worker count per RTJ |
| `elastic_reclaimable_workers` | GaugeVec | `rtj` | Current reclaimable worker count per RTJ (workers pending release via reclaimablePods) |
| `reclaimable_pods_publications_total` | Counter | — | Total reclaimablePods SSA patch writes to Workload.status |
| `shrink_in_place_successes_total` | Counter | — | Total in-place shrink operations that completed successfully |
| `shrink_in_place_failures_total` | Counter | — | Total in-place shrink operations that failed (SSA patch error) |
| `grow_via_relaunch_total` | Counter | — | Total grow-via-checkpoint-and-relaunch operations initiated |
| `resize_fallback_relaunch_total` | Counter | — | Total shrink operations that fell back to checkpoint-and-relaunch because in-place shrink was not supported |
| `resize_checkpoint_creations_total` | Counter | — | Total checkpoint creations triggered by resize operations (both grow and fallback shrink) |
| `workload_status_patch_failures_total` | Counter | — | Total failures patching Workload.status (reclaimablePods SSA patch conflicts or API errors) |
| `resize_plan_evaluations_total` | CounterVec | `kind` | Total elastic plan evaluations by plan kind (NoResize, ShrinkInPlace, ShrinkViaRelaunch, GrowViaRelaunch, ResizeBlocked, ResizeInProgress, ReclaimPublished) |

---

## 8. Querying the Operator Metrics Endpoint

The operator exposes Prometheus metrics on port `8080` at `/metrics`.

### Port-forward and query

```bash
# Start port-forward in the background
kubectl -n checkpoint-dev port-forward deploy/rtj-operator 8080:8080 &
PF_PID=$!

# Query all Phase 9 elastic metrics
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_elastic\|checkpoint_native_operator_resize\|checkpoint_native_operator_shrink\|checkpoint_native_operator_grow\|checkpoint_native_operator_reclaimable_pods\|checkpoint_native_operator_workload_status_patch\|checkpoint_native_operator_rtjs_by_resize_state'

# Query resize state distribution
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_rtjs_by_resize_state'

# Query per-RTJ worker counts
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_elastic_active_workers\|checkpoint_native_operator_elastic_target_workers\|checkpoint_native_operator_elastic_reclaimable_workers'

# Query plan evaluation counts by kind
curl -s http://localhost:8080/metrics | \
  grep 'checkpoint_native_operator_resize_plan_evaluations_total'

# Kill port-forward when done
kill $PF_PID
```

### Example metric output

```
# HELP checkpoint_native_operator_rtjs_by_resize_state Current ResumableTrainingJobs by resize state (Idle, Pending, InProgress, Blocked, Completed, Failed).
# TYPE checkpoint_native_operator_rtjs_by_resize_state gauge
checkpoint_native_operator_rtjs_by_resize_state{state="Idle"} 1
checkpoint_native_operator_rtjs_by_resize_state{state="InProgress"} 1

# HELP checkpoint_native_operator_elastic_active_workers Current active (admitted) worker count per RTJ.
# TYPE checkpoint_native_operator_elastic_active_workers gauge
checkpoint_native_operator_elastic_active_workers{rtj="checkpoint-dev/elastic-a"} 4

# HELP checkpoint_native_operator_elastic_target_workers Current target worker count per RTJ.
# TYPE checkpoint_native_operator_elastic_target_workers gauge
checkpoint_native_operator_elastic_target_workers{rtj="checkpoint-dev/elastic-a"} 2

# HELP checkpoint_native_operator_reclaimable_pods_publications_total Total reclaimablePods SSA patch writes to Workload.status.
# TYPE checkpoint_native_operator_reclaimable_pods_publications_total counter
checkpoint_native_operator_reclaimable_pods_publications_total 3

# HELP checkpoint_native_operator_resize_plan_evaluations_total Total elastic plan evaluations by plan kind.
# TYPE checkpoint_native_operator_resize_plan_evaluations_total counter
checkpoint_native_operator_resize_plan_evaluations_total{kind="NoResize"} 142
checkpoint_native_operator_resize_plan_evaluations_total{kind="ShrinkInPlace"} 1
checkpoint_native_operator_resize_plan_evaluations_total{kind="GrowViaRelaunch"} 2
```

### Using the Makefile target

```bash
# Show queues, quota, workloads, and RTJs including elastic status
make phase9-status
```

---

## 9. Common Operational Workflows

### Trigger a manual resize

Enable elasticity (one-time, while the RTJ is suspended):

```bash
kubectl patch rtj <name> -n checkpoint-dev --type=merge -p '{
  "spec": {
    "resume": {"allowWorldSizeChange": true},
    "elasticity": {"mode": "Manual"}
  }
}'
```

Request a resize (at any time while Running):

```bash
kubectl patch rtj <name> -n checkpoint-dev --type=merge -p '{
  "spec": {"elasticity": {"targetWorkerCount": 2}}
}'
```

### Watch resize progress

```bash
kubectl -n checkpoint-dev get rtj <name> -w \
  -o custom-columns=\
'NAME:.metadata.name,PHASE:.status.phase,RESIZE:.status.elasticity.resizeState,PATH:.status.elasticity.resizePath,ACTIVE:.status.elasticity.activeWorkerCount,TARGET:.status.elasticity.targetWorkerCount'
```

### Confirm Kueue quota was released after in-place shrink

```bash
# Check reclaimablePods on the Workload (should be populated during shrink)
kubectl -n checkpoint-dev get workload.kueue.x-k8s.io <name> \
  -o jsonpath='{.status.reclaimablePods}' | jq .

# Check ClusterQueue usage (should decrease after release)
kubectl get clusterqueues.kueue.x-k8s.io phase9-cq \
  -o jsonpath='{.status.flavorsUsage}' | jq .
```

### Diagnose a blocked resize

```bash
# Check the ResizeBlocked condition for the blocking reason
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.conditions}' | \
  jq '.[] | select(.type == "ResizeBlocked")'

# Check if the Workload is admitted
kubectl -n checkpoint-dev get workload.kueue.x-k8s.io <name> \
  -o jsonpath='{.status.admission}' | jq .

# Check resize reason for a machine-readable code
kubectl -n checkpoint-dev get rtj <name> \
  -o jsonpath='{.status.elasticity.resizeReason}'
```

Common block reasons and remediation:

| Reason | Cause | Remediation |
|---|---|---|
| `ResizeBlockedByWorkload` | Workload is not admitted | Wait for admission; check ClusterQueue capacity |
| `ResizeBlockedByPreemption` | Preemption is in progress concurrently | Wait for preemption to resolve; resize target is preserved |
| `ResizeBlockedByBounds` | Target is outside `[minCount, preferredCount]` | Adjust `targetWorkerCount` to be within spec bounds |
| `ResizeBlockedByDRA` | Device profile fingerprint mismatch | Ensure checkpoint and current RTJ use the same device profile |

### Reset a failed resize

A `ResizeFailed` state (SSA patch failure or other execution error) does not
prevent the controller from retrying. The next reconcile will re-evaluate the
plan. If the underlying cause is resolved (e.g., API server is reachable
again), the controller will proceed automatically. To force an immediate
reconcile, annotate the RTJ:

```bash
kubectl annotate rtj <name> -n checkpoint-dev \
  training.checkpoint.example.io/force-reconcile="$(date -u +%s)" \
  --overwrite
```
