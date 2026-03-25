# Phase 0 Signoff

- Date: 2026-03-20
- Status: Phase 0 contract pack accepted for Phase 1 planning

## Locked v1 Contract

Phase 0 locks the following `v1` contract:

- one Kubernetes cluster only
- Kueue authority for queueing, admission, and queue-driven preemption intent
- one product-specific operator coordinating the `ResumableTrainingJob` lifecycle
- JobSet as the only supported runtime primitive
- PyTorch `DDP` and `FSDP` as the only supported execution modes
- PyTorch `DCP` as the only supported checkpoint mechanism and format
- S3-compatible object storage as the only supported checkpoint storage target
- graceful yield only at training step boundaries
- manual yield and Kueue-driven yield converging on one lifecycle
- resume only from the latest compatible complete checkpoint
- strict fail-closed resume compatibility on cluster identity, lineage, runtime mode, world size, GPU shape, image identity, code version, optimizer mode, and sharding mode

## Non-Goals

The following remain explicit non-goals for `v1`:

- multi-cluster resume
- topology-aware placement
- elastic shrink or grow in place
- non-PyTorch frameworks
- transparent CUDA or container snapshots
- custom scheduler implementation
- generalized node-failure recovery as a product guarantee
- dynamic world-size change on resume

## Accepted Success Metrics

The accepted `v1` success bar is:

- p95 lost progress after controlled yield: `<= 1` completed step on every required workload
- p95 drain time after yield request:
  - `RW-1`: `<= 180s`
  - `RW-2`: `<= 300s`
  - `RW-3`: `<= 600s`
- repeated-cycle resume success: `>= 95%` over `40` controlled cycles per required workload
- checkpoint overhead:
  - `RW-1`: `<= 5%` median step-time inflation
  - `RW-2`: `<= 8%`
  - `RW-3`: `<= 15%`
- resumes from incomplete checkpoints: `0` allowed

The required benchmark workload set is fixed to `RW-1`, `RW-2`, and `RW-3` in [benchmarks/reference-workloads.md](/Users/rishivinodkumar/Daedelus/docs/phase0/benchmarks/reference-workloads.md).

## Known Risks

Phase 0 accepts the following remaining risks:

- Kueue integration semantics still require a concrete signal choice
- operator-to-runtime transport still requires a concrete signaling design
- strict compatibility and fail-closed behavior may reject some operationally tempting but unsafe resumes
- step-boundary latency may limit usefulness for some operational events
- checkpoint integrity and fallback behavior must be implemented carefully to preserve the manifest-last contract

These are known implementation and integration risks, not unresolved product-scope decisions.

## What Phase 1 May Build Without Reopening Phase 0

Phase 1 MAY build the following without reopening Phase 0:

- a concrete implementation plan for the `ResumableTrainingJob` operator within the locked scope
- a concrete Kueue integration design that preserves Kueue authority
- a concrete SDK or agent signaling design that preserves the accepted protocol semantics
- a concrete status, event, and metrics implementation aligned to the accepted lifecycle and failure contracts
- a benchmark harness for `RW-1`, `RW-2`, and `RW-3`
- a validation suite derived from `test-plan.md`
- a later ADR for the final manual-yield transport or other implementation-shaped API decisions

Phase 1 MUST NOT reopen:

- the `v1` scope
- the non-goals
- the authority model
- the strict compatibility rules
- the failure and degraded-behavior boundaries
- the accepted benchmark workload set
- the accepted numeric success targets

## Signoff Statement

The Phase 0 contract pack is now specific enough to start Phase 1 implementation planning.
Phase 1 MUST treat the accepted Phase 0 documents as the baseline contract and MUST use [review/decision-gaps.md](/Users/rishivinodkumar/Daedelus/docs/phase0/review/decision-gaps.md) for the remaining deferred implementation-shaped choices.
