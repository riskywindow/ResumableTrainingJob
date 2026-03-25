# Phase 4 Document Index

## Phase 4 Scope

Phase 4: Topology-Aware Admission Pipeline.

Build on Phase 3's admission-aware launch and flavor-aware resume to:

1. Synthesize topology-aware Workloads with `TopologyRequest` on PodSets.
2. Materialize Kueue topology assignments into child JobSet scheduling constraints.
3. Gate admission with a custom ResumeReadiness AdmissionCheck controller.
4. Optionally support ProvisioningRequest for node auto-provisioning.

## Documents

### Design

- [README.md](README.md) - overview and quick context
- [goals.md](goals.md) - goals, non-goals, success criteria, and exit criteria
- [architecture.md](architecture.md) - component diagram, four sequence diagrams, detailed design
- [adr/0001-phase4-admission-pipeline.md](adr/0001-phase4-admission-pipeline.md) - Phase 4 admission pipeline contract and design decisions

### Migration

- [migration-from-phase3.md](migration-from-phase3.md) - what stays, what changes, backward compatibility

### Tracking

- [open-questions.md](open-questions.md) - unresolved questions with resolution plans
- [session-handoff.md](session-handoff.md) - session state for prompt continuity

## Upstream Phase References

- [Phase 0 index](../phase0/index.md) - locked v1 contract
- [Phase 1 index](../phase1/index.md) - manual pause/resume vertical slice
- [Phase 2 index](../phase2/index.md) - native Kueue integration with preemption and resume
- [Phase 3 index](../phase3/index.md) - admission-aware launch, flavor-aware resume, partial admission
