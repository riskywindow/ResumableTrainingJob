# Phase 4 Open Questions

## OQ-1: TopologyRequest API Surface in Kueue v0.15.1

**Question:** Does `kueuev1beta2.PodSet` in Kueue v0.15.1 expose
`TopologyRequest` with `Required` and `Preferred` fields? What is the exact
struct shape?

**Impact:** Determines whether the operator can synthesize topology-aware
PodSets without a Kueue version bump. If the field does not exist or has a
different shape, we must document the divergence and either pin a newer version
or stub the functionality.

**Resolution plan:** Inspect `PodSet` type in the pinned Kueue v0.15.1 Go
module cache. If the field exists, use it directly. If not, document the
divergence in `session-handoff.md` and the ADR, and define a compatibility
shim or defer TAS to a Kueue upgrade.

## OQ-2: TopologyAssignment Propagation Through PodSetAssignment

**Question:** When Kueue's TAS assigns topology domains, how does the
`TopologyAssignment` appear on the `PodSetAssignment`? Does it include
per-domain pod counts and topology level values? Can the operator read it
from `Workload.Status.Admission.PodSetAssignments[i].TopologyAssignment`?

**Impact:** Determines how the operator reads topology assignments and
materializes them into child JobSet scheduling constraints.

**Resolution plan:** Inspect the `PodSetAssignment` type in Kueue v0.15.1.
Verify the `TopologyAssignment` struct has `Levels []string` and
`Domains []TopologyDomainAssignment`. If the surface differs, document it.

## OQ-3: AdmissionCheck Controller Registration

**Question:** How does a custom AdmissionCheck controller register with Kueue?
What is the exact contract for setting admission check state on a Workload?

**Impact:** Determines the implementation pattern for the ResumeReadiness
controller. The controller must be able to:
1. Register with a `controllerName` that Kueue recognizes.
2. Watch Workloads that have the corresponding admission check.
3. Set the admission check state to `Ready` or `Retry`.

**Resolution plan:** Review Kueue's AdmissionCheck documentation and the
`SingleInstanceHandler` pattern used by built-in admission check controllers
(e.g., ProvisioningRequest). The operator creates an `AdmissionCheck` CR with
its controller name; Kueue routes Workloads with that check to the controller
by convention. The controller updates `Workload.Status.AdmissionChecks[]`
entries.

## OQ-4: Topology Domain Materialization Strategy

**Question:** What scheduling constraints should the operator inject into
child JobSet pod templates to honor topology assignments? Options include:

- **Option A:** Per-domain pod affinity with topology keys matching assigned
  levels. Pods in the same domain get mutual pod affinity.
- **Option B:** NodeSelector with topology domain labels (e.g.,
  `topology.kubernetes.io/zone=us-east-1a`). Simple but requires one
  replicatedJob per domain or pod-level scheduling directives.
- **Option C:** Kueue-managed scheduling gates that Kueue itself resolves
  after admission. The operator does not inject constraints; Kueue handles it.

**Impact:** Affects child JobSet structure and whether the operator needs to
split pods across multiple replicatedJobs or use pod-level scheduling.

**Resolution plan:** Investigate how Kueue's built-in JobSet integration
materializes TAS assignments. If Kueue uses scheduling gates + pod labels,
the operator should follow the same pattern for consistency. If Kueue relies
on the job controller to inject constraints, the operator must do so explicitly.

## OQ-5: ResumeReadiness and Preemption Re-Admission Ordering

**Question:** When a preempted RTJ is re-admitted, does the ResumeReadiness
check run again? Must the operator re-validate topology and checkpoint
compatibility on every re-admission?

**Impact:** Determines whether the ResumeReadiness controller is stateless
(re-validates on every admission cycle) or stateful (caches previous
validation results).

**Resolution plan:** The ResumeReadiness controller SHOULD be stateless and
re-validate on every admission cycle. This is simpler and safer: topology
may change on re-admission, and the latest compatible checkpoint may differ
after a preemption cycle. The cost of re-validation is low (one checkpoint
catalog scan and one topology check per admission).

## OQ-6: ProvisioningRequest Interaction with TAS

**Question:** When both ProvisioningRequest and TAS are active on a
ClusterQueue, what is the ordering? Does ProvisioningRequest provision nodes
first, then TAS assigns topology on the provisioned nodes?

**Impact:** Determines whether the operator needs to handle the case where
nodes are provisioned but topology is not yet assigned.

**Resolution plan:** Review Kueue documentation for admission check ordering.
If Kueue handles the ordering internally (ProvisioningRequest → TAS →
ResumeReadiness), the operator only needs to handle the final
ResumeReadiness gate. Document the expected ordering.

## OQ-7: Kind Cluster TAS Testing

**Question:** Can TAS be meaningfully tested in a `kind` cluster? Kind nodes
do not have real rack or zone topology. Can we simulate topology with custom
node labels?

**Impact:** Determines whether Phase 4 e2e tests can cover topology-aware
placement in the local dev environment.

**Resolution plan:** Label kind worker nodes with simulated topology keys
(e.g., `topology.kubernetes.io/zone=zone-a`, `zone-b`). Configure Kueue's
Topology CR to use these labels. This is the same pattern as Phase 3's
`label-kind-nodes.sh` for ResourceFlavor pools.
