# Phase 2 Signoff

Phase 2 is signed off as a correctness-first native Kueue integration slice.
This signoff is for local development, demo, and hardening of the accepted Phase 0 control path.
It is not a production-readiness claim.

## What Phase 2 Can Do

- Reconcile `ResumableTrainingJob` as a native Kueue-managed custom job through the current external `jobframework` integration path.
- Create an RTJ-owned Kueue `Workload` and keep RTJ as the only Kueue-managed admission object.
- Keep the child `JobSet` as a plain runtime carrier with Kueue management metadata stripped.
- Defer child `JobSet` creation until RTJ is admitted and unsuspended by Kueue.
- Preserve the existing manual hold surface through `spec.control.desiredState` without bypassing Kueue admission.
- React to Kueue-driven suspend and preemption intent through `spec.suspend=true`.
- Request graceful yield through the runtime control `ConfigMap`.
- Wait for yield-marker and completed-checkpoint evidence newer than the accepted stop request.
- Tear down the active child `JobSet` after graceful yield completes or fail closed on drain timeout.
- Select the latest compatible complete checkpoint and launch a fresh restoring run attempt from that checkpoint.
- Prove one strong deterministic live preemption and resume cycle with forward-progress validation after resume.
- Expose lightweight metrics, demo commands, inspection commands, operations docs, and troubleshooting docs for the Phase 2 path.

## What Phase 2 Cannot Do

- It cannot implement MultiKueue, topology-aware scheduling, elastic workloads, world-size changes, or transparent CUDA or container snapshots.
- It cannot add a custom scheduler or a custom preemption algorithm.
- It cannot resume from incomplete or incompatible checkpoints.
- It cannot yet fall back to an older compatible checkpoint after a restore-start failure on the newest selected checkpoint.
- It cannot yet project `status.workloadReference` and `status.admittedClusterQueue` on RTJ, so operators still inspect Kueue `Workload` objects directly.
- It cannot yet prove repeated multi-cycle live preemption and resume behavior through a soak test.
- It cannot claim durable monitoring, production deployment ergonomics, or a UI.

## Main Known Risks

- RTJ status still exposes less Kueue admission detail than the API shape allows.
- The default live path still relies on a local operator process and explicit manifest fields instead of an in-cluster webhook deployment.
- Metrics are process-local and reset on operator restart.
- Restore recovery depth remains single-selection rather than bounded multi-candidate fallback.
- The live signoff bar proves one strong deterministic preemption and resume path, not the repeated-cycle benchmark target from Phase 0.

## Test And Evidence Summary

Unit and helper coverage:

- `api/v1alpha1/resumabletrainingjob_webhook_test.go`
- `internal/kueue/rtj_podsets_test.go`
- `internal/kueue/setup_test.go`
- `internal/jobset/render_test.go`
- `internal/checkpoints/compatibility_test.go`
- `internal/checkpoints/selector_test.go`

Controller coverage:

- `internal/controller/resumabletrainingjob_controller_test.go`

End-to-end live coverage:

- `test/e2e/native_kueue_admission_test.go`
- `test/e2e/priority_preemption_resume_test.go`

Execution note:

- The Go test suite compiles the e2e package, but the live `kind` e2e paths remain environment-gated behind `RUN_KIND_E2E=1` and a loaded trainer image through `PHASE2_TRAINER_IMAGE` or `PAUSE_FLOW_TRAINER_IMAGE`.
- This signoff pass did not rerun the live `kind` e2e flows in this prompt environment.

Operational documentation:

- `docs/phase2/demo.md`
- `docs/phase2/operations.md`
- `docs/phase2/troubleshooting.md`
- `docs/phase2/review/consistency-audit.md`
- `docs/phase2/review/gaps.md`

## What Phase 3 Should Build Next

1. Project Kueue workload identity and admitted cluster queue into RTJ status so RTJ becomes the single operator-facing inspection object.
2. Add a less manual in-cluster operator and webhook deployment path for live testing and demo.
3. Add bounded next-candidate resume fallback with clearer skipped-checkpoint diagnostics.
4. Add a faster or less flaky repeated-cycle live soak path for Kueue-driven preemption and resume.
5. Tighten runtime truthfulness further if Phase 3 needs stronger progress or restore-complete evidence than child-`JobSet` existence alone.
