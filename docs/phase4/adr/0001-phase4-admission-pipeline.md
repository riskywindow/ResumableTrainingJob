# ADR 0001: Phase 4 Admission Pipeline

- **Status:** Accepted
- **Date:** 2026-03-24
- **Context:** Phase 4 design lock for the checkpoint-native preemption controller.

## Decision

Phase 4 extends the RTJ Kueue integration with four capabilities:

1. **Topology-aware Workload synthesis.**
2. **Topology-aware runtime materialization.**
3. **Custom ResumeReadiness AdmissionCheck controller.**
4. **Optional ProvisioningRequest cloud profile.**

These capabilities compose with Phase 3's flavor-aware rendering and
partial admission without replacing them.

## Context

Phase 3 delivered admission-aware launch (flavor nodeSelector, tolerations,
admitted replica counts) and world-size-flexible resume. However, Phase 3
explicitly deferred topology-aware scheduling:

> "Topology-aware runtime enforcement | Kueue may consider topology for
> admission, but the RTJ controller does not enforce topology constraints
> beyond nodeSelector/tolerations from flavors."
> — Phase 3 goals.md, Non-Goals

For distributed training workloads, topology matters: co-locating workers
within the same rack or zone reduces NCCL all-reduce latency by 2-10x. Kueue
provides TopologyAwareScheduling (TAS) to solve this at the admission level,
but the job controller must participate by:

- Declaring topology requirements in PodSets.
- Materializing topology assignments into pod scheduling constraints.
- Gating launch until topology is assigned.

Additionally, the admission pipeline needs a readiness gate so the operator
can validate topology and checkpoint compatibility before Kueue unsuspends
the RTJ. Without this gate, Kueue may admit before the operator is ready.

## Decisions

### D1: RTJ Remains the Only Kueue-Managed Admission Object

**Decision:** RTJ is the only object that Kueue manages for admission.
The child JobSet remains a plain runtime resource with no Kueue metadata.

**Rationale:** This preserves the Phase 2 authority model. Making the child
JobSet a second Kueue-managed object would create dual-admission complexity
and break the single-admission-object invariant.

### D2: Child JobSet Remains Plain Runtime Only

**Decision:** The child JobSet carries no Kueue queue labels, priority
classes, or admission check annotations. It is created by the RTJ controller
after admission completes and is garbage-collected when the RTJ is deleted.

**Rationale:** Unchanged from Phase 2. The child JobSet is runtime
materialization, not an admission object.

### D3: TopologyRequest on PodSets from RTJ spec.topology

**Decision:** When `spec.topology` is set on the RTJ, `PodSetsFromRTJTemplate`
includes `TopologyRequest` on the worker PodSets (and optionally on all
PodSets). The `Required` and `Preferred` fields map directly to the RTJ's
topology spec.

**Alternative considered:** Auto-detect topology from node labels or
ResourceFlavor topology. Rejected because explicit declaration is simpler,
more predictable, and consistent with Kueue's model where the workload
author declares requirements.

### D4: Custom ResumeReadiness AdmissionCheck

**Decision:** A custom admission check controller gates Workload admission
until the operator confirms readiness. The check uses `controllerName:
checkpoint-native.example.io/resume-readiness`.

The custom AdmissionCheck gates readiness before launch. The operator is
responsible for:
- Waiting for topology assignment on the Workload.
- Validating that the topology is renderable into a child JobSet.
- Validating checkpoint compatibility on resume.
- Marking the check as `Ready`.

Kueue does not launch the workload. The operator owns the launch decision
(child JobSet creation) after admission completes.

**Alternative considered:** Use Kueue's built-in scheduling gates without
a custom admission check. Rejected because scheduling gates do not provide
the operator-controlled validation step needed for checkpoint compatibility
on resume.

**Alternative considered:** Have the RTJ controller check topology after
admission (after `suspend=false`). Rejected because this creates a window
where the RTJ is admitted but the controller has not yet validated topology.
The admission check gate eliminates this window.

### D5: ProvisioningRequest Is Optional

**Decision:** ProvisioningRequest support is optional in Phase 4 and is NOT
required for local success. The operator does not implement ProvisioningRequest
logic. It only ensures PodSet synthesis is compatible with the built-in
ProvisioningRequest admission check.

**Rationale:** ProvisioningRequest requires cloud provider infrastructure
that is not available in the local `kind` dev environment. Phase 4 is
complete without it. The operator's PodSets are already compatible with
ProvisioningRequest because they follow standard Kueue PodSet conventions.

### D6: Phase 3 Behavior Preserved When Features Disabled

**Decision:** When `spec.topology` is nil and no ResumeReadiness admission
check is configured on the ClusterQueue, Phase 3 behavior is preserved
exactly. No regression, no behavioral change.

**Rationale:** Phase 4 features are opt-in. Existing RTJs must not break.

### D7: ResumeReadiness Controller Is Stateless

**Decision:** The ResumeReadiness controller re-validates topology and
checkpoint compatibility on every admission cycle. It does not cache
previous validation results across preemption boundaries.

**Rationale:** Topology may change on re-admission (different zone, different
rack). Checkpoints may change after preemption (new checkpoint written during
drain). Re-validation is cheap (one catalog scan, one topology check) and
eliminates cache-invalidation bugs.

### D8: Topology Materialization Via Kueue-Consistent Mechanism

**Decision:** The operator materializes topology assignments using the same
mechanism that Kueue's built-in job integrations use. If Kueue uses
scheduling gates and pod labels (the `kueue.x-k8s.io/topology` label +
scheduling gate pattern), the operator follows that pattern. If Kueue
expects the job controller to inject nodeSelector/affinity, the operator
does that.

**Rationale:** Consistency with Kueue's expectations. The exact mechanism
is an open question (OQ-4) that must be resolved by inspecting Kueue v0.15.1
source.

### D9: Pinned Versions Unchanged

**Decision:** Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4.
No version bumps in Phase 4.

**Risk:** If Kueue v0.15.1 does not expose `TopologyRequest` on `PodSet`
or `TopologyAssignment` on `PodSetAssignment`, the topology features cannot
be implemented as designed. In this case, document the divergence in
`session-handoff.md` and either:
- Defer topology to a Kueue upgrade in a future phase.
- Implement a compatibility shim.

## Consequences

### Positive

- Training pods are co-located within topology domains, reducing NCCL latency.
- The admission check gate prevents premature launch before topology and
  checkpoint validation.
- The ProvisioningRequest path enables cloud-native node auto-provisioning
  without operator changes.
- Phase 3 behavior is fully preserved for existing RTJs.

### Negative

- The ResumeReadiness controller adds a new reconciliation loop and a new
  source of admission latency.
- Topology materialization depends on Kueue v0.15.1's TAS surface, which
  must be verified (OQ-1, OQ-2).
- The admission pipeline is more complex (queue → schedule → topology →
  admission checks → admit vs. Phase 3's queue → schedule → admit).

### Neutral

- The child JobSet remains plain runtime. No change to the JobSet controller
  or JobSet CRD.
- Checkpoint compatibility checking is unchanged. Topology is a scheduling
  concern, not a compatibility dimension.

## Verification

| Decision | Verification |
| --- | --- |
| D1 | Unit test: child JobSet has no `kueue.x-k8s.io/*` labels. |
| D2 | Unit test: child JobSet has no Kueue admission annotations. |
| D3 | Unit test: PodSet synthesis includes TopologyRequest when spec.topology is set. |
| D4 | Unit test: ResumeReadiness controller sets check state. E2E: Workload stays QuotaReserved until check is Ready. |
| D5 | Documented configuration examples. No blocking test. |
| D6 | Unit test: Phase 3 RTJ without topology produces identical Workload. E2E: existing Phase 3 tests pass unchanged. |
| D7 | Unit test: ResumeReadiness re-validates on re-admission. |
| D8 | Unit test: topology constraints in child JobSet match Workload assignment. |
| D9 | `go.mod` inspection: pinned versions unchanged. |
