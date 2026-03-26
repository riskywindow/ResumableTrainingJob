# ADR 0002: CheckpointPriorityPolicy API Design

- Status: Accepted
- Date: 2026-03-25
- Scope: Phase 5 — checkpoint-aware priority shaping API surface

## Context

Phase 5 requires a cluster-scoped policy object that configures checkpoint-aware
priority shaping for RTJ-backed Kueue Workloads. The design must:

1. Be narrow and focused on checkpoint-aware preemption behavior.
2. Preserve Phase 4 backward compatibility when no policy is referenced.
3. Not implement controller reconciliation (API-only in this phase).
4. Follow existing patterns established by ResumeReadinessPolicy (Phase 4).

## Decision

### D1: CRD Name and Scope

**CheckpointPriorityPolicy** — cluster-scoped, short name `cpp`.

Rationale: The name clearly ties the policy to checkpoint-aware priority shaping
rather than generic priority manipulation. Cluster scope matches
ResumeReadinessPolicy and allows a single policy to be referenced by RTJs across
namespaces.

### D2: RTJ References Policy by Name

RTJ spec gains an optional `priorityPolicyRef` field containing a `name` string
pointing to a CheckpointPriorityPolicy. When nil, Phase 4 behavior is preserved
(no priority shaping).

Alternative considered: Using a label selector or namespace/name pair. Rejected
because CheckpointPriorityPolicy is cluster-scoped (no namespace needed) and
a direct name reference is simpler and more explicit than label selection.

### D3: Required Duration Fields

Three duration fields are required on every policy:

- `checkpointFreshnessTarget`: Defines what "stale" means for a checkpoint.
- `startupProtectionWindow`: Duration of priority protection after start/resume.
- `minRuntimeBetweenYields`: Anti-thrashing guard between successive yields.

These are required because a policy without these values cannot meaningfully
shape priority. There are no safe universal defaults for these — they depend on
the training workload's checkpoint cadence and expected runtime.

### D4: Yield Budget as Optional Pair

`maxYieldsPerWindow` and `yieldWindow` form a paired constraint:

- `yieldWindow` is required when `maxYieldsPerWindow > 0`.
- When `maxYieldsPerWindow` is 0 (default), yield counting is disabled.

This avoids forcing operators to configure yield budgets when they only want
checkpoint-freshness-based shaping.

### D5: Fail-Open Defaults

- `failOpenOnTelemetryLoss`: defaults to **true** (fail-safe: no silent demotion
  when checkpoint timestamps are unavailable due to I/O errors).
- `failOpenOnCheckpointStoreErrors`: defaults to **false** (conservative: treat
  unreachable store as stale to incentivize fixing the store).

This asymmetry is intentional: telemetry loss is often transient and outside the
operator's control, while store errors may indicate a real problem.

### D6: Priority Adjustments as Offsets

Four int32 fields control priority adjustments per preemption state:

- `protectedBoost`: Added during Protected state (default 0).
- `cooldownBoost`: Added during Cooldown state (default 0).
- `staleCheckpointBoost`: Added when checkpoint exceeds freshness target (default 0).
- `preemptibleOffset`: Added during Preemptible state (default 0).

All values are bounded to [-1_000_000_000, +1_000_000_000] to prevent int32
overflow in priority arithmetic. Negative values are explicitly allowed for
`preemptibleOffset` (the primary mechanism for lowering priority of stale jobs).

### D7: Priority Clamping

Optional `minEffectivePriority` and `maxEffectivePriority` fields provide a
safety clamp on the computed effective priority. When both are set,
`min <= max` is enforced.

### D8: RTJ Status Fields

RTJ status gains a `priorityShaping` sub-object with observability fields:

| Field | Type | Purpose |
|-------|------|---------|
| basePriority | int32 | Static priority from WorkloadPriorityClass |
| effectivePriority | int32 | Computed priority written to Workload |
| preemptionState | enum | Protected/Active/Cooldown/Preemptible |
| preemptionStateReason | string | Machine-readable reason for current state |
| protectedUntil | *Time | When protection window expires |
| lastCompletedCheckpointTime | *Time | Most recent checkpoint timestamp |
| checkpointAge | string | Human-readable checkpoint age |
| lastYieldTime | *Time | Most recent yield timestamp |
| lastResumeTime | *Time | Most recent resume timestamp |
| recentYieldCount | int32 | Yields within the yield window |
| appliedPolicyRef | string | Name of the policy used for computation |

The entire sub-object is nil when no policy is referenced, preserving Phase 4
status shape.

### D9: Effective Priority Relationship to WorkloadPriorityClass

The existing immutable `spec.workloadPriorityClassName` determines the base
priority. This base priority is resolved by looking up the Kueue
WorkloadPriorityClass and reading its integer value. The
CheckpointPriorityPolicy then adjusts this base:

```
effective_priority = clamp(
    base_priority + state_adjustment,
    minEffectivePriority,
    maxEffectivePriority,
)
```

Key invariants:
- The WorkloadPriorityClass and its value are **never modified** by the operator.
- The base priority is a **read-only input** to the shaping formula.
- Only `Workload.Spec.Priority` (the integer field) is written by the controller.
- When no policy is attached, the Workload priority equals the base priority
  (standard Kueue behavior, Phase 4 preserved).

## Consequences

### Positive

- Clean separation: base priority (Kueue-owned) vs. effective priority (operator-derived).
- Nil policyRef preserves Phase 4 behavior exactly.
- Bounded offsets prevent arithmetic overflow.
- Rich status enables observability without requiring log analysis.

### Negative

- Operators must create a separate cluster-scoped object (not inline in RTJ spec).
- The controller implementation is deferred, so the API cannot be validated
  end-to-end until the priority shaping controller is built.

### Risks

- OQ-1 (Workload.Spec.Priority mutability) remains unresolved. If Kueue's
  GenericJob adapter clobbers the Priority field on sync, the design needs
  an adapter-level fix. This is documented in ADR 0001 and does not affect
  the API surface defined here.
