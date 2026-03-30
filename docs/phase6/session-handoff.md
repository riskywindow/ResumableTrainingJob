# Session Handoff

- Date: 2026-03-29
- Scope: Phase 6 design lock for `checkpoint-native preemption controller`

## Session 1: Design Lock

### Decisions Made

1. **Phase 6 scope is locked as five goals:**
   - G1: MultiKueue external-framework integration for RTJ (dispatch via
     MultiKueue, worker runs full Phase 5 path).
   - G2: Manager/worker operator split (`--mode=manager|worker` flag,
     manager is control-plane only, worker is default).
   - G3: Shared-checkpoint remote pause/resume (shared S3 store, cross-
     worker resume, existing yield protocol).
   - G4: Manager-visible remote status (mirrored phase, checkpoint state,
     worker identity, conditions).
   - G5: Deterministic local three-cluster dev/test profile (manager +
     2 workers + shared MinIO).

2. **RTJ remains the only Kueue-managed admission object.** Child JobSets
   remain plain runtime resources with no Kueue management metadata.
   Unchanged from Phase 2/3/4/5.

3. **The manager cluster does NOT create local child JobSets for
   MultiKueue-managed RTJs.** The manager is control-plane only. No local
   runtime resources.

4. **Worker clusters run the existing Phase 5 reconciliation path
   unchanged.** Launch gating, topology rendering, priority shaping,
   graceful yield, checkpoint selection, and resume all apply identically
   to Phase 5.

5. **Single-cluster Phase 5 remains the default stable path.** Worker
   mode is the default (`--mode=worker`). No behavioral changes when
   MultiKueue is not configured.

6. **The shared checkpoint store is required for multi-cluster.**
   All worker clusters must access the same S3-compatible store. The
   checkpoint contract (manifest-last, completeness, compatibility)
   is unchanged. Only the operational requirement (shared access) is new.

7. **Live migration of an admitted Workload across workers is NOT a
   core Phase 6 requirement.** Phase 6 provides cross-worker resume
   (pause + checkpoint + re-dispatch + restore). Live migration is
   deferred.

8. **No custom cross-cluster preemption engine or custom scheduler.**
   Kueue and MultiKueue own scheduling, dispatch, and preemption.

9. **No custom MultiKueue external dispatch implementation.** The RTJ
   operator uses the upstream MultiKueue external-framework protocol.

10. **The manager cluster does NOT also act as a worker.** Manager and
    worker roles are cleanly separated.

11. **Operator mode is determined at startup via `--mode` flag.** No
    runtime mode switching. The mode is fixed for the process lifetime.

12. **MultiKueue + autoscaler / ProvisioningRequest is NOT a required
    milestone.** Worker-side ProvisioningRequest is independent of the
    multi-cluster design.

13. **The manager does NOT do checkpoint I/O.** It reflects checkpoint
    metadata from the worker's RTJ status.

14. **Pinned versions unchanged:** Kueue v0.15.1, JobSet v0.10.1,
    controller-runtime v0.22.4.

### Files Created (Session 1)

- `docs/phase6/README.md` -- overview and quick context
- `docs/phase6/index.md` -- document index and navigation
- `docs/phase6/goals.md` -- goals, non-goals, success criteria, exit criteria
- `docs/phase6/architecture.md` -- component diagram, manager/worker mode
  diagram, three sequence diagrams (manager submit -> worker dispatch ->
  worker launch, manager pause -> worker yield/checkpoint -> manager paused,
  manager resume -> worker restore), detailed design
- `docs/phase6/migration-from-phase5.md` -- what stays, what moves to
  manager vs worker responsibility, why live migration is deferred, why
  shared checkpoint store is required, upgrade path
- `docs/phase6/open-questions.md` -- ten open questions with resolution
  plans and recommendations
- `docs/phase6/session-handoff.md` -- this file
- `docs/phase6/adr/0001-multicluster-spillover.md` -- Phase 6 multi-cluster
  spillover contract (11 decisions, alternatives considered, must-ship demo,
  manager/worker ownership split, experimental vs stable classification,
  verification plan)

### Tests Run

No runtime code was implemented. Design-only session.

- All existing Go tests continue to pass: verified by prior Phase 5 signoff.
- No new compilation targets affected (docs-only changes).

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | Open -- must inspect Kueue v0.15.1 MultiKueue source |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | Open -- must inspect MultiKueue spec mutation propagation |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | Open -- must inspect MultiKueue status mirroring |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | Tentatively resolved: all-or-nothing |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | Tentatively resolved: operational concern |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | Open -- requires kind networking investigation |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | Open -- must inspect Kueue feature gates |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | Open -- must inspect MultiKueue deletion behavior |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | Open -- must inspect MultiKueue preemption handling |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Phase 5 single-cluster path preserved when MultiKueue is not in use.
- MultiKueue path isolated behind `--mode=manager` startup flag.
- No custom MultiKueue external dispatch implementation.
- No MultiKueue + autoscaler / ProvisioningRequest as required milestone.
- Manager does not act as a worker.
- No custom scheduler or cross-cluster preemption engine.

---

## Session 2: RTJ API Extension for MultiKueue

- Date: 2026-03-30

### Decisions Made

1. **`spec.managedBy` is the sole user-facing MultiKueue signal.**
   No separate mode switch or annotation. Follows Kubernetes `managedBy`
   convention (domain-prefixed, immutable). Empty = Phase 5; value
   `kueue.x-k8s.io/multikueue` = MultiKueue dispatch.

2. **`status.multiCluster` groups all remote execution status.**
   Nil in single-cluster mode. Controller-owned. Contains dispatch
   phase, nominated clusters, execution cluster, remote object ref,
   remote phase, remote checkpoint summary, sync marker, and local
   execution suppressed indicator.

3. **Remote checkpoint is a summary, not a full copy.** Only ID,
   completion time, and storage URI are mirrored. The manager does
   not perform checkpoint I/O (preserves ADR-0001 D5).

4. **Dispatch phase and training phase are separate dimensions.**
   `dispatchPhase` (Pending/Dispatched/Active) tracks dispatch
   lifecycle. `remotePhase` mirrors the worker's training phase.

5. **`managedBy` validation requires domain prefix (`/`).**
   Max 256 characters. No whitelist -- forward-compatible with
   new external controllers.

6. **`managedBy` is immutable once set.** Prevents runtime ambiguity
   about controller ownership. Enforced by webhook ValidateUpdate.

7. **No speculative fields added.** Every field has a concrete Phase 6
   use case. Custom dispatch policy fields are explicitly excluded.

### Files Created / Modified (Session 2)

- `api/v1alpha1/resumabletrainingjob_types.go` -- added `ManagedBy` to
  spec, `MultiCluster` to status, `MultiClusterStatus`,
  `MultiClusterDispatchPhase`, `RemoteObjectReference`,
  `RemoteCheckpointSummary` types, `validateManagedBy()`,
  `IsManagedByMultiKueue()` helper.
- `api/v1alpha1/resumabletrainingjob_webhook.go` -- added `managedBy`
  immutability check in `ValidateUpdate`.
- `api/v1alpha1/resumabletrainingjob_webhook_test.go` -- added Phase 6
  webhook tests: default preserves Phase 5, create accepts/rejects
  managedBy values, update enforces immutability, backward compat,
  full-feature spec, `IsManagedByMultiKueue` helper tests.
- `api/v1alpha1/resumabletrainingjob_types_test.go` -- added Phase 6
  backward-compat tests, deep copy tests for `MultiClusterStatus`,
  `managedBy` validation tests.
- `api/v1alpha1/zz_generated.deepcopy.go` -- added DeepCopy for
  `MultiClusterStatus`, `RemoteObjectReference`,
  `RemoteCheckpointSummary`, wired `MultiCluster` into status deep copy.
- `config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml`
  -- added `managedBy` to spec, `multiCluster` to status.
- `docs/phase6/api.md` -- API reference for Phase 6 extensions.
- `docs/phase6/adr/0002-managedby-and-remote-status.md` -- ADR for
  managedBy field and remote status design decisions.
- `docs/phase6/session-handoff.md` -- this file (updated).

### Tests Run

- All Phase 6 type validation tests pass.
- All Phase 6 webhook tests pass.
- All Phase 6 deep copy tests pass.
- All existing Phase 1-5 tests pass (backward compatibility preserved).

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | Open -- must inspect Kueue v0.15.1 MultiKueue source |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | Open -- must inspect MultiKueue spec mutation propagation |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Partially resolved:** API surface defined. Status mirroring mechanism depends on OQ-1. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | Tentatively resolved: all-or-nothing |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | Tentatively resolved: operational concern |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | Open -- requires kind networking investigation |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | Open -- must inspect Kueue feature gates |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | Open -- must inspect MultiKueue deletion behavior |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | Open -- must inspect MultiKueue preemption handling |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Phase 5 single-cluster path preserved when `managedBy` is empty.
- MultiKueue path isolated behind `spec.managedBy` + `--mode=manager`.
- No custom MultiKueue external dispatch implementation.
- No speculative fields for custom dispatch policy.
- No controller logic implemented (API-only session).
- Manager does not act as a worker.
- No custom scheduler or cross-cluster preemption engine.

---

## Session 3: Operator Mode Split Implementation

- Date: 2026-03-30

### Decisions Made

1. **Two modes only: `worker` (default) and `manager`.** A separate
   "single" mode was considered but adds no behavioral difference over
   worker mode. Worker covers both single-cluster and multi-cluster
   worker deployments.

2. **Mode is startup-time via `--mode` flag, fixed for process lifetime.**
   Per-RTJ routing comes from `spec.managedBy`, not the mode flag.
   The mode flag determines the process-wide role.

3. **`ShouldSuppressRuntime` is the single predicate for mode branching.**
   Returns true only when `mode == manager AND job.IsManagedByMultiKueue()`.
   All other combinations follow the full Phase 5 path.

4. **Manager mode preserves normal path for non-MultiKueue RTJs.**
   Safety-first: if a non-MultiKueue RTJ exists on a manager cluster,
   the full Phase 5 runtime path runs to prevent data loss.

5. **Suppression check is positioned early in Reconcile.** After finalizer
   management and status initialization but before `getActiveJobSet` and
   all runtime path code. This ensures no runtime resources are ever
   accidentally created for suppressed RTJs.

6. **Manager-mode status uses existing MultiClusterStatus fields.**
   Sets `localExecutionSuppressed=true`, `dispatchPhase=Pending`,
   phase=`Queued`, reason=`LocalExecutionSuppressed`. No new API types.

7. **Zero-value Mode field preserves pre-Phase 6 behavior.** Existing
   tests and deployments work unchanged without setting `--mode`.

8. **No MultiKueue configuration wired yet.** This session implements
   the mode split only. MultiKueue external-framework protocol,
   dispatch, and status mirroring are deferred.

### Files Created / Modified (Session 3)

- `internal/controller/mode.go` -- new file. `OperatorMode` type with
  `ModeWorker` and `ModeManager` constants, `ParseOperatorMode` validator,
  `ShouldSuppressRuntime` predicate with `multiKueueChecker` interface.

- `internal/controller/mode_test.go` -- new file. 14 tests:
  - `ParseOperatorMode` valid/invalid modes (3 tests)
  - `ShouldSuppressRuntime` all four (mode, managedBy) combinations (4 tests)
  - Manager mode suppresses runtime for MultiKueue RTJ (1 test)
  - Manager mode allows normal path for non-MultiKueue RTJ (1 test)
  - Worker mode launches runtime for MultiKueue RTJ (1 test)
  - Single-cluster behavior unchanged (1 test)
  - Manager mode suppresses even when unsuspended (1 test)
  - Repeated reconcile is idempotent (1 test)
  - Zero-value mode backward compat (1 test)

- `internal/controller/resumabletrainingjob_controller.go` -- modified.
  Added `Mode OperatorMode` field to `ResumableTrainingJobReconciler`.
  Inserted `ShouldSuppressRuntime` check early in `Reconcile` that
  branches to `reconcileManagerIntent`. Added `reconcileManagerIntent`
  method.

- `internal/controller/status_helpers.go` -- modified. Added
  `markManagerSuppressed` helper that sets `MultiCluster` status fields
  and `reasonLocalExecutionSuppressed` constant.

- `cmd/operator/main.go` -- modified. Added `--mode` flag with default
  `worker`, `ParseOperatorMode` validation at startup, `Mode` field
  passed to reconciler, mode logged at startup.

- `docs/phase6/operator-modes.md` -- new file. Documents mode semantics,
  ownership split table, detection logic, backward compatibility,
  configuration examples, test coverage, and design decisions.

- `docs/phase6/session-handoff.md` -- this file (updated).

### Tests Run

- `go vet ./internal/controller/...` -- clean
- `go vet ./cmd/operator/...` -- clean
- `go test ./internal/controller/... -v` -- 87 tests pass (14 new + 73 existing)
- `go test ./...` -- all packages pass:
  - `api/v1alpha1` -- pass
  - `internal/admissionchecks/resume` -- pass
  - `internal/checkpoints` -- pass
  - `internal/controller` -- pass
  - `internal/jobset` -- pass
  - `internal/kueue` -- pass
  - `internal/policy/checkpointpriority` -- pass
  - `internal/topology` -- pass
  - `test/e2e` -- pass

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| Manager must not create local child JobSets for MultiKueue-managed RTJs | Verified: `TestManagerModeSuppressesRuntimeForMultiKueueManagedRTJ`, `TestManagerModeDoesNotCreateJobSetEvenWhenUnsuspended` |
| Worker must continue to own the real runtime path | Verified: `TestWorkerModeLaunchesRuntimeForMultiKueueManagedRTJ` |
| Single-cluster behavior preserved when MultiKueue not in use | Verified: `TestSingleClusterBehaviorUnchangedWhenMultiKueueNotUsed` |
| No accidental reintroduction of manager-local JobSets for remote jobs | Verified: all manager-mode tests assert `apierrors.IsNotFound` for child JobSets |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | Open -- must inspect Kueue v0.15.1 MultiKueue source |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | Open -- must inspect MultiKueue spec mutation propagation |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Partially resolved:** API surface defined, `reconcileManagerIntent` populates manager-side status. Status mirroring from worker depends on OQ-1. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** All-or-nothing `--mode` flag with per-RTJ `spec.managedBy` routing. Implemented in Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | Tentatively resolved: operational concern |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | Open -- requires kind networking investigation |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | Open -- must inspect Kueue feature gates |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | Open -- must inspect MultiKueue deletion behavior |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | Open -- must inspect MultiKueue preemption handling |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Manager does NOT create local child JobSets for MultiKueue-managed RTJs (tested).
- Worker continues to own the real runtime path (tested).
- Single-cluster Phase 5 behavior preserved when MultiKueue is not in use (tested).
- Implementation is simple and explicit (two modes, one predicate, one early branch).
- No MultiKueue configuration wired yet (deferred as requested).

---

## Session 4: MultiKueue External-Framework Integration

- Date: 2026-03-30

### Decisions Made

1. **RTJ uses Kueue's generic external-framework adapter, not a custom
   MultiKueueAdapter.** Kueue v0.15.1 ships `externalframeworks.Adapter`
   (in `pkg/controller/admissionchecks/multikueue/externalframeworks/adapter.go`)
   which handles any CRD listed in `integrations.externalFrameworks` via
   unstructured objects. No RTJ-specific Go code is needed in Kueue.

2. **Two feature gates required (both Beta, default-on in v0.15.1):**
   - `MultiKueue` (Beta since v0.9, default true)
   - `MultiKueueAdaptersForCustomJobs` (Beta since v0.15, default true)
   No explicit configuration needed unless previously disabled.

3. **The generic adapter strips `spec.managedBy` on remote creation.**
   When creating the remote RTJ copy on a worker, the adapter removes
   `spec.managedBy` so the worker Kueue and RTJ operator treat it as a
   normal local job.

4. **Status mirroring is full `.status` unstructured copy.** The adapter
   copies the entire `.status` from remote to local via unstructured patch.
   All Phase 1-5 status fields flow through automatically.

5. **Spec mutations are NOT propagated.** The adapter deletes and recreates
   the remote Workload if the local spec drifts (except elastic scale-down).
   Pause propagation must use Kueue Workload suspension, not spec patching.

6. **Remote cleanup is automatic.** The adapter handles deletion on
   manager-side deletion, spec drift, and periodic GC (default 1 minute).

7. **`KeepAdmissionCheckPending` returns true for external frameworks.**
   This keeps the manager-side RTJ suspended while the remote copy runs,
   providing a second layer of protection (in addition to `--mode=manager`)
   against accidental local JobSet creation.

8. **RBAC is required on both clusters.** Manager-side Kueue needs read +
   status-write on RTJ. Worker-side remote client needs create + get +
   delete on RTJ and full Workload management.

### OQ Resolutions

| ID | Resolution | Evidence |
|---|---|---|
| OQ-1 | **Resolved.** Generic adapter handles external-framework CRDs. No custom MultiKueueAdapter needed. The adapter checks `spec.managedBy` via `IsJobManagedByKueue()` and uses unstructured operations. | `externalframeworks/adapter.go` lines 50-120 |
| OQ-2 | **Resolved.** Spec mutations are NOT propagated. Out-of-sync remote Workloads are deleted and recreated. Pause requires Kueue Workload suspension. | `workload.go` lines 366-383 |
| OQ-3 | **Resolved.** Full `.status` copy via `adapter.syncStatus()` using unstructured map copy. Combined with Workload-level status sync. | `externalframeworks/adapter.go` `syncStatus()` + `copyStatusFromRemote()` |
| OQ-7 | **Resolved.** `MultiKueue` Beta (default-on since v0.9), `MultiKueueAdaptersForCustomJobs` Beta (default-on since v0.15). No explicit config needed. | `pkg/features/kube_features.go` |
| OQ-8 | **Resolved.** Manager-side deletion triggers `RemoveRemoteObjects()` which deletes remote job (background propagation) + removes finalizer + deletes remote Workload. Periodic GC also handles orphans. | `workload.go` lines 140-161, 234-243; `multikueuecluster.go` `runGC()` |
| OQ-9 | **Resolved.** Kueue preemption on the manager side suspends the Workload. MultiKueue then deletes the out-of-sync remote Workload. On re-admission, a new remote copy is created. Worker-side preemption is handled locally by the worker Kueue. | `workload.go` reconcileGroup flow |

### Files Created (Session 4)

- `internal/multikueue/config.go` -- MultiKueue configuration types,
  constants for external-framework name and feature gates,
  `ManagerClusterConfig` struct, `ValidateManagerConfig()` and
  `ValidateExternalFrameworkList()` validation functions,
  `RequiredFeatureGates()` helper.

- `internal/multikueue/config_test.go` -- 18 tests:
  - Valid config (1 test)
  - Missing/empty external framework (2 tests)
  - Feature gate disabled/missing (2 tests)
  - No worker clusters (1 test)
  - Multiple simultaneous errors (1 test)
  - External framework list validation (4 tests)
  - Required feature gates (1 test)
  - Effective name defaults/custom (4 tests)
  - ValidationError.Error() (1 test)
  - Framework name consistency (1 test)

- `internal/multikueue/framework.go` -- RTJ-specific MultiKueue framework
  integration: `RTJGroupVersionKind`, `RTJGroupVersionResource`,
  `FormatExternalFrameworkName()`, `IsRTJEligibleForMultiKueue()`,
  `RTJManagerRBACRules()`, `RemoteObjectExpectations` documentation type.

- `internal/multikueue/framework_test.go` -- 12 tests:
  - FormatExternalFrameworkName (2 tests)
  - GVK/GVR correctness (2 tests)
  - IsRTJEligibleForMultiKueue nil/empty/wrong/valid (4 tests)
  - Consistency with IsManagedByMultiKueue (1 subtest table)
  - RTJManagerRBACRules (1 test)
  - Label constants (1 test)
  - Admission check controller consistency (1 test)

- `deploy/multikueue/manager-config/kueue-controller-manager-config.yaml`
  -- Kueue Configuration with RTJ external framework for manager cluster.

- `deploy/multikueue/manager-config/admissioncheck.yaml` -- MultiKueue
  AdmissionCheck resource.

- `deploy/multikueue/manager-config/multikueueconfig.yaml` -- MultiKueueConfig
  listing worker clusters.

- `deploy/multikueue/manager-config/multikueuecluster-template.yaml` --
  MultiKueueCluster templates (one per worker) with kubeconfig Secret refs.

- `deploy/multikueue/manager-config/clusterqueue-multikueue.yaml` --
  ClusterQueue with MultiKueue admission check.

- `deploy/multikueue/manager-rbac/clusterrole-kueue-rtj.yaml` -- ClusterRole
  for Kueue controller manager to manage RTJ on manager cluster.

- `deploy/multikueue/manager-rbac/clusterrolebinding-kueue-rtj.yaml` --
  ClusterRoleBinding for Kueue ServiceAccount.

- `deploy/multikueue/manager-rbac/worker-kubeconfig-rbac.yaml` -- RBAC for
  MultiKueue remote client on worker clusters.

- `docs/phase6/multikueue-integration.md` -- Integration guide: manager
  cluster setup (8 requirements), worker cluster setup (6 requirements),
  why spec.managedBy matters, mirror-copy execution model, deploy artifact
  reference, Kueue v0.15.1 version accuracy table.

- `docs/phase6/adr/0003-rtj-as-external-framework.md` -- ADR with 8
  decisions: generic adapter (D1), feature gates (D2), managedBy signal (D3),
  full status copy (D4), no spec propagation (D5), automatic cleanup (D6),
  KeepAdmissionCheckPending (D7), dual-cluster RBAC (D8). Three alternatives
  considered and rejected.

- `docs/phase6/index.md` -- updated with links to new docs.

- `docs/phase6/session-handoff.md` -- this file (updated).

### Tests Run

- `go vet ./internal/multikueue/...` -- clean
- `go test ./internal/multikueue/... -v` -- 30 tests pass (18 config + 12 framework)
- `go test ./...` -- all packages pass:
  - `api/v1alpha1` -- pass
  - `internal/admissionchecks/resume` -- pass
  - `internal/checkpoints` -- pass
  - `internal/controller` -- pass
  - `internal/jobset` -- pass
  - `internal/kueue` -- pass
  - `internal/multikueue` -- pass (NEW)
  - `internal/policy/checkpointpriority` -- pass
  - `internal/topology` -- pass
  - `test/e2e` -- pass

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| No custom MultiKueue dispatcher in the core milestone | Verified: uses Kueue's generic `externalframeworks.Adapter`. No `MultiKueueAdapter` implemented. |
| RTJ is the only Kueue-managed object | Verified: external framework name matches `internal/kueue/register.go` |
| Child JobSets remain plain runtime only | Verified: no changes to child JobSet creation. Manager suppression tested in Session 3. |
| Single-cluster path preserved | Verified: all Phase 1-5 tests pass. No behavioral changes when MultiKueue not configured. |
| Version-accurate to Kueue v0.15.1 | Verified: all adapter behavior, feature gates, and interface details confirmed from Kueue v0.15.1 source in Go module cache. |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | **Resolved:** Generic adapter handles external CRDs. Session 4. |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | **Resolved:** Spec mutations not propagated; pause via Kueue Workload suspension. Session 4. |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Resolved:** Full `.status` copy via unstructured adapter. Session 4. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** All-or-nothing `--mode` flag. Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | Tentatively resolved: operational concern |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | Open -- requires kind networking investigation |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | **Resolved:** Both gates Beta, default-on. Session 4. |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | **Resolved:** Automatic via `RemoveRemoteObjects` + periodic GC. Session 4. |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | **Resolved:** Preemption suspends Workload, remote deleted and recreated. Session 4. |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- No custom MultiKueue dispatcher built (uses Kueue's generic adapter).
- RTJ remains the only Kueue-managed object.
- Child JobSets remain plain runtime only.
- Single-cluster Phase 5 path preserved.
- Version-accurate to pinned Kueue v0.15.1.
- Full multi-cluster dev environment NOT added (deferred to G5).

---

## Session 5: Remote Status Plumbing and Shared Checkpoint Store

- Date: 2026-03-30

### Decisions Made

1. **Smallest coherent approach chosen: reuse the Kueue adapter's full
   status mirror.** The Kueue generic adapter already copies the entire
   `.status` from the remote worker RTJ to the manager RTJ (Session 4,
   OQ-3). No separate remote watcher or polling mechanism is needed. The
   manager controller simply reads the mirrored status and extracts
   relevant fields into `status.multiCluster`.

2. **Remote status detection uses a heuristic signal.** The manager
   detects that the adapter has mirrored remote status by checking
   `activeJobSetName != ""` or `currentRunAttempt > 0`. These fields
   are only set by the worker-side controller, never by the manager.

3. **Execution cluster resolved from Workload admission check.** When
   MultiKueue dispatches a Workload to a worker cluster, the admission
   check state transitions to Ready and the `message` field contains
   the worker cluster name. The `WorkloadClusterResolver` reads this.

4. **Dispatch phase classification is three-state.** Pending (no
   cluster, no signal), Dispatched (cluster known, no signal), Active
   (remote status signal present).

5. **Remote checkpoint is a summary, not a full copy.** Only ID,
   completion time, and storage URI are mirrored from the worker's
   `lastCompletedCheckpoint`. This preserves ADR-0001 D5 (manager
   does not do checkpoint I/O).

6. **Shared checkpoint store contract made explicit.** Added
   `SharedStoreConfig`, `NewStoreFromConfig`, and
   `ValidateSharedEndpoint` to reject cluster-local endpoints
   (`.svc.cluster.local`, `localhost`, loopback) that would break
   cross-cluster checkpoint access.

7. **Shared endpoint validation is advisory, not blocking.** Single-
   cluster deployments can continue using cluster-local endpoints.
   MultiKueue deployments are warned but not prevented from starting.

8. **ClusterResolver is optional on the reconciler.** A nil resolver
   is handled gracefully (execution cluster reported as unknown).
   This ensures backward compatibility for tests that don't wire
   a resolver.

9. **No duplicate worker logic on the manager.** The manager does not
   run checkpoint I/O, launch gating, topology rendering, or priority
   shaping for MultiKueue-managed RTJs. It only reflects status.

### Files Created / Modified (Session 5)

- `internal/remote/cluster_resolver.go` -- new file. `ClusterResolver`
  interface, `WorkloadClusterResolver` (reads Kueue Workload admission
  check), `StaticClusterResolver` (testing).

- `internal/remote/cluster_resolver_test.go` -- new file. 11 tests:
  - Ready admission check returns cluster (1 test)
  - No workload ref returns empty (1 test)
  - Pending check returns empty (1 test)
  - Filters by admission check name (1 test)
  - Defaults to job namespace (1 test)
  - Static resolver (1 test)
  - extractClusterFromAdmissionChecks unit tests (5 tests)

- `internal/controller/remote_status.go` -- new file. Remote status
  reflector: `syncRemoteStatus`, `classifyRemoteState`,
  `hasRemoteStatusSignal`, `syncRemoteObjectRef`,
  `syncRemoteCheckpointSummary`, equality helpers.

- `internal/controller/remote_status_test.go` -- new file. 17 tests:
  - syncRemoteStatus initializes MultiCluster (1 test)
  - syncRemoteStatus detects remote phase (1 test)
  - syncRemoteStatus mirrors checkpoint summary (1 test)
  - syncRemoteStatus dispatched but no mirrored status (1 test)
  - syncRemoteStatus builds remote object ref (1 test)
  - syncRemoteStatus clears ref when no cluster (1 test)
  - syncRemoteStatus idempotent (1 test)
  - syncRemoteStatus clears checkpoint when nil (1 test)
  - hasRemoteStatusSignal table tests (4 subtests)
  - Integration: reflects execution cluster (1 test)
  - Integration: reflects remote phase after adapter sync (1 test)
  - Integration: reflects remote checkpoint data (1 test)
  - Integration: survives repeated reconciles (1 test)
  - Integration: nil cluster resolver is graceful (1 test)
  - Equality helper tests (2 tests)

- `internal/controller/resumabletrainingjob_controller.go` -- modified.
  Added `remote` import. Added `ClusterResolver` field to reconciler.
  Updated `reconcileManagerIntent` to call `syncRemoteStatus` and
  resolve execution cluster via `ClusterResolver`.

- `internal/checkpoints/store.go` -- modified. Added `SharedStoreConfig`
  struct, `NewStoreFromConfig` constructor, `ValidateSharedEndpoint`
  validation, `IsSharedStoreConfigured` pre-flight check.

- `internal/checkpoints/store_test.go` -- new file. 10 tests:
  - ValidateSharedEndpoint accepts external endpoints (1 test)
  - ValidateSharedEndpoint rejects cluster-local (1 test)
  - ValidateSharedEndpoint rejects loopback (1 test)
  - ValidateSharedEndpoint rejects empty (1 test)
  - ValidateSharedEndpoint case-insensitive (1 test)
  - NewStoreFromConfig requires all fields (4 subtests)
  - NewStoreFromConfig accepts valid config (1 test)
  - Shared store not cluster-local in MultiKueue mode (1 test)
  - ParseS3URI valid/invalid (3 tests)

- `docs/phase6/remote-status.md` -- new file. Design doc for remote
  status plumbing: approach, status fields, dispatch classification,
  cluster resolution, file index.

- `docs/phase6/shared-checkpoint-store.md` -- new file. Design doc for
  shared checkpoint store: contract, migration path, endpoint validation,
  design decisions.

- `docs/phase6/session-handoff.md` -- this file (updated).

### Tests Run

- `go vet ./internal/remote/...` -- clean
- `go vet ./internal/controller/...` -- clean
- `go vet ./internal/checkpoints/...` -- clean
- `go test ./internal/remote/... -v` -- 11 tests pass
- `go test ./internal/controller/... -v` -- 107 tests pass (17 new + 90 existing)
- `go test ./internal/checkpoints/... -v` -- 30 tests pass (10 new + 20 existing)
- `go test ./...` -- all packages pass:
  - `api/v1alpha1` -- pass
  - `internal/admissionchecks/resume` -- pass
  - `internal/checkpoints` -- pass
  - `internal/controller` -- pass
  - `internal/jobset` -- pass
  - `internal/kueue` -- pass
  - `internal/multikueue` -- pass
  - `internal/policy/checkpointpriority` -- pass
  - `internal/remote` -- pass (NEW)
  - `internal/topology` -- pass
  - `test/e2e` -- pass

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| Manager-side RTJ status surfaces executionCluster | Verified: `TestManagerModeReflectsRemoteExecutionCluster` |
| Manager-side RTJ status surfaces remote phase | Verified: `TestManagerModeReflectsRemotePhaseAfterAdapterSync`, `TestSyncRemoteStatusDetectsRemotePhase` |
| Manager-side RTJ status surfaces remote checkpoint | Verified: `TestManagerModeReflectsRemoteCheckpointData`, `TestSyncRemoteStatusMirrorsCheckpointSummary` |
| Manager-side RTJ shows whether remote runtime exists | Verified: `TestHasRemoteStatusSignal` (heuristic), `TestSyncRemoteStatusDispatchedButNoMirroredStatus` |
| Manager-side RTJ shows whether local launch is suppressed | Verified: `TestSyncRemoteStatusInitializesMultiCluster` (LocalExecutionSuppressed=true) |
| Shared checkpoint store not cluster-local in MultiKueue | Verified: `TestSharedStoreConfigNotClusterLocalInMultiKueueMode` |
| Implementation survives repeated reconciles | Verified: `TestManagerModeRemoteStatusSurvivesRepeatedReconciles`, `TestSyncRemoteStatusIdempotent` |
| No duplicate worker logic on manager | Verified: no checkpoint I/O, no child JobSet creation, no launch gating, no priority shaping for manager-mode RTJs |
| Single-cluster Phase 5 path preserved | Verified: all existing Phase 1-5 tests pass unchanged |
| Version-accurate to pinned Kueue v0.15.1 | Verified: uses `kueuev1beta2.AdmissionCheckState` and `CheckStateReady` from v0.15.1 |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | **Resolved:** Generic adapter handles external CRDs. Session 4. |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | **Resolved:** Spec mutations not propagated; pause via Kueue Workload suspension. Session 4. |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Resolved:** Full `.status` copy via adapter, remote status reflector populates MultiClusterStatus. Sessions 4 & 5. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** All-or-nothing `--mode` flag. Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | **Resolved:** Operational concern. `ValidateSharedEndpoint` added for pre-flight checking. Session 5. |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | Open -- requires kind networking investigation |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | **Resolved:** Both gates Beta, default-on. Session 4. |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | **Resolved:** Automatic via `RemoveRemoteObjects` + periodic GC. Session 4. |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | **Resolved:** Preemption suspends Workload, remote deleted and recreated. Session 4. |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Smallest coherent approach used: reuses Kueue adapter status mirror.
- No duplicate worker logic on the manager.
- Shared checkpoint store contract is simple: one store, all clusters,
  no replication service.
- No custom MultiKueue dispatcher built.
- RTJ remains the only Kueue-managed object.
- Child JobSets remain plain runtime only.
- Single-cluster Phase 5 path preserved.
- Version-accurate to pinned Kueue v0.15.1.
- Full multi-cluster dev environment NOT added (deferred to G5).

---

## Recommended Next Prompt

Nine of ten open questions are now resolved. G1 (MultiKueue external-
framework integration), G2 (manager/worker split), and G4 (manager-
visible remote status) are complete. The recommended next session is:

### Session 6: Shared-Checkpoint Remote Pause/Resume (G3)

**Goal:** Validate that the shared checkpoint store enables cross-worker
resume: pause on worker-1, re-dispatch to worker-2, resume from the
checkpoint written by worker-1.

**Prompt:**

```
You are working on Phase 6 only for the checkpoint-native preemption
controller repo.

Mission:
Validate and test the shared-checkpoint cross-worker resume path.

Hard boundaries:
- Keep the shared checkpoint-store contract simple.
- No replication service.
- Reuse the existing pause/resume protocol.

Process:
- Read docs/phase6/session-handoff.md for current state.
- Review the existing pause flow (suspend_flow.go) and resume flow
  (resume_flow.go) to confirm they work with the shared store model.
- Add integration tests proving cross-worker resume.
- Update docs/phase6/session-handoff.md.
```
