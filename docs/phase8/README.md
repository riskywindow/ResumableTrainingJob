# Phase 8 -- Quick Start

Phase 8 adds **accelerator-native DRA device requests** to ResumableTrainingJob.

Today (Phase 7) an RTJ declares GPU requirements via the legacy
`spec.identity.gpuShape` string and extended-resource requests
(`nvidia.com/gpu: N`) embedded in the JobSet template. Kueue accounts for
these through resource-flavor quotas. This works but couples the RTJ to a
single resource-naming convention and does not leverage the Kubernetes
Dynamic Resource Allocation (DRA) framework for structured device requests,
device profiles, or claim lifecycle management.

Phase 8 closes this gap:

1. **Native DRA device requests for RTJ** -- the RTJ spec gains an optional
   `spec.devices` section that declares per-worker device requirements using
   the Kubernetes DRA vocabulary: device class, device selectors, and count.
   When present, the operator manages companion ResourceClaimTemplates that
   the child JobSet references.

2. **Companion ResourceClaimTemplate lifecycle** -- the RTJ operator creates,
   updates, and garbage-collects ResourceClaimTemplate objects that match
   the RTJ's device spec. These are helper runtime objects owned by the RTJ,
   not Kueue-managed workloads.

3. **Kueue deviceClassMappings-based quota/accounting** -- Kueue already
   supports DRA device classes via `deviceClassMappings` on ClusterQueue
   ResourceGroups. Phase 8 uses this native mechanism for quota/accounting.
   No custom quota engine is introduced.

4. **DRA-aware child JobSet rendering** -- the child JobSet's pod templates
   include `spec.resourceClaims` entries referencing the RTJ-managed
   ResourceClaimTemplates, and container `resources.claims` entries to bind
   the claims.

5. **Conservative checkpoint compatibility for device profiles** -- the
   resume compatibility contract (Phase 0 ADR 0003) is extended to include
   device class and device selector fingerprint. A checkpoint taken with one
   device profile is incompatible with a different device profile unless the
   profiles are explicitly equivalent.

6. **Example DRA driver local dev profile** -- the local/dev path uses an
   upstream/example DRA driver (e.g., `dra-example-driver` or
   `k8s-dra-driver-gpu`) with fake/simulated devices, so Phase 8 is testable
   without real accelerators.

7. **Compatibility with existing worker-mode runtime behavior** -- when no
   `spec.devices` section is present, the RTJ follows the Phase 7 path
   unchanged. Worker clusters continue to be the execution site where DRA
   claims are allocated.

## What stays the same

- RTJ is the only Kueue-managed object.
- Child JobSets are plain runtime resources.
- Kueue remains the admission and preemption authority.
- All Phase 1-7 features work unchanged when Phase 8 is not configured.
- The RTJ lifecycle state machine is unchanged.
- The launch gate (Phase 7) continues to gate child JobSet rendering.

## See also

- [index.md](index.md) -- full document index
- [goals.md](goals.md) -- acceptance criteria
- [architecture.md](architecture.md) -- diagrams and design
