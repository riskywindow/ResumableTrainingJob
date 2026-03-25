# Checkpoint-Native Preemption Controller Phase 2

Phase 2 turns `ResumableTrainingJob` (`RTJ`) into a native Kueue-managed custom job using Kueue's external integration path via `jobframework`.
The child `JobSet` remains the plain runtime resource.

This is the core ownership change from Phase 1:

- `RTJ` becomes the only Kueue-managed admission object in Phase 2.
- The child `JobSet` is no longer Kueue-managed.
- Kueue-driven suspend and preemption are now in scope.
- Manual pause and resume remain useful, but they must collapse onto the same graceful-yield and checkpoint path.

Phase 2 does not widen the product boundary.
It still stays within the accepted Phase 0 contract:

- one cluster only
- JobSet as the only runtime primitive
- PyTorch DDP and FSDP only
- PyTorch DCP only
- S3-compatible object storage only
- strict fail-closed resume compatibility

## Reading Order

1. [index.md](index.md)
2. [goals.md](goals.md)
3. [architecture.md](architecture.md)
4. [kueue-external-integration.md](kueue-external-integration.md)
5. [api-and-webhooks.md](api-and-webhooks.md)
6. [adr/0001-native-kueue-integration.md](adr/0001-native-kueue-integration.md)
7. [adr/0002-suspend-semantics.md](adr/0002-suspend-semantics.md)
8. [migration-from-phase1.md](migration-from-phase1.md)
9. [open-questions.md](open-questions.md)
10. [session-handoff.md](session-handoff.md)

## Phase 2 In One Page

The intended Phase 2 happy path is:

1. User creates an RTJ that targets a Kueue queue.
2. Kueue creates and manages the RTJ-owned `Workload` through external integration.
3. The RTJ stays suspended and queued until Kueue admits it.
4. After admission, the RTJ controller creates one plain child `JobSet` attempt.
5. The trainer runs and writes periodic checkpoints.
6. If Kueue suspends or preempts the RTJ, the RTJ controller asks the runtime to yield gracefully, waits for a complete checkpoint, and then tears down the child `JobSet`.
7. When Kueue re-admits the RTJ, the controller creates a fresh child `JobSet` attempt that restores from the latest compatible complete checkpoint.

## Locked Phase 2 Boundaries

- `RTJ` is the only Kueue-managed admission object.
- Child `JobSet` resources must not carry queue or priority labels for Kueue admission.
- Phase 2 uses Kueue's current external integration path via `jobframework`.
- Phase 2 does not add a custom scheduler or a custom preemption algorithm.
- Phase 2 does not add MultiKueue, topology-aware scheduling or resume, elastic workloads, world-size changes, or transparent CUDA or container snapshots.
- The existing Phase 1 manual pause and resume surface should be preserved if practical, but it cannot override Kueue's authority for queueing and admission.
