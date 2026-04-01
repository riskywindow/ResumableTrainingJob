# Phase 7 -- waitForPodsReady Integration

## Overview

Kueue's `waitForPodsReady` feature monitors pod readiness after workload
admission. When pods fail to reach or maintain Ready state within configured
timeout windows, Kueue evicts the workload by setting `spec.active = false`
and adding an `Evicted` condition on the Workload resource.

The RTJ operator **observes** these eviction conditions and classifies them
for status reporting. It does not implement its own timeout timers and does
not replace Kueue's built-in behavior.

This document covers the implementation in `internal/controller/startup_recovery.go`.

---

## Kueue eviction mechanism (v0.15.1)

### Workload condition format

When Kueue evicts a workload via waitForPodsReady, it sets:

```yaml
status:
  conditions:
    - type: Evicted
      status: "True"
      reason: PodsReadyTimeout
      message: "Exceeded the PodsReady timeout ..."
```

### Eviction reasons

| Reason | Source | Meaning |
|---|---|---|
| `PodsReadyTimeout` | waitForPodsReady | Pods did not reach/maintain Ready within timeout |
| `Preempted` | Kueue preemption | Higher-priority workload preempted this one |
| `InactiveWorkload` | Kueue | Workload deactivated for other reasons |

The RTJ operator distinguishes `PodsReadyTimeout` from `Preempted` to
classify startup vs recovery timeouts correctly. All other eviction reasons
are treated as generic evictions.

---

## Classification logic

### EvictionClassification

The `ClassifyEviction()` function inspects a Kueue Workload's conditions
and returns an `EvictionClassification`:

```go
type EvictionClassification struct {
    Evicted            bool   // True when Evicted=True condition found
    Reason             string // The eviction reason from Kueue
    Message            string // Human-readable eviction message
    IsPodsReadyTimeout bool   // True when reason is PodsReadyTimeout
    IsPreemption       bool   // True when reason is Preempted
}
```

### Startup vs Recovery distinction

The key classification question: was the workload previously running when
eviction occurred?

| Prior state | Eviction reason | StartupState |
|---|---|---|
| Never reached Running | PodsReadyTimeout | `StartupTimedOut` |
| Was Running (pods were Ready) | PodsReadyTimeout | `RecoveryTimedOut` |
| Any | Preempted | `Evicted` |
| Any | InactiveWorkload | `Evicted` |

The `wasPhaseRunning()` helper determines prior running state by checking:
1. Current phase == Running
2. Previously recorded `StartupRecovery.StartupState == Running`

The second check handles the case where the phase has already transitioned
away from Running (e.g., to YieldRequested) on a subsequent reconcile.

---

## Status fields populated

### StartupRecoveryStatus

| Field | Populated when | Value |
|---|---|---|
| `startupState` | Every state transition | One of: NotStarted, Starting, Running, StartupTimedOut, RecoveryTimedOut, Evicted |
| `podsReadyState` | Every state transition | PodsReady, PodsNotReady, or Unknown |
| `lastEvictionReason` | Eviction detected | Kueue's eviction reason string |
| `lastRequeueReason` | PodsReadyTimeout eviction | `RequeuedAfterEviction` |
| `lastTransitionTime` | StartupState changes | Timestamp of state change |

### Conditions

Two mutually exclusive conditions are managed:

| Condition | Set when | Cleared when |
|---|---|---|
| `StartupTimeoutEvicted` | PodsReadyTimeout eviction, was NOT running | Pods reach Running, or non-timeout eviction |
| `RecoveryTimeoutEvicted` | PodsReadyTimeout eviction, WAS running | Pods reach Running, or non-timeout eviction |

Both conditions use:
- `status: True`
- `reason: PodsReadyTimeout`
- `message`: forwarded from Kueue's eviction message

---

## Integration points

### 1. Eviction detection (main reconcile loop)

In `Reconcile()`, before entering the stop flow:

```
if stopSource == stopSourceKueue && workloadReference exists:
    detectAndRecordEviction(ctx, job, now)
    update status if changed
```

This ensures eviction classification is recorded before phase transitions.

### 2. Launch/resume paths

In `reconcileLaunch()`, `reconcileResume()`, and their `*WithGate` variants:

```
markStarting(job) or markRestoring(job)
syncStartupRecoveryOnLaunch(job, now)  // → Starting + PodsNotReady
```

This resets the startup recovery state for each new launch attempt.

### 3. Running transition

In the main reconcile loop when child runtime is detected as running:

```
markRunning(job)
syncStartupRecoveryOnRunning(job, now)          // → Running + PodsReady
clearStartupRecoveryTimeoutConditions(job)      // clear timeout conditions
```

### 4. What is NOT changed

- **Stop/yield flow**: Eviction detection happens before the stop flow,
  not inside it. The existing yield path handles the actual phase transitions.
- **Checkpoint semantics**: Existing code already preserves
  `lastCompletedCheckpoint` across run attempts. Recovery timeout does not
  clear it; startup timeout (when no checkpoint was ever written) naturally
  has no checkpoint to clear.
- **Manual pause path**: Manual pause uses `stopSourceManual`, not
  `stopSourceKueue`, so eviction detection is never triggered.

---

## Idempotency

All sync functions check for changes before writing:

- `syncStartupRecoveryStatus()` compares each field before updating
- `setStartupRecoveryConditions()` uses `setCondition()` / `clearCondition()`
  which are no-ops when the condition already has the target value
- `detectAndRecordEviction()` returns false when no change occurred

This ensures:
- Repeated reconciles with the same state produce no status writes
- Operator restarts re-derive the same classification from Workload conditions
- No timestamps are unnecessarily updated

---

## Checkpoint semantics

### Startup timeout (no prior checkpoint)

When a workload times out during initial startup (pods never reached Ready),
there is no checkpoint to preserve or clear. The `lastCompletedCheckpoint`
field remains empty, and the next launch attempt starts fresh.

### Recovery timeout (checkpoint exists)

When a running workload times out during recovery, the existing checkpoint
is preserved. The `lastCompletedCheckpoint` field is NOT cleared by the
eviction detection or yield path. On re-admission, the workload resumes
from the last checkpoint via the normal restore path.

### Manual pause

Not affected by startup/recovery tracking. Manual pause enters via
`stopSourceManual` and does not trigger eviction detection.

### Kueue preemption

Preemption is classified as `Evicted` (not `StartupTimedOut` or
`RecoveryTimedOut`). Checkpoint semantics are the same as preemption in
Phase 2: checkpoints are preserved for resume.

---

## Test coverage

See `internal/controller/startup_recovery_test.go`.

### Unit tests (23 tests)

- `ClassifyEviction`: nil workload, no condition, PodsReadyTimeout,
  Preempted, InactiveWorkload, condition False
- `ClassifyStartupState`: startup timeout, recovery timeout, preemption,
  Starting, Running, Restoring, not started
- `syncStartupRecovery`: on launch, on running, eviction records reason,
  idempotent, eviction idempotent
- `setStartupRecoveryConditions`: startup timeout, recovery timeout,
  cleared on non-timeout
- `wasPhaseRunning`: from phase, from startup recovery state, false when starting
- `startupRecoveryStatusEqual`: both nil, one nil, same values, different state

### Integration tests (9 tests)

- Startup timeout classification via full reconcile
- Recovery timeout classification via full reconcile
- Normal ready path (Starting → Running)
- Manual pause not confused with timeout
- Kueue preemption not confused with timeout
- Idempotent after operator restart
- Resume after timeout preserves checkpoint
- Checkpoint preserved on recovery timeout
- No workload reference skips eviction detection

---

## Open questions resolved

### OQ2: waitForPodsReady eviction condition format

**Resolved.** Kueue v0.15.1 sets `Evicted` condition with `status: True`
and `reason: PodsReadyTimeout` for waitForPodsReady evictions. The condition
type is `Evicted` (not `PodsReady`). The reason string `PodsReadyTimeout`
is used for both startup and recovery timeouts; the RTJ operator
distinguishes them via prior running state.
