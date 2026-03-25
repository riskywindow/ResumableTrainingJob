# Phase 1 Gaps

This page records the remaining known gaps after the Phase 1 hardening and signoff pass.
These are the main places where the implementation is intentionally narrower than the full Phase 0 contract or where observability is still thin.

## 1. Runtime Evidence Is Still Indirect

Current state:

- The controller publishes `Running` when the active child `JobSet` exists.
- The resume path publishes `Running` after the restored child exists on the next reconcile.

Gap:

- The conceptual Phase 0 lifecycle expects runtime heartbeat and restore-complete evidence.
- Phase 1 does not yet require that evidence before publishing `Running`.

Impact:

- `Running` means "control-plane carrier exists" more than "training loop is definitely making forward progress."

Recommended follow-up:

- Add a small runtime heartbeat or restore-complete status signal before tightening the `Running` semantics.

## 2. `Queued` And `Admitted` Are Not Surfaced

Current state:

- The API exposes `Queued` and `Admitted`.
- The controller currently uses `Pending`, `Starting`, `Running`, `YieldRequested`, `Draining`, `Paused`, `Restoring`, and `Failed`.

Gap:

- The Phase 0 status and lifecycle contracts define queue and admission visibility that the current Phase 1 controller does not publish.

Impact:

- Status is usable for the local demo, but it does not yet explain Kueue progress with the full conceptual lifecycle vocabulary.

Recommended follow-up:

- Surface Kueue admission state explicitly before Phase 2 broadens operator behavior.

## 3. Resume Fallback Is Still Single-Selection

Current state:

- Selection scans the object-store catalog and chooses the newest compatible complete checkpoint.
- If no compatible checkpoint exists, the RTJ fails closed.
- The trainer also rejects an obviously incompatible manifest at restore time.

Gap:

- The full Phase 0 selection contract allows trying the next newest compatible checkpoint if restore-start validation fails for the newest selected checkpoint.
- Phase 1 does not yet implement that fallback chain.

Impact:

- A restore-start failure on the selected checkpoint is terminal for the attempt even if an older compatible checkpoint exists.

Recommended follow-up:

- Add bounded next-candidate fallback and clearer failure reasons when Phase 2 expands recovery behavior.

## 4. Skipped Checkpoint Reasons Are Not Exposed In Status

Current state:

- The catalog filters incomplete, unreadable, or incompatible manifests.
- The controller only records the selected checkpoint and terminal resume failure state.

Gap:

- There is no RTJ status field or event stream that tells an operator why a specific checkpoint candidate was skipped.

Impact:

- Resume debugging still requires reading operator logs and object-store contents directly.

Recommended follow-up:

- Emit Kubernetes events or bounded status diagnostics for skipped manifest reasons.

## 5. Repeated Pause And Resume Is Documented, Not Proven Live

Current state:

- The repo has one e2e pause-flow smoke test and one e2e resume-flow smoke test.
- The resume smoke verifies continued progress after a resume by pausing again and checking that the later checkpoint step advanced.

Gap:

- There is no dedicated repeated multi-cycle live test that exercises several pause and resume rounds in one run.

Why this is deferred:

- The current kind smoke path is already multi-minute, depends on a locally built trainer image and live MinIO access, and is intentionally kept small for Phase 1 correctness-first signoff.
- Adding a longer repeated-cycle live test now would increase runtime and flake surface more than it would improve the Phase 1 confidence bar.

Recommended follow-up:

- Add a small repeated-cycle soak test after the runtime heartbeat and restore evidence are stronger, or after a faster integration harness exists.

## 6. Metrics Are Local-Process Metrics

Current state:

- The operator exports the Phase 1 counters and gauges from the controller-runtime metrics endpoint.

Gap:

- Metrics are process-local and restart-local in the current local-operator workflow.

Impact:

- The counters and gauges are useful for demo and smoke inspection, not for durable historical monitoring.

Recommended follow-up:

- Keep the current metrics for Phase 1 and defer any durable monitoring stack until a later phase needs it.
