# Phase 4: Topology-Aware Admission Pipeline

Phase 4 makes RTJ participate fully in Kueue's admission pipeline by adding
topology-aware Workload synthesis, topology-aware child JobSet launch and
resume, a custom ResumeReadiness AdmissionCheck controller, and an optional
cloud profile for built-in ProvisioningRequest support.

## What Phase 4 Delivers

1. **Topology-aware Workload synthesis.** PodSets emitted by `PodSetsFromRTJTemplate`
   include `TopologyRequest` when the RTJ spec declares topology requirements,
   enabling Kueue's TopologyAwareScheduling (TAS) to make rack- or zone-aware
   placement decisions.

2. **Topology-aware runtime materialization.** When Kueue admits an RTJ with
   topology assignments, the controller reads `TopologyAssignment` from the
   Workload admission and injects the corresponding scheduling constraints
   (nodeSelector, affinity, topology spread) into the child JobSet pod templates.

3. **Custom ResumeReadiness AdmissionCheck.** A new admission check controller
   (`checkpoint-native.example.io/resume-readiness`) gates Workload admission
   until the operator confirms it can render the child JobSet with the assigned
   topology. The operator waits for topology assignment, validates the placement,
   then marks the check as ready. Kueue completes admission only after this
   gate clears.

4. **Optional ProvisioningRequest cloud profile.** When the ClusterQueue is
   configured with a ProvisioningRequest admission check, Kueue auto-provisions
   nodes before topology assignment. This path is optional and not required for
   local Phase 4 success.

## What Does Not Change

- RTJ remains the **only** Kueue-managed admission object.
- The child JobSet remains a **plain runtime resource** with no Kueue metadata.
- All Phase 0 through Phase 3 invariants are preserved.
- When topology and readiness-gate features are disabled (no `TopologyRequest`
  on PodSets, no ResumeReadiness AdmissionCheck on the ClusterQueue), Phase 3
  behavior is preserved exactly.
- Pinned versions: Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4.

## What Remains Out of Scope

- MultiKueue or multi-cluster admission.
- Elastic Workloads or in-place scaling.
- Custom scheduling algorithms.
- Transparent CUDA or container snapshots.
- ProvisioningRequest is optional; local Phase 4 success does not require it.

## Quick Navigation

See [index.md](index.md) for the full document map.
