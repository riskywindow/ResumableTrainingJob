# Phase 6 Document Index

## Phase 6 Scope

Phase 6: Multi-Cluster Checkpoint-Native Spillover.

Build on Phase 5's checkpoint-aware priority shaping to:

1. Integrate RTJ with Kueue MultiKueue external-framework dispatch.
2. Split the RTJ operator into manager mode and worker mode.
3. Enable shared-checkpoint remote pause/resume across worker clusters.
4. Surface remote worker status on the manager-side RTJ.
5. Provide a deterministic three-cluster local dev/test profile.

## Documents

### Design

- [README.md](README.md) - overview and quick context
- [goals.md](goals.md) - goals, non-goals, success criteria, and exit criteria
- [architecture.md](architecture.md) - component diagrams, three sequence diagrams, detailed design
- [adr/0001-multicluster-spillover.md](adr/0001-multicluster-spillover.md) - Phase 6 multi-cluster spillover contract and design decisions
- [adr/0002-managedby-and-remote-status.md](adr/0002-managedby-and-remote-status.md) - managedBy field and remote status API design

### Implementation

- [operator-modes.md](operator-modes.md) - operator mode split: manager vs worker mode semantics, ownership table, detection logic, configuration, test coverage
- [multikueue-integration.md](multikueue-integration.md) - MultiKueue external-framework integration: manager/worker setup, RBAC, spec.managedBy, mirror-copy execution model
- [remote-status.md](remote-status.md) - remote status plumbing: approach, status fields, dispatch classification, cluster resolution
- [shared-checkpoint-store.md](shared-checkpoint-store.md) - shared checkpoint store contract: migration, endpoint validation, design decisions
- [remote-pause-resume.md](remote-pause-resume.md) - remote pause/resume propagation model: mechanism, flows, difference from single-cluster, controller plumbing
- [adr/0003-rtj-as-external-framework.md](adr/0003-rtj-as-external-framework.md) - ADR for RTJ as MultiKueue external framework via Kueue's generic adapter

### API

- [api.md](api.md) - Phase 6 RTJ API extensions reference (spec.managedBy, status.multiCluster)

### Migration

- [migration-from-phase5.md](migration-from-phase5.md) - what stays, what changes, manager vs worker ownership, why live migration is deferred, why shared checkpoint store is required

### Dev Environment

- [dev-environment.md](dev-environment.md) - three-cluster local dev environment: architecture, scripts, Makefile targets, smoke test, shared store

### E2E Testing

- [e2e.md](e2e.md) - Phase 6 e2e test coverage: remote dispatch/execution, manager suppression, deterministic cluster selection, deferred items

### Operations and Observability

- [demo.md](demo.md) - demo command sequences: remote dispatch, manager-visible status, remote pause, remote resume
- [operations.md](operations.md) - operational procedures: inspecting manager/worker RTJs, MultiKueue objects, checkpoint evidence, metrics
- [troubleshooting.md](troubleshooting.md) - failure scenarios: missing config, local launch, no worker, namespace mismatch, shared store, pause/resume

### Review and Signoff

- [PHASE6_SIGNOFF.md](PHASE6_SIGNOFF.md) - Phase 6 signoff: capabilities, experimental items, deferred items, risks, Phase 7 recommendations
- [review/consistency-audit.md](review/consistency-audit.md) - audit of implementation against Phases 0-6 contracts, goal-by-goal verification, test coverage audit
- [review/gaps.md](review/gaps.md) - gaps and tightening items: metrics instrumentation, code hygiene, documentation improvements

### Tracking

- [open-questions.md](open-questions.md) - unresolved questions with resolution plans
- [session-handoff.md](session-handoff.md) - session state for prompt continuity

## Upstream Phase References

- [Phase 0 index](../phase0/index.md) - locked v1 contract
- [Phase 1 index](../phase1/index.md) - manual pause/resume vertical slice
- [Phase 2 index](../phase2/index.md) - native Kueue integration with preemption and resume
- [Phase 3 index](../phase3/index.md) - admission-aware launch, flavor-aware resume, partial admission
- [Phase 4 index](../phase4/index.md) - topology-aware admission pipeline
- [Phase 5 index](../phase5/index.md) - checkpoint-aware priority shaping
