# Phase 1 Goals

## Objective

Phase 1 defines a thin, working vertical slice that proves manual yield and resume for one in-scope workload shape without widening the accepted `v1` contract.

The slice is intentionally small:

- Go `ResumableTrainingJob` operator
- child `JobSet` created by the operator
- Kueue built-in JobSet integration for queueing and admission
- Python toy trainer using PyTorch DCP
- S3-compatible object storage
- local `kind` development environment
- one end-to-end smoke test on CPU with `gloo`

## In Scope

- A concrete `v1alpha1` RTJ API derived from the Phase 0 conceptual contract.
- Manual control through `spec.control.desiredState`.
- One active child `JobSet` per RTJ attempt.
- Manual pause from `Running` to `Paused`.
- Manual resume from `Paused` back to `Running`.
- Synchronous DCP checkpointing with local filesystem staging before upload to object storage.
- Checkpoint selection using the latest compatible complete checkpoint only.
- Object storage that can run locally in `kind`, such as MinIO or another S3-compatible endpoint.
- CPU-first local execution with `torch.distributed` using `gloo`.

## Explicitly Deferred

- Native Kueue custom-job integration
- Kueue-driven preemption implementation
- MultiKueue
- topology-aware placement
- elastic workloads
- world-size changes on resume
- async checkpoint pipelines or performance tuning
- transparent CUDA, process, or container snapshots
- GPU-only or `NCCL`-required development paths

## Phase 1 Deliverables

- Repo scaffold for API, operator, trainer SDK, fixtures, deploy assets, and e2e tests
- Documentation pack for the vertical slice
- Initial CRD and controller skeletons
- A toy distributed trainer fixture that can checkpoint and resume
- Local development assets for `kind`, Kueue, JobSet, and object storage
- One smoke test that validates create -> pause -> resume on CPU

## Happy Path

```text
create RTJ
-> operator creates child JobSet
-> Kueue admits JobSet
-> training starts
-> user requests pause
-> trainer reaches step boundary
-> trainer writes checkpoint locally
-> trainer uploads checkpoint to object storage
-> trainer publishes manifest last and exits
-> operator verifies checkpoint and marks RTJ Paused
-> user requests Running
-> operator creates a new child JobSet
-> trainer restores from latest compatible complete checkpoint
-> RTJ returns to Running
```

## Success Bar For This Slice

Phase 1 is not the full Phase 0 benchmark bar.
The success bar for this slice is narrower:

- the default path works on CPU in `kind`
- pause and resume use the same RTJ lifecycle surface described in Phase 0
- Kueue remains authoritative for admission of the child `JobSet`
- the operator never creates two active child `JobSet` attempts for one RTJ
- resume uses only a valid complete checkpoint
- one e2e smoke test proves the happy path end to end
