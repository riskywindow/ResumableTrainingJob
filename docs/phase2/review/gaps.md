# Phase 2 Gaps

This page records the remaining known gaps after the Phase 2 hardening and signoff pass.
These are the main places where the implementation is intentionally narrower than the full accepted contract or where operator visibility is still thin.

## 1. RTJ Status Still Does Not Project Full Kueue Admission Details

Current state:

- RTJ is the only Kueue-managed admission object.
- Operators can inspect the RTJ-owned `Workload` directly.
- `status.currentSuspension` is populated on RTJ.

Gap:

- `status.workloadReference` and `status.admittedClusterQueue` are defined in the API but are not yet projected by the controller.

Impact:

- Operators still need a second inspection step against Kueue `Workload` objects to see the full admission view.

Recommended follow-up:

- Project workload identity and admitted cluster queue into RTJ status.

## 2. Live Webhook Path Is Not The Default Demo Or E2E Path

Current state:

- The webhook has unit coverage for defaulting and validation semantics.
- The live demo and e2e manifests set `spec.suspend` and Kueue labels explicitly.

Gap:

- The default live path still runs the operator locally rather than through an in-cluster deployment with active webhook serving.

Impact:

- The correctness of webhook behavior is unit-tested, but the full live admission path through webhook serving is not the default demonstrated or e2e-proven flow.

Recommended follow-up:

- Add an in-cluster operator plus webhook path for live testing or CI once the repo wants a less manual local flow.

## 3. Restore Fallback Remains Single-Selection

Current state:

- Resume selects the newest compatible complete checkpoint.
- If no compatible checkpoint exists, the RTJ fails closed.

Gap:

- The repo still does not walk to the next newest compatible checkpoint after a restore-start failure on the selected checkpoint.

Impact:

- A restore-start failure on the selected checkpoint remains terminal for that attempt even if an older compatible checkpoint exists.

Recommended follow-up:

- Add bounded next-candidate fallback with explicit skipped-candidate reasons.

## 4. Repeated Preemption And Resume Cycles Are Deferred

Current state:

- The repo has one strong deterministic live preemption and resume test.
- That test proves checkpointed yield, high-priority run, low-priority resume, and post-resume forward progress.

Gap:

- There is no repeated multi-cycle live preemption and resume soak.

Why this is deferred:

- The current `kind` path already depends on a live cluster, a locally loaded trainer image, MinIO, and multi-minute timing.
- Extending it into a longer repeated-cycle soak now would materially increase runtime and flake surface.
- The current Phase 2 signoff bar is correctness of one strong deterministic cycle, not benchmark-grade repeated-cycle confidence.

Recommended follow-up:

- Add a small repeated-cycle soak after the live harness is faster or after stronger runtime-progress signaling exists.

## 5. Metrics Remain Process-Local

Current state:

- The operator exports useful Phase 2 metrics for RTJ phase, RTJ-owned workloads, admission, Kueue suspension, yield completion, resume, and duplicate-child prevention.

Gap:

- Those metrics remain tied to the current operator process lifetime.

Impact:

- They are useful for local inspection and demo, not durable historical monitoring.

Recommended follow-up:

- Keep the current metrics for Phase 2 and defer durable monitoring or dashboarding to a later phase.
