# ResumableTrainingJob Status Contract

This document defines the conceptual Phase 0 status contract for `ResumableTrainingJob`.
It exists to make lifecycle visibility reviewable before any controller or CRD implementation begins.

The phase enum in this document MUST match [lifecycle-state-machine.md](lifecycle-state-machine.md).

## Status Ownership

- `status` MUST be controller-authored.
- Users MUST treat `status` as read-only.
- Fields marked as derived are computed from persisted workload state, checkpoint metadata, and the fixed `v1` policy rules.

## Required Status Fields

| Field | Authorship | Required | Meaning |
| --- | --- | --- | --- |
| `status.phase` | Derived | Yes | High-level lifecycle phase. |
| `status.conditions` | Controller-authored | Yes | Bounded condition list suitable for automation and dashboards. |
| `status.currentRunAttempt` | Controller-authored | Yes | The active or most recently attempted runtime attempt number. |
| `status.selectedCheckpoint` | Derived | Yes | The checkpoint currently selected for resume, or `null` when none is selected. |
| `status.lastCompletedCheckpoint` | Derived | Yes | The newest completed checkpoint known for the RTJ lineage, or `null` when none exists. |
| `status.transitionTimestamps` | Derived | Yes | Important lifecycle timestamps. |
| `status.reason` | Controller-authored | Yes | Short machine-oriented reason for the current state. |
| `status.message` | Controller-authored | Yes | Human-readable explanation of the current state. |
| `status.observedGeneration` | Controller-authored | No | The latest `metadata.generation` observed by the controller. |

## Phase Enum

The conceptual `v1` phase enum is:

| Phase | Meaning |
| --- | --- |
| `Pending` | The RTJ exists but is not yet ready to enter queueing or runtime reconciliation. |
| `Queued` | The RTJ is waiting for Kueue admission. |
| `Admitted` | Kueue has granted admission, but runtime startup or restore has not begun yet. |
| `Starting` | The operator is creating or converging the active runtime attempt. |
| `Running` | The RTJ has valid admission and one active JobSet-backed runtime attempt. |
| `YieldRequested` | A yield request has been accepted but not yet acknowledged by the runtime. |
| `Draining` | The runtime acknowledged yield and is waiting for a step boundary or writing the checkpoint. |
| `Paused` | The RTJ has no active training Pods and has converged to a yielded state. |
| `Restoring` | The controller has selected a compatible checkpoint and is converging a new runtime attempt to restore from it. |
| `Succeeded` | The workload finished successfully under the narrow `v1` contract. |
| `Failed` | The RTJ cannot safely continue because prerequisites, checkpoint readiness, or resume retries were exhausted. |

These phases are intentionally high-level.
Detailed automation SHOULD key off `conditions`, `reason`, and `message`, not `phase` alone.

## Conditions

Each condition entry SHOULD use the standard Kubernetes condition shape:

- `type`
- `status`
- `reason`
- `message`
- `lastTransitionTime`
- `observedGeneration`

Suggested conceptual condition types for `v1` are:

- `Admitted`
- `Running`
- `YieldRequested`
- `Draining`
- `CheckpointReady`
- `ResumeReady`
- `Degraded`

Phase 0 does not require an exhaustive final condition taxonomy, but condition names SHOULD stay stable enough for implementation planning.

## Checkpoint Status Objects

`status.selectedCheckpoint` and `status.lastCompletedCheckpoint` use the same conceptual shape:

- `id`: stable checkpoint identifier within the RTJ lineage
- `storageURI`: S3-compatible URI for the checkpoint prefix or manifest root
- `completionTime`: time the checkpoint became complete
- `sourceRunAttempt`: runtime attempt that produced the checkpoint
- `compatibilityState`: `Compatible`, `Incompatible`, or `Unknown`
- `compatibilityReason`: short reason for the compatibility evaluation

`status.selectedCheckpoint` MUST be either `null` or a checkpoint whose `compatibilityState` is `Compatible`.
`status.lastCompletedCheckpoint` MAY be incompatible with the latest desired spec if the user changed declared identity fields after the checkpoint was written.

## Transition Timestamps

`status.transitionTimestamps` MUST include enough timestamps to reconstruct the major lifecycle edges.
The conceptual `v1` set is:

- `lastTransitionTime`
- `queuedAt`
- `admittedAt`
- `startingAt`
- `runningAt`
- `yieldRequestedAt`
- `drainingAt`
- `lastCheckpointCompletedAt`
- `pausedAt`
- `restoringAt`
- `restoreCompletedAt`
- `succeededAt`
- `failedAt`

Every field other than `lastTransitionTime` MAY be `null` when that transition has not occurred.

## State Rules That MUST Hold

- `Queued` MUST imply the RTJ is waiting for Kueue admission and has no active runtime attempt.
- `Admitted` MUST imply Kueue admission is valid and no active training runtime is yet confirmed as running.
- `Starting` MUST imply the operator is converging a JobSet-backed runtime attempt.
- `Running` MUST imply valid admission and one active JobSet-backed runtime attempt.
- `Paused` MUST imply no active training Pods for the RTJ.
- `YieldRequested` MUST imply a yield request is in progress but not yet acknowledged by the runtime.
- `Draining` MUST imply the current yield request was acknowledged and the controller has not yet converged to `Paused`.
- `Restoring` MUST imply `status.selectedCheckpoint` is not `null`.
- `Failed` MUST preserve the last known `reason`, `message`, and checkpoint references needed for diagnosis.

## What Status Does Not Decide

- `status` does not relax compatibility rules from ADR 0003.
- `status.lastCompletedCheckpoint` does not by itself authorize resume.
- `status.phase` does not replace detailed conditions for automation.
- `status` does not standardize audit retention, event emission, or metrics names.
