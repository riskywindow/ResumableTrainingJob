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
- [adr/0002-topology-and-launch-status-api.md](adr/0002-topology-and-launch-status-api.md) - topology intent and launch-readiness status API decisions
- [adr/0003-resume-readiness-policy.md](adr/0003-resume-readiness-policy.md) - ResumeReadiness AdmissionCheck policy design decisions

### API

- [api.md](api.md) - Phase 4 API reference (spec.topology, status.launchReadiness, status.topology, status.effectiveLaunchShape)

### Implementation

- [topology-workload-synthesis.md](topology-workload-synthesis.md) - RTJ → Kueue PodSet topology field mapping reference (G1)
- [resume-readiness-acc.md](resume-readiness-acc.md) - ResumeReadiness AdmissionCheck controller scaffold (G3)
- [resume-readiness-logic.md](resume-readiness-logic.md) - ResumeReadiness decision logic, state mapping, pre-launch boundary (G3)
- [topology-aware-launch.md](topology-aware-launch.md) - Topology-aware child JobSet materialization and admission-gated launch (G2/G4)

### Dev Environment

- [dev-environment.md](dev-environment.md) - local kind-based dev environment for Phase 4 (topology model, Makefile targets, smoke test)

### Observability and Operations

- [demo.md](demo.md) - demo walkthroughs (blocked launch, topology-aware launch, topology-aware resume)
- [operations.md](operations.md) - operations guide (inspect AdmissionCheck, RTJ/Workload, topology, child JobSet, checkpoints, metrics)
- [troubleshooting.md](troubleshooting.md) - troubleshooting guide (inactive check, stuck gate, missing topology, unsupported shapes)

### E2E Testing

- [e2e.md](e2e.md) - Phase 4 e2e test strategy, test matrix, and known limitations

### Migration

- [migration-from-phase3.md](migration-from-phase3.md) - what stays, what changes, backward compatibility

### Review and Signoff

- [PHASE4_SIGNOFF.md](PHASE4_SIGNOFF.md) - Phase 4 signoff: capabilities, deferrals, risks, Phase 5 recommendations
- [review/consistency-audit.md](review/consistency-audit.md) - contract compliance audit against Phases 0-4
- [review/gaps.md](review/gaps.md) - gaps, hardening opportunities, and deferred items

### Tracking

- [open-questions.md](open-questions.md) - unresolved questions with resolution plans
- [session-handoff.md](session-handoff.md) - session state for prompt continuity

## Upstream Phase References

- [Phase 0 index](../phase0/index.md) - locked v1 contract
- [Phase 1 index](../phase1/index.md) - manual pause/resume vertical slice
- [Phase 2 index](../phase2/index.md) - native Kueue integration with preemption and resume
- [Phase 3 index](../phase3/index.md) - admission-aware launch, flavor-aware resume, partial admission
