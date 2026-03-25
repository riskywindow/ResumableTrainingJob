# Phase 1 Index

This is the entry point for the implemented Phase 1 slice of the `checkpoint-native preemption controller`.
It is the fastest way for a new engineer to understand what exists in the repo, how to run the demo, what the tests cover, and where the remaining gaps are.

## Phase 1 In One Page

Phase 1 delivers a thin, working vertical slice with these concrete properties:

- Go `ResumableTrainingJob` operator
- child `JobSet` managed by Kueue through built-in JobSet integration
- Python toy trainer using PyTorch DCP
- S3-compatible checkpoint storage
- manual pause and manual resume only
- CPU-first `kind` path with `gloo`
- one e2e pause smoke and one e2e resume smoke

The default happy path is:

1. Create an RTJ with `spec.control.desiredState=Running`.
2. The operator creates child `JobSet` attempt 1.
3. Kueue admits that `JobSet`.
4. The trainer runs and writes periodic checkpoints.
5. Patch `desiredState=Paused`.
6. The trainer reaches a step boundary, writes a DCP checkpoint, uploads artifacts, publishes the manifest last, and exits.
7. The operator records the completed checkpoint and converges the RTJ to `Paused`.
8. Patch `desiredState=Running`.
9. The operator selects the newest compatible complete checkpoint from object storage, creates child `JobSet` attempt 2, and injects the selected manifest URI.
10. The trainer restores and training continues from a monotonically increasing global step.

## Repo And Demo Path

Read these in order:

1. [README.md](README.md)
2. [goals.md](goals.md)
3. [architecture.md](architecture.md)
4. [demo.md](demo.md)
5. [operations.md](operations.md)
6. [PHASE1_SIGNOFF.md](PHASE1_SIGNOFF.md)
7. [review/consistency-audit.md](review/consistency-audit.md)
8. [review/gaps.md](review/gaps.md)

The shortest practical demo path is:

1. `make dev-up`
2. `go run ./cmd/operator --leader-elect=false`
3. `docker build -t phase1-ddp-counter:dev -f fixtures/pytorch_ddp_counter/Dockerfile .`
4. `make load-images IMAGES=phase1-ddp-counter:dev`
5. `make submit-example EXAMPLE_TRAINER_IMAGE=phase1-ddp-counter:dev`
6. `make inspect-example`
7. `make pause-example`
8. `make inspect-example`
9. `make resume-example`
10. `make inspect-example`
11. `curl -s http://127.0.0.1:8080/metrics | rg 'checkpoint_native_operator'`

See [demo.md](demo.md) for the exact shell blocks.

## Where The Code Lives

- [api/v1alpha1](../../api/v1alpha1): RTJ API types, defaults, validation, and status helpers
- [internal/controller](../../internal/controller): RTJ reconciliation, launch, pause, and resume logic
- [internal/jobset](../../internal/jobset): child `JobSet` rendering
- [internal/checkpoints](../../internal/checkpoints): manifest parsing, compatibility checks, catalog discovery, and selection
- [internal/metrics](../../internal/metrics): lightweight operator metrics
- [sdk/python/yield_sdk](../../sdk/python/yield_sdk): Python runtime helpers for control, DCP checkpointing, and restore
- [fixtures/pytorch_ddp_counter](../../fixtures/pytorch_ddp_counter): CPU-first toy trainer used by the demo and e2e path
- [hack/dev](../../hack/dev): local setup, inspection, and example workflow scripts
- [test/e2e](../../test/e2e): live kind smoke tests

## Test Coverage That Exists Today

Phase 1 has at least the required minimum coverage:

- unit coverage for API helpers in `api/v1alpha1/resumabletrainingjob_types_test.go`
- unit coverage for manifest compatibility and selection in `internal/checkpoints/compatibility_test.go` and `internal/checkpoints/selector_test.go`
- Python unit coverage for manifest encoding and restore behavior in `sdk/python/tests/test_manifest.py` and `sdk/python/tests/test_resume.py`
- one e2e pause-flow smoke in `test/e2e/pause_flow_test.go`
- one e2e resume-flow smoke in `test/e2e/resume_flow_test.go`

The resume smoke pauses again after resume and verifies that the later checkpoint step is greater than the first paused checkpoint step.

## Locked Boundaries

Phase 1 stays inside the accepted Phase 0 contract:

- one Kubernetes cluster only
- Kueue authority for queueing and admission
- child `JobSet` managed through Kueue built-in integration
- no native Kueue custom-job integration
- no Kueue-driven preemption implementation
- no world-size change on resume
- no MultiKueue, topology-aware placement, or elastic workloads
- no async checkpoint performance work
- no transparent CUDA or container snapshots

## Known Caveats

Phase 1 is signed off, but a few gaps remain visible:

- `Running` currently means an active child `JobSet` exists, not that runtime heartbeat or restore-complete evidence was observed.
- `Queued` and `Admitted` are present in the API but not yet surfaced by the controller.
- Resume does not yet fall back to an older compatible checkpoint after restore-start failure on the newest selected checkpoint.
- There is no dedicated repeated multi-cycle live soak test yet.

Those gaps are tracked in [review/gaps.md](review/gaps.md).

## Usage Rule

If a future change needs to alter the accepted Phase 0 scope, authority model, or resume compatibility rules, it must be captured in a new ADR before code implements it.
