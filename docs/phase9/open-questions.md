# Phase 9 — Open Questions

## OQ-1: Can the current JobSet controller handle live replica reduction?

**Context**: In-place shrink requires patching the child JobSet's
`replicatedJobs[].replicas` down.  The upstream JobSet controller must
handle this gracefully — scaling down the corresponding Job, terminating
surplus pods without restarting them.

**Current understanding**: The JobSet v0.7+ controller supports mutable
replicas on replicatedJobs, but the behavior under live training workloads
(PyTorch DDP/FSDP with NCCL) is not validated.  Reducing NCCL world size
on a running communicator group causes a hang or crash on remaining workers
unless the training framework (TorchElastic / `torchrun`) is configured for
elastic membership.

**Decision**: Ship with the conservative default — in-place shrink is gated
on the `training.io/supports-in-place-shrink: "true"` annotation.  If the
annotation is absent, the controller falls back to checkpoint-and-relaunch.
This defers runtime validation to the operator who knows their training
framework's capabilities.

**Status**: Resolved (design-time); runtime validation deferred to operator.

---

## OQ-2: Should Workload PodSet counts be mutated in-place or via suspend cycle?

**Context**: For checkpoint-and-relaunch resize, the controller needs to
change the Workload's PodSet counts before re-admission.  Two options:

1. **Mutate in-place**: Patch `spec.podSets[].count` on the live Workload.
   Requires Kueue to handle in-flight spec changes on an admitted Workload.
2. **Suspend/mutate/re-admit**: Suspend the Workload, mutate counts, then
   un-suspend.  Uses the existing suspend/un-suspend cycle.

**Decision**: Use the suspend/mutate/re-admit cycle.  This is the
conservative path that works with all Kueue versions and does not require
Kueue to support in-flight Workload spec mutation (which may or may not be
supported depending on the Kueue version and configuration).

**Status**: Resolved.

---

## OQ-3: Does the MultiKueue adapter mirror Workload.status.reclaimablePods?

**Context**: For in-place shrink on a worker cluster, the worker writes
`reclaimablePods` to the worker-side Workload.  The manager-side Workload
should reflect this so the manager-side Kueue can account for freed quota
in global scheduling decisions.

**Current understanding**: The MultiKueue adapter mirrors selected
Workload status fields (admission, conditions).  It is unclear whether
`reclaimablePods` is included in the mirror set.

**Options**:
1. Extend the adapter to mirror `reclaimablePods` (requires upstream change
   or adapter plugin).
2. Accept that manager-side Kueue does not see freed quota until the
   Workload is fully suspended (acceptable for MVP).
3. Use the RTJ status mirror (status.elasticity.reclaimablePods) as a
   manager-side signal and build a shim that writes to the manager Workload.

**Status**: Open.  Deferred to stretch goal G-9.

---

## OQ-4: How does resize interact with an in-progress Kueue preemption?

**Context**: If Kueue preempts the RTJ (sets suspend=true) at the same
time the operator patches targetWorkerCount, the controller sees two
concurrent intents: preemption and resize.

**Options**:
1. **Preemption wins**: If the Workload is being preempted, the resize
   request is queued until the RTJ is re-admitted.  After re-admission,
   the controller evaluates the resize at the newly admitted size.
2. **Coalesce**: Treat the preemption as an opportunity to resize.  When
   relaunching after preemption, use the target worker count instead of
   the original count.

**Leaning toward**: Option 2 (coalesce).  Since checkpoint-and-relaunch is
already happening due to preemption, incorporating the resize target avoids
a double checkpoint cycle.

**Status**: Open.  Implementation must handle the race safely.

---

## OQ-5: Should resize be allowed during the Draining phase?

**Context**: If the RTJ is in the Draining phase (graceful yield in
progress, waiting for checkpoint completion), should a new
targetWorkerCount patch be accepted?

**Options**:
1. **Reject**: Return a validation error or condition indicating resize is
   not allowed during drain.  The operator must wait for the drain to
   complete.
2. **Queue**: Accept the new target but defer evaluation until the drain
   completes.  The resize takes effect on the next launch.

**Leaning toward**: Option 2 (queue).  This is simpler for operators and
avoids a confusing error.  The controller records the pending target and
evaluates it after the drain/pause cycle completes.

**Status**: Open.

---

## OQ-6: What happens if Kueue cannot admit the larger size after grow?

**Context**: Checkpoint-and-relaunch grow suspends the Workload and
requests re-admission at a larger size.  If the ClusterQueue does not have
enough quota, the Workload stays suspended indefinitely.

**Options**:
1. **Wait indefinitely**: The RTJ stays in resize-in-progress state until
   quota becomes available.  This is consistent with normal admission
   behavior.
2. **Timeout and revert**: After a configurable timeout, revert the target
   to the previous size and relaunch at the old size.
3. **Timeout and relaunch at smaller size**: After timeout, relaunch at
   the old (or min) size from the checkpoint.

**Leaning toward**: Option 1 for MVP.  The operator can observe the blocked
state via `status.elasticity.resizePhase: Blocked` and manually adjust the
target if needed.  Timeout-based revert adds complexity and may cause
unexpected behavior.

**Status**: Open.

---

## OQ-7: Should `spec.parallelism.preferredCount` be updated when targetWorkerCount changes?

**Context**: `spec.parallelism.preferredCount` is the Workload PodSet count
that Kueue uses for admission decisions.  When the operator sets a new
targetWorkerCount, should the controller also update preferredCount?

**Options**:
1. **Yes**: targetWorkerCount becomes the new preferredCount for the next
   Workload admission cycle.  This ensures Kueue sees the correct demand.
2. **No**: preferredCount stays at the original value.  targetWorkerCount
   is a controller-internal field that drives resize logic but does not
   change the Workload spec directly.

**Leaning toward**: Option 1.  The Workload PodSet count must reflect the
target for re-admission to work correctly.  The controller updates
preferredCount (and the Workload PodSet count) as part of the
suspend/mutate/re-admit cycle.

**Status**: Open; implementation-time decision.

---

## OQ-8: Phase 8 P1 gaps — wire into Phase 9 or separate patch?

**Context**: Phase 8 left three P1 gaps:
1. `observeDRAClaimStatus()` not wired into reconcile loop.
2. `syncDeviceResumeFingerprint()` not wired into resume flow.
3. Phase 8 metric emission not wired.

**Options**:
1. Wire them as part of Phase 9 implementation (since the reconcile loop is
   being modified anyway).
2. Ship a separate Phase 8.1 patch before starting Phase 9 code.

**Leaning toward**: Option 1.  Since Phase 9 modifies the reconcile loop,
it's natural to wire the Phase 8 gaps in the same pass.

**Status**: Open; depends on implementation sequencing.
