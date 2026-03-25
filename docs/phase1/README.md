# Checkpoint-Native Preemption Controller Phase 1

Phase 1 builds the thinnest useful implementation slice on top of the accepted Phase 0 contract pack.
The goal is a correctness-first path that proves the end-to-end lifecycle on a local `kind` cluster:

- `ResumableTrainingJob` operator in Go
- child `JobSet` managed through Kueue's built-in JobSet integration
- Python toy trainer using PyTorch DCP
- S3-compatible object storage for checkpoints
- manual pause and resume only
- one CPU-first smoke test that runs under `kind` with `gloo`

Phase 1 does not widen the product boundary.
It keeps the accepted `v1` scope: one cluster, JobSet only, PyTorch only, DCP only, object storage only, step-boundary yield only, and strict fail-closed resume compatibility.

## Reading Order

1. [index.md](index.md)
2. [goals.md](goals.md)
3. [architecture.md](architecture.md)
4. [demo.md](demo.md)
5. [operations.md](operations.md)
6. [PHASE1_SIGNOFF.md](PHASE1_SIGNOFF.md)
7. [review/consistency-audit.md](review/consistency-audit.md)
8. [review/gaps.md](review/gaps.md)
9. [repo-layout.md](repo-layout.md)
10. [adr/0001-phase1-vertical-slice.md](adr/0001-phase1-vertical-slice.md)
11. [open-questions.md](open-questions.md)
12. [resume-flow.md](resume-flow.md)
13. [session-handoff.md](session-handoff.md)

## Key Phase 1 Rules

- Kueue continues to own queueing and admission.
- The operator manages only a child `JobSet`; native Kueue custom-job integration is deferred.
- Kueue-driven preemption is deferred; Phase 1 exercises manual pause and resume only.
- Checkpointing is synchronous DCP with local filesystem staging followed by object-storage upload and manifest-last publication.
- The default development and test path must work on CPU in `kind` with `gloo`; GPU or `NCCL` support may be added later but cannot be required.
