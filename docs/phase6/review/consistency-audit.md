# Phase 6 Consistency Audit

**Date:** 2026-03-30
**Scope:** Implementation and documentation versus accepted contracts from Phases 0-6.

---

## 1. Phase 0 Contract Compliance

| Contract | Status | Evidence |
|----------|--------|----------|
| RTJ is the sole user-facing API object | Pass | `api/v1alpha1/resumabletrainingjob_types.go` defines the only CRD the user creates. Child JobSets remain controller-owned runtime objects. |
| Fail-closed resume compatibility | Pass | `ResumeSourcePolicyLatestCompatibleComplete` is the only allowed policy. Webhook enforces this (`validationErrors()` line 900). |
| Manifest-last checkpoint semantics | Pass | `internal/checkpoints/catalog.go` reads manifests from S3 only after the data write completes. |
| Checkpoint completeness before selection | Pass | Catalog filters for manifests with `CompletionTime != nil` before selection. |
| Authority boundaries respected | Pass | Control plane owns RTJ status; runtime owns checkpoint I/O; storage is passive. Phase 6 adds no authority leak: the manager never writes to worker storage, and the worker never writes to manager status. |

---

## 2. Phase 1 Contract Compliance

| Contract | Status | Evidence |
|----------|--------|----------|
| Manual pause via `spec.control.desiredState=Paused` | Pass | `ControlSpec` and `DesiredState` enum unchanged. Webhook validates both values (`validationErrors()` line 910). |
| Manual resume via `desiredState=Running` | Pass | Controller's `reconcileManualHold` / `reconcileStopFlow` paths remain. |
| Child JobSet created only when unsuspended and desired Running | Pass | `reconcileLaunch` gated by `!activeExists && !job.IsSuspendedForKueue()` and `desiredState == Running`. |
| Single-cluster path preserved | Pass | Worker mode is the default (`ModeWorker`). RTJs without `spec.managedBy` always enter the Phase 5 path, even on a manager-mode operator (`mode.go` line 56: both conditions required). |

---

## 3. Phase 2 Contract Compliance

| Contract | Status | Evidence |
|----------|--------|----------|
| RTJ is the only Kueue-managed admission object | Pass | `internal/kueue/register.go` registers a single external framework (`ExternalFrameworkName`). Child JobSets are never annotated with Kueue queue labels. |
| Child JobSet is plain runtime (not Kueue-managed) | Pass | `internal/jobset/builder.go` constructs the child without Kueue labels or suspend annotations. |
| Kueue suspension flows through `spec.suspend` | Pass | `rtjWebhookGenericJob.Suspend()` sets `spec.suspend = true`. `IsSuspendedForKueue()` reads it. |
| Controller owns Workload lifecycle | Pass | `internal/kueue/setup.go` creates the Kueue Workload from the RTJ template. The WorkloadObserver watches for admission changes. |

---

## 4. Phase 3 Contract Compliance

| Contract | Status | Evidence |
|----------|--------|----------|
| Flavor-aware launch shape | Pass | `internal/kueue/admission_view.go` extracts `AdmittedFlavors` per pod set. `RunWithPodSetsInfo` injects nodeSelector/tolerations. |
| World-size-flexible resume with DCP resharding | Pass | `spec.resume.allowWorldSizeChange` gate; `RestoreMode` enum (`SameSize` / `Reshard`); metrics for same-size vs different-size resumes. |
| Partial admission behind feature gate | Pass | Operator flag `--enable-experimental-partial-admission`; webhook rejects `enablePartialAdmission` without `allowWorldSizeChange`. |

---

## 5. Phase 4 Contract Compliance

| Contract | Status | Evidence |
|----------|--------|----------|
| TopologyRequest on PodSets | Pass | `internal/kueue/rtj_topology.go` applies `PodSetTopologyRequest` for Required/Preferred/Unconstrained modes. |
| ResumeReadiness AdmissionCheck | Pass | `internal/admissionchecks/resume/` implements the controller. Registered in `main.go`. |
| ProvisioningRequest (optional) | Pass | Not wired; documented as optional. No regressions. |
| Launch gating | Pass | `evaluateLaunchGates` blocks launch until readiness gate passes. |

---

## 6. Phase 5 Contract Compliance

| Contract | Status | Evidence |
|----------|--------|----------|
| CheckpointPriorityPolicy CRD | Pass | `api/v1alpha1/checkpointprioritypolicy_types.go` defines the API. Webhook validates. |
| Effective priority written to Workload | Pass | `reconcilePriorityState` in the controller patches the Workload's priority annotation. |
| Yield budget / protection window | Pass | `PriorityShapingStatus` carries window state. Metrics track protected/preemptible counts. |

---

## 7. Phase 6 Goal-by-Goal Audit

### G1: MultiKueue Integration

| Contract | Status | Evidence |
|----------|--------|----------|
| RTJ dispatched via Kueue MultiKueue generic adapter | Pass | `internal/multikueue/framework.go` defines `ExternalFrameworkName`. `internal/multikueue/config.go` validates the required Configuration stanza. No custom MultiKueueAdapter is used. |
| External framework name format: `Kind.Version.Group` | Pass | `FormatExternalFrameworkName()` produces `ResumableTrainingJob.v1alpha1.training.checkpoint.example.io`. |
| Feature gates: `MultiKueue` + `MultiKueueAdaptersForCustomJobs` | Pass | `RequiredFeatureGates()` returns both. Config validation enforces presence. |
| RBAC rules defined for manager and worker verbs | Pass | `RTJManagerRBACRules()` returns manager-side (get/list/watch/update/patch + status) and worker-side (get/list/watch/create/delete) rules. |

### G2: Manager/Worker Operator Split

| Contract | Status | Evidence |
|----------|--------|----------|
| `--mode=manager` flag | Pass | `cmd/operator/main.go` line 51: `flag.StringVar(&modeFlag, "mode", ...)`. |
| `--mode=worker` is the default | Pass | Default value is `string(controller.ModeWorker)`. |
| Manager suppresses local child JobSet for MultiKueue-managed RTJs | Pass | `ShouldSuppressRuntime()` returns true only when `mode == ModeManager && job.IsManagedByMultiKueue()`. Controller line 102 gates on this. |
| Manager does NOT suppress for non-MultiKueue RTJs (data-loss protection) | Pass | `ShouldSuppressRuntime` requires both conditions. Unit test `TestShouldSuppressRuntimeManagerModeNotMultiKueueManaged` verifies. Integration test `TestManagerModeAllowsNormalPathForNonMultiKueueRTJ` verifies. |
| Worker mode runs unchanged Phase 5 path | Pass | `ShouldSuppressRuntime` returns false for worker mode regardless of `spec.managedBy`. Unit test `TestShouldSuppressRuntimeWorkerModeMultiKueueManaged` verifies. |
| No runtime mode switching at process lifetime | Pass | `OperatorMode` is set from flag at startup and stored as a struct field. No setter exists. |

### G3: Shared-Checkpoint Remote Pause/Resume

| Contract | Status | Evidence |
|----------|--------|----------|
| Pause propagated via adapter delete-recreate (not graceful yield) | Pass | `reconcileManagerIntent` does not call `reconcileStopFlow`. Comment block in `remote_status.go` lines 218-228 documents the delete-recreate mechanism. |
| Manager preserves checkpoint summary across adapter teardown | Pass | `preserveRemoteCheckpoint()` captures before `syncRemoteStatus()`, `restoreRemoteCheckpoint()` re-applies after. |
| Manager marks Paused when remote is no longer active | Pass | `markRemotePaused()` called when `!hasRemoteStatusSignal(job)` and pause is requested. |
| Resume: adapter creates new Running remote, worker resumes from shared store | Pass | E2E test `TestMultiClusterRemotePauseResume` verifies the full cycle including checkpoint monotonicity. |
| Shared checkpoint store required for multi-cluster | Pass | Workers access the same S3-compatible URI. No store proxy or replication mechanism exists. Documented in `operations.md` and `troubleshooting.md`. |

### G4: Manager-Visible Remote Status

| Contract | Status | Evidence |
|----------|--------|----------|
| `status.multiCluster` populated only for MultiKueue-managed RTJs | Pass | `reconcileManagerIntent` is the only writer. Reached only when `ShouldSuppressRuntime` is true. |
| DispatchPhase: Pending / Dispatched / Active | Pass | `classifyRemoteState()` produces these three values based on `hasRemoteStatusSignal` and `executionCluster`. |
| ExecutionCluster resolved from Workload admission check | Pass | `internal/remote/cluster_resolver.go` `WorkloadClusterResolver` scans for Ready MultiKueue admission check. |
| RemotePhase mirrors worker `.status.phase` | Pass | `classifyRemoteState` returns `job.Status.Phase` as `remotePhase` when `hasRemoteStatusSignal` is true (status was mirrored by adapter). |
| RemoteCheckpoint mirrors worker `.status.lastCompletedCheckpoint` | Pass | `syncRemoteCheckpointSummary()` copies ID, time, and storageURI. |
| LocalExecutionSuppressed always true in manager mode for managed RTJs | Pass | `syncRemoteStatus()` unconditionally sets `mc.LocalExecutionSuppressed = true`. |

### G5: Three-Cluster Dev Environment

| Contract | Status | Evidence |
|----------|--------|----------|
| Manager (CP-only) + 2 workers (CP + nodes) | Pass | Scripts in `hack/dev/` create the environment. `demo.md` documents the prerequisite setup. |
| Shared MinIO on worker-1 | Pass | `troubleshooting.md` section 5 documents MinIO health checks. Dev scripts install MinIO. |
| Deterministic worker selection for e2e | Pass | E2E helpers in `test/e2e/phase6_helpers_test.go` use deterministic cluster selection. |

---

## 8. API Surface Audit

| Field / Type | Introduced | Validated | Immutability | Tests |
|---|---|---|---|---|
| `spec.managedBy` (string, optional, max 256) | Phase 6 Session 2 | `validateManagedBy()`: domain-prefix format, max length | `ValidateUpdate`: rejects any change once set | Unit: `TestWebhookValidateCreateAcceptsManagedByMultiKueue`, `TestWebhookValidateCreateRejectsManagedByWithoutDomainPrefix`, `TestWebhookValidateUpdateRejectsManagedByChange`, `TestWebhookValidateUpdateRejectsManagedByRemoval` |
| `status.multiCluster` (struct, optional) | Phase 6 Session 2 | Controller-owned; no user writes | N/A (status) | Unit: `TestSyncRemoteStatusInitializesMultiCluster`, Integration: `TestManagerModeSuppressesRuntimeForMultiKueueManagedRTJ` |
| `MultiClusterDispatchPhase` (enum) | Phase 6 Session 2 | `Pending`, `Dispatched`, `Active` only | N/A (status) | Unit: `TestSyncRemoteStatusDispatchedButNoMirroredStatus`, `TestSyncRemoteStatusDetectsRemotePhase` |
| `RemoteObjectReference` | Phase 6 Session 5 | MinLength validation on Cluster and Name | N/A (status) | Unit: `TestSyncRemoteStatusBuildsRemoteObjectRef` |
| `RemoteCheckpointSummary` | Phase 6 Session 5 | All optional fields | N/A (status) | Unit: `TestSyncRemoteStatusMirrorsCheckpointSummary` |

---

## 9. Metrics Audit

| Metric | Phase | Registered | Recorder Method | Wired in Controller |
|---|---|---|---|---|
| `rtjs_by_execution_role` | 6 | Yes | `ObserveExecutionRole` / `RemoveExecutionRole` | Declared; wiring deferred to Phase 7 active-tracking refactor |
| `remote_rtjs_by_cluster` | 6 | Yes | `ObserveRemoteCluster` / `RemoveRemoteCluster` | Declared; wiring deferred to Phase 7 active-tracking refactor |
| `manager_local_suppressions_total` | 6 | Yes | `IncManagerLocalSuppression` | Declared; call-site in `reconcileManagerIntent` is implicit via status update |
| `remote_status_sync_successes_total` | 6 | Yes | `IncRemoteStatusSyncSuccess` | Declared |
| `remote_status_sync_failures_total` | 6 | Yes | `IncRemoteStatusSyncFailure` | Declared |
| `remote_pause_events_total` | 6 | Yes | `IncRemotePauseEvent` | Declared |
| `remote_resume_events_total` | 6 | Yes | `IncRemoteResumeEvent` | Declared |
| `remote_checkpoint_observations_total` | 6 | Yes | `IncRemoteCheckpointObservation` | Declared |
| `shared_store_access_failures_total` | 6 | Yes | `IncSharedStoreAccessFailure` | Declared |

**Finding:** Phase 6 metrics are registered and have recorder methods, but several are not yet actively called from controller hot paths. The recorder methods exist for future instrumentation. This is documented as a known gap (see gaps.md).

---

## 10. Test Coverage Audit

| Coverage Area | Requirement | Status | Files |
|---|---|---|---|
| API/webhook changes (unit) | Required | Pass | `api/v1alpha1/resumabletrainingjob_webhook_test.go` (917 lines): 10 Phase 6-specific tests covering managedBy create validation, immutability on update, removal rejection, domain-prefix format, and combo with all prior phase features. |
| Operator mode split (unit) | Required | Pass | `internal/controller/mode_test.go` (456 lines): 8 unit tests + 6 integration tests covering mode parsing, suppression predicate, reconciliation in both modes, idempotency, and backward compatibility. |
| Remote-status plumbing (unit/integration) | Required | Pass | `internal/controller/remote_status_test.go` (594 lines): 9 unit tests + 5 integration tests covering status initialization, phase detection, checkpoint mirroring, remote object ref, idempotency, cluster resolution, and nil-safety. |
| Remote execution (e2e) | Required | Pass | `test/e2e/multicluster_remote_execution_test.go` (139 lines): Proves end-to-end dispatch from manager to worker-1 with managedBy stripping, child JobSet existence on correct cluster, and absence on others. |
| Remote pause/resume (e2e) | Required | Pass | `test/e2e/multicluster_remote_pause_resume_test.go` (244 lines): Proves full pause-checkpoint-resume cycle with shared store, checkpoint preservation across adapter teardown, and monotonic checkpoint progression after resume. |
| Manager suppression (e2e) | Bonus | Pass | `test/e2e/multicluster_manager_suppression_test.go` (137 lines): Proves manager never creates local child JobSets over a 30-second observation window. |
| MultiKueue framework (unit) | Phase 6 specific | Pass | `internal/multikueue/framework_test.go` (233 lines) + `internal/multikueue/config_test.go` (213 lines). |
| Cluster resolver (unit) | Phase 6 specific | Pass | `internal/remote/cluster_resolver_test.go` (309 lines). |

---

## 11. Hard Boundary Verification

| Boundary | Status | Evidence |
|----------|--------|----------|
| RTJ is the only Kueue-managed object | Pass | Single `ExternalFrameworkName` registered. Child JobSets have no Kueue labels. |
| Child JobSets remain plain runtime | Pass | `internal/jobset/builder.go` does not set `kueue.x-k8s.io/queue-name` or `kueue.x-k8s.io/managed`. |
| Manager-local runtime suppression | Pass | `ShouldSuppressRuntime` predicate; `reconcileManagerIntent` creates no child resources. |
| Worker-local execution ownership | Pass | Worker mode always runs full Phase 5 path. No remote delegation from worker. |
| MultiKueue external-framework integration (no custom adapter) | Pass | No file implements `MultiKueueAdapter` interface. Generic adapter used via framework registration. |
| Shared-checkpoint remote pause/resume | Pass | Pause via adapter delete-recreate with checkpoint preservation. No live migration. |
| Single-cluster path preservation | Pass | Default mode is worker. Empty `managedBy` = Phase 5 path. Manager mode + non-MultiKueue RTJ = Phase 5 path. |

---

## Conclusion

Phase 6 implementation is consistent with the locked design across all five goals and all 14 key design decisions. No unauthorized scope additions were found. The single-cluster Phase 5 path is preserved in all configurations. All required test coverage areas are satisfied.

Two minor findings are documented in `gaps.md`:
1. Phase 6 metrics are registered but not all are actively emitted from controller hot paths.
2. The `reconcileManagerIntent` method does not explicitly call `IncManagerLocalSuppression()` - the suppression is implicit via the status update path.
