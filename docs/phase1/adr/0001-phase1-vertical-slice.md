# ADR 0001: Phase 1 Vertical Slice

- Status: Accepted
- Date: 2026-03-20

## Context

Phase 0 locked the `v1` contract but intentionally deferred implementation-shaped choices.
Phase 1 needs a narrow, working slice that proves the product can create a Kueue-managed child `JobSet`, run a PyTorch trainer, pause manually, checkpoint safely, and resume from the latest compatible complete checkpoint in a local development environment.

The slice must preserve all accepted Phase 0 constraints:

- one cluster only
- Kueue authority for queueing and admission
- JobSet as the only runtime primitive
- PyTorch `DDP` or `FSDP` only
- DCP only
- S3-compatible storage only
- step-boundary yield only
- strict fail-closed compatibility rules

## Decision

Phase 1 will implement a manual pause and resume vertical slice with these properties:

1. The control-plane object remains `ResumableTrainingJob`.
2. The operator is written in Go.
3. The runtime workload created by the operator is always a child `JobSet`.
4. Kueue manages that child `JobSet` through its built-in JobSet integration.
5. Phase 1 does not implement native Kueue custom-job integration for RTJ.
6. Phase 1 does not implement Kueue-driven preemption yet.
7. Manual control uses the existing Phase 0 field `spec.control.desiredState` with values `Running` and `Paused`.
8. The trainer and runtime SDK code are written in Python.
9. Checkpointing uses synchronous PyTorch DCP with local filesystem staging and S3-compatible upload.
10. The default developer and e2e path runs on CPU in `kind` with `gloo`.

## Manual Control Surface

No backward-compatible API extension is needed for Phase 1 manual pause and resume.
The accepted Phase 0 API contract already includes:

- `spec.control.desiredState=Paused` for manual yield intent
- `spec.control.desiredState=Running` for run or resume intent

Phase 1 will use that field directly rather than introducing a second manual-control API.

## Happy Path

The Phase 1 happy path is:

1. User creates an RTJ with `desiredState=Running`.
2. The operator creates a child `JobSet`.
3. Kueue admits the child `JobSet`.
4. The trainer starts and trains.
5. User patches `desiredState=Paused`.
6. The trainer reaches a step boundary, writes a synchronous checkpoint, uploads it, publishes the manifest last, and exits.
7. The operator verifies the checkpoint and marks the RTJ `Paused`.
8. User patches `desiredState=Running`.
9. The operator creates a new child `JobSet` that restores from the latest compatible complete checkpoint.
10. The trainer restores and returns to `Running`.

## Deferred By This ADR

This ADR explicitly defers:

- native Kueue custom-job integration
- Kueue-driven preemption
- MultiKueue
- topology-aware placement
- elastic world-size changes
- async checkpoint optimizations
- GPU-required local development flows
- transparent CUDA or container snapshot behavior

## Consequences

Positive consequences:

- The slice stays inside the accepted Phase 0 contract.
- The first implementation path is runnable on a laptop with `kind`.
- The same RTJ lifecycle surface can later absorb Kueue-driven yield without inventing a second product path.

Negative consequences:

- Phase 1 will not yet prove queue-driven yield.
- CPU plus `gloo` is useful for correctness, not for performance conclusions.
- A later ADR or design note may still be needed to freeze the concrete operator-to-trainer signal transport.
