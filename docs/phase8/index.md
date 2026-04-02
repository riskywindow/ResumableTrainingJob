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

## Pinned dependencies

| Dependency | Version | Source |
|---|---|---|
| Kueue | v0.15.1 | go.mod |
| JobSet | v0.10.1 | go.mod |
| controller-runtime | v0.22.4 | go.mod |
| Kubernetes API | v0.34.2 | go.mod |

**Note**: Kubernetes v1.33+ is assumed for stable DRA support
(`resource.k8s.io/v1beta2` or `v1`). The pinned Kubernetes API version
must be validated against DRA GA status. If the pinned version predates
DRA GA, the phase must use the beta API (`v1beta1`/`v1beta2`) and document
the compatibility boundary.

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
