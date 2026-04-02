# Phase 8 -- Session Handoff

## Session 1: Design lock

**Date**: 2026-04-01

### Decisions made

1. **Phase 8 scope locked**: accelerator-native DRA device requests for
   RTJ, companion ResourceClaimTemplate lifecycle managed by the RTJ
   operator, Kueue deviceClassMappings-based quota/accounting, DRA-aware
   child JobSet rendering, conservative checkpoint compatibility for
   device profiles, example DRA driver local dev profile.

2. **RTJ remains the only Kueue-managed object.** No change to the
   Kueue integration boundary. ResourceClaimTemplates and ResourceClaims
   are helper runtime objects, not Kueue-managed workloads.

3. **Child JobSets remain plain runtime resources.** DRA claim references
   are injected into the rendered pod template by the RTJ operator.

4. **Native DRA claims are the core path.** The alpha extended-resource
   bridge is explicitly deferred/experimental. Phase 8 uses
   `ResourceClaimTemplate` + `ResourceClaim` objects directly.

5. **Shared/manual ResourceClaims are deferred.** Per-pod
   ResourceClaimTemplates are the core path. Shared claims (pre-allocated
   devices reused across pods or runs) are deferred to a future phase.

6. **Prioritized device alternatives are deferred.** A single DeviceClass
   per RTJ is the Phase 8 scope. Multi-class or priority-ordered
   alternatives are deferred.

7. **`spec.devices` is optional and additive.** When absent, the RTJ
   follows the Phase 7 path unchanged. No Phase 7 behavior changes.

8. **ResourceClaimTemplate is owned by the RTJ** via ownerReference with
   `controller=true`. Garbage-collected on RTJ deletion. Survives child
   JobSet deletion (not owned by the JobSet).

9. **Checkpoint compatibility extended with device dimensions.** Device
   class and device selector fingerprint (SHA256 hash of sorted CEL
   selectors) are added to the checkpoint manifest. Fail-closed:
   mismatch is incompatible.

10. **`spec.identity.gpuShape` and `spec.devices.deviceClassName` coexist.**
    Both fields are required when `spec.devices` is present (gpuShape for
    Phase 0 compat, deviceClassName for DRA). Reconciliation between them
    is deferred to a future phase.

11. **Kueue `deviceClassMappings` is the quota mechanism.** No custom
    quota engine. Cluster admin configures `deviceClassMappings` on
    ClusterQueue ResourceGroups.

12. **Manager-mode RTJ does not create ResourceClaimTemplates.** DRA
    claims are worker-local, matching the Phase 6 manager/worker split.

13. **Example DRA driver for local dev.** The local/dev path uses an
    upstream example DRA driver with simulated devices. No real GPUs or
    vendor-specific drivers required for local success.

14. **Phase 7 backward compatibility is unconditional.** When `spec.devices`
    is nil, the operator does not create ResourceClaimTemplates, does not
    inject DRA claims, and does not add device dimensions to checkpoint
    compatibility. All Phase 7 features work unchanged.

### Files changed

| File | Action |
|---|---|
| docs/phase8/README.md | Created |
| docs/phase8/index.md | Created |
| docs/phase8/goals.md | Created |
| docs/phase8/architecture.md | Created |
| docs/phase8/migration-from-phase7.md | Created |
| docs/phase8/open-questions.md | Created |
| docs/phase8/session-handoff.md | Created |
| docs/phase8/adr/0001-dra-accelerator-native-rtj.md | Created |

### Tests run

None (documentation-only session).

### Open issues

1. **OQ1**: Kubernetes DRA API version stability -- must audit pinned
   k8s client-go for DRA type availability.
2. **OQ2**: Kueue deviceClassMappings maturity in v0.15.1 -- must audit
   Kueue source. Potentially blocking if missing.
3. **OQ3**: ResourceClaimTemplate API shape for CEL selectors -- must
   verify exact field path.
4. **OQ4**: Example DRA driver availability for kind clusters -- must
   test candidate drivers.
5. **OQ5**: Device fingerprint storage format -- hash vs full selectors.
6. **OQ6**: Interaction between spec.devices and spec.identity.gpuShape
   -- coexist for Phase 8 (decided).
7. **OQ7**: ResourceClaimTemplate update vs recreate on spec change --
   lean toward recreate.
8. **OQ8**: Multi-device-class support -- deferred (decided).
9. **OQ9**: DRA + ProvisioningRequest interaction -- test in e2e.
10. **OQ10**: Feature gate naming -- lean toward no gate (spec.devices
    presence is opt-in).

### Recommended next prompt (superseded by Session 2)

See Session 2 below.

---

## Session 2: API implementation

**Date**: 2026-04-01

### Decisions made

1. **API shape: structured claims with constrained request fragments**
   (ADR 0002). The Session 1 flat `DeviceSpec` was replaced with a
   richer design supporting one or more claims per RTJ:
   - `spec.devices.mode`: `Disabled` | `DRA`
   - `spec.devices.claims[]`: array of `DeviceClaimSpec`
   - Each claim has `name`, `containers[]`, and `request` (constrained
     DRA fragment with `deviceClassName`, `count`, `selectors[]`)

2. **Multi-claim support from day one.** Unlike Session 1's single
   `DeviceSpec`, the new design supports multiple claims per RTJ
   (e.g., GPU + RDMA). Each claim produces a separate
   ResourceClaimTemplate.

3. **Explicit container attachment targets.** Each `DeviceClaimSpec`
   lists which containers receive the claim via `containers[]`.

4. **Constrained DRA subset: three fields only.** Only
   `deviceClassName`, `count`, and `selectors` are exposed from the
   DRA DeviceRequest type. Unsupported DRA fields are not surfaced.

5. **OQ5 resolved: SHA256 fingerprint for checkpoint compatibility.**
   The `currentDeviceProfileFingerprint` in status uses SHA256 of
   sorted device classes + selectors. Full selectors are not stored
   in the fingerprint field (compact hash only).

6. **OQ6 confirmed: gpuShape and deviceClassName coexist.**
   Both fields are independent. `gpuShape` remains required for Phase 0
   compatibility. `deviceClassName` is required within each claim's
   request when mode=DRA.

7. **OQ10 resolved: no feature gate.** The presence of `spec.devices`
   with `mode=DRA` is the opt-in mechanism. No operator-level feature
   gate needed.

8. **Device mode defaults to Disabled.** When `spec.devices` is set
   but mode is empty, it defaults to `Disabled`. This preserves Phase 7
   behavior if the user accidentally includes an empty devices section.

9. **Request count defaults to 1.** When `count` is not set (zero value),
   it defaults to 1.

10. **Claim names must be unique per RTJ.** Validation rejects duplicate
    claim names.

11. **Container names must be unique per claim.** Duplicate container
    names within a single claim are rejected.

12. **Device status is entirely controller-owned.** The webhook does not
    inject device status during defaulting.

13. **spec.devices is mutable.** Unlike `managedBy`, the devices spec
    can change between run attempts. The operator recreates
    ResourceClaimTemplates on spec change.

### Open questions resolved

| OQ | Resolution |
|---|---|
| OQ5 | SHA256 hash for fingerprint field |
| OQ6 | Confirmed: gpuShape + deviceClassName coexist independently |
| OQ10 | No feature gate; spec.devices presence is opt-in |

### Open questions remaining

| OQ | Status |
|---|---|
| OQ1 | Unresolved -- Kubernetes DRA API version audit deferred to controller implementation session |
| OQ2 | Unresolved -- Kueue deviceClassMappings audit deferred to controller implementation session |
| OQ3 | Partially resolved -- CEL selectors exposed as string array; exact upstream field path verified during controller implementation |
| OQ4 | Unresolved -- Example DRA driver testing deferred to e2e session |
| OQ7 | Lean toward recreate; final decision during controller implementation |
| OQ8 | Deferred -- multi-device-class supported at API level via claims array; single-class-per-claim is Phase 8 scope |
| OQ9 | Unresolved -- DRA + ProvisioningRequest interaction test deferred to e2e |

### Files changed

| File | Action |
|---|---|
| api/v1alpha1/resumabletrainingjob_types.go | Updated: added DeviceMode, DeviceSpec, DeviceClaimSpec, DeviceRequestSpec, DeviceStatus, ResourceClaimTemplateReference, ClaimAllocationState types; added spec.devices and status.devices fields; added validateDevices() and helper validation; added IsDevicesEnabled(); added defaulting for device mode and request count |
| api/v1alpha1/resumabletrainingjob_webhook_test.go | Updated: added 20 Phase 8 tests covering defaulting, validation, status preservation, deep copy independence, backward compatibility |
| api/v1alpha1/zz_generated.deepcopy.go | Updated: added DeepCopy methods for DeviceSpec, DeviceClaimSpec, DeviceRequestSpec, DeviceStatus, ResourceClaimTemplateReference; updated spec and status deep copy |
| config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml | Updated: added spec.devices and status.devices OpenAPI schema |
| docs/phase8/api.md | Created: full API reference for Phase 8 DRA device types |
| docs/phase8/adr/0002-dra-api.md | Created: ADR documenting API shape decision |
| docs/phase8/session-handoff.md | Updated: Session 2 results |

### Tests run

All API tests pass (60+ tests including 20 new Phase 8 tests):

- `TestWebhookDefaultPreservesPhase7SpecWithoutDevices`
- `TestWebhookDefaultSetsDeviceModeWhenEmpty`
- `TestWebhookDefaultPreservesExplicitDeviceMode`
- `TestWebhookDefaultSetsDeviceRequestCount`
- `TestWebhookValidateCreateAcceptsDRADeviceSpec`
- `TestWebhookValidateCreateAcceptsMultipleClaims`
- `TestWebhookValidateCreateAcceptsDeviceDisabled`
- `TestWebhookValidateCreateRejectsDRAWithoutClaims`
- `TestWebhookValidateCreateRejectsDisabledWithClaims`
- `TestWebhookValidateCreateRejectsDuplicateClaimNames`
- `TestWebhookValidateCreateRejectsEmptyContainers`
- `TestWebhookValidateCreateRejectsMissingDeviceClassName`
- `TestWebhookValidateCreateRejectsEmptySelector`
- `TestWebhookDefaultDoesNotInjectPhase8Status`
- `TestWebhookValidateUpdatePreservesPhase8StatusFields`
- `TestWebhookValidateCreateAcceptsPhase7ManifestForPhase8`
- `TestWebhookValidateCreateAcceptsFullPhase8Spec`
- `TestIsDevicesEnabled`
- `TestWebhookValidateCreateRejectsDuplicateContainerNames`
- `TestWebhookDeepCopyIndependenceForDeviceSpec`
- `TestWebhookDeepCopyIndependenceForDeviceStatus`

### Recommended next prompt (superseded by Session 3)

See Session 3 below.

---

## Session 3: Internal DRA model and ResourceClaimTemplate lifecycle

**Date**: 2026-04-01

### Mission

Implement the internal DRA device-profile abstraction and companion
ResourceClaimTemplate lifecycle for RTJ. Per-Pod ResourceClaimTemplate
generation only -- no child JobSet rendering or Kueue Workload synthesis
updates in this session.

### Decisions made

1. **OQ1 resolved: DRA import path is `k8s.io/api/resource/v1beta1`.**
   k8s.io/api@v0.34.2 provides ResourceClaimTemplate, ResourceClaim,
   DeviceRequest, DeviceSelector, and CELDeviceSelector at v1beta1
   (also available at v1, v1beta2, v1alpha3). v1beta1 chosen as the
   stable beta target matching Kubernetes 1.34.

2. **OQ3 resolved: CEL selector field path confirmed.** The DRA
   `DeviceSelector` has a `CEL` field containing a `CELDeviceSelector`
   with an `Expression` string. The RTJ `DeviceRequestSpec.Selectors[]`
   strings map directly to `DeviceSelector{CEL: &CELDeviceSelector{Expression: sel}}`.

3. **OQ7 resolved: recreate on spec drift.** When `TemplateSpecMatches()`
   detects that an existing ResourceClaimTemplate's device request spec
   differs from the desired spec, the operator deletes and recreates
   the template. In-place update is unsafe because active ResourceClaims
   reference the template.

4. **Ownership model: one stable template per RTJ claim spec (per-RTJ,
   not per-run).** Template named `<rtj-name>-<claim-name>`, owned by
   RTJ with `controller=true, blockOwnerDeletion=true`. Survives child
   JobSet deletion. See `docs/phase8/dra-template-lifecycle.md` for
   full rationale.

5. **Device profile fingerprint: SHA256 of canonical sorted entries.**
   Each claim contributes a canonical entry
   `class=<className>;selectors=<sorted,joined>;count=<count>`.
   Entries are sorted, joined with newlines, and hashed. The fingerprint
   is order-independent across both claims and selectors within claims.
   Container targets do not affect the fingerprint (they are a rendering
   concern, not a hardware requirement).

6. **Template labels for discovery and cleanup:**
   - `training.checkpoint.example.io/rtj-name`: RTJ name
   - `training.checkpoint.example.io/claim-name`: claim name
   - `training.checkpoint.example.io/managed-by`: `rtj-operator`

7. **DeviceRequest allocation mode: ExactCount.** All RTJ device
   requests use `DeviceAllocationModeExactCount` with the user-specified
   count. The `All` allocation mode is not exposed.

8. **DeviceRequest name: `<claim-name>-req`.** Each claim produces one
   device request named `<claim-name>-req` within the ResourceClaimTemplate.

9. **Status sync preserves allocation fields.** When `syncDeviceStatus()`
   updates the device profile (fingerprint, classes, refs), it preserves
   allocation-tracking fields (`allocatedClaimCount`,
   `lastClaimFailureReason`, `lastCheckpointDeviceProfileFingerprint`,
   `lastResumeDeviceProfileFingerprint`) from the existing status.

10. **Orphan cleanup via label selector.** Templates with the RTJ label
    that are not in the desired set are deleted. Ownership is verified
    defensively before deletion.

### Open questions resolved

| OQ | Resolution |
|---|---|
| OQ1 | `k8s.io/api/resource/v1beta1` -- ResourceClaimTemplate and related types available in k8s.io/api@v0.34.2 |
| OQ3 | `DeviceSelector{CEL: &CELDeviceSelector{Expression: expr}}` -- confirmed |
| OQ7 | Recreate (delete + create) on spec drift |

### Open questions remaining

| OQ | Status |
|---|---|
| OQ2 | Unresolved -- Kueue deviceClassMappings audit deferred to Workload synthesis session |
| OQ4 | Unresolved -- Example DRA driver testing deferred to e2e session |
| OQ8 | Deferred -- multi-device-class supported at API level; single-class-per-claim is Phase 8 scope |
| OQ9 | Unresolved -- DRA + ProvisioningRequest interaction test deferred to e2e |

### Files changed

| File | Action |
|---|---|
| internal/dra/profile.go | Created: DeviceProfile abstraction, BuildProfile(), TemplateNameForClaim(), TemplateRefs() |
| internal/dra/profile_test.go | Created: 17 tests for fingerprint generation, determinism, order independence, name generation |
| internal/dra/templates.go | Created: DesiredTemplate, BuildDesiredTemplates(), buildTemplate(), TemplateSpecMatches(), TemplateKey() |
| internal/dra/templates_test.go | Created: 13 tests for template construction, owner refs, labels, spec matching, determinism |
| internal/controller/dra_templates.go | Created: reconcileDRATemplates(), reconcileSingleTemplate(), cleanupOrphanedTemplates(), isOwnedByRTJ() |
| internal/controller/dra_templates_test.go | Created: 22 tests for reconciliation lifecycle (create, idempotent, drift, orphan cleanup, status sync, ownership) |
| internal/controller/status_helpers.go | Updated: added syncDeviceStatus(), clearDeviceStatus(), deviceStatusEqual(), stringSlicesEqual(), claimTemplateRefsEqual(); added dra import |
| docs/phase8/dra-template-lifecycle.md | Created: full lifecycle documentation |
| docs/phase8/session-handoff.md | Updated: Session 3 results |

### Tests run

All tests pass (17 packages, 0 failures):

**New tests (52 total):**

`internal/dra/` (30 tests):
- `TestBuildProfile_NilDeviceSpec`
- `TestBuildProfile_DisabledMode`
- `TestBuildProfile_EmptyClaims`
- `TestBuildProfile_SingleClaim`
- `TestBuildProfile_Deterministic`
- `TestBuildProfile_SelectorOrderIndependent`
- `TestBuildProfile_ClaimOrderIndependent`
- `TestBuildProfile_DifferentCountProducesDifferentFingerprint`
- `TestBuildProfile_DifferentClassProducesDifferentFingerprint`
- `TestBuildProfile_MultipleClaims_DeviceClassesSorted`
- `TestBuildProfile_DuplicateDeviceClassDeduped`
- `TestBuildProfile_ContainersDoNotAffectFingerprint`
- `TestTemplateNameForClaim`
- `TestTemplateNameForClaim_Deterministic`
- `TestTemplateRefs_NilClaims`
- `TestTemplateRefs_SingleClaim`
- `TestTemplateRefs_SortedByClaimName`
- `TestBuildDesiredTemplates_NilDevices`
- `TestBuildDesiredTemplates_DisabledMode`
- `TestBuildDesiredTemplates_SingleClaim`
- `TestBuildDesiredTemplates_MultipleClaims`
- `TestBuildDesiredTemplates_Labels`
- `TestBuildDesiredTemplates_NoSelectors`
- `TestBuildDesiredTemplates_Deterministic`
- `TestTemplateSpecMatches_Identical`
- `TestTemplateSpecMatches_DifferentClass`
- `TestTemplateSpecMatches_DifferentCount`
- `TestTemplateSpecMatches_DifferentSelectorCount`
- `TestTemplateSpecMatches_DifferentRequestCount`
- `TestTemplateKey`

`internal/controller/` (22 new DRA tests):
- `TestReconcileDRATemplates_NoDevices`
- `TestReconcileDRATemplates_DisabledMode`
- `TestReconcileDRATemplates_ClearsStatusWhenDisabled`
- `TestReconcileDRATemplates_CreatesSingleTemplate`
- `TestReconcileDRATemplates_CreatesMultipleTemplates`
- `TestReconcileDRATemplates_Idempotent`
- `TestReconcileDRATemplates_SpecDriftRecreatesTemplate`
- `TestReconcileDRATemplates_SpecDriftDifferentClass`
- `TestReconcileDRATemplates_CleansUpOrphans`
- `TestReconcileDRATemplates_OwnerReference`
- `TestReconcileDRATemplates_Labels`
- `TestReconcileDRATemplates_SyncsDeviceStatus`
- `TestReconcileDRATemplates_FingerprintStableAcrossReconciles`
- `TestReconcileDRATemplates_TransitionDRAToDisabled`
- `TestIsOwnedByRTJ` (4 subtests)
- `TestSyncDeviceStatus_SetsFields`
- `TestSyncDeviceStatus_Idempotent`
- `TestClearDeviceStatus_AlreadyNil`
- `TestClearDeviceStatus_ClearsExisting`
- `TestDeviceStatusEqual` (6 subtests)
- `TestSyncDeviceStatus_PreservesAllocationFields`

### Recommended next prompt

```
You are working on Phase 8 Session 4 for the checkpoint-native preemption
controller repo.

Read docs/phase8/session-handoff.md for context (Sessions 1-3).

Session 3 implemented:
- internal/dra/profile.go: DeviceProfile fingerprinting (SHA256, order-independent)
- internal/dra/templates.go: ResourceClaimTemplate builder from DeviceClaimSpec
- internal/controller/dra_templates.go: reconcileDRATemplates() lifecycle
  (create, idempotent, spec-drift recreate, orphan cleanup, status sync)
- internal/controller/status_helpers.go: syncDeviceStatus(), clearDeviceStatus()
- 52 new tests, all passing, zero regressions

Now integrate the DRA template lifecycle into the main reconcile loop
and implement DRA-aware child JobSet rendering:

1. Wire reconcileDRATemplates() into the main Reconcile() method:
   - Call early in the reconcile loop (before launch gate evaluation)
   - Gate on worker mode (skip for ShouldSuppressRuntime)
   - Ensure templates are ready before proceeding to launch

2. Implement DRA-aware child JobSet rendering:
   - Inject spec.resourceClaims[] into worker pod template
   - Inject container resources.claims[] for targeted containers
   - Apply after topology and podSetUpdate injection
   - Use status.devices.resourceClaimTemplateRefs for template names

3. Add RBAC markers for ResourceClaimTemplate CRUD.

4. Update Kueue PodSet synthesis with DRA device request (OQ2 audit).

5. Add unit tests for rendering and integration.

6. Update docs/phase8/session-handoff.md with Session 4 results.
```
