# Phase 2 Consistency Audit

This audit checks the implemented Phase 2 slice against the accepted Phase 0 and Phase 1 contracts.
It is a signoff-oriented audit, not a redesign document.

## Audit Basis

The audit compared the current code, tests, and docs against these contract documents:

- `docs/phase0/contracts/actor-responsibilities.md`
- `docs/phase0/contracts/assumptions-and-invariants.md`
- `docs/phase0/contracts/checkpoint-contract.md`
- `docs/phase0/contracts/checkpoint-selection-and-compatibility.md`
- `docs/phase0/contracts/failure-semantics.md`
- `docs/phase0/contracts/lifecycle-state-machine.md`
- `docs/phase0/contracts/non-goal-boundaries.md`
- `docs/phase0/contracts/resumabletrainingjob-api.md`
- `docs/phase0/contracts/resumabletrainingjob-status.md`
- `docs/phase0/contracts/yield-resume-protocol.md`
- `docs/phase0/PHASE0_SIGNOFF.md`
- `docs/phase1/PHASE1_SIGNOFF.md`
- `docs/phase1/review/consistency-audit.md`
- `docs/phase1/review/gaps.md`

The audit also reviewed the current Phase 2 implementation and coverage:

- `api/v1alpha1/resumabletrainingjob_types.go`
- `api/v1alpha1/resumabletrainingjob_webhook.go`
- `api/v1alpha1/resumabletrainingjob_webhook_test.go`
- `internal/kueue/*.go`
- `internal/kueue/*_test.go`
- `internal/jobset/render.go`
- `internal/jobset/render_test.go`
- `internal/checkpoints/*.go`
- `internal/checkpoints/*_test.go`
- `internal/controller/*.go`
- `internal/controller/resumabletrainingjob_controller_test.go`
- `test/e2e/native_kueue_admission_test.go`
- `test/e2e/priority_preemption_resume_test.go`
- `docs/phase2/*.md`

## Contract Audit

### Scope And Authority Boundaries

Status: aligned

- The implementation stays single-cluster.
- Kueue remains authoritative for queueing, admission, and queue-driven preemption intent.
- Phase 2 uses Kueue's current external integration path via `jobframework`.
- The RTJ controller remains responsible for graceful yield, checkpoint observation, checkpoint selection, runtime teardown, and runtime relaunch.
- Deferred Phase 0 non-goals remain deferred:
  - MultiKueue
  - topology-aware scheduling
  - elastic world-size changes
  - transparent CUDA or container snapshots
  - custom scheduler or custom preemption algorithm

Evidence:

- `internal/kueue/setup.go`
- `internal/kueue/rtj_generic_job.go`
- `internal/controller/resumabletrainingjob_controller.go`
- `docs/phase2/README.md`
- `docs/phase2/kueue-external-integration.md`

### Kueue Ownership Boundary

Status: aligned

- RTJ is the only Kueue-managed admission object in the Phase 2 runtime contract.
- Creating an RTJ results in a Kueue `Workload` owned by RTJ.
- The child `JobSet` is not created before RTJ admission clears `spec.suspend`.
- The rendered child `JobSet` strips queue, workload-priority, and other top-level Kueue management metadata.
- The child `JobSet` keeps a non-controller owner reference back to RTJ so it is linked for garbage collection without becoming a controller-owned Kueue descendant.
- The e2e suite explicitly asserts that no second `Workload` is created for the child `JobSet`.

Evidence:

- `internal/jobset/render.go`
- `internal/jobset/render_test.go`
- `internal/controller/resume_flow.go`
- `internal/controller/resumabletrainingjob_controller.go`
- `internal/controller/resumabletrainingjob_controller_test.go`
- `test/e2e/native_kueue_admission_test.go`

### Kueue-Driven Suspend And Preemption Semantics

Status: aligned for the signed-off Phase 2 slice

- Kueue-driven suspend arrives through `RTJ.spec.suspend=true`.
- When a child `JobSet` is active, the controller treats that as an authoritative Kueue stop request.
- The controller writes a stable Kueue-specific stop request id into the control `ConfigMap`.
- The controller waits for yield-marker and completed-checkpoint evidence newer than the accepted suspend request.
- The controller tears down the active child `JobSet` only after that evidence is observed or after `maxDrainTime` fails closed.
- Once the runtime is torn down, RTJ settles into `Queued` while Kueue still holds it suspended.
- When Kueue later clears `spec.suspend`, the controller creates a fresh run attempt and restores from the selected checkpoint.

Evidence:

- `internal/controller/suspend_flow.go`
- `internal/controller/status_helpers.go`
- `internal/controller/resume_flow.go`
- `internal/controller/resumabletrainingjob_controller_test.go`
- `test/e2e/priority_preemption_resume_test.go`

### Checkpoint Compatibility And Resume Correctness

Status: aligned, with one explicit narrowing carried forward from Phase 1

- Resume uses only the latest compatible complete checkpoint.
- Selection rejects incomplete manifests.
- Selection rejects incompatible manifests across the locked compatibility dimensions:
  - cluster identity
  - RTJ lineage identity
  - runtime mode
  - world size
  - GPU shape
  - image identity
  - code version identity
  - optimizer mode
  - sharding mode
  - supported format version
- Pause completion requires storage-visible yield-marker and manifest evidence newer than the stop request.
- The strong Phase 2 e2e verifies that a preempted low-priority RTJ later resumes from the recorded checkpoint and advances to a greater global step.

Evidence:

- `internal/checkpoints/compatibility.go`
- `internal/checkpoints/selector.go`
- `internal/checkpoints/catalog.go`
- `internal/checkpoints/compatibility_test.go`
- `internal/checkpoints/selector_test.go`
- `internal/controller/resumabletrainingjob_controller_test.go`
- `test/e2e/priority_preemption_resume_test.go`

Explicit narrowing:

- The repo still does not walk a next-candidate fallback chain after a restore-start failure on the newest selected compatible checkpoint.
- That is a known recovery-depth gap, not a Phase 2 scope drift.

### API And Webhook Surface

Status: mostly aligned

- `spec.suspend` is the Kueue-facing admission gate.
- `spec.control.desiredState` remains the backward-compatible manual hold surface and does not bypass Kueue admission.
- The webhook defaults RTJ to Kueue-suspended creation semantics and projects Kueue labels onto RTJ.
- The validating webhook enforces the pinned Kueue helper invariants on create and update.

Evidence:

- `api/v1alpha1/resumabletrainingjob_webhook.go`
- `api/v1alpha1/resumabletrainingjob_webhook_test.go`

Remaining drift:

- `status.workloadReference` and `status.admittedClusterQueue` are defined in the API but are not yet populated by the controller.
- This is an observability gap in RTJ status, not a control-plane ownership violation.

### Coverage Audit

Status: signoff bar met for this phase

Required coverage present:

- unit coverage for webhook/defaulting logic:
  - `api/v1alpha1/resumabletrainingjob_webhook_test.go`
- unit coverage for workload-shape synthesis:
  - `internal/kueue/rtj_podsets_test.go`
  - `internal/kueue/setup_test.go`
- unit coverage for checkpoint selection in the suspend and resume path:
  - `internal/checkpoints/selector_test.go`
  - `internal/checkpoints/compatibility_test.go`
  - `internal/controller/resumabletrainingjob_controller_test.go`
- one strong e2e preemption and resume test:
  - `test/e2e/priority_preemption_resume_test.go`

Repeated-cycle note:

- A repeated multi-cycle live preemption and resume test is not part of the current signoff bar.
- The current live `kind` path is already multi-minute, environment-gated, and storage-dependent.
- Extending it into a longer soak now would add flake surface faster than it would improve Phase 2 confidence.
- The signed-off position is to defer repeated-cycle live soak coverage until a faster harness or a stronger runtime-progress signal exists.

### Demo, Operations, And Documentation

Status: aligned after this pass

- The main demo path is documented end to end.
- Operations docs point to RTJ status, Kueue `Workload` inspection, runtime-only child `JobSet` checks, checkpoint manifests, yield markers, and metrics.
- Troubleshooting covers webhook/defaulting issues, external-framework registration, accidental child double-management, and missing checkpoint evidence.

Evidence:

- `docs/phase2/demo.md`
- `docs/phase2/operations.md`
- `docs/phase2/troubleshooting.md`
- `docs/phase2/e2e.md`

## Docs Drift Found During Audit

These were documentation drifts, not control-plane drift:

- `docs/phase2/index.md` still read like a planning entry point.
- `docs/phase2/api-and-webhooks.md` still described missing runtime-side `GenericJob` behavior that is already implemented.
- `docs/phase2/kueue-external-integration.md` still described the runtime contract as scaffold-only.

Those items were corrected in this pass.

## Signoff Conclusion

Phase 2 is consistent enough to sign off as a correctness-first native Kueue integration slice for local development, demo, and hardening.
The remaining gaps are mainly RTJ status visibility, restore-depth recovery, and repeated-cycle soak depth.
Those gaps are tracked in [gaps.md](gaps.md).
