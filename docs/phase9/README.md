# Phase 9 — Hybrid Elastic RTJ

Phase 9 adds **dynamic worker-count elasticity** to the ResumableTrainingJob
controller while preserving every invariant established in Phases 0–8.

## Quick orientation

| Document | Purpose |
|---|---|
| [index.md](index.md) | Phase scope, invariants, feature matrix |
| [goals.md](goals.md) | Must-ship, stretch, and non-goals |
| [architecture.md](architecture.md) | Component & sequence diagrams |
| [migration-from-phase8.md](migration-from-phase8.md) | What stays, what changes, backward compat |
| [open-questions.md](open-questions.md) | Unresolved design questions |
| [session-handoff.md](session-handoff.md) | Session log for multi-session continuity |
| [adr/0001-hybrid-elastic-rtj.md](adr/0001-hybrid-elastic-rtj.md) | Architectural decision record |

## One-paragraph summary

An operator or platform team can now set `spec.elasticity.targetWorkerCount`
on an existing RTJ.  The controller compares the target against the current
running worker count, decides whether the change can be applied in-place or
requires a checkpoint-and-relaunch cycle, releases surplus quota via
`Workload.status.reclaimablePods`, and drives the transition to completion.
Scale-down that the runtime supports in-place is the fast path; every other
resize goes through the proven checkpoint-and-relaunch path.  Automatic
metric-driven autoscaling is explicitly out of scope for this milestone.

## Key design constraints (inherited from Phases 0–8)

1. RTJ is the only Kueue-managed admission object.
2. Child JobSets are plain runtime resources — never Kueue workloads.
3. Kueue is sole authority for admission, preemption, and quota.
4. RTJ operator is lifecycle owner for launch, yield, resize, checkpoint, and
   runtime rendering.
5. Native Kueue Workload Slices are **not** the required/core path.
6. No custom scheduler or custom quota engine.
7. No automatic metric-driven autoscaling in the core milestone.
