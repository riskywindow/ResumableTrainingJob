# Phase 1 Signoff

Phase 1 is signed off as a thin, correctness-first vertical slice.
This signoff is for local development, demo, and hardening of the accepted Phase 0 control path.
It is not a production-readiness claim.

## What Phase 1 Can Do

- Reconcile a Go `ResumableTrainingJob` CRD.
- Create one child `JobSet` per run attempt.
- Let Kueue manage the child `JobSet` through built-in JobSet integration.
- Run the Python toy trainer on CPU in kind with `gloo`.
- Write synchronous PyTorch DCP checkpoints to local staging and upload them to S3-compatible object storage.
- Pause manually through `spec.control.desiredState=Paused`.
- Detect a completed checkpoint from object storage and converge the RTJ to `Paused`.
- Resume manually through `spec.control.desiredState=Running`.
- Discover checkpoint manifests in object storage, reject incomplete or incompatible candidates, and select the newest compatible complete checkpoint.
- Start a new run attempt with the selected manifest URI injected into the trainer.
- Restore trainer state and continue with monotonically increasing global step progress.
- Expose lightweight metrics, logs, and inspection commands for the local demo flow.

## What Phase 1 Cannot Do

- It cannot implement Kueue-driven preemption yet.
- It cannot manage RTJ as a native Kueue custom job.
- It cannot resume with a different world size.
- It cannot do elastic training, topology-aware placement, or MultiKueue.
- It cannot do async checkpoint performance work or transparent CUDA or container snapshots.
- It cannot present durable monitoring or a UI.
- It cannot yet fall back to an older compatible checkpoint after a restore-start failure on the newest selected checkpoint.
- It cannot yet prove runtime liveness through a richer restore-complete or heartbeat contract.

## Main Known Risks

- `Running` is still inferred from active child `JobSet` existence, not from explicit runtime progress evidence.
- `Queued` and `Admitted` exist in the API, but the current controller does not surface those phases yet.
- Resume debugging still depends on operator logs and direct manifest inspection because skipped checkpoint reasons are not exposed in RTJ status.
- Metrics are process-local and reset when the local operator process restarts.
- The live signoff bar proves one pause-flow smoke path and one resume-flow smoke path, not a longer repeated-cycle soak.

## Test And Evidence Summary

Unit and helper coverage:

- `api/v1alpha1/resumabletrainingjob_types_test.go`
- `internal/checkpoints/compatibility_test.go`
- `internal/checkpoints/selector_test.go`
- `sdk/python/tests/test_manifest.py`
- `sdk/python/tests/test_resume.py`

Controller coverage:

- `internal/controller/resumabletrainingjob_controller_test.go`

End-to-end smoke coverage:

- `test/e2e/pause_flow_test.go`
- `test/e2e/resume_flow_test.go`

Execution note:

- The e2e tests are environment-gated live kind smokes. They require `RUN_KIND_E2E=1` and a loaded trainer image through `PAUSE_FLOW_TRAINER_IMAGE`.
- The signoff pass reran the Go test suite that includes the e2e package, but it did not rerun those smokes against a live kind cluster in this prompt.

Operational documentation:

- `docs/phase1/demo.md`
- `docs/phase1/operations.md`
- `docs/phase1/review/consistency-audit.md`
- `docs/phase1/review/gaps.md`

## What Phase 2 Should Build Next

1. Tighten lifecycle truthfulness by adding bounded runtime heartbeat and restore-complete evidence before publishing `Running`.
2. Surface Kueue queue and admission state in RTJ status so `Queued` and `Admitted` become real controller phases.
3. Add bounded resume fallback to the next newest compatible checkpoint and publish clearer failure reasons for skipped or unusable manifests.
4. Improve the in-cluster operator deployment path so the demo and CI do not depend on a separate local `go run` process plus manual MinIO port-forwarding.
5. Add a repeated pause and resume soak path once the runtime evidence and restore semantics are stronger.
