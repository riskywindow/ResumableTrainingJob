# Phase 2 Preemption Flow

This note documents the operator-side suspend, preemption, and resume path implemented for Phase 2.

## Scope

- `ResumableTrainingJob` remains the only Kueue-managed admission object.
- The child `JobSet` remains a plain runtime object.
- Kueue drives suspend and unsuspend by mutating `RTJ.spec.suspend`.
- The operator remains responsible for graceful yield, checkpoint selection, child teardown, and restart.

## Admission And Launch

When `RTJ.spec.suspend=false` and no active child `JobSet` exists:

1. If no compatible checkpoint is selected, the operator creates a fresh run attempt.
2. If a compatible completed checkpoint is already available from a prior drained attempt, the operator creates a new run attempt that resumes from that checkpoint.
3. The operator creates exactly one control `ConfigMap` and one child `JobSet` for the chosen run attempt.
4. Repeated reconcile loops reuse the existing attempt instead of creating duplicate child `JobSet` objects.

## Kueue-Driven Suspend

When `RTJ.spec.suspend=true` while a child `JobSet` is active, the operator treats it as a Kueue-driven stop request.

1. The operator records a stable stop request id:
   - `kueue-suspend-run-<runAttempt>-gen-<generation>`
2. The operator writes `desiredState=Paused` plus that request id and timestamp into the active control `ConfigMap`.
3. The trainer is expected to finish an in-flight step, publish the yield marker, and then publish a completed checkpoint manifest newer than the suspend request.
4. The operator polls the checkpoint catalog until both of these are true:
   - the yield marker for the active run attempt is present
   - a completed compatible checkpoint newer than the suspend request is present
5. After that observation succeeds, the operator deletes the active child `JobSet`.
6. Once the active child is gone, RTJ settles into `Queued` while still Kueue-suspended.

The operator also surfaces Kueue-specific status during this path:

- `status.currentSuspension.source = Kueue`
- `status.conditions[type=KueueSuspended] = True`
- phase transitions through `YieldRequested`, `Draining`, then `Queued`

## Resume After Suspension

When Kueue later clears `RTJ.spec.suspend`:

1. If no active child `JobSet` exists, the operator selects the latest compatible completed checkpoint.
2. The selector prefers manifests with valid completion timestamps and then the newest completion time.
3. The operator creates a new run attempt and passes the selected manifest URI into the rendered child `JobSet`.
4. The RTJ moves into `Restoring` until the new child `JobSet` is observed active, then back to `Running`.

## Idempotency Rules

- The stop request id is stable for a given run attempt and RTJ generation.
- If the status already carries the current stop request id, reconcile reuses it.
- If a checkpoint has already been observed for the stop request, reconcile does not reselect an older checkpoint.
- If `Restoring` already has `status.selectedCheckpoint`, reconcile reuses that checkpoint instead of re-querying and drifting.
- Resource creation is guarded by create-if-missing semantics, so repeated reconcile loops do not create duplicate control `ConfigMap` or child `JobSet` objects.

## Manual Pause Compatibility

The existing manual pause path remains available:

- `spec.control.desiredState=Paused` still requests a graceful stop.
- Phase 2 treats the Kueue-driven path as authoritative when `spec.suspend=true`.
- Both paths reuse the same graceful-yield and checkpoint-observation machinery, but Kueue suspension now drives the status surface and restart behavior for admission control.
