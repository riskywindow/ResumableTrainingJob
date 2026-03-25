# Failure Semantics

This document defines the concrete `v1` failure contract for the `checkpoint-native preemption controller`.
It exists to make Phase 1 implementation planning reviewable against explicit failure modes instead of informal assumptions.

The rules in this document MUST be read alongside:

- [lifecycle-state-machine.md](lifecycle-state-machine.md)
- [yield-resume-protocol.md](yield-resume-protocol.md)
- [checkpoint-contract.md](checkpoint-contract.md)
- [checkpoint-selection-and-compatibility.md](checkpoint-selection-and-compatibility.md)
- [degraded-behavior.md](degraded-behavior.md)
- [non-goal-boundaries.md](non-goal-boundaries.md)

## Classification Rule

- `In-SLO` means `v1` MUST detect the case, surface it clearly, and either recover idempotently or fail closed within bounded timers.
- `Out-of-scope` means `v1` MUST surface the case and stop unsafe progress, but it does not promise successful graceful yield or successful resume because the event crosses the controlled-preemption boundary.

## Global Failure Rules

- The operator MUST prefer fail-closed behavior over ambiguous progress.
- The operator MUST NOT create a second active JobSet for one RTJ lineage while handling any failure.
- The operator MUST derive recovery decisions from persisted Kubernetes state and persisted checkpoint metadata, not from in-memory continuity.
- Missing or stale runtime heartbeats alone MUST NOT be treated as proof of checkpoint completion or restore completion.
- A checkpoint MUST NOT be treated as complete without a committed manifest and the required artifact evidence for the current request and run attempt.
- Resume MUST NOT proceed from a checkpoint that is incomplete, invalid, incompatible, or no longer readable at restore-start validation time.
- Best-effort crash recovery MUST NOT be reported as successful controlled preemption.

## Operator Restart Recovery

| Failure mode | Detection point | Expected system behavior | User-visible phase / conditions / result | SLO class |
| --- | --- | --- | --- | --- |
| Operator restarts while the RTJ is `Running` and no yield or restore is active. | Fresh reconcile after operator startup finds valid admission plus one active JobSet for the RTJ lineage. | Reconstruct runtime ownership from persisted state, preserve the current run attempt, and MUST NOT create a second JobSet. | `Running`; `Running=True`, `Degraded=True`; result `ControllerRecoveringRunningState` until reconciliation settles. | In-SLO |
| Operator restarts after `YieldRequest` was published but before `YieldAck` was persisted. | Fresh reconcile finds `phase=YieldRequested`, a live run attempt, and no ack for the current request ID. | Replay the same unresolved yield intent with the same request ID and the remaining ack timer; MUST NOT mint a second logical yield request. | `YieldRequested`; `YieldRequested=True`, `Degraded=True`; result `YieldRequestReplayedAfterControllerRestart`. | In-SLO |
| Operator restarts during `Draining` after `YieldAck` but before `DrainComplete`. | Fresh reconcile finds `phase=Draining`, an ack for the current request ID, and no valid completion evidence. | Resume drain tracking from persisted request state, heartbeat state, and checkpoint metadata; MUST NOT restart the runtime solely because the controller restarted. | `Draining`; `Draining=True`, `CheckpointReady=False`, `Degraded=True`; result `DrainTrackingRecoveredAfterControllerRestart`. | In-SLO |
| Operator restarts after a valid `DrainComplete` was persisted but before `Paused` was published. | Fresh reconcile finds a valid completed checkpoint for the current request and either stale status or a still-present active JobSet. | Revalidate the checkpoint, converge the active runtime to zero Pods, and publish `Paused` only after the runtime is actually removed. | `Paused`; `CheckpointReady=True`, `Degraded=False`; result `PausedRecoveredFromCommittedCheckpoint`. | In-SLO |
| Operator restarts during `Restoring` after `selectedCheckpoint` was persisted. | Fresh reconcile finds `phase=Restoring`, a selected checkpoint, and an active restore attempt. | Reattach to the same restore attempt, preserve `selectedCheckpoint`, and MUST NOT create a second restore attempt unless normal retry policy later requires it. | `Restoring`; `ResumeReady=True`, `Degraded=True`; result `RestoreTrackingRecoveredAfterControllerRestart`. | In-SLO |

## Kueue Signal Timing Edge Cases

| Failure mode | Detection point | Expected system behavior | User-visible phase / conditions / result | SLO class |
| --- | --- | --- | --- | --- |
| Kueue-driven preemption intent arrives while the RTJ is still `Queued`. | Operator observes preemption intent before the current generation has admission or a running JobSet. | Record or normalize the intent, but MUST NOT start a runtime solely to take a checkpoint for work that never began. | `Queued`; `Admitted=False`, `Running=False`; result `PreemptionIntentLatchedBeforeAdmission`. | In-SLO |
| Kueue-driven preemption intent arrives while the RTJ is `Admitted` or `Starting` but `Running` has not yet been proven. | Operator sees preemption intent before a running heartbeat or equivalent persisted evidence exists. | Suppress or unwind startup rather than fabricating a drain path for a runtime that never confirmed `Running`; MUST NOT publish `Paused` as if a checkpoint had completed. | `Admitted` or `Queued`; `Running=False`; result `StartupSuppressedBeforeTrainingBegan`. | In-SLO |
| Duplicate Kueue preemption intent is delivered for the same RTJ generation and run attempt. | Operator receives another Kueue signal while a yield request for the current attempt is already unresolved. | Deduplicate the signal, preserve the active request ID, and keep one yield flow. | Phase unchanged, typically `YieldRequested` or `Draining`; dominant condition unchanged; result `DuplicatePreemptionIntentIgnored`. | In-SLO |
| A stale Kueue preemption signal arrives after the RTJ is already `Paused`, `Succeeded`, or `Failed`. | Operator observes preemption intent whose epoch or sequencing is older than current terminal or paused state. | Ignore the stale signal and preserve the current valid phase. | Phase unchanged; conditions unchanged; result `StalePreemptionIntentIgnored`. | In-SLO |
| Kueue admission is revoked during `Restoring` before the new runtime becomes `Running`. | Fresh reconcile finds `phase=Restoring` but admission is no longer valid for the current attempt. | Abort safe progress for the current restore attempt, avoid creating or keeping a new runtime, and transition fail closed. | `Failed`; `Admitted=False`, `ResumeReady=False`, `Degraded=True`; result `AdmissionLostDuringRestore`. | In-SLO |

## JobSet And Runtime Failure

| Failure mode | Detection point | Expected system behavior | User-visible phase / conditions / result | SLO class |
| --- | --- | --- | --- | --- |
| The newly created JobSet never reaches a running state before startup timeout or retry budget exhaustion. | `Starting` exceeds the implementation's bounded startup policy without persisted evidence of `Running`. | Retry only within bounded startup policy; after that, transition fail closed and preserve attempt diagnostics. | `Starting` then `Failed` if exhausted; `Running=False`, `Degraded=True`; result `StartupTimeout`. | In-SLO |
| The active JobSet disappears or irrecoverably fails while `Running` and no accepted yield is in progress. | Fresh reconcile finds `phase=Running` but the active JobSet or required Pods vanished without a completed checkpoint for the current attempt. | Surface runtime loss, preserve the most recent completed checkpoint reference if one exists, and MUST NOT claim controlled yield success. | `Failed`; `Running=False`, `Degraded=True`; result `RuntimeLostOutsideControlledYield`. | Out-of-scope |
| The active JobSet fails during `Draining` before the current checkpoint becomes complete. | Fresh reconcile or runtime diagnostics show the JobSet ended while `phase=Draining` and current request completion evidence is absent or unusable. | Transition fail closed; retain any older completed checkpoint for later best-effort resume selection, but the current yield attempt is unsuccessful. | `Failed`; `Draining=False`, `CheckpointReady=False`, `Degraded=True`; result `DrainInterruptedByRuntimeFailure`. | Out-of-scope |

## Agent Responsiveness And Heartbeats

| Failure mode | Detection point | Expected system behavior | User-visible phase / conditions / result | SLO class |
| --- | --- | --- | --- | --- |
| The runtime never sends `YieldAck` before `yieldAckTimeout`. | `YieldRequested` exceeds `yieldAckTimeout` for the current request ID. | Stop assuming graceful drain is in progress and transition fail closed. | `Failed`; `YieldRequested=False`, `Degraded=True`; result `YieldAckTimeout`. | In-SLO |
| The runtime never sends `RestoreAck` before `restoreAckTimeout`. | `Restoring` exceeds `restoreAckTimeout` for the current restore request. | Treat the current restore attempt as failed; later retry is allowed only within `maxResumeRetries`. | `Restoring` then `Failed` for the current attempt if not retried immediately; `ResumeReady=False`, `Degraded=True`; result `RestoreAckTimeout`. | In-SLO |
| Runtime heartbeats become stale while the RTJ is `Running`, but the JobSet still exists. | No fresh heartbeat by `heartbeatStaleAfter`, while JobSet and Pods still indicate the attempt exists. | Surface degraded liveness, avoid claiming new drain or restore success from stale telemetry, and continue waiting for decisive evidence. | `Running`; `Running=True`, `Degraded=True`; result `RuntimeHeartbeatStale`. | In-SLO |
| Runtime heartbeats become stale while the RTJ is `Draining`, but `maxDrainTime` has not yet expired. | `Draining` has stale heartbeat state and no valid `DrainComplete`, yet the drain deadline has not expired. | Keep waiting only until the bounded drain deadline or stronger failure evidence; MUST NOT claim checkpoint completion from silence. | `Draining`; `Draining=True`, `CheckpointReady=False`, `Degraded=True`; result `DrainHeartbeatStale`. | In-SLO |
| The runtime sends `YieldAck`, `DrainComplete`, `RestoreAck`, or `RestoreComplete` for an old request ID or old run attempt. | Operator compares the received signal with the current RTJ lineage, run attempt, and request identifier. | Ignore the stale signal, keep the current valid control flow, and preserve diagnostics explaining staleness. | Phase unchanged; current dominant condition unchanged; result `StaleRuntimeSignalIgnored`. | In-SLO |

## Storage, Checkpoint Integrity, And Restore Validation

| Failure mode | Detection point | Expected system behavior | User-visible phase / conditions / result | SLO class |
| --- | --- | --- | --- | --- |
| Storage write fails during drain, or the runtime explicitly reports `DrainFailed`. | Runtime emits `DrainFailed`, object-write errors, or no valid completion evidence appears before the drain deadline. | Treat the current drain attempt as unsuccessful and transition fail closed. | `Failed`; `CheckpointReady=False`, `Degraded=True`; result `CheckpointWriteFailed`. | In-SLO |
| The current drain attempt never produces a committed manifest or `completionTimestamp`. | Operator validates the current checkpoint candidate and finds the manifest missing, unreadable, or uncommitted. | Treat the current checkpoint as incomplete and refuse to publish `Paused`. | `Failed`; `CheckpointReady=False`, `Degraded=True`; result `ManifestMissingOrUncommitted`. | In-SLO |
| The newest historical checkpoint candidate is missing a required artifact or has unreadable required objects. | Restore selection or revalidation finds that a candidate manifest points to a missing or unreadable required artifact. | Mark that candidate unusable for the current selection cycle and fall back to the next newest compatible complete checkpoint if one exists. | `Paused` or `Restoring`; `ResumeReady=False` until a replacement candidate is selected, `Degraded=True`; result `CheckpointCandidateSkippedIncomplete`. | In-SLO |
| The newest historical checkpoint candidate fails integrity verification. | Restore selection or revalidation finds digest mismatch or corruption in a required artifact. | Mark the candidate invalid, skip it, and fall back only to an older candidate that is complete, valid, and compatible. | `Paused` or `Restoring`; `Degraded=True`; result `CheckpointCandidateSkippedCorrupt`. | In-SLO |
| Restore-start validation finds the selected checkpoint incompatible, unsupported, or otherwise invalid for the current spec. | Just before `RestoreRequest`, the operator rechecks compatibility fields, manifest version, lineage, and object availability. | Clear or replace the bad candidate for this selection cycle and fail closed if no acceptable candidate remains. | `Paused` or `Failed`; `ResumeReady=False`, `Degraded=True`; result `CheckpointRejectedAtRestoreValidation`. | In-SLO |
| Storage list or read operations are unavailable while the RTJ is `Paused` and trying to resume. | Manifest listing, manifest reads, or required object reads fail during candidate selection or validation. | Keep the RTJ paused, do not create a restoring JobSet, and retry only through later reconciliation. | `Paused`; `ResumeReady=False`, `Degraded=True`; result `RestoreBlockedByStorageReadUnavailable`. | In-SLO |
| Restore does not complete before `restoreTimeout`, and no further retry budget remains. | `Restoring` exceeds `restoreTimeout`, and the controller has exhausted `maxResumeRetries`. | Mark restore unsuccessful, preserve the last selected checkpoint and diagnostics, and transition fail closed. | `Failed`; `ResumeReady=False`, `Degraded=True`; result `RestoreTimeoutExhausted`. | In-SLO |

## Node Loss And Forced Termination

| Failure mode | Detection point | Expected system behavior | User-visible phase / conditions / result | SLO class |
| --- | --- | --- | --- | --- |
| A node hosting part of the active runtime is lost during `Draining` before the manifest commit point. | JobSet or Pod status shows node loss or hard runtime loss while the current checkpoint is still incomplete. | Treat the current yield as unsuccessful, keep any older completed checkpoints eligible for later best-effort selection, and MUST NOT publish `Paused` as if the drain succeeded. | `Failed`; `CheckpointReady=False`, `Degraded=True`; result `DrainInterruptedByNodeLoss`. | Out-of-scope |
| The platform or scheduler forces termination after `maxDrainTime` expires. | Drain deadline expires and the runtime is later observed terminated or is intentionally torn down by the platform path. | Stop waiting for graceful yield, preserve the last previously completed checkpoint only, and surface that the controlled-yield guarantee was not met. | `Failed`; `Draining=False`, `CheckpointReady=False`, `Degraded=True`; result `ForcedTerminationAfterDrainDeadline`. | Out-of-scope |

## Operational Implications

- Phase 1 implementations SHOULD keep stable machine-readable reason codes close to the result names in this document.
- Alerting SHOULD distinguish `In-SLO` fail-closed cases from `Out-of-scope` crash-recovery cases.
- Dashboards SHOULD surface whether the current blocker is admission, runtime responsiveness, checkpoint completeness, checkpoint validity, compatibility, or storage availability.
