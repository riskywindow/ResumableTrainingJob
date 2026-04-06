# Phase 8 -- Consistency Audit

**Date**: 2026-04-05
**Scope**: Implementation and documentation audited against accepted
contracts from Phases 0 through 8.

---

## 1. RTJ is the only Kueue-managed object

| Area | Status | Evidence |
|------|--------|----------|
| API spec | CONSISTENT | `spec.suspend`, `spec.queueName`, `spec.workloadPriorityClassName` are on RTJ only |
| Webhook | CONSISTENT | Kueue label projection (`kueue.x-k8s.io/queue-name`, priority class) applied to RTJ metadata only |
| Workload synthesis | CONSISTENT | `internal/kueue/rtj_generic_job.go` registers RTJ as Kueue GenericJob; child JobSet is never registered |
| Phase 8 DRA claims | CONSISTENT | ResourceClaimTemplates are helper runtime objects owned by RTJ, not Kueue workloads. Kueue accounts for DRA devices via `deviceClassMappings` on ClusterQueue ResourceGroups. No claim is ever submitted to Kueue as a workload. |
| Test coverage | CONSISTENT | `TestRenderChildJobSetNoKueueManagementOnChildWithDRA` verifies Phase 2 invariant holds with DRA |

**Verdict**: NO DRIFT.

---

## 2. Child JobSets remain plain runtime resources

| Area | Status | Evidence |
|------|--------|----------|
| Render pipeline | CONSISTENT | `internal/jobset/render.go` renders child JobSet without Kueue metadata; `stripKueueFields()` removes any stale labels/annotations |
| DRA injection | CONSISTENT | `internal/jobset/dra_render.go` injects `PodResourceClaim` entries and container `Resources.Claims` into the JobSet pod template. No Kueue annotations are added. |
| E2e assertion | CONSISTENT | `TestDRAQuotaAndAllocation` verifies child JobSet is plain runtime (no kueue.x-k8s.io labels) |
| Multi-cluster | CONSISTENT | Worker-side child JobSets created by the adapter are also plain runtime resources |

**Verdict**: NO DRIFT.

---

## 3. Native DRA claims as the core device path

| Area | Status | Evidence |
|------|--------|----------|
| API spec | CONSISTENT | `spec.devices.mode=DRA` enables native ResourceClaimTemplate + ResourceClaim path. The alpha extended-resource bridge is explicitly deferred (ADR 0001 Decision 4, Session 1 Decision 4). |
| Import path | CONSISTENT | `k8s.io/api/resource/v1beta1` used for ResourceClaimTemplate, ResourceClaim, DeviceRequest, DeviceSelector (Session 3 OQ1 resolution) |
| Template construction | CONSISTENT | `internal/dra/templates.go` builds ResourceClaimTemplate objects with `DeviceRequest` containing `DeviceClassName`, `Count`, and `CELDeviceSelector`. No extended-resource translation. |
| Phase 7 compat | CONSISTENT | When `spec.devices` is nil or mode=Disabled, no DRA objects are created, no DRA fields injected. All Phase 7 behavior preserved unchanged. |

**Verdict**: NO DRIFT.

---

## 4. Companion ResourceClaimTemplate lifecycle

| Area | Status | Evidence |
|------|--------|----------|
| Creation | CONSISTENT | `reconcileDRATemplates()` creates templates via `r.Create()` with deterministic `<rtj-name>-<claim-name>` naming |
| Ownership | CONSISTENT | Templates have ownerReference with `controller=true, blockOwnerDeletion=true` pointing to RTJ. GC'd on RTJ deletion. |
| Spec drift | CONSISTENT | `TemplateSpecMatches()` detects drift; operator deletes and recreates (Session 3 OQ7 resolution) |
| Orphan cleanup | CONSISTENT | `cleanupOrphanedTemplates()` deletes templates with RTJ label that are not in desired set |
| Labels | CONSISTENT | `training.checkpoint.example.io/rtj-name`, `claim-name`, `managed-by` labels applied |
| Reconcile position | CONSISTENT | Runs early in Reconcile, before launch gates and child JobSet creation (controller.go:137) |
| Manager suppression | CONSISTENT | `ShouldSuppressRuntime()` returns before `reconcileDRATemplates()` (controller.go:129-131), so manager never creates local templates |

**Verdict**: NO DRIFT.

---

## 5. Kueue deviceClassMappings-based quota/accounting

| Area | Status | Evidence |
|------|--------|----------|
| PodSet synthesis | CONSISTENT | `internal/kueue/rtj_podsets.go` injects DRA pod spec fields (`PodResourceClaim`, container `Resources.Claims`) into PodSets for Kueue accounting |
| Dev environment | CONSISTENT | `deploy/dev/phase8/kueue/controller_manager_config.phase8.yaml` configures `deviceClassMappings` mapping DeviceClass `example-gpu` to logical `example.dev/gpu` |
| Quota config | CONSISTENT | ClusterQueue covers `example.dev/gpu` with nominalQuota=8 |
| E2e proof | CONSISTENT | `TestDRAQuotaAndAllocation` verifies quota exhaustion blocks second RTJ, and freeing quota unblocks it |

**Verdict**: NO DRIFT.

---

## 6. Conservative device-profile-aware resume compatibility

| Area | Status | Evidence |
|------|--------|----------|
| Fingerprint | CONSISTENT | `internal/dra/profile.go` generates SHA256 fingerprint from canonical sorted device classes + selectors. Order-independent. |
| Checkpoint manifest | CONSISTENT | `DeviceProfileFingerprint` field added to `CheckpointManifest` (checkpoints/types.go). Optional with omitempty. |
| Fail-closed rule | CONSISTENT | `CheckManifestCompatibility()` rejects checkpoint if current RTJ has DRA profile and checkpoint profile differs. Checkpoint without profile + current with profile = REJECTED. |
| Downgrade compat | CONSISTENT | Checkpoint WITH profile + current WITHOUT DRA = COMPATIBLE (Phase 7 path accepts Phase 8 checkpoints) |
| Python SDK | CONSISTENT | `device_profile_fingerprint` field in manifest.py with optional serialization |
| E2e proof | CONSISTENT | `TestDRAIncompatibleResumeRejection` exercises the fail-closed path; `TestDRAResumeCompatibleProfile` exercises the matching path |

**Verdict**: NO DRIFT.

---

## 7. Preservation of earlier behavior when DRA is disabled

| Area | Status | Evidence |
|------|--------|----------|
| Nil spec.devices | CONSISTENT | When `spec.devices` is nil, `IsDevicesEnabled()` returns false, `reconcileDRATemplates()` returns TemplatesReady=true immediately, no templates created |
| Empty mode | CONSISTENT | Webhook defaults empty mode to `Disabled`. Disabled with claims is rejected by validation. |
| Checkpoint compat | CONSISTENT | When neither manifest nor request has device profile, the device-profile check is skipped entirely |
| Kueue synthesis | CONSISTENT | `injectDRAIntoPodSets()` is a no-op when devices are nil or disabled |
| JobSet rendering | CONSISTENT | `InjectDRAClaims()` with empty injections is a no-op |
| Phase 5 priority | CONSISTENT | Priority shaping is unaffected by DRA presence/absence |
| Phase 6 multi-cluster | CONSISTENT | Manager/worker mode operates identically with or without DRA |
| Phase 7 capacity | CONSISTENT | Launch gating and ProvisioningRequest are independent of DRA |

**Verdict**: NO DRIFT.

---

## 8. Test coverage audit

### Unit tests (Phase 8 specific)

| Module | Count | Coverage areas |
|--------|-------|----------------|
| api/v1alpha1 (webhook) | 20 | Defaulting, validation, deep copy, backward compat |
| internal/dra/profile | 17 | Fingerprint, determinism, order independence, naming |
| internal/dra/templates | 13 | Construction, owner refs, labels, spec matching |
| internal/dra/claims | 19 | Allocation summary, filtering, failure detection |
| internal/controller/dra_templates | 22 | Lifecycle CRUD, idempotency, drift, orphan cleanup, status sync |
| internal/controller/dra_status | 13 | Observation, allocation fields, conditions |
| internal/checkpoints/compatibility | 6 | Device profile match, mismatch, downgrade, world-size interaction |
| internal/checkpoints/selector | 3 | Profile-aware selection |
| internal/jobset/dra_render | 13 | Claim injection, targeting, idempotency |
| internal/jobset/render | 4 | DRA integration, coexistence with topology |
| internal/kueue/rtj_podsets | 8 | DRA PodSet injection, targeting, backward compat |
| internal/controller/remote_status | 10 | DRA summary, Phase 8 detection, manager suppression |
| sdk/python | 6 | Fingerprint round-trip, serialization, backward compat |
| **Total** | **154** | |

### E2e tests (Phase 8 specific)

| Test | What it proves |
|------|---------------|
| `TestDRAQuotaAndAllocation` | Full DRA launch lifecycle + Kueue quota accounting + quota exhaustion/release |
| `TestDRAResumeCompatibleProfile` | Pause/resume with matching device profile preserves fingerprint |
| `TestDRAIncompatibleResumeRejection` | Fail-closed rejection when device profile changes |
| `TestMultiClusterDRASmoke` | Manager suppression + worker DRA execution + status mirroring |

### Required coverage checklist

| Requirement | Status |
|-------------|--------|
| Unit: API/webhook changes | COVERED (20 tests) |
| Unit: DRA profile/template logic | COVERED (30 + 13 tests) |
| Unit: DRA-aware render/workload synthesis | COVERED (13 + 4 + 8 tests) |
| Unit: claim-allocation observation and checkpoint compat | COVERED (19 + 13 + 6 tests) |
| E2e: single-cluster DRA launch/allocation | COVERED (`TestDRAQuotaAndAllocation`) |
| E2e: single-cluster compatible resume | COVERED (`TestDRAResumeCompatibleProfile`) |
| E2e: incompatible-profile rejection | COVERED (`TestDRAIncompatibleResumeRejection`) |
| E2e/smoke: worker-mode multi-cluster | COVERED (`TestMultiClusterDRASmoke`) |

---

## 9. API contract alignment

| Contract clause (Phase 8 index.md) | Implementation status |
|-------------------------------------|----------------------|
| Invariant 1: RTJ only Kueue-managed | ALIGNED |
| Invariant 2: Child JobSets plain runtime | ALIGNED |
| Invariant 3: Kueue sole admission authority | ALIGNED |
| Invariant 4: Operator lifecycle owner | ALIGNED |
| Invariant 5: ProvisioningRequest capacity truth | ALIGNED (Phase 7 unchanged) |
| Invariant 6: Phase 7 behavior preserved when Phase 8 disabled | ALIGNED |
| Invariant 7: ResourceClaimTemplates are helper objects | ALIGNED |
| Invariant 8: Native DRA claims core path | ALIGNED |
| Invariant 9: Fail-closed checkpoint compatibility | ALIGNED |

---

## Summary

All 9 core invariants and the 6 Phase-8-specific design contracts are
consistent between documentation and implementation. No behavioral drift
detected. Test coverage meets minimum requirements across all audit areas.

Three implementation wiring gaps are documented in [gaps.md](gaps.md):
`observeDRAClaimStatus()`, `syncDeviceResumeFingerprint()`, and Phase 8
metric callsite emission. These are observation and telemetry gaps, not
correctness or safety issues.
