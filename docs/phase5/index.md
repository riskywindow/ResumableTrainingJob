# Phase 5 Document Index

## Phase 5 Scope

Phase 5: Checkpoint-Aware Priority Shaping.

Build on Phase 4's topology-aware admission pipeline to:

1. Derive checkpoint-aware effective priority for RTJ-backed Workloads.
2. Implement yield budgets / protection windows that shield newly-started jobs.
3. Write effective priority to Kueue Workloads for within-ClusterQueue preemption.
4. Provide a deterministic local/e2e preemption profile for development.

## Documents

### Design

- [README.md](README.md) - overview and quick context
- [goals.md](goals.md) - goals, non-goals, success criteria, and exit criteria
- [architecture.md](architecture.md) - component diagram, three sequence diagrams, detailed design
- [adr/0001-checkpoint-aware-priority-shaping.md](adr/0001-checkpoint-aware-priority-shaping.md) - Phase 5 priority shaping contract and design decisions
- [adr/0002-checkpointprioritypolicy-api.md](adr/0002-checkpointprioritypolicy-api.md) - CheckpointPriorityPolicy API design decisions

### API

- [api.md](api.md) - CheckpointPriorityPolicy CRD reference, RTJ extensions, validation rules, effective priority formula

### Implementation

- [telemetry.md](telemetry.md) - telemetry fields, data sources, idempotency guarantees, Prometheus metrics
- [policy-engine.md](policy-engine.md) - decision state model, evaluation order, effective priority formula, test coverage

### Migration

- [migration-from-phase4.md](migration-from-phase4.md) - what stays, what changes, why effective priority is derived, why cohort/fair-sharing is deferred

### Tracking

- [open-questions.md](open-questions.md) - unresolved questions with resolution plans
- [session-handoff.md](session-handoff.md) - session state for prompt continuity

## Upstream Phase References

- [Phase 0 index](../phase0/index.md) - locked v1 contract
- [Phase 1 index](../phase1/index.md) - manual pause/resume vertical slice
- [Phase 2 index](../phase2/index.md) - native Kueue integration with preemption and resume
- [Phase 3 index](../phase3/index.md) - admission-aware launch, flavor-aware resume, partial admission
- [Phase 4 index](../phase4/index.md) - topology-aware admission pipeline
