# Phase 3: Admission-Aware Launch and Flavor-Aware Resume

Phase 3 makes ResumableTrainingJob admission-aware by materializing Kueue
`podSetAssignments` into the child JobSet launch shape, adding flavor-aware
runtime placement, supporting world-size-flexible resume via DCP resharding,
and introducing an experimental partial-admission path behind a feature gate.

## Key Documents

| Document | Purpose |
| --- | --- |
| [index.md](index.md) | Phase 3 document index and navigation |
| [goals.md](goals.md) | Phase 3 goals, non-goals, and success criteria |
| [architecture.md](architecture.md) | Component diagrams, sequence diagrams, and detailed design |
| [migration-from-phase2.md](migration-from-phase2.md) | What changes and what stays the same from Phase 2 |
| [open-questions.md](open-questions.md) | Unresolved questions and decisions deferred within Phase 3 |
| [session-handoff.md](session-handoff.md) | Session state for continuity across prompts |
| [adr/0001-adaptive-parallelism-and-flavor-aware-resume.md](adr/0001-adaptive-parallelism-and-flavor-aware-resume.md) | End-to-end Phase 3 contract and must-ship demo |

## Quick Context

- **Phase 0** locked the v1 contract: single cluster, Kueue authority, JobSet runtime, PyTorch DDP/FSDP, DCP checkpointing, S3 storage, strict compatibility, fail-closed semantics.
- **Phase 1** delivered a manual pause/resume vertical slice with a Go operator, Python DCP trainer, and synchronous checkpoint storage.
- **Phase 2** made RTJ the native Kueue-managed admission object via the external `jobframework` integration, with Kueue-driven preemption, graceful yield, and checkpoint-based resume.
- **Phase 3** closes the gap between "admitted" and "launched at the right shape" by reading Kueue's admission decisions and rendering child JobSets that land on the correct node pools with the correct replica counts.

## Invariants Preserved from Earlier Phases

- RTJ is the **only** Kueue-managed admission object.
- Child JobSets are **plain runtime resources** with no Kueue management metadata.
- Kueue is the **exclusive authority** for queueing, admission, and preemption intent.
- The operator is the **exclusive authority** for checkpoint selection, compatibility, and runtime lifecycle.
- All Phase 0 fail-closed semantics, bounded timers, and single-active-runtime invariants remain in force.

## Pinned Versions

| Dependency | Version |
| --- | --- |
| Kueue | v0.15.1 |
| JobSet | v0.10.1 |
| controller-runtime | v0.22.4 |
