# Phase 8 -- Open Questions

## OQ1: Kubernetes DRA API version stability

**Question**: Which DRA API version is stable in the pinned Kubernetes
version (v0.34.2 / k8s 1.34)? Is `resource.k8s.io/v1beta2` available,
or must we use `v1beta1`?

**Impact**: Determines the ResourceClaimTemplate and ResourceClaim API
version used in the operator and rendered manifests. If the pinned
version only has beta DRA APIs, we must document the upgrade path to GA.

**Action**: Audit `k8s.io/api/resource/v1beta2` (or `v1beta1`, `v1`)
availability in the pinned Kubernetes client-go version. Check whether
`DeviceClass`, `ResourceClaimTemplate`, and `ResourceClaim` types exist
with the expected fields.

**Resolution**: TBD -- must be resolved in Session 2 implementation.

---

## OQ2: Kueue deviceClassMappings maturity in v0.15.1

**Question**: Does Kueue v0.15.1 support `deviceClassMappings` on
ClusterQueue ResourceGroups? If so, what is the exact API shape?

**Impact**: If `deviceClassMappings` is not in v0.15.1, Phase 8 must
either upgrade Kueue or find an alternative quota mechanism.

**Action**: Audit Kueue v0.15.1 source for `deviceClassMappings` in
`ClusterQueueSpec.ResourceGroups`. Check whether Kueue's admission
controller uses it for DRA device accounting.

**Resolution**: TBD -- must be resolved in Session 2. If missing, this
is a blocking issue that may require a Kueue version bump.

---

## OQ3: ResourceClaimTemplate API shape for CEL selectors

**Question**: What is the exact API shape for device selectors in
`ResourceClaimTemplate.spec.spec.devices.requests[].selectors`? Does
the beta API use `cel.expression` or a different field path?

**Impact**: Determines the ResourceClaimTemplate generation code. An
incorrect field path will cause claim creation to fail silently or
with schema validation errors.

**Action**: Read the Kubernetes DRA API types for the pinned version.
Verify that CEL selectors are available and document the exact field
path.

**Resolution**: TBD -- must be resolved in Session 2.

---

## OQ4: Example DRA driver availability for kind clusters

**Question**: Which example DRA driver works reliably in a `kind` cluster
for local dev? Options include:

- `kubernetes-sigs/dra-example-driver` (maintained upstream)
- `registry.k8s.io/e2e-test-images/sample-device-plugin`
- A minimal custom fake driver

**Impact**: Determines the local dev profile setup. The driver must:
- Run in a kind cluster without real hardware.
- Publish ResourceSlices with fake device attributes.
- Allocate devices to ResourceClaims.
- Be stable enough for CI.

**Action**: Test candidate drivers in a kind cluster. Verify end-to-end
ResourceClaimTemplate → ResourceClaim → pod allocation flow.

**Resolution**: TBD -- should be resolved in Session 2 or 3.

---

## OQ5: Device fingerprint storage format

**Question**: Should the device fingerprint in the checkpoint manifest be:
(a) `SHA256(sorted(selectors))` -- a compact hash, or
(b) the full sorted selector list -- preserving debuggability, or
(c) a structured object with both hash and selectors?

**Impact**: Affects checkpoint manifest size and debuggability. A hash is
compact but opaque. Full selectors are debuggable but may be large.

**Recommendation**: (a) hash for the fingerprint field, with the full
selector list stored in a separate optional metadata field for debugging.

**Resolution**: TBD -- decide in Session 2.

---

## OQ6: Interaction between spec.devices and spec.identity.gpuShape

**Question**: When `spec.devices` is present, should `spec.identity.gpuShape`
still be required? They describe overlapping concerns (what kind of
accelerator the training uses).

**Options**:
- (a) Both required, independently validated (current assumption).
- (b) `gpuShape` derived from `deviceClassName` when devices is set.
- (c) `gpuShape` deprecated when devices is set (future phase).

**Impact**: Affects the Phase 0 compatibility contract. `gpuShape` is a
locked Phase 0 field used in all compatibility checks.

**Recommendation**: (a) for Phase 8 -- keep both required. This preserves
the Phase 0 contract. Reconciliation can be addressed in a future phase.

**Resolution**: Decided in Session 1. Both fields coexist independently
for Phase 8. `gpuShape` remains required for Phase 0 compatibility.
`deviceClassName` is required when `spec.devices` is present.
Reconciliation between the two fields is deferred to a future phase.

---

## OQ7: ResourceClaimTemplate update vs recreate on device spec change

**Question**: When the RTJ's `spec.devices` changes between run attempts,
should the operator:
(a) Update the existing ResourceClaimTemplate in place, or
(b) Delete and recreate the ResourceClaimTemplate?

**Impact**: In-place updates may cause issues if existing ResourceClaims
(from a prior run's pods) reference the old template spec. Recreating
ensures a clean slate.

**Recommendation**: (b) delete and recreate. ResourceClaimTemplates are
lightweight and recreating ensures no stale claims reference an outdated
spec.

**Resolution**: TBD -- decide in Session 2.

---

## OQ8: Multi-device-class support (future)

**Question**: Should `spec.devices` support multiple device classes per
RTJ (e.g., GPU + FPGA)? Or is single device class per RTJ sufficient
for Phase 8?

**Impact**: API design. A single device class simplifies the
ResourceClaimTemplate, compatibility check, and quota accounting. Multiple
classes add complexity but may be needed for heterogeneous training.

**Recommendation**: Single device class for Phase 8. Multi-class can be
added later by making `spec.devices` an array. Explicitly listed as a
non-goal (NG5).

**Resolution**: Deferred -- single class is the Phase 8 scope.

---

## OQ9: DRA + ProvisioningRequest interaction

**Question**: When both DRA device requests and ProvisioningRequest AC
are configured, does the ProvisioningRequest backend see the DRA device
requirements? Or are they handled independently?

**Impact**: If the ProvisioningRequest backend needs to know about DRA
devices to provision appropriate nodes, there may be an integration gap.

**Action**: Test with fake ProvisioningRequest backend + example DRA
driver. Verify that nodes are provisioned with the DRA driver and that
claims can be allocated.

**Resolution**: TBD -- test in e2e during Session 3 or later.

---

## OQ10: Feature gate naming for Phase 8

**Question**: Should Phase 8 DRA support be behind a feature gate? If so,
what should it be named?

**Options**:
- `DRADeviceRequests`
- `AcceleratorNativeDRA`
- No feature gate (presence of `spec.devices` is the opt-in)

**Recommendation**: No explicit feature gate. The presence of
`spec.devices` is sufficient opt-in. The operator validates the field
and creates DRA resources only when it is set.

**Resolution**: TBD -- decide in Session 2.
