# Phase 5 Policy Engine

## Overview

The checkpoint priority decision engine is a pure policy evaluation function
that computes effective priority and preemption state for a ResumableTrainingJob.
It is implemented in `internal/policy/checkpointpriority/` and is designed to
be called by the Priority Shaping Controller on each evaluation cycle.

The engine does NOT:
- Materialize effective priority into Workload objects.
- Read from or write to Kubernetes resources.
- Perform I/O of any kind.

The engine DOES:
- Accept a structured input (base priority, timestamps, telemetry, policy).
- Apply a deterministic evaluation order to classify the job's state.
- Compute the effective priority with clamping.
- Return a Decision with the preemption state, effective priority, and reason.

## Decision State Model

The engine defines 8 internal decision states that provide more granular
reasons than the API's 4-value `PreemptionState` enum:

| DecisionState | API PreemptionState | Adjustment | When |
|---|---|---|---|
| `Disabled` | *(none)* | `0` | No `CheckpointPriorityPolicy` attached |
| `StartupProtected` | `Protected` | `+protectedBoost` | Within startup protection window |
| `Active` | `Active` | `0` | Checkpoint fresh, normal operation |
| `CheckpointStale` | `Preemptible` | `+preemptibleOffset` | Checkpoint age > freshness target |
| `CoolingDown` | `Cooldown` | `+cooldownBoost` | Within `minRuntimeBetweenYields` since last resume |
| `YieldBudgetExhausted` | `Cooldown` | `+cooldownBoost` | `recentYieldCount >= maxYieldsPerWindow` |
| `TelemetryUnknown` | `Active` or `Preemptible` | `0` or `+preemptibleOffset` | Checkpoint telemetry unavailable (see fail-open) |
| `Preemptible` | `Preemptible` | `+preemptibleOffset` | Reserved for future preemption triggers |

### TelemetryUnknown Branching

When checkpoint telemetry is unavailable, the engine checks two separate
fail-open flags:

| Condition | Flag | Fail-open result | Fail-closed result |
|---|---|---|---|
| Checkpoint store error | `failOpenOnCheckpointStoreErrors` | Active, base priority | Preemptible, +preemptibleOffset |
| Telemetry loss (no store error) | `failOpenOnTelemetryLoss` | Active, base priority | Preemptible, +preemptibleOffset |

## Evaluation Order

The engine evaluates states in this fixed order. The first matching state wins:

```
1. Disabled             → policy is nil
2. StartupProtected     → within startup protection window
3. YieldBudgetExhausted → yield count ≥ maxYieldsPerWindow
4. CoolingDown          → within minRuntimeBetweenYields since last resume
5. TelemetryUnknown     → no checkpoint telemetry available
6. CheckpointStale      → checkpoint age > freshness target
7. Active               → checkpoint is fresh, normal operation
```

### Why This Order

1. **Disabled** must be first to short-circuit when no policy is attached.
2. **StartupProtected** is the strongest protection — it overrides all other
   states including stale checkpoints and telemetry loss.
3. **YieldBudgetExhausted** takes priority over CoolingDown because it is a
   more severe condition (the entire budget is used up, not just a temporal
   anti-thrashing measure).
4. **CoolingDown** prevents immediate re-preemption after a yield+resume
   cycle (anti-thrashing).
5. **TelemetryUnknown** must be checked before checkpoint freshness because
   freshness cannot be evaluated without telemetry.
6. **CheckpointStale** applies the preemptible offset when the checkpoint
   exceeds the freshness target.
7. **Active** is the default state when all checks pass.

## Effective Priority Formula

```
effective_priority = clamp(
    base_priority + state_adjustment,
    minEffectivePriority,    // floor (if configured)
    maxEffectivePriority     // ceiling (if configured)
)
```

Where:
- `base_priority` is the integer value from the Kueue `WorkloadPriorityClass`
  referenced by `spec.workloadPriorityClassName`.
- `state_adjustment` is determined by the decision state (see table above).
- `clamp(value, min, max)` ensures the result stays within configured bounds.
- If `minEffectivePriority` or `maxEffectivePriority` is nil, that bound is
  not applied.
- The computation uses int64 internally to prevent int32 overflow, then
  clamps to int32 range as a safety net.

## Protection Window Anchor

The startup protection window is measured from an **anchor time**, which is
the later of:

- `RunStartTime` — when the current run attempt started (Starting/Running).
- `LastResumeTime` — when the job last resumed from a checkpoint (Restoring → Running).

This means the protection window resets on every resume, giving the job time
to produce its first checkpoint in the new run.

If both times are nil (job hasn't started), the protection window is inactive.

## Cooldown Window

The cooldown period is measured from `LastResumeTime`. A job is in cooldown
if:

```
LastResumeTime ≠ nil AND now - LastResumeTime < minRuntimeBetweenYields
```

Cooldown only applies after a yield+resume cycle (first runs have nil
`LastResumeTime`). The cooldown check is below StartupProtected in the
evaluation order, so it only triggers when the protection window has expired.

## Input and Output Types

### EvaluationInput

| Field | Type | Source |
|---|---|---|
| `BasePriority` | `int32` | WorkloadPriorityClass value |
| `Now` | `time.Time` | Current evaluation time |
| `LastCompletedCheckpointTime` | `*time.Time` | TelemetrySnapshot / RTJ status |
| `RunStartTime` | `*time.Time` | TransitionTimestamps.StartingAt |
| `LastResumeTime` | `*time.Time` | TelemetrySnapshot.LastResumeTime |
| `LastYieldTime` | `*time.Time` | TransitionTimestamps.YieldRequestedAt |
| `RecentYieldCount` | `int32` | Yield history annotation (windowed count) |
| `CheckpointStoreError` | `bool` | Catalog I/O failure flag |

### Decision

| Field | Type | Description |
|---|---|---|
| `State` | `DecisionState` | Internal decision reason |
| `PreemptionState` | `PreemptionState` | API state for RTJ status |
| `EffectivePriority` | `int32` | Computed priority for Workload.Spec.Priority |
| `Reason` | `string` | Machine-readable reason |
| `Message` | `string` | Human-readable explanation |
| `ProtectedUntil` | `*time.Time` | Protection window expiry (nil when not protected) |

## Examples

### Example 1: Fresh checkpoint, normal running

```
Input:  basePriority=500, checkpoint age=3m, freshness target=10m
Policy: protectedBoost=50, preemptibleOffset=-100
Result: DecisionActive, PreemptionState=Active, effectivePriority=500
```

### Example 2: Stale checkpoint

```
Input:  basePriority=500, checkpoint age=15m, freshness target=10m
Policy: preemptibleOffset=-100, minEffectivePriority=-500
Result: DecisionCheckpointStale, PreemptionState=Preemptible, effectivePriority=400
```

### Example 3: Within startup protection with stale checkpoint

```
Input:  basePriority=500, run started 2m ago, checkpoint age=15m
Policy: startupProtectionWindow=5m, protectedBoost=50
Result: DecisionStartupProtected, PreemptionState=Protected, effectivePriority=550
        (protection overrides stale checkpoint)
```

### Example 4: Yield budget exhausted

```
Input:  basePriority=500, recentYieldCount=3
Policy: maxYieldsPerWindow=3, cooldownBoost=25
Result: DecisionYieldBudgetExhausted, PreemptionState=Cooldown, effectivePriority=525
```

### Example 5: Telemetry unavailable, fail-open

```
Input:  basePriority=500, no checkpoint time
Policy: failOpenOnTelemetryLoss=true
Result: DecisionTelemetryUnknown, PreemptionState=Active, effectivePriority=500
```

### Example 6: Telemetry unavailable, fail-closed

```
Input:  basePriority=500, no checkpoint time
Policy: failOpenOnTelemetryLoss=false, preemptibleOffset=-100
Result: DecisionTelemetryUnknown, PreemptionState=Preemptible, effectivePriority=400
```

## Test Coverage

The engine has comprehensive test coverage across all required scenarios:

| Category | Tests |
|---|---|
| Disabled / no policy | 2 tests |
| Startup protection | 5 tests |
| Stale checkpoint | 3 tests |
| Cooldown after resume/yield | 4 tests |
| Yield budget exhaustion | 6 tests |
| Fail-open vs fail-closed | 6 tests |
| Clamping (min, max, overflow) | 6 tests |
| Evaluation order precedence | 3 tests |
| Edge cases | 4 tests |
| computeEffectivePriority unit | 6 tests |
| deref helpers | 2 tests |
| Window functions | 26 tests |
| **Total** | **73 tests** |
