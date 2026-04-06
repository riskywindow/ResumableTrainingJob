# Phase 8 -- Accelerator-Native DRA Device Requests

## Mission

Implement accelerator-native ResumableTrainingJob (RTJ) execution using
Kubernetes Dynamic Resource Allocation (DRA).

RTJ gains native DRA device requests, companion ResourceClaimTemplate
lifecycle management, Kueue deviceClassMappings-based quota/accounting,
DRA-aware child JobSet rendering, conservative checkpoint compatibility
for device profiles, and a local dev path using an example DRA driver
with simulated devices.

## Document index

| Document | Purpose |
|---|---|
| [README.md](README.md) | One-page summary for new readers |
| [goals.md](goals.md) | Mission, acceptance criteria, non-goals |
| [architecture.md](architecture.md) | Component diagram, sequence diagrams, design detail |
| [migration-from-phase7.md](migration-from-phase7.md) | What stays, what changes, upgrade path |
| [open-questions.md](open-questions.md) | Unresolved design questions |
| [session-handoff.md](session-handoff.md) | Per-session decisions, files changed, next prompt |
| [api.md](api.md) | Phase 8 DRA device API reference |
| [adr/0001-dra-accelerator-native-rtj.md](adr/0001-dra-accelerator-native-rtj.md) | ADR: native DRA device requests for RTJ |
| [adr/0002-dra-api.md](adr/0002-dra-api.md) | ADR: DRA API shape decision |
| [multicluster-compatibility.md](multicluster-compatibility.md) | Phase 8 DRA + Phase 6 multi-cluster compatibility |
| [dra-template-lifecycle.md](dra-template-lifecycle.md) | ResourceClaimTemplate lifecycle documentation |
| [dra-rendering-and-kueue-accounting.md](dra-rendering-and-kueue-accounting.md) | DRA rendering and Kueue accounting integration |
| [checkpoint-device-compatibility.md](checkpoint-device-compatibility.md) | Device profile checkpoint compatibility design |

### Dev Environment

- [dev-environment.md](dev-environment.md) | Phase 8 local dev environment setup and DRA driver

### Operations and Observability

- [demo.md](demo.md) | Step-by-step demo walkthrough for DRA-backed RTJ lifecycle
- [operations.md](operations.md) | Operational inspection: DRA status, templates, claims, Kueue accounting
- [troubleshooting.md](troubleshooting.md) | DRA failure diagnosis: driver, claims, quota, compatibility

### E2E Testing

- [e2e.md](e2e.md) | Phase 8 e2e test coverage and what remains deferred

### Review and Signoff

- [PHASE8_SIGNOFF.md](PHASE8_SIGNOFF.md) | Phase 8 signoff: capabilities, deferred items, risks, Phase 9 next steps
- [review/consistency-audit.md](review/consistency-audit.md) | Audit of implementation against Phase 0-8 contracts
- [review/gaps.md](review/gaps.md) | Gap analysis with severity classification and recommendations

## Pinned dependencies

| Dependency | Version | Source |
|---|---|---|
| Kueue | v0.15.1 | go.mod |
| JobSet | v0.10.1 | go.mod |
| controller-runtime | v0.22.4 | go.mod |
| Kubernetes API | v0.34.2 | go.mod |

**DRA API version**: `k8s.io/api/resource/v1beta1` is the chosen import
path for k8s.io/api@v0.34.2. DRA types (ResourceClaimTemplate,
ResourceClaim, DeviceRequest, DeviceSelector, CELDeviceSelector) are
available at v1beta1 (also v1, v1beta2, v1alpha3). v1beta1 was chosen as
the stable beta target matching Kubernetes 1.33-1.34. Migration to v1
when DRA reaches GA is mechanical (import path change only; type shapes
are identical). Resolved in Session 3 (OQ1).

## Core invariants (carried from Phase 0-7, extended)

1. RTJ is the **only** Kueue-managed object.
2. Child JobSets are **plain runtime resources** -- never Kueue workloads.
3. Kueue is the **sole authority** for admission, preemption, and quota.
4. The RTJ operator is the **lifecycle owner** for launch, yield, resume,
   and child-resource rendering.
5. The built-in ProvisioningRequest AdmissionCheck is the **source of
   physical-capacity truth** when configured (Phase 7).
6. Phase 7 single-cluster and manager/worker behavior is **preserved
   unchanged** when Phase 8 features are not configured.
7. **ResourceClaimTemplates and ResourceClaims are helper runtime objects**,
   not Kueue-managed workloads. Kueue accounts for DRA devices through
   `deviceClassMappings` on ClusterQueue ResourceGroups, not through
   direct claim management.
8. **Native DRA claims are the core path**. The alpha extended-resource
   bridge (`resource.k8s.io` extended-resource translation) is not a
   required or recommended path.
9. **Checkpoint compatibility is fail-closed for device profiles**. A
   resume from a checkpoint that was created with a different device
   class or device selector fingerprint is rejected unless explicitly
   marked as compatible.

## Upstream Phase References

- [Phase 0 index](../phase0/index.md) - locked v1 contract
- [Phase 1 index](../phase1/index.md) - manual pause/resume vertical slice
- [Phase 2 index](../phase2/index.md) - native Kueue integration with preemption and resume
- [Phase 3 index](../phase3/index.md) - admission-aware launch, flavor-aware resume, partial admission
- [Phase 4 index](../phase4/index.md) - topology-aware admission pipeline
- [Phase 5 index](../phase5/index.md) - checkpoint-aware priority shaping
- [Phase 6 index](../phase6/index.md) - multi-cluster checkpoint-native spillover
- [Phase 7 index](../phase7/index.md) - capacity-guaranteed launch
