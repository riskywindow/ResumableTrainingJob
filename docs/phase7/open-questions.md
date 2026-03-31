# Phase 7 -- Open Questions

## OQ1: Kueue v0.15.1 ProvisioningRequest AC maturity

**Question**: Does Kueue v0.15.1 (our pinned version) fully support the
ProvisioningRequest AdmissionCheck flow as described in upstream docs?
The upstream docs may describe features from a newer version.

**Impact**: If the pinned version lacks features we need, we must either
work around them or document the divergence.

**Action**: Audit the Kueue v0.15.1 source for:
- `AdmissionCheck` CRD with `controllerName: kueue.x-k8s.io/provisioning-request`
- `ProvisioningRequestConfig` CRD support
- Automatic ProvisioningRequest creation from Workload
- AC state propagation to Workload status
- Topology assignment after AC satisfaction (two-pass)

**Resolution**: TBD -- must be resolved in Session 1 implementation.
Document any divergences in session-handoff.md and an ADR if needed.

---

## OQ2: waitForPodsReady eviction condition format

**Question**: What is the exact condition type and reason Kueue v0.15.1
sets on the Workload when it evicts due to waitForPodsReady timeout?

**Impact**: The RTJ operator must match on this condition to distinguish
timeout eviction from preemption eviction.

**Action**: Read Kueue v0.15.1 source for the eviction condition format.
Verify in e2e that the condition matches expectations.

**Resolution**: TBD -- must be resolved in Session 1 implementation.

---

## OQ3: Topology assignment timing with ProvisioningRequest

**Question**: In Kueue v0.15.1, when a ProvisioningRequest AC is
configured alongside topology, does topology assignment happen:
(a) in the same pass as AC satisfaction, or
(b) in a separate reconciliation after AC satisfaction?

**Impact**: Determines whether the launch gate needs an explicit topology
wait or if topology is always present when all ACs are Ready.

**Action**: Test with fake backend + topology in e2e.

**Resolution**: TBD -- the architecture assumes (b) as the conservative
case. If (a) is the actual behavior, the topology wait is a no-op.

---

## OQ4: ProvisioningRequest cleanup on yield/preemption

**Question**: When Kueue preempts a workload that has an active
ProvisioningRequest, does Kueue automatically clean up the
ProvisioningRequest? Or does the RTJ operator need to handle this?

**Impact**: If Kueue does not clean up, stale ProvisioningRequests could
block future admissions or waste cloud resources.

**Action**: Read Kueue source and test in e2e.

**Resolution**: TBD.

---

## OQ5: Fake backend scope -- in-process vs separate Deployment

**Question**: Should the fake ProvisioningRequest backend run as:
(a) a separate Deployment (like a real backend), or
(b) an in-process mode toggle in the RTJ operator, or
(c) a standalone binary that can be either?

**Impact**: Affects deployment complexity and test isolation.

**Recommendation**: (a) separate Deployment. This most closely matches
the real-world topology and avoids coupling the fake into the RTJ
operator binary. The fake is small enough to be a single Go binary.

**Resolution**: TBD -- decide in Session 1.

---

## OQ6: Multi-cluster provisioning status mirroring

**Question**: Should the manager cluster's RTJ status include
provisioning gate state from the worker cluster? Or is it sufficient
to show only the worker's phase (which implicitly reflects provisioning)?

**Impact**: Operator UX. If the manager shows "Admitted" while the worker
is waiting for provisioning, the operator has no visibility into why
launch hasn't happened.

**Recommendation**: Extend the Phase 6 status mirroring to include
provisioning state from the worker. This is a status-only change to the
mirror path.

**Resolution**: TBD -- should-ship, not must-ship.

---

## OQ7: Backoff behavior after repeated startup timeouts

**Question**: If a workload repeatedly fails startup timeout (e.g.,
broken image), Kueue's backoff will increase delays between requeues.
Should the RTJ operator surface the backoff state and/or provide a
way to break the cycle?

**Impact**: Operator UX for debugging persistent failures.

**Recommendation**: Surface backoff count and next-requeue time in RTJ
status. Provide a mechanism to reset backoff (e.g., touch a spec field
or annotation). Defer implementation to should-ship.

**Resolution**: TBD -- should-ship.

---

## OQ8: Feature gate naming

**Question**: What should the feature gate be named for Phase 7?

**Options**:
- `CapacityGuaranteedLaunch`
- `ProvisioningAwareLaunch`
- `ProvisioningRequestIntegration`

**Impact**: API surface. Feature gate names are semi-public.

**Recommendation**: `ProvisioningAwareLaunch` -- descriptive, not tied
to a specific Kueue implementation detail.

**Resolution**: TBD -- decide in Session 1.
