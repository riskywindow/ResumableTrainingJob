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

### Recommended next prompt (superseded by Session 4)

See Session 4 below.

---

## Session 4: DRA-aware Workload synthesis and child JobSet rendering

**Date**: 2026-04-01

### Mission

Integrate DRA into Kueue Workload PodSet synthesis and child JobSet
rendering. Wire `reconcileDRATemplates()` into the main reconcile loop.
Add RBAC markers for ResourceClaimTemplate CRUD. Ensure backward
compatibility when DRA is not configured.

### Decisions made

1. **DRA claims injected into PodSet pod templates for Kueue accounting.**
   `PodSetsFromRTJTemplate()` now adds `PodResourceClaim` entries with
   `ResourceClaimTemplateName` and container `Resources.Claims` entries
   to PodSet templates. Template names use the deterministic
   `<rtj-name>-<claim-name>` format from `dra.TemplateNameForClaim()`.

2. **Container-scoped DRA injection.** Claims are only injected into
   PodSets/replicatedJobs where at least one container matches the
   claim's `containers[]` list. This prevents over-allocation (e.g.,
   driver pods that don't need GPUs won't request them).

3. **DRA injection is the last step in the render pipeline.** Applied
   after topology constraints and podSetUpdates to ensure clean
   composition. Order: Kueue metadata stripping → admitted counts →
   env vars → volumes → topology → podSetUpdates → DRA claims.

4. **`reconcileDRATemplates()` runs early, before launch gates.**
   Placed after manager-mode check and before `getActiveJobSet()`.
   This ensures templates exist before Kueue evaluates the Workload
   for admission. When DRA is not configured, it's a no-op.

5. **Launch gated on DRA template readiness.** The controller will
   not create a child JobSet until `TemplatesReady` is true. This is
   a safety gate for the rare transient race condition where template
   creation is in progress.

6. **Child JobSet rendering uses status refs, PodSet synthesis uses
   computed names.** The rendering path reads template names from
   `status.devices.resourceClaimTemplateRefs` (populated by
   `reconcileDRATemplates()`). PodSet synthesis computes names
   deterministically from `dra.TemplateNameForClaim()` since status
   may not be populated when Kueue's webhook calls `PodSets()`.

7. **RBAC: `get;list;watch;create;delete` for ResourceClaimTemplates.**
   No `update` or `patch` -- the operator recreates on spec drift.

8. **OQ2 partially resolved: Kueue deviceClassMappings integration.**
   The synthesized PodSets now include DRA pod spec fields that Kueue
   can resolve via `deviceClassMappings`. Full e2e verification of
   Kueue accounting is deferred to the e2e session, but the data path
   is now complete.

### Open questions resolved

| OQ | Resolution |
|---|---|
| OQ2 | Partially resolved -- PodSet synthesis includes DRA fields for deviceClassMappings; e2e verification deferred |

### Open questions remaining

| OQ | Status |
|---|---|
| OQ4 | Unresolved -- Example DRA driver testing deferred to e2e session |
| OQ8 | Deferred -- multi-device-class supported at API level; single-class-per-claim is Phase 8 scope |
| OQ9 | Unresolved -- DRA + ProvisioningRequest interaction test deferred to e2e |

### Files changed

| File | Action |
|---|---|
| internal/jobset/dra_render.go | Created: DRAClaimInjection, InjectDRAClaims(), BuildDRAClaimInjections(), container matching helpers |
| internal/jobset/dra_render_test.go | Created: 13 tests for DRA injection (single/multi claim, targeting, idempotency, build logic) |
| internal/jobset/render.go | Updated: added DRAClaims field to RenderInput, call InjectDRAClaims() after podSetUpdates |
| internal/jobset/render_test.go | Updated: added 4 Phase 8 DRA render integration tests |
| internal/kueue/rtj_podsets.go | Updated: added injectDRAIntoPodSets(), container matching helpers; called from PodSetsFromRTJTemplate() |
| internal/kueue/rtj_podsets_test.go | Updated: added 8 Phase 8 DRA PodSet tests |
| internal/controller/resumabletrainingjob_controller.go | Updated: added RBAC marker for ResourceClaimTemplate; wired reconcileDRATemplates() into Reconcile(); added DRA template readiness gate |
| internal/controller/resume_flow.go | Updated: populated DRAClaims in simple launch path |
| internal/controller/launch_plan.go | Updated: populated DRAClaims in plan-based launch path |
| docs/phase8/dra-rendering-and-kueue-accounting.md | Created: full documentation of DRA rendering and accounting integration |
| docs/phase8/session-handoff.md | Updated: Session 4 results |

### Tests run

All tests pass (13 packages, 0 failures, 0 regressions):

**New tests (25 total):**

`internal/jobset/dra_render_test.go` (13 tests):
- `TestInjectDRAClaims_SingleClaim`
- `TestInjectDRAClaims_MultipleClaims`
- `TestInjectDRAClaims_TargetedContainers`
- `TestInjectDRAClaims_NoMatchingContainers`
- `TestInjectDRAClaims_EmptyClaimsIsNoOp`
- `TestInjectDRAClaims_Idempotent`
- `TestInjectDRAClaims_MultipleReplicatedJobs`
- `TestInjectDRAClaims_AllContainersWhenTargetsEmpty`
- `TestBuildDRAClaimInjections_Disabled`
- `TestBuildDRAClaimInjections_NoStatus`
- `TestBuildDRAClaimInjections_Builds`
- `TestBuildDRAClaimInjections_SkipsMissingRef`
- `TestBuildDRAClaimInjections_DisabledMode`

`internal/jobset/render_test.go` (4 new tests):
- `TestRenderChildJobSetInjectsDRAClaims`
- `TestRenderChildJobSetNoDRAWhenEmpty`
- `TestRenderChildJobSetNoKueueManagementOnChildWithDRA`
- `TestRenderChildJobSetDRAWithTopologyCoexist`

`internal/kueue/rtj_podsets_test.go` (8 new tests):
- `TestPodSetsFromRTJTemplateInjectsDRAClaims`
- `TestPodSetsFromRTJTemplateNoDRAWhenDisabled`
- `TestPodSetsFromRTJTemplateNoDRAWhenNilDevices`
- `TestPodSetsFromRTJTemplateDRATargetsCorrectContainers`
- `TestPodSetsFromRTJTemplateDRASkipsNonMatchingContainers`
- `TestPodSetsFromRTJTemplateDRATemplateNameFormat`
- `TestPodSetsFromRTJTemplateDRAMultipleClaims`
- `TestPodSetsFromRTJTemplateDRAPreservesExistingPodSetBehavior`

### Recommended next prompt

See Session 5 below.

---

## Session 5: DRA status observation and checkpoint device compatibility

**Date**: 2026-04-02

### Mission

Implement DRA claim-allocation observation and device-profile-aware
checkpoint compatibility. Surface useful RTJ status for DRA claim
lifecycle and enforce conservative fail-closed checkpoint compatibility
when DRA device profiles are active.

### Decisions made

1. **Claim allocation observation uses per-device conditions.** The DRA
   v1beta1 `ResourceClaimStatus` does not have a top-level `Conditions`
   field. Failure detection uses `AllocatedDeviceStatus.Conditions`
   (per-device conditions reported by DRA drivers). A `Ready=False`
   condition or failure-indicating reasons (`AllocationFailed`,
   `DriverError`, `Unschedulable`, `UnsatisfiedConstraints`) are
   treated as failures.

2. **Claim filtering uses RTJ labels and template annotations.** Claims
   are matched to RTJs by the `training.checkpoint.example.io/rtj-name`
   label (inherited from ResourceClaimTemplate) or by the
   `resource.kubernetes.io/claim-template-name` annotation.

3. **Checkpoint device profile is fail-closed.** When the current RTJ
   has a DRA device profile, checkpoints without a device profile are
   rejected. When both have profiles, they must match exactly. When
   neither has a profile, the check is skipped (Phase 7 preserved).

4. **Downgrade (DRA to non-DRA) is compatible.** A checkpoint saved
   with a device profile can be used when the current RTJ has no DRA
   configured. The rationale is that the checkpoint data is valid
   regardless of whether the current runtime uses DRA.

5. **Device profile fingerprint stored in manifest JSON.** The
   `deviceProfileFingerprint` field is optional with `omitempty` in
   Go and `None` default in Python. Phase 7 manifests decode without
   error.

6. **DRAClaimAllocationFailed condition.** A condition is set on the
   RTJ when claim allocation fails, and cleared when all claims are
   allocated. This provides clear status visibility.

7. **Resume request carries device fingerprint.** Both the controller's
   `resumeCheckpointForAttempt()` and `ResumeRequestFromRTJ()` populate
   `CurrentDeviceProfileFingerprint` from `status.devices`.

### Files changed

| File | Action |
|---|---|
| internal/dra/claims.go | Created: ClaimAllocationSummary, SummarizeClaimAllocations(), FilterClaimsForRTJ(), device-level failure detection |
| internal/dra/claims_test.go | Created: 19 tests for claim summarization, filtering, failure detection |
| internal/controller/dra_status.go | Created: observeDRAClaimStatus(), syncClaimAllocationFields(), syncDRAClaimConditions(), DRAClaimStatusResult |
| internal/controller/dra_status_test.go | Created: 13 tests for DRA status observation and condition management |
| internal/controller/status_helpers.go | Updated: added syncDeviceResumeFingerprint(), syncDeviceCheckpointFingerprint() |
| internal/checkpoints/types.go | Updated: added DeviceProfileFingerprint field to CheckpointManifest |
| internal/checkpoints/compatibility.go | Updated: added CurrentDeviceProfileFingerprint to ResumeRequest; added device profile compatibility check (fail-closed) |
| internal/checkpoints/compatibility_test.go | Updated: added 6 device profile compatibility tests |
| internal/checkpoints/selector.go | Updated: ResumeRequestFromRTJ() now includes device profile fingerprint from RTJ status |
| internal/checkpoints/selector_test.go | Updated: added 3 device profile selector tests |
| internal/controller/resume_flow.go | Updated: resumeCheckpointForAttempt() now includes device profile fingerprint in ResumeRequest |
| sdk/python/yield_sdk/manifest.py | Updated: added device_profile_fingerprint field with optional serialization |
| sdk/python/tests/test_manifest.py | Updated: added 6 Phase 8 device profile tests |
| docs/phase8/checkpoint-device-compatibility.md | Created: full documentation of device profile compatibility design |
| docs/phase8/session-handoff.md | Updated: Session 5 results |

### Tests run

All tests pass (15 packages, 0 failures, 0 regressions):

**New Go tests (41 total):**

`internal/dra/claims_test.go` (19 tests):
- `TestSummarizeClaimAllocations_NoClaims`
- `TestSummarizeClaimAllocations_EmptyClaims`
- `TestSummarizeClaimAllocations_AllAllocated`
- `TestSummarizeClaimAllocations_AllPending`
- `TestSummarizeClaimAllocations_MixedAllocatedAndPending`
- `TestSummarizeClaimAllocations_FailedDeviceCondition`
- `TestSummarizeClaimAllocations_MultipleFailed_PicksLatest`
- `TestSummarizeClaimAllocations_SingleAllocated`
- `TestSummarizeClaimAllocations_FailedTakesPrecedenceOverPending`
- `TestSummarizeClaimAllocations_ReadyFalseIsFailure`
- `TestFilterClaimsForRTJ_ByLabel`
- `TestFilterClaimsForRTJ_ByTemplateAnnotation`
- `TestFilterClaimsForRTJ_Empty`
- `TestFilterClaimsForRTJ_SortedByName`
- `TestIsDeviceFailureCondition_AllocationFailed`
- `TestIsDeviceFailureCondition_DriverError`
- `TestIsDeviceFailureCondition_ReadyFalse`
- `TestIsDeviceFailureCondition_ReadyTrue`
- `TestIsDeviceFailureCondition_NotFailure`

`internal/controller/dra_status_test.go` (13 tests):
- `TestObserveDRAClaimStatus_NoDevicesIsNoOp`
- `TestObserveDRAClaimStatus_DisabledIsNoOp`
- `TestObserveDRAClaimStatus_NoClaimsYet`
- `TestObserveDRAClaimStatus_AllAllocated`
- `TestObserveDRAClaimStatus_FailedClaim`
- `TestObserveDRAClaimStatus_MixedAllocatedAndPending`
- `TestObserveDRAClaimStatus_FiltersUnrelatedClaims`
- `TestSyncClaimAllocationFields_SetsAllocated`
- `TestSyncClaimAllocationFields_IdempotentForSameState`
- `TestSyncClaimAllocationFields_NilDeviceStatusIsNoOp`
- `TestSyncClaimAllocationFields_TracksFailure`
- `TestSyncDRAClaimConditions_SetsFailureCondition`
- `TestSyncDRAClaimConditions_ClearsOnSuccess`

`internal/checkpoints/compatibility_test.go` (6 new tests):
- `TestCheckManifestCompatibilitySameDeviceProfile`
- `TestCheckManifestCompatibilityDifferentDeviceProfileRejected`
- `TestCheckManifestCompatibilityCheckpointWithoutProfileRequestWithProfile`
- `TestCheckManifestCompatibilityCheckpointWithProfileRequestWithout`
- `TestCheckManifestCompatibilityBothWithoutDeviceProfile`
- `TestCheckManifestCompatibilityDeviceProfileWithWorldSizeChange`

`internal/checkpoints/selector_test.go` (3 new tests):
- `TestSelectLatestCompatibleSkipsIncompatibleDeviceProfile`
- `TestSelectLatestCompatibleDeviceProfileMatchSelected`
- `TestSelectLatestCompatibleNoDeviceProfileBackwardCompat`

**New Python tests (6 total):**

`sdk/python/tests/test_manifest.py`:
- `test_device_profile_fingerprint_round_trip`
- `test_device_profile_fingerprint_in_serialized_json`
- `test_device_profile_fingerprint_omitted_when_none`
- `test_phase7_manifest_without_device_profile_decodes`
- `test_device_profile_with_phase3_fields_coexist`
- `test_manifest_completeness_with_all_phase8_fields`

### Open questions remaining

| OQ | Status |
|---|---|
| OQ4 | Unresolved -- Example DRA driver testing deferred to e2e session |
| OQ9 | Unresolved -- DRA + ProvisioningRequest interaction test deferred to e2e |

### Recommended next prompt

See Session 6 below.

---

## Session 6: Phase 8 local dev/test profile

**Date**: 2026-04-02

### Mission

Build the local Phase 8 development/test environment using a self-contained
example DRA driver and Kueue deviceClassMappings. Provide deterministic
infrastructure for DRA integration testing without real accelerators.

### Decisions made

1. **Self-contained DRA driver via DaemonSet + shell script.** Rather than
   building the upstream `kubernetes-sigs/dra-example-driver` (requires Go
   binary, custom container, kubelet plugin socket), the dev profile uses a
   `bitnami/kubectl:1.33` DaemonSet that publishes ResourceSlice objects
   directly via the API server. Trades kubelet-level device allocation for
   simpler, zero-build setup. Sufficient for operator-level DRA integration
   testing (template lifecycle, Kueue accounting, JobSet rendering,
   checkpoint compatibility).

2. **Driver name: `dra.example.dev`.** Scoped to dev environment. Published
   in ResourceSlice `spec.driver` and matched by DeviceClass CEL selector
   `device.driver == 'dra.example.dev'`.

3. **4 simulated GPUs per node.** Each worker node gets a ResourceSlice with
   4 devices named `gpu-0` through `gpu-3`, with attributes: `model`
   (Example-GPU-v1), `memory` (80Gi), `index` (0-3). Total quota = 8 devices
   for a 2-node kind cluster.

4. **Kind node image bumped to v1.33.0 for stable DRA.** The
   `PHASE8_KIND_NODE_IMAGE` variable defaults to `kindest/node:v1.33.0`.
   The base dev stack (`dev-up.sh`) accepts `KIND_NODE_IMAGE` override,
   which Phase 8 wires through. Existing dev profiles are unaffected
   (they continue to use the default `kindest/node:v1.31.2`).

5. **Kueue config: `deviceClassMappings` + `DynamicResourceAllocation`
   feature gate.** Maps DeviceClass `example-gpu` to logical resource
   `example.dev/gpu`. ClusterQueue covers `example.dev/gpu` with
   nominalQuota=8. Kueue resolves DRA pod claims through the mapping.

6. **ResourceFlavor and queues are Phase 8-specific.** Named `phase8-flavor`,
   `phase8-cq`, `phase8-training` to avoid colliding with other phase
   profiles that may be active on the same cluster.

7. **Three sample RTJs cover the core test matrix:**
   - `rtj-dra-launch.yaml`: successful DRA-backed launch (2 GPUs per worker)
   - `rtj-dra-pause-resume.yaml`: pause/resume with same device profile
     (compatible fingerprint)
   - `rtj-dra-incompatible-profile.yaml`: different DeviceClass triggers
     fail-closed checkpoint rejection

8. **Smoke test validates 11 infrastructure checks.** Covers DRA API
   availability, driver readiness, DeviceClass, ResourceSlices, Kueue
   deviceClassMappings, queue health, RTJ CRD, and sample manifest
   dry-run. Does not run e2e RTJ lifecycle tests.

9. **OQ4 resolved: example DRA driver for local dev.** The self-contained
   driver satisfies OQ4 for local testing. Full kubelet-level allocation
   testing with the upstream driver is deferred to CI/production profiles.

### Open questions resolved

| OQ | Resolution |
|---|---|
| OQ4 | Resolved -- self-contained example DRA driver with simulated devices for local dev |

### Open questions remaining

| OQ | Status |
|---|---|
| OQ9 | Unresolved -- DRA + ProvisioningRequest interaction test deferred to e2e |

### Files changed

| File | Action |
|---|---|
| deploy/dev/phase8/dra-driver/00-namespace.yaml | Created: DRA driver namespace |
| deploy/dev/phase8/dra-driver/05-device-class.yaml | Created: DeviceClass example-gpu |
| deploy/dev/phase8/dra-driver/10-service-account.yaml | Created: Driver ServiceAccount |
| deploy/dev/phase8/dra-driver/15-rbac.yaml | Created: Driver ClusterRole + ClusterRoleBinding |
| deploy/dev/phase8/dra-driver/20-daemonset.yaml | Created: Driver DaemonSet (ResourceSlice publisher) |
| deploy/dev/phase8/kueue/controller_manager_config.phase8.yaml | Created: Kueue config with deviceClassMappings |
| deploy/dev/phase8/queues/00-resource-flavor.yaml | Created: Phase 8 ResourceFlavor |
| deploy/dev/phase8/queues/10-cluster-queue.yaml | Created: ClusterQueue with example.dev/gpu quota |
| deploy/dev/phase8/queues/20-local-queue.yaml | Created: LocalQueue phase8-training |
| deploy/dev/phase8/samples/rtj-dra-launch.yaml | Created: DRA launch sample |
| deploy/dev/phase8/samples/rtj-dra-pause-resume.yaml | Created: Pause/resume sample |
| deploy/dev/phase8/samples/rtj-dra-incompatible-profile.yaml | Created: Incompatible profile sample |
| hack/dev/install-phase8-profile.sh | Created: Full Phase 8 profile installer |
| hack/dev/phase8-profile.sh | Created: Thin wrapper for re-apply |
| hack/dev/install-phase8-dra-driver.sh | Created: DRA driver installer |
| hack/dev/phase8-smoke.sh | Created: Infrastructure smoke test (11 checks) |
| Makefile | Updated: Phase 8 variables and targets |
| docs/phase8/dev-environment.md | Created: Full dev environment documentation |
| docs/phase8/session-handoff.md | Updated: Session 6 results |

### Makefile targets added

| Target | Description |
|---|---|
| `make phase8-up` | Full environment: kind v1.33 + base stack + Phase 8 profile |
| `make phase8-down` | Delete kind cluster |
| `make phase8-status` | Show DRA driver, DeviceClass, ResourceSlices, queues |
| `make phase8-load-images` | Load images into kind |
| `make phase8-smoke` | 11-check infrastructure validation |
| `make phase8-profile` | Re-apply Phase 8 profile on existing cluster |

### Recommended next prompt

```
You are working on Phase 8 Session 7 for the checkpoint-native preemption
controller repo.

Read docs/phase8/session-handoff.md for context (Sessions 1-6).

Session 6 implemented:
- Local dev/test profile with self-contained example DRA driver
- Kueue deviceClassMappings configuration
- DRA-aware ClusterQueue with example.dev/gpu quota
- 3 sample RTJs (launch, pause/resume, incompatible profile)
- 11-check infrastructure smoke test
- Makefile targets: phase8-up/down/status/load-images/smoke/profile
- OQ4 resolved (example DRA driver for local dev)

Remaining Phase 8 work:
1. Wire observeDRAClaimStatus() into the main Reconcile() loop
   (call after reconcileDRATemplates(), update status, handle
   requeue for pending allocations)
2. Wire syncDeviceResumeFingerprint() into resume flow
3. DRA + ProvisioningRequest interaction e2e test (OQ9)
4. Full e2e test with Kueue deviceClassMappings accounting
5. Phase 8 integration e2e tests (DRA launch, pause/resume,
   incompatible profile rejection)
```

---

## Session 7: Single-cluster e2e test coverage

**Date**: 2026-04-03

### Goals

Implement deterministic single-cluster e2e coverage for DRA-backed launch,
DRA-aware quota/accounting, and DRA-aware resume. Three strong e2e tests
rather than many shallow ones.

### What was implemented

#### 1. E2e test: DRA quota and allocation (`TestDRAQuotaAndAllocation`)

**File**: `test/e2e/dra_quota_and_allocation_test.go`

Exercises the full DRA launch lifecycle:
- Submits a DRA-backed RTJ requesting 2 GPUs per worker
- Verifies ResourceClaimTemplates are created with correct ownership and spec
- Verifies status.devices is populated (fingerprint, template refs)
- Verifies Workload is created and admitted by Kueue with DRA quota accounting
- Verifies RTJ transitions to Running after DRA allocation
- Verifies child JobSet is plain runtime (Phase 2 invariant)
- Submits a second quota-hog RTJ (4 GPUs per worker = 8 total, exhausting quota)
- Verifies the second RTJ stays Queued while quota is exhausted
- Deletes first RTJ to free quota
- Verifies second RTJ is admitted and reaches Running

#### 2. E2e test: DRA compatible resume (`TestDRAResumeCompatibleProfile`)

**File**: `test/e2e/dra_resume_compatible_profile_test.go`

Exercises the pause/resume cycle with matching device profiles:
- Submits a DRA-backed RTJ, waits for Running
- Records device profile fingerprint from status.devices
- Waits for a checkpoint to be saved
- Pauses the RTJ (desiredState=Paused)
- Verifies checkpoint manifest in S3 includes deviceProfileFingerprint
- Resumes the RTJ (desiredState=Running)
- Verifies resume succeeds (run attempt 2)
- Verifies device profile fingerprint is preserved across pause/resume
- Verifies child JobSet is recreated as plain runtime

#### 3. E2e test: DRA incompatible resume rejection (`TestDRAIncompatibleResumeRejection`)

**File**: `test/e2e/dra_incompatible_resume_rejection_test.go`

Exercises the fail-closed device profile compatibility check:
- Creates a checkpoint under "example-gpu" device profile
- Verifies checkpoint manifest has device profile fingerprint
- Submits a second RTJ with "example-gpu-alt" (different DeviceClass)
- Verifies fingerprints differ between the two RTJs
- Verifies the incompatible checkpoint is NOT selected for resume
- Verifies clear status surfacing (Degraded condition / Failed phase)
- Documents the comprehensive unit test complement

#### 4. Test infrastructure

- **`test/e2e/phase8_helpers_test.go`**: Phase 8 helpers including
  `phase8RTJView` with DRA status fields, `setupPhase8Env()` with
  Phase 8 infrastructure validation, ResourceClaimTemplate/ResourceClaim
  query helpers, Phase 8 Workload helpers, cleanup functions
- **`test/e2e/testdata/phase8/`**: 5 YAML fixtures for DRA e2e tests

#### 5. Documentation

- **`docs/phase8/e2e.md`**: Explains what each test proves, test
  infrastructure requirements, and what remains deferred

### Test data files

| File | Purpose |
|---|---|
| test/e2e/testdata/phase8/rtj-dra-launch.yaml | DRA-backed RTJ (2 GPUs, launch test) |
| test/e2e/testdata/phase8/rtj-dra-quota-hog.yaml | DRA RTJ (4 GPUs/worker, quota exhaustion) |
| test/e2e/testdata/phase8/rtj-dra-pause-resume.yaml | DRA RTJ (pause/resume with same profile) |
| test/e2e/testdata/phase8/rtj-dra-incompatible-profile.yaml | DRA RTJ (example-gpu-alt, incompatible) |
| test/e2e/testdata/phase8/localqueue-hold-phase8.yaml | LocalQueue with hold policy |

### Design decisions

1. **Three strong tests over many shallow ones**: Each test exercises a
   distinct Phase 8 integration surface end-to-end. The quota test proves
   the Kueue/DRA accounting pipeline. The resume test proves checkpoint
   fingerprint preservation. The rejection test proves fail-closed safety.

2. **Shared test helpers pattern**: `phase8_helpers_test.go` follows the
   established pattern from Phases 3-7 with phase-specific RTJ view
   structs, environment setup, and polling helpers.

3. **kubectl-based assertions**: All status checks use kubectl JSON
   output, matching the project's e2e convention. No direct API client
   imports in e2e tests.

4. **Deterministic via fixture control**: Tests control determinism
   through queue quotas, unique RTJ names, and polling-based state
   assertions (not sleep-based).

5. **Quota-hog approach for exhaustion**: Instead of creating many small
   RTJs, a single hog RTJ requests 4 GPUs per worker (8 total) to
   exhaust the 8-device quota in one shot. This is deterministic and
   fast.

### What remains for Session 8+

1. **Wire `observeDRAClaimStatus()`** into the main Reconcile loop
   (currently the observation logic exists but is not called during
   reconciliation)
2. **Wire `syncDeviceResumeFingerprint()`** into the resume flow
3. **DRA + ProvisioningRequest interaction** e2e test (OQ9)
4. **Multi-cluster DRA e2e** (deferred)
5. **Device failure recovery e2e** (deferred; unit-tested)

### Recommended next prompt

```
You are working on Phase 8 Session 8 for the checkpoint-native preemption
controller repo.

Read docs/phase8/session-handoff.md for context (Sessions 1-7).

Session 7 implemented:
- 3 single-cluster e2e tests: DRA quota/allocation, compatible resume,
  incompatible resume rejection
- Phase 8 test helpers and 5 YAML test fixtures
- docs/phase8/e2e.md documenting coverage and deferrals

Remaining Phase 8 work:
1. Wire observeDRAClaimStatus() into the main Reconcile() loop
2. Wire syncDeviceResumeFingerprint() into resume flow
3. DRA + ProvisioningRequest interaction e2e test (OQ9)
4. Multi-cluster DRA e2e (deferred)
```
