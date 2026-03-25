# Session Handoff

- Date: 2026-03-24
- Scope: Phase 4 design lock for `checkpoint-native preemption controller`

## Session 1: Design Lock

### Decisions Made

1. **Phase 4 scope is locked as five goals:**
   - G1: Topology-aware Workload synthesis (TopologyRequest on PodSets).
   - G2: Topology-aware runtime materialization (child JobSet scheduling constraints from TopologyAssignment).
   - G3: Custom ResumeReadiness AdmissionCheck controller (gates admission until operator confirms readiness).
   - G4: Admission-gated launch and resume (full pipeline: topology → readiness → admit → launch).
   - G5: Optional ProvisioningRequest cloud profile (not required for local success).

2. **RTJ remains the only Kueue-managed admission object.** Child JobSets
   remain plain runtime resources with no Kueue management metadata. This
   is unchanged from Phase 2/3.

3. **The child JobSet remains plain runtime only.** No Kueue queue labels,
   priority classes, or admission check annotations on the child JobSet.
   Unchanged from Phase 2/3.

4. **The custom AdmissionCheck gates readiness before launch.** The operator
   is responsible for waiting for topology assignment and then rendering the
   child JobSet. Kueue does not launch the workload; it only clears the
   admission gate. The operator owns the launch decision.

5. **ProvisioningRequest is optional in Phase 4 and not required for local
   success.** The operator does not implement ProvisioningRequest logic.
   It only ensures PodSet synthesis is compatible with Kueue's built-in
   ProvisioningRequest admission check.

6. **Phase 3 behavior is preserved when topology and readiness-gate features
   are disabled.** When `spec.topology` is nil and no ResumeReadiness
   admission check is on the ClusterQueue, behavior is identical to Phase 3.

7. **Elastic Workloads are still deferred.** Phase 4 handles topology-aware
   placement, not live in-place scaling. Same reasoning as Phase 3.

8. **Pinned versions unchanged:** Kueue v0.15.1, JobSet v0.10.1,
   controller-runtime v0.22.4.

9. **ResumeReadiness controller is stateless.** Re-validates topology and
   checkpoint compatibility on every admission cycle. No caching across
   preemption boundaries.

### Files Created (Session 1)

- `docs/phase4/README.md` — overview and quick context
- `docs/phase4/index.md` — document index and navigation
- `docs/phase4/goals.md` — goals, non-goals, success criteria, exit criteria
- `docs/phase4/architecture.md` — component diagram, three sequence diagrams (topology-aware launch, topology-aware resume, optional ProvisioningRequest), detailed design
- `docs/phase4/migration-from-phase3.md` — what stays, what changes in launch gating, what changes in topology handling, why Elastic Workloads are still deferred
- `docs/phase4/open-questions.md` — seven open questions with resolution plans
- `docs/phase4/session-handoff.md` — this file
- `docs/phase4/adr/0001-phase4-admission-pipeline.md` — Phase 4 admission pipeline contract (9 decisions, alternatives considered, verification plan)

### Tests Run

No runtime code was implemented. Design-only session.

- All existing Go tests pass (`go test ./... -count=1`): verified build is not broken.
- `go build ./...` succeeds: no compilation errors from docs-only changes.

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | TopologyRequest API surface in Kueue v0.15.1 | Blocks G1 implementation | Open — must inspect Go module cache |
| OQ-2 | TopologyAssignment propagation through PodSetAssignment | Blocks G2 implementation | Open — must inspect Go module cache |
| OQ-3 | AdmissionCheck controller registration pattern | Blocks G3 implementation | Open — review Kueue docs/source |
| OQ-4 | Topology domain materialization strategy | Affects child JobSet structure | Open — inspect Kueue built-in integrations |
| OQ-5 | ResumeReadiness and preemption re-admission ordering | Affects controller design | Tentatively resolved: stateless re-validation |
| OQ-6 | ProvisioningRequest interaction with TAS | Affects optional cloud path | Open — review Kueue docs |
| OQ-7 | Kind cluster TAS testing | Affects e2e test strategy | Tentatively resolved: simulated topology labels |

### Divergence Notes

If any of the above open questions reveal that Kueue v0.15.1 does not
support the assumed API surface (TopologyRequest, TopologyAssignment,
AdmissionCheck controller pattern), the divergence MUST be documented here
and in the ADR. The design will be adjusted to match the pinned version's
actual surface.

---

## Recommended Next Prompt (Phase 4 Implementation)

The Phase 4 design is locked. The next session should:

1. **Resolve OQ-1 and OQ-2.** Inspect `kueuev1beta2.PodSet` and
   `kueuev1beta2.PodSetAssignment` in the Kueue v0.15.1 Go module cache
   to verify `TopologyRequest` and `TopologyAssignment` exist and match
   the assumed shape. Document any divergence.

2. **Resolve OQ-3.** Review Kueue's AdmissionCheck controller registration
   pattern (inspect `ProvisioningRequest` controller as reference). Determine
   how to set admission check state on Workloads.

3. **Resolve OQ-4.** Inspect how Kueue's built-in JobSet integration
   materializes TAS topology assignments into pods. Determine whether the
   operator should use scheduling gates, nodeSelector injection, or affinity.

4. **Implement topology-aware PodSet synthesis (G1).** Add `TopologySpec`
   to RTJ API types. Update `PodSetsFromRTJTemplate` to include
   `TopologyRequest`. Add unit tests.

5. **Implement ResumeReadiness AdmissionCheck controller (G3).** Add the
   controller to the operator manager. Watch Workloads with the
   `resume-readiness` admission check. Implement validation and state
   transitions. Add unit tests.

6. **Implement topology-aware materialization (G2).** Update the child
   JobSet renderer to inject topology constraints from `TopologyAssignment`.
   Add unit tests.

7. **Wire admission-gated launch and resume (G4).** Ensure the full
   pipeline (topology → readiness → admit → launch) works for both initial
   launch and resume-after-preemption.

8. **Set up Phase 4 dev environment.** Create kind cluster config with
   simulated topology labels. Create Topology CR, AdmissionCheck CR,
   ClusterQueue with admission checks.

9. **Add Phase 4 e2e tests.** Topology-aware launch, admission-gated
   resume, Phase 3 backward compatibility.

10. **Document ProvisioningRequest (G5).** Configuration examples for the
    optional cloud path. Live testing deferred if cloud infra unavailable.

Suggested prompt:

> You are working on Phase 4 of the checkpoint-native preemption controller.
> Read docs/phase4/session-handoff.md for context. Resolve OQ-1, OQ-2, OQ-3,
> and OQ-4 by inspecting the Kueue v0.15.1 Go module types. Document
> findings in session-handoff.md. Then implement G1 (topology-aware PodSet
> synthesis) with API types, PodSet synthesis, validation, and unit tests.
