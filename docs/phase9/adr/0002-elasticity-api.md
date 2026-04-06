# ADR-0002: Elasticity API Design

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-05 |
| Deciders | Phase 9 implementation session |
| Supersedes | None |
| Depends on | ADR-0001 (Hybrid Elastic RTJ) |

## Context

ADR-0001 established the hybrid elastic resize architecture.  This ADR records
the specific API surface decisions made during implementation.

The core tension is between a narrow, manual-only API and future extensibility
for automatic scaling.  We need enough structure to support manual resize,
reclaim tracking, and resize observability — without introducing speculative
fields for an autoscaler that does not exist yet.

## Decision

### 1. `spec.elasticity` is a separate top-level section

**Not** folded into `spec.parallelism` because:
- Parallelism controls the admission-time shape (preferred/min counts).
- Elasticity controls runtime resize behavior (target, shrink policy, reclaim).
- Keeping them separate avoids ambiguity about which fields are admission-time
  vs. runtime-mutable.

### 2. `ElasticityMode` has exactly two values: `Disabled` and `Manual`

- `Disabled` (default): Phase 8 behavior.  Zero overhead when not opted in.
- `Manual`: Operator-initiated resize via `targetWorkerCount` patch.

**Rejected alternative**: Adding an `Auto` mode placeholder.  Speculative — adds
API surface with no implementation.  Can be added later without breaking changes
because `ElasticityMode` is an open enum.

### 3. `targetWorkerCount` is bounded by existing Phase 3 fields

- Lower bound: `parallelism.minCount` (or 1 when unset).
- Upper bound: `parallelism.preferredCount` (or `identity.worldSize` when unset).

This reuses the existing partial-admission semantics rather than introducing a
new pair of min/max fields.  The Phase 3 fields define the admission envelope;
Phase 9's `targetWorkerCount` moves within it.

### 4. `inPlaceShrinkPolicy` is on the spec, not per-operation

The shrink behavior is a property of the RTJ configuration, not of individual
resize requests.  This avoids a request/response pattern and keeps the API
declarative.

### 5. `reclaimMode` is narrow with a single value

Only `ReclaimablePods` is supported.  This documents the mechanism used and
provides an extension point for future alternatives (e.g., native Kueue
Workload Slices) without changing the API shape.

### 6. `status.elasticity` is a flat struct, not nested per-operation

Status fields describe the current state, not a history of operations.  This
matches the pattern used by `status.admission`, `status.devices`, etc.

### 7. Elasticity mode changes require suspension

Same pattern as queue name changes — prevents races between an in-flight resize
and a mode transition.

### 8. `allowWorldSizeChange: true` is required for Manual mode

Every resize changes the effective world size, so DCP resharding is always
needed.  Requiring this flag makes the dependency explicit and prevents
confusion when a user enables elasticity but hasn't opted into resharding.

### 9. Controller-owned status preserves the full resize lifecycle

The status includes resize state, path, reason, timestamps, checkpoint
references, and reclaimable pod counts.  This provides complete observability
without requiring external logging.

## Alternatives considered

### Folding elasticity into `spec.parallelism`

Adding `targetWorkerCount`, `inPlaceShrinkPolicy`, etc. as siblings of
`preferredCount` and `minCount`.

**Rejected**: Conflates admission-time and runtime concerns.  Makes it unclear
which fields are user-authored vs. controller-influenced.

### Per-resize request object (like a Kubernetes Event or custom resource)

Creating a `ResizeRequest` sub-resource per resize operation.

**Rejected**: Over-engineering for manual resize.  A declarative
`targetWorkerCount` field with controller-owned status is simpler and follows
the existing RTJ pattern.

### Separate `resizeMin` / `resizeMax` fields

Adding new bounds specifically for elasticity, independent of `parallelism`.

**Rejected**: Would duplicate the existing `minCount` / `preferredCount`
semantics.  The Phase 3 fields already define the valid range.

### Adding an `ExecutionMode` to the spec (not just status)

Letting the user explicitly set `executionMode: Elastic` on the spec.

**Rejected**: Redundant with `elasticity.mode: Manual`.  The execution mode
is a derived status property, not a user intent.

## Consequences

### Positive

- Narrow, coherent API with no speculative fields.
- Clear separation between admission-time (parallelism) and runtime (elasticity).
- Reuses existing Phase 3 bounds — no new min/max fields.
- Complete resize observability via status fields.
- Backward compatible: nil `spec.elasticity` == Phase 8 behavior.

### Negative

- Adding automatic scaling later requires extending `ElasticityMode` and
  possibly adding new fields (e.g., scaling policy, metric sources).
- `reclaimMode` has only one value, which might seem like over-engineering.
  However, it documents the mechanism and provides a clean extension point.

### Neutral

- The validation that `allowWorldSizeChange: true` is required may surprise
  users who don't realize resize implies resharding.  Clear error messages
  mitigate this.
