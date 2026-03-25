# Phase 4 Goals

## Mission

Make RTJ participate in Kueue's admission pipeline by adding topology-aware
Workload synthesis, topology-aware child JobSet launch and resume, a custom
ResumeReadiness AdmissionCheck controller, and an optional cloud profile for
built-in ProvisioningRequest support, without making the cloud profile required
for local success.

## Goals

### G1: Topology-Aware Workload Synthesis

When an RTJ declares topology requirements (e.g., same-rack placement for
NCCL-efficient all-reduce), `PodSetsFromRTJTemplate` MUST include a
`TopologyRequest` on the relevant PodSets. This enables Kueue's
TopologyAwareScheduling (TAS) to assign pods to topology domains (racks,
zones, blocks) during the scheduling phase.

**Acceptance:** A Workload synthesized from an RTJ with
`spec.topology.required: "topology.kubernetes.io/zone"` carries
`TopologyRequest.Required` on the worker PodSet. Kueue's TAS assigns topology
domains in the Workload admission.

### G2: Topology-Aware Runtime Materialization

When Kueue admits an RTJ Workload with `TopologyAssignment` on its
`PodSetAssignments`, the RTJ controller MUST read the topology assignments
and inject the corresponding scheduling constraints into the child JobSet pod
templates. This includes per-domain nodeSelector values or pod affinity rules
that ensure pods land in their assigned topology domains.

The operator is responsible for:
- Waiting for topology assignment on the Workload.
- Reading the assigned topology domains and pod counts.
- Rendering the child JobSet with topology-aware scheduling constraints.

**Acceptance:** A child JobSet launched after topology-aware admission places
pods in the assigned topology domains, verified by node labels matching the
Workload's `TopologyAssignment.Domains`.

### G3: Custom ResumeReadiness AdmissionCheck

A custom AdmissionCheck controller
(`checkpoint-native.example.io/resume-readiness`) gates Workload admission
until the operator confirms readiness. The admission check prevents Kueue from
unsuspending the RTJ before the operator has:

1. Observed the topology assignment on the Workload.
2. Validated that the assigned topology is renderable into a child JobSet.
3. Confirmed that checkpoint selection (on resume) is compatible with the
   assigned shape and topology.

The custom AdmissionCheck gates readiness before launch, but the operator is
responsible for waiting for topology assignment and then rendering the child
JobSet. Kueue does not launch the workload; it only clears the admission gate.
The operator owns the launch decision.

**Acceptance:** A Workload with the ResumeReadiness admission check remains
in `QuotaReserved` state until the operator marks the check as `Ready`. Once
ready, Kueue completes admission (unsuspends RTJ), and the operator creates
the topology-aware child JobSet.

### G4: Admission-Gated Launch and Resume

Both initial launch and resume-after-preemption MUST be gated by the full
admission pipeline (quota reservation, topology assignment, admission check
clearance). On resume, the operator additionally validates checkpoint
compatibility with the new admitted shape and topology before clearing the
ResumeReadiness gate.

**Acceptance:** An RTJ preempted and re-admitted with a different topology
assignment resumes from the latest compatible checkpoint and launches the
child JobSet in the newly assigned topology domains.

### G5: Optional ProvisioningRequest Cloud Profile

When the ClusterQueue includes a ProvisioningRequest admission check, Kueue
creates a `ProvisioningRequest` to auto-provision nodes before topology
assignment. The ProvisioningRequest path is optional and uses Kueue's built-in
ProvisioningRequest controller.

The RTJ operator does NOT implement ProvisioningRequest logic. It only:
- Synthesizes PodSets that are compatible with ProvisioningRequest admission.
- Respects the admission pipeline ordering (provision → topology → readiness).

ProvisioningRequest is optional in Phase 4 and is NOT required for local
success. Phase 4 is complete without it.

**Acceptance:** When a ProvisioningRequest admission check is configured on the
ClusterQueue, the Workload admission pipeline includes node provisioning before
topology assignment and ResumeReadiness gating. Documented with configuration
examples; live testing is optional.

## Non-Goals (Explicitly Out of Scope)

| Non-Goal | Reason |
| --- | --- |
| MultiKueue or multi-cluster admission | Phase 0 contract: single cluster only. |
| Elastic Workloads or in-place scaling | Deferred; Phase 3 handles resume-time shape changes only. |
| Custom scheduling algorithms | Kueue owns scheduling; the operator does not implement custom preemption or priority logic. |
| Transparent CUDA or container snapshots | Phase 0 contract: DCP checkpointing only. |
| ProvisioningRequest as mandatory | Optional cloud profile; local success without it. |
| Topology enforcement beyond Kueue TAS | The operator materializes Kueue's topology decisions; it does not implement its own topology solver. |

## Success Criteria

### Must-Ship

1. RTJ with topology requirements produces a Workload with `TopologyRequest`
   on PodSets.
2. Topology assignments from Kueue admission flow into child JobSet scheduling
   constraints (nodeSelector or affinity for topology domains).
3. The ResumeReadiness AdmissionCheck gates admission until the operator
   confirms readiness.
4. Resume after preemption goes through the full admission pipeline (topology
   re-assignment, readiness gate, topology-aware child JobSet).
5. Phase 3 behavior is preserved when topology and readiness-gate features are
   disabled.

### Should-Ship

6. ProvisioningRequest integration is documented with configuration examples.
7. Status surfaces the assigned topology domains and admission check state.
8. Metrics track topology-aware launches and admission check latency.

### Exit Criteria

- All must-ship acceptance criteria pass in the local `kind` dev environment.
- Unit tests cover: topology-aware PodSet synthesis, topology materialization
  into child JobSet, ResumeReadiness state machine, admission-gated
  launch/resume.
- E2E test covers: topology-aware launch with TAS in kind.
- Documentation updated: architecture, migration, ADR, session handoff.
- ProvisioningRequest path documented; live testing deferred if cloud infra
  is unavailable.
