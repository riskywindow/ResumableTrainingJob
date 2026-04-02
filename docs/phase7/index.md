# Phase 7 -- Capacity-Guaranteed Launch

## Mission

Implement capacity-guaranteed launch and startup recovery for
ResumableTrainingJob (RTJ) using Kueue's built-in ProvisioningRequest
AdmissionCheck, Kueue topology assignment outputs, and waitForPodsReady
semantics.

RTJ must not launch child runtime until the workload is actually launchable
on physical capacity -- not just quota-reserved.

## Document index

| Document | Purpose |
|---|---|
| [README.md](README.md) | One-page summary for new readers |
| [goals.md](goals.md) | Mission, acceptance criteria, non-goals |
| [architecture.md](architecture.md) | Component diagram, sequence diagrams, design detail |
| [migration-from-phase6.md](migration-from-phase6.md) | What stays, what changes, upgrade path |
| [open-questions.md](open-questions.md) | Unresolved design questions |
| [session-handoff.md](session-handoff.md) | Per-session decisions, files changed, next prompt |
| [api.md](api.md) | Phase 7 API reference (new status fields) |
| [adr/0001-capacity-guaranteed-launch.md](adr/0001-capacity-guaranteed-launch.md) | ADR: launch gate contract and demo scope |
| [waitforpodsready.md](waitforpodsready.md) | waitForPodsReady startup/recovery integration |
| [e2e.md](e2e.md) | Phase 7 e2e test coverage and what remains deferred |
| [demo.md](demo.md) | Step-by-step demo scenarios for capacity-guaranteed launch |
| [operations.md](operations.md) | Operational inspection procedures |
| [troubleshooting.md](troubleshooting.md) | Common issues and diagnostic checklist |
| [adr/0002-launch-gate-status-api.md](adr/0002-launch-gate-status-api.md) | ADR: status-only API design decision |
| [provisioning-observation.md](provisioning-observation.md) | Provisioning/topology observation layer field-level docs |
| [launch-gating.md](launch-gating.md) | Phase 7 provisioning-aware launch gating |
| [dev-environment.md](dev-environment.md) | Phase 7 local dev environment setup |
| [multicluster-compatibility.md](multicluster-compatibility.md) | Multi-cluster Phase 7 compatibility |
| [PHASE7_SIGNOFF.md](PHASE7_SIGNOFF.md) | Phase 7 signoff: capabilities, risks, Phase 8 next steps |
| [review/consistency-audit.md](review/consistency-audit.md) | Audit of implementation against Phase 0-7 contracts |
| [review/gaps.md](review/gaps.md) | Gaps, drift analysis, and open questions |

## Pinned dependencies

| Dependency | Version | Source |
|---|---|---|
| Kueue | v0.15.1 | go.mod |
| JobSet | v0.10.1 | go.mod |
| controller-runtime | v0.22.4 | go.mod |
| Kubernetes API | v0.34.2 | go.mod |

## Core invariants (carried from Phase 0-6, extended)

1. RTJ is the **only** Kueue-managed object.
2. Child JobSets are **plain runtime resources** -- never Kueue workloads.
3. Kueue is the **sole authority** for admission, preemption, and quota.
4. The RTJ operator is the **lifecycle owner** for launch, yield, resume,
   and child-resource rendering.
5. The built-in ProvisioningRequest AdmissionCheck is the **source of
   physical-capacity truth** when configured.
6. Phase 6 single-cluster and manager/worker behavior is **preserved
   unchanged** when Phase 7 features are not configured.
