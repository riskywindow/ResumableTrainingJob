# Phase 3 Document Index

## Phase 3 Scope

Phase 3: Admission-Aware Launch and Flavor-Aware Resume.

Build on the Phase 2 native Kueue integration to:

1. Materialize Kueue `podSetAssignments` into the child JobSet launch shape.
2. Add flavor-aware runtime placement (nodeSelector, tolerations from ResourceFlavors).
3. Support world-size-flexible resume using PyTorch DCP resharding.
4. Add an experimental partial-admission path for RTJ behind a feature gate/profile.

## Documents

### Design

- [README.md](README.md) - overview and quick context
- [goals.md](goals.md) - goals, non-goals, success criteria, and exit criteria
- [architecture.md](architecture.md) - component diagram, sequence diagrams, detailed design
- [adr/0001-adaptive-parallelism-and-flavor-aware-resume.md](adr/0001-adaptive-parallelism-and-flavor-aware-resume.md) - end-to-end Phase 3 contract and must-ship demo

### Implementation

- [api.md](api.md) - Phase 3 API reference
- [admission-materialization.md](admission-materialization.md) - AdmissionView, world-size-aware compatibility
- [flavor-aware-rendering.md](flavor-aware-rendering.md) - bridge annotation, renderer changes
- [checkpoint-resharding.md](checkpoint-resharding.md) - manifest extensions, DCP resharding
- [partial-admission.md](partial-admission.md) - experimental partial admission
- [adr/0002-parallelism-and-resume-contract.md](adr/0002-parallelism-and-resume-contract.md) - Phase 3 API surface decisions
- [adr/0003-experimental-partial-admission.md](adr/0003-experimental-partial-admission.md) - experimental partial admission decisions

### Dev Environment and Testing

- [dev-environment.md](dev-environment.md) - Phase 3 local dev setup, profiles, and smoke tests
- [e2e.md](e2e.md) - Phase 3 e2e test coverage, prerequisites, and run instructions

### Operations

- [demo.md](demo.md) - step-by-step demo walkthroughs for flavor-aware launch and mixed-size resume
- [operations.md](operations.md) - inspecting admission, flavors, worker counts, and checkpoint manifests
- [troubleshooting.md](troubleshooting.md) - diagnosing flavor injection, admitted counts, reshard restore, and partial admission issues

### Migration

- [migration-from-phase2.md](migration-from-phase2.md) - what stays, what changes, backward compatibility

### Review and Signoff

- [PHASE3_SIGNOFF.md](PHASE3_SIGNOFF.md) - signoff statement: capabilities, experimental features, deferred items, risks, Phase 4 recommendations
- [review/consistency-audit.md](review/consistency-audit.md) - audit of implementation against Phase 0-3 contracts
- [review/gaps.md](review/gaps.md) - explicit gaps register with severity and resolution paths

### Tracking

- [open-questions.md](open-questions.md) - unresolved questions
- [session-handoff.md](session-handoff.md) - session state for prompt continuity

## Upstream Phase References

- [Phase 0 index](../phase0/index.md) - locked v1 contract
- [Phase 1 index](../phase1/index.md) - manual pause/resume vertical slice
- [Phase 2 index](../phase2/index.md) - native Kueue integration with preemption and resume
