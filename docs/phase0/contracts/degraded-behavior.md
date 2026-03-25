# Degraded Behavior

This document defines the degraded behaviors that `v1` MAY allow without immediately transitioning an RTJ to `Failed`.
The goal is to keep `v1` operationally useful while preserving strict safety boundaries.

Degraded behavior is not silent partial success.
When an RTJ is degraded, the system MUST continue to preserve the `v1` invariants and MUST surface the degradation explicitly.

## Degraded Means

For `v1`, a degraded RTJ is one that is still within a safe control path, but one or more required capabilities are reduced, stale, blocked, or operating with fallback.

Degraded does not mean:

- compatibility rules are relaxed
- checkpoint completeness rules are relaxed
- a second active JobSet is allowed
- crash recovery has become a supported guarantee
- the operator may guess through missing runtime or storage evidence

## Surfacing Rules

- `status.conditions` SHOULD include `Degraded`.
- `Degraded=True` MUST be set whenever an RTJ is in one of the allowed degraded states below.
- `status.reason` MUST identify the dominant degraded cause using a stable machine-readable reason.
- `status.message` MUST explain what capability is reduced, what timer or dependency is blocking progress, and what decisive event will clear or terminate the degraded state.
- Other conditions such as `Running`, `CheckpointReady`, and `ResumeReady` MUST continue to report the affected capability truthfully.
- Clearing a degraded condition MUST require fresh decisive evidence, not mere passage of time.

## Allowed Degraded Behaviors In v1

| Degraded behavior | Trigger | Allowed system behavior | Required surfacing | Exit criteria |
| --- | --- | --- | --- | --- |
| `ControllerRecovering` | Operator restart or leader failover while the RTJ has unresolved work. | Rebuild lifecycle truth from persisted state, replay unresolved request IDs idempotently, and preserve the single-active-runtime invariant. | Phase remains the last valid lifecycle phase; `Degraded=True`; `status.reason=ControllerRecovering`. | Reconciliation completes and the controller has fresh decisive state for the RTJ. |
| `RuntimeHeartbeatStale` | Runtime heartbeats are older than `heartbeatStaleAfter`, but the JobSet still exists and no stronger failure evidence exists. | Keep the RTJ in its current non-terminal phase, refuse to infer completion from silence, and wait only within the existing bounded timers. | Usually `Running` or `Draining`; `Degraded=True`; `status.reason=RuntimeHeartbeatStale`. | A fresh heartbeat arrives or a stronger failure condition transitions the RTJ to `Failed`. |
| `CheckpointFreshnessExceeded` | The latest complete checkpoint is older than `spec.checkpoint.freshnessBudget` while the RTJ is otherwise healthy. | Allow training to continue, but the operator MUST treat the workload as requiring a fresh controlled drain before it is considered safely preemptible. | Usually `Running`; `Degraded=True`; `CheckpointReady=False`; `status.reason=CheckpointFreshnessExceeded`. | A new complete compatible checkpoint is recorded within freshness budget. |
| `CheckpointFallbackInUse` | The newest checkpoint candidate is incomplete, corrupt, incompatible, or otherwise unusable, but an older compatible complete candidate still exists. | Skip the unusable candidate and continue selection using the next newest compatible complete checkpoint. | Usually `Paused` or `Restoring`; `Degraded=True`; `status.reason=CheckpointFallbackInUse`; `status.message` MUST name the skipped-candidate class. | A restore candidate is selected successfully or no compatible complete candidates remain. |
| `StorageReadBlockedWhilePaused` | Object-store list or read operations fail while the RTJ is `Paused` and resume selection or validation is in progress. | Keep the RTJ paused, refuse to start a new restore attempt, and retry only via later reconciliation. | `Paused`; `Degraded=True`; `ResumeReady=False`; `status.reason=StorageReadBlockedWhilePaused`. | Storage reads recover and selection or validation succeeds, or the platform/user abandons the resume. |
| `DrainTelemetryPartial` | The runtime acknowledged yield, but drain-status telemetry is stale or partial while `maxDrainTime` has not yet expired. | Keep waiting within the existing drain deadline and do not claim checkpoint completion until manifest and artifact evidence exists. | `Draining`; `Degraded=True`; `CheckpointReady=False`; `status.reason=DrainTelemetryPartial`. | Valid `DrainComplete` evidence arrives or the RTJ reaches `Failed` due to timeout or stronger failure evidence. |

## Behaviors That Are Not Allowed To Stay Degraded

The following cases MUST NOT remain indefinitely in a degraded state:

- missing `YieldAck` after `yieldAckTimeout`
- missing `RestoreAck` after `restoreAckTimeout`
- missing drain completion after `maxDrainTime`
- missing restore completion after `restoreTimeout` when retry budget is exhausted
- incomplete current checkpoint with no manifest commit
- incompatible or invalid selected checkpoint with no fallback candidate
- loss of the active runtime during a supposedly controlled drain

Those cases MUST transition to `Failed` or another non-degraded valid phase according to the lifecycle and failure contracts.

## Relationship To SLOs

- A degraded RTJ MAY still be within the `v1` SLO if the system is following the bounded safe behavior defined here.
- A degraded RTJ becomes out-of-scope when the triggering event crosses into best-effort crash recovery or forced termination behavior from [non-goal-boundaries.md](non-goal-boundaries.md).
- `Degraded=True` MUST NOT be used to hide a terminal failure that should already be reported as `Failed`.
