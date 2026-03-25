# Phase 3 Consistency Audit

**Auditor:** automated session
**Date:** 2026-03-24
**Basis:** Phase 0 contract pack, Phase 1 signoff, Phase 2 signoff, Phase 3 locked design (ADR 0001-0003, goals.md, architecture.md, api.md, admission-materialization.md, flavor-aware-rendering.md, checkpoint-resharding.md, partial-admission.md)
**Scope:** All Go source in `api/`, `internal/`, `cmd/`, all Python source in `sdk/python/`, all test files, all Phase 3 documentation.

---

## 1. Phase 0 Contract Preservation

| Phase 0 Invariant | Phase 3 Status | Evidence |
| --- | --- | --- |
| Single cluster only | **PRESERVED** | No MultiKueue references in code or config |
| Kueue is authority for queueing, admission, preemption intent | **PRESERVED** | RTJ remains only Kueue-managed object; Kueue drives suspend/admit |
| JobSet as only runtime primitive | **PRESERVED** | `RenderChildJobSetUnstructured` is the sole runtime renderer |
| PyTorch DCP as only checkpoint mechanism | **PRESERVED** | SDK checkpoint.py uses `dcp.save()`/`dcp.load()`; no alternative paths |
| S3-compatible storage only | **PRESERVED** | All storage URIs begin `s3://`; MinIO is the test target |
| Graceful yield at step boundaries only | **PRESERVED** | `SafePointMode: StepBoundary` remains the only mode |
| Single active runtime invariant | **PRESERVED** | `createIfMissing` pattern in resume_flow.go prevents duplicate child JobSets |
| Incomplete checkpoint rejection | **PRESERVED** | `CheckManifestCompatibility` rejects manifests without `CompletionTimestamp` |
| Checkpoint lineage | **PRESERVED** | Resume selection requires cluster identity, RTJ identity exact match |
| Fail-closed resume | **PRESERVED** | `NoCompatibleCheckpoint` → `Failed` phase; no fallback to partial state |

### Resume Compatibility Dimensions

Phase 0 locks ten strict dimensions. Phase 3 relaxes exactly one, under explicit opt-in.

| Dimension | Phase 3 Rule | Evidence |
| --- | --- | --- |
| Cluster identity | Exact match | compatibility.go line 54 |
| RTJ lineage identity | Exact match | compatibility.go line 57 |
| Runtime mode | Exact match | compatibility.go line 60 |
| **World size** | **Exact match unless `AllowWorldSizeChange=true` AND `CrossSizeRestoreSupported=true`** | compatibility.go lines 43-52 |
| GPU shape | Exact match | compatibility.go line 63 |
| Image identity | Exact match | compatibility.go line 66 |
| Code version | Exact match | compatibility.go line 69 |
| Checkpoint format version | Exact match | compatibility.go line 72 |
| Optimizer mode | Exact match | compatibility.go line 40 (via strict fields) |
| Sharding mode | Exact match | compatibility.go line 40 (via strict fields) |

**Verdict: ALIGNED.** World size relaxation requires double opt-in (RTJ spec + manifest flag). All other dimensions untouched.

---

## 2. Phase 1 Contract Preservation

| Phase 1 Invariant | Phase 3 Status | Evidence |
| --- | --- | --- |
| Manual pause/resume via `spec.control.desiredState` | **PRESERVED** | No changes to control field semantics; Phase 3 tests use same patch pattern |
| Control file transport (mounted ConfigMap) | **PRESERVED** | render.go still mounts control ConfigMap at `/var/run/yield-sdk/control` |
| Checkpoint manifest-last publication | **PRESERVED** | SDK checkpoint.py writes manifest after artifacts; trainer fixture unchanged |
| Yield marker evidence required | **PRESERVED** | suspend_flow.go still polls for yield marker before completing pause |
| Phase transitions (11 phases) | **PRESERVED** | No new phases added in Phase 3; all transitions use existing `SetPhase` |

**Verdict: ALIGNED.** Phase 3 adds no new lifecycle phases and does not alter the pause/resume protocol.

---

## 3. Phase 2 Contract Preservation

| Phase 2 Invariant | Phase 3 Status | Evidence |
| --- | --- | --- |
| RTJ is only Kueue-managed admission object | **PRESERVED** | rtj_generic_job.go implements GenericJob; child JobSet has no Kueue labels |
| `spec.suspend` is Kueue admission gate | **PRESERVED** | Webhook defaults suspend=true; Kueue toggles it |
| `spec.control.desiredState` is manual intent | **PRESERVED** | Separate from suspend; no cross-wiring |
| Child JobSet is plain runtime resource | **PRESERVED** | `stripKueuePodTemplateLabels()` in flavor_injection.go; `assertChildJobSetPlainRuntime()` in e2e |
| Non-controller owner reference on child JobSet | **PRESERVED** | `SetOwnerReference` (not `SetControllerReference`) in resume_flow.go line 186 |
| Workload owned by RTJ, not child JobSet | **PRESERVED** | TestAdmissionMaterialization verifies no Workload owns child JobSet |
| Graceful yield converges for Kueue and manual sources | **PRESERVED** | Same `reconcileStopFlow` path for both sources |

**Verdict: ALIGNED.** All Phase 2 ownership and management invariants are preserved.

---

## 4. Phase 3 Design Compliance

### G1: Admission-Aware Runtime Launch

| Design Requirement | Status | Evidence |
| --- | --- | --- |
| `RunWithPodSetsInfo` stores admitted counts as bridge annotation | **IMPLEMENTED** | rtj_generic_job.go stores JSON `{"worker": N}` annotation |
| Controller reads annotation and adjusts child JobSet replicas | **IMPLEMENTED** | resume_flow.go `parseAdmittedCounts()` → render.go `applyAdmittedReplicaCount()` |
| `status.admission.admittedWorkerCount` populated | **IMPLEMENTED** | resume_flow.go calls `syncAdmissionStatus()` with total admitted count |
| `status.admission.preferredWorkerCount` populated | **IMPLEMENTED** | Passes `job.EffectivePreferredCount()` to `syncAdmissionStatus()` |
| Phase 2 RTJs (no annotation) get original behavior | **IMPLEMENTED** | `parseAdmittedCounts()` returns nil → render uses template replicas unchanged |

**Verdict: G1 COMPLETE.**

### G2: Flavor-Aware Child JobSet Rendering

| Design Requirement | Status | Evidence |
| --- | --- | --- |
| `podset.Merge` injects nodeSelector and tolerations from ResourceFlavor | **IMPLEMENTED** | rtj_generic_job.go calls `podset.Merge()` which applies PodSetInfo fields |
| Kueue management labels stripped from child JobSet pod template | **IMPLEMENTED** | `stripKueuePodTemplateLabels()` in flavor_injection.go |
| E2E verifies pods land on correct flavor nodes | **IMPLEMENTED** | TestFlavorAwareLaunch checks `checkpoint-native.dev/pool` on pod nodes |
| `status.admission.admittedFlavors` populated from Workload | **NOT WIRED** | `syncAdmissionStatus()` receives `nil` for flavors in both launch and resume paths |
| `RenderInput.AdmittedFlavor` carries flavor name to renderer | **NOT WIRED** | Field exists but is always empty string |
| `YIELD_SDK_ADMITTED_FLAVOR` env var set on pods | **CONDITIONALLY WIRED** | render.go injects it, but value is always empty because RenderInput.AdmittedFlavor is empty |

**Verdict: G2 SUBSTANTIALLY COMPLETE.** The critical path (nodeSelector/tolerations → pod placement) works. The observability path (flavor name in status and env var) is scaffolded but not wired. This is documented in session-handoff.md as OQ-9.

### G3: World-Size-Flexible Resume

| Design Requirement | Status | Evidence |
| --- | --- | --- |
| `spec.resume.allowWorldSizeChange` field | **IMPLEMENTED** | resumabletrainingjob_types.go, validated in webhook |
| Compatibility checker accepts cross-size when flags set | **IMPLEMENTED** | compatibility.go three-way decision logic, 8 unit tests |
| `CrossSizeRestoreSupported` field on manifest | **IMPLEMENTED** | Go types.go and Python manifest.py; nil treated as false |
| DCP resharding in Python SDK | **IMPLEMENTED** | checkpoint.py handles `RESTORE_MODE_RESHARD`, skips RNG |
| `status.restore` records restore mode and world sizes | **IMPLEMENTED** | `syncRestoreStatus()` in status_helpers.go |
| Phase 3 env vars injected (`YIELD_SDK_WORLD_SIZE`, `YIELD_SDK_ORIGINAL_WORLD_SIZE`, `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE`) | **IMPLEMENTED** | render.go lines 119-128, gated on `OriginalWorldSize > 0` |
| E2E verifies pause/resume cycle with `allowWorldSizeChange=true` | **IMPLEMENTED** | TestFlexibleResume exercises same-size path |

**Verdict: G3 COMPLETE for same-size path.** Different-size path is validated by unit tests (28+) across compatibility, selector, PodSet, and SDK packages. Live different-size e2e requires the experimental partial admission profile.

### G4: Experimental Partial Admission

| Design Requirement | Status | Evidence |
| --- | --- | --- |
| Operator flag `--enable-experimental-partial-admission` | **IMPLEMENTED** | cmd/operator/main.go line 44 |
| Per-job field `spec.parallelism.enablePartialAdmission` | **IMPLEMENTED** | resumabletrainingjob_types.go line 259 |
| Double gating (both required) | **IMPLEMENTED** | rtj_podsets.go checks both flags; validation requires allowWorldSizeChange |
| `PodSet.MinCount` synthesized for worker pod set only | **IMPLEMENTED** | rtj_podsets.go lines 61-65; 8 unit tests |
| `PreferredCount` overrides template replica count | **IMPLEMENTED** | rtj_podsets.go lines 55-58 |
| Off by default | **VERIFIED** | Operator flag defaults to false; per-job field defaults to false |

**Verdict: G4 COMPLETE as experimental.** The full scaffolding, gating, and unit test coverage are in place. Live e2e testing requires the experimental profile and operator flag.

---

## 5. Cross-Cutting Concerns

### Backward Compatibility

| Scenario | Behavior | Evidence |
| --- | --- | --- |
| Phase 2 RTJ (no `spec.parallelism`, no `allowWorldSizeChange`) | Identical to Phase 2 | `parseAdmittedCounts()` returns nil; `EffectivePreferredCount()` returns worldSize; no Phase 3 env vars injected |
| Phase 2 checkpoint (no `CrossSizeRestoreSupported`) | nil treated as false; cross-size rejected | compatibility.go line 49 |
| Phase 2 cluster (no Phase 3 queue/flavors) | Phase 3 tests auto-skip | `setupPhase3Env()` checks for `phase3-cq` |

### Metrics

| Phase 3 Metric | Wired | Called |
| --- | --- | --- |
| `admission_comparisons_total` | Yes | Yes — reconcileLaunch, reconcileResume |
| `reshard_restores_attempted_total` | Yes | Yes — via ObserveResumeWorldSize |
| `reshard_restores_succeeded_total` | Yes | **No** — IncReshardRestoreSucceeded defined but never called |
| `reshard_restores_failed_total` | Yes | **No** — IncReshardRestoreFailed defined but never called |
| `flavor_assignments_total` | Yes | **No** — ObserveFlavorAssignment defined but never called |
| `partial_admission_launches_total` | Yes | Yes — via ObserveAdmissionComparison |
| `same_size_resumes_total` | Yes | Yes — via ObserveResumeWorldSize |
| `different_size_resumes_total` | Yes | Yes — via ObserveResumeWorldSize |

### Documentation

| Document | Accuracy | Notes |
| --- | --- | --- |
| goals.md | Accurate | All four goals match implementation |
| architecture.md | Mostly accurate | References `admittedFlavors` in status which is always nil |
| api.md | Accurate | All types match code |
| admission-materialization.md | Accurate | AdmissionView correctly described |
| flavor-aware-rendering.md | Mostly accurate | Describes AdmittedFlavor in RenderInput but notes it is not yet populated |
| checkpoint-resharding.md | Accurate | Matches Python SDK and Go implementation |
| partial-admission.md | Accurate | Double-gating matches code |
| demo.md | Accurate | Command sequences verified against Makefile targets |
| operations.md | Accurate | Inspection procedures verified against scripts |
| troubleshooting.md | Accurate | Covers known failure modes |

---

## 6. Findings Summary

### No Contradictions Found

No Phase 3 implementation contradicts Phase 0, Phase 1, or Phase 2 contracts.

### Drift Items (Design vs. Implementation)

| ID | Area | Design Says | Implementation Does | Severity |
| --- | --- | --- | --- | --- |
| D-1 | `admittedFlavors` | Populated from Workload admission | Always nil | Medium |
| D-2 | `YIELD_SDK_ADMITTED_FLAVOR` | Carries assigned flavor name | Always empty string | Low |
| D-3 | Reshard restore success/fail metrics | Defined | Never incremented | Low |
| D-4 | `AdmissionView` as central controller abstraction | Used by controller | Used only by Kueue bridge; controller uses annotation | Low |

All drift items are documented in session-handoff.md (OQ-9) and gaps.md. None represent correctness bugs; they are incomplete wiring for observability features.

### Vague Language Tightened

| Location | Before | After (Concrete Rule) |
| --- | --- | --- |
| compatibility.go | "world-size-flexible" | Three-way decision: exact match → pass; mismatch + no AllowWorldSizeChange → reject; mismatch + AllowWorldSizeChange + no CrossSizeRestoreSupported → reject; mismatch + both flags → pass |
| syncRestoreStatus | "computes restore mode" | `if checkpointWorldSize != restoreWorldSize { Reshard } else { SameSize }` — no ambiguity |
| applyAdmittedReplicaCount | "scales replicas" | `replicas = max(1, admittedPodCount / podsPerReplica)` — integer division, floor clamped to 1 |
| Phase 3 env var gating | "when admission data present" | `if OriginalWorldSize > 0` — zero value means Phase 2 path, no env vars |
| Partial admission activation | "experimental and gated" | Both `experimentalPartialAdmissionEnabled` (operator) AND `spec.parallelism.enablePartialAdmission` (per-job) must be true; validation also requires `allowWorldSizeChange: true` and `minCount` set |
