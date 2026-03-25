# Phase 1 Consistency Audit

This audit checks the implemented Phase 1 slice against the accepted Phase 0 contracts.
It is a signoff-oriented audit, not a redesign document.

## Audit Basis

The audit compared the current code, tests, and docs against these Phase 0 contract documents:

- `docs/phase0/contracts/resumabletrainingjob-api.md`
- `docs/phase0/contracts/resumabletrainingjob-status.md`
- `docs/phase0/contracts/lifecycle-state-machine.md`
- `docs/phase0/contracts/yield-resume-protocol.md`
- `docs/phase0/contracts/checkpoint-contract.md`
- `docs/phase0/contracts/checkpoint-selection-and-compatibility.md`
- `docs/phase0/contracts/failure-semantics.md`
- `docs/phase0/contracts/non-goal-boundaries.md`

The audit also reviewed the current Phase 1 implementation and test surfaces:

- `api/v1alpha1/resumabletrainingjob_types.go`
- `api/v1alpha1/resumabletrainingjob_types_test.go`
- `internal/checkpoints/*.go`
- `internal/checkpoints/*_test.go`
- `internal/controller/*.go`
- `internal/controller/resumabletrainingjob_controller_test.go`
- `sdk/python/tests/*.py`
- `test/e2e/pause_flow_test.go`
- `test/e2e/resume_flow_test.go`

## Contract Audit

### Scope And Authority Boundaries

Status: aligned

- The implementation stays single-cluster.
- The operator manages a child `JobSet`, not a native Kueue custom job.
- Kueue remains responsible for queueing and admission of that child `JobSet`.
- Manual pause and manual resume are the only control path implemented for the default Phase 1 slice.
- Deferred items from Phase 0 remain deferred: Kueue-driven preemption, MultiKueue, topology-aware placement, elastic world-size change, async checkpoint performance work, and transparent container snapshots.

Evidence:

- `internal/jobset`
- `internal/controller/resumabletrainingjob_controller.go`
- `docs/phase1/goals.md`
- `docs/phase1/architecture.md`

### RTJ API Surface

Status: aligned

- `spec.control.desiredState` is the manual control surface used by Phase 1.
- The API keeps the fixed Phase 0 resume policy shape instead of adding user-directed checkpoint selection.
- The CRD-level defaults and validation have unit coverage for the Phase 1 helper behavior that exists today.

Evidence:

- `api/v1alpha1/resumabletrainingjob_types.go`
- `api/v1alpha1/resumabletrainingjob_types_test.go`

### Checkpoint Completion Contract

Status: aligned for the implemented slice

- The trainer writes DCP data to local staging first, uploads artifacts to S3-compatible storage, and publishes the manifest last.
- Pause completion requires storage-visible evidence newer than the accepted pause request.
- Resume selection is manifest-driven and does not resume from raw checkpoint directories.

Evidence:

- `sdk/python/yield_sdk`
- `internal/checkpoints/catalog.go`
- `docs/phase1/pause-flow.md`
- `docs/phase1/resume-flow.md`

### Resume Compatibility And Selection

Status: aligned, with one explicit Phase 1 simplification

- Phase 1 rejects incomplete manifests.
- Phase 1 requires required artifacts to exist.
- Phase 1 enforces strict identity matching for:
  - cluster identity
  - RTJ lineage identity
  - runtime mode
  - world size
  - GPU shape
  - image identity
  - code version identity
  - checkpoint format version
  - optimizer mode
  - sharding mode
- Phase 1 sorts compatible candidates by `completionTimestamp` descending and selects the newest compatible complete checkpoint.
- The Python restore path re-checks compatibility before loading state.

Evidence:

- `internal/checkpoints/compatibility.go`
- `internal/checkpoints/selector.go`
- `internal/checkpoints/catalog.go`
- `internal/checkpoints/compatibility_test.go`
- `internal/checkpoints/selector_test.go`
- `sdk/python/tests/test_manifest.py`
- `sdk/python/tests/test_resume.py`

Explicit simplification:

- The accepted Phase 0 selection contract allows fallback to the next candidate if restore-start validation fails.
- The current Phase 1 implementation does not walk a fallback chain after a restore-start failure inside the trainer; it fails closed for that attempt.

That simplification is a documented Phase 1 gap, not silent scope drift.

### Pause And Resume Lifecycle

Status: partially aligned

What aligns:

- Pause moves through `YieldRequested` and `Draining` before `Paused`.
- Pause is bounded by `spec.checkpoint.maxDrainTime`.
- Resume creates a new run attempt and records `status.selectedCheckpoint`.
- Resume goes through `Restoring` before the next reconcile returns the RTJ to `Running`.

What does not fully align:

- The conceptual Phase 0 lifecycle includes `Queued` and `Admitted`, but the current controller does not publish those phases.
- The controller currently treats `Running` as "an active child JobSet exists" rather than "runtime heartbeat or restore-complete evidence was observed."

Evidence:

- `internal/controller/status_helpers.go`
- `internal/controller/resumabletrainingjob_controller.go`
- `internal/controller/resume.go`
- `internal/controller/yield.go`
- `internal/controller/resumabletrainingjob_controller_test.go`

### Failure Semantics

Status: partially aligned

What aligns:

- Pause timeout fails closed.
- No compatible complete checkpoint fails closed.
- Incompatible manifests are rejected in both Go selection logic and Python restore logic.

What remains weaker than the Phase 0 contract:

- Skipped checkpoint reasons are not surfaced in RTJ status or events.
- Restore retry and next-candidate fallback behavior are not fully implemented.
- The signoff path does not yet prove repeated pause and resume cycles on a live kind cluster.

### Demo, Operations, And Developer UX

Status: aligned after this pass

- The core demo path is documented as one exact sequence.
- Lightweight operations docs point to operator logs, trainer logs, manifests, child JobSets, Kueue objects, and metrics.
- The index now points a new engineer to the repo entry points, demo, runbook, tests, review docs, and signoff page from one place.

## Docs Drift Found During Audit

These were documentation drifts, not code-path drift:

- `docs/phase1/pause-flow.md` still described resume selection as a placeholder path from `status.selectedCheckpoint`.
- `docs/phase1/open-questions.md` still listed decisions that are already implemented.
- `docs/phase1/adr/0001-phase1-vertical-slice.md` was still marked `Proposed` even though the repo now implements that direction.
- `docs/phase1/index.md` still read like a planning-only pack instead of an implementation and signoff entry point.

Those items were corrected in this pass.

## Signoff Conclusion

Phase 1 is consistent enough to sign off as a correctness-first vertical slice for local development and demo use.
It is not yet fully aligned with every richer Phase 0 lifecycle and failure-handling detail, and those remaining gaps are tracked in `docs/phase1/review/gaps.md`.
