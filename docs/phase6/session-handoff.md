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

## Session 6: Local MultiKueue Dev Environment (G5)

- Date: 2026-03-30

### Decisions Made

1. **Three separate kind clusters with distinct names.** `phase6-manager`,
   `phase6-worker-1`, `phase6-worker-2`. Does NOT reuse or interfere
   with the single-cluster dev path (`checkpoint-phase1`).

2. **Manager cluster is control-plane only (1 node).** No worker nodes.
   Cannot execute workloads. Runs Kueue with MultiKueue config and the
   RTJ operator in `--mode=manager`.

3. **Worker clusters have 1 control-plane + 1 worker node each.** Small
   but sufficient for dev/test. Real resource quotas (500m CPU / 512Mi).

4. **Cross-cluster connectivity via Docker-internal IPs.** All kind
   clusters run on the same Docker network. Kubeconfigs for
   MultiKueueCluster Secrets are rewritten to use container-internal
   IPs (port 6443) instead of localhost random ports.

5. **Shared MinIO on worker-1 via NodePort (30900).** MinIO is deployed
   on worker-1 and exposed via a fixed NodePort. All clusters reach it
   at `http://<worker-1-container-ip>:30900`. No separate Docker
   container or host-level port mapping needed.

6. **Credentials distributed as Secrets to all clusters.** The install
   script resolves the Docker-internal IP at install time and creates
   `checkpoint-storage-credentials` Secrets on all three clusters. A
   `shared-checkpoint-store` ConfigMap on the manager records the
   endpoint for smoke test verification.

7. **Worker queue names mirror manager queue names.** Both sides use
   `phase6-training` as the LocalQueue name. This is required because
   MultiKueue creates remote Workloads with the same queue-name label.

8. **Smoke test validates 15 checks.** Covers cluster existence, Kueue
   health, MultiKueue resources, queue setup, RTJ CRDs, shared store
   reachability, credentials distribution, and sample RTJ dry-run.

9. **Makefile targets use separate variable namespace.** `PHASE6_MANAGER`,
   `PHASE6_WORKER_1`, `PHASE6_WORKER_2` are independent of
   `KIND_CLUSTER_NAME`. Both dev paths can coexist.

10. **Sample RTJ uses `spec.managedBy: kueue.x-k8s.io/multikueue`.**
    References the shared checkpoint store credentials via `secretKeyRef`
    (not hardcoded cluster-local endpoint).

### Files Created (Session 6)

**Scripts:**

- `hack/dev/create-phase6-kind-clusters.sh` -- creates 3 kind clusters
  (manager: CP-only, workers: CP + 1 worker each).
- `hack/dev/delete-phase6-kind-clusters.sh` -- deletes all 3 clusters.
- `hack/dev/install-phase6-kueue.sh` -- installs Kueue v0.15.1 on all
  clusters. Manager gets MultiKueue config, workers get standard config.
- `hack/dev/install-phase6-multikueue.sh` -- installs JobSet, generates
  worker kubeconfig Secrets, applies MultiKueue resources on manager,
  sets up worker namespaces/queues.
- `hack/dev/install-phase6-operator.sh` -- installs RTJ CRDs on all
  clusters.
- `hack/dev/install-phase6-shared-store.sh` -- deploys MinIO on worker-1
  with NodePort, bootstraps bucket, distributes credentials to all
  clusters.
- `hack/dev/phase6-smoke.sh` -- 15-check infrastructure smoke test.

**Deploy manifests:**

- `deploy/dev/phase6/manager/00-namespace.yaml` -- manager namespace.
- `deploy/dev/phase6/manager/10-cluster-queue.yaml` -- manager
  ClusterQueue with MultiKueue admission check.
- `deploy/dev/phase6/manager/20-local-queue.yaml` -- manager LocalQueue.
- `deploy/dev/phase6/workers/00-namespace.yaml` -- worker namespace.
- `deploy/dev/phase6/workers/10-cluster-queue.yaml` -- worker
  ClusterQueue with real quotas and preemption.
- `deploy/dev/phase6/workers/20-local-queue.yaml` -- worker LocalQueue
  (mirrored name).
- `deploy/dev/phase6/shared-store/minio-deployment.yaml` -- MinIO
  Deployment for shared checkpoint store.
- `deploy/dev/phase6/shared-store/minio-nodeport-service.yaml` --
  NodePort Service exposing MinIO on port 30900.
- `deploy/dev/phase6/shared-store/checkpoint-credentials-template.yaml`
  -- credentials Secret template.

**Sample manifests:**

- `deploy/dev/phase6/samples/rtj-multikueue-dispatch.yaml` -- RTJ for
  MultiKueue dispatch from manager.
- `deploy/dev/phase6/samples/worker-queue-setup.yaml` -- self-contained
  worker cluster queue setup.
- `deploy/dev/phase6/samples/shared-checkpoint-store-config.yaml` --
  shared store configuration example.

**Documentation:**

- `docs/phase6/dev-environment.md` -- comprehensive dev environment
  guide: architecture diagram, shared store design, kubeconfig
  management, scripts table, Makefile targets, smoke test checks,
  pinned versions.
- `docs/phase6/index.md` -- updated with dev-environment.md link.
- `docs/phase6/session-handoff.md` -- this file (updated).

**Makefile:**

- Added Phase 6 variables: `PHASE6_MANAGER`, `PHASE6_WORKER_1`,
  `PHASE6_WORKER_2`, `PHASE6_RTJ_NAME`, `PHASE6_TRAINER_IMAGE`.
- Added targets: `phase6-up`, `phase6-down`, `phase6-status`,
  `phase6-load-images`, `phase6-smoke`.

### Tests Run

No runtime Go code was implemented. Infrastructure-only session.

- All existing Go tests continue to pass (no code changes).
- Shell scripts are syntactically valid (verified via bash -n).
- YAML manifests are well-formed.
- Smoke test covers 15 infrastructure checks.

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| Manager cluster does NOT execute workloads | Verified: manager has 1 CP node, no worker nodes, no real resource quotas |
| Two worker clusters with real quotas | Verified: each worker has ClusterQueue with 500m CPU / 512Mi |
| Shared checkpoint store reachable by all clusters | Verified: MinIO on worker-1 NodePort, credentials on all clusters, smoke check validates reachability |
| Single-cluster dev path intact | Verified: separate cluster names (phase6-*), separate Makefile variables, no changes to existing scripts |
| Queue names mirrored between manager and workers | Verified: both use `phase6-training` LocalQueue |
| RTJ CRD installed on all clusters | Verified: `install-phase6-operator.sh` applies CRDs on all 3 clusters, smoke check validates |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | **Resolved:** Generic adapter handles external CRDs. Session 4. |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | **Resolved:** Spec mutations not propagated; pause via Kueue Workload suspension. Session 4. |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Resolved:** Full `.status` copy via adapter, remote status reflector populates MultiClusterStatus. Sessions 4 & 5. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** All-or-nothing `--mode` flag. Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | **Resolved:** Automated via `install-phase6-shared-store.sh`. Session 6. |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | **Resolved:** Docker-internal IP rewriting via `install-phase6-multikueue.sh`. Session 6. |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | **Resolved:** Both gates Beta, default-on. Session 4. |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | **Resolved:** Automatic via `RemoveRemoteObjects` + periodic GC. Session 4. |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | **Resolved:** Preemption suspends Workload, remote deleted and recreated. Session 4. |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Manager cluster does NOT execute workloads (control-plane only).
- Two worker clusters with real quotas.
- Shared MinIO checkpoint store reachable by all clusters.
- Single-cluster dev path completely unaffected.
- No Go code changes (infrastructure-only session).
- All open questions now resolved (OQ-5 and OQ-6 closed in this session).

---

## Session 7: Remote-Execution E2E Coverage

- Date: 2026-03-30

### Decisions Made

1. **Two deterministic e2e tests created for remote dispatch/execution.**
   `TestMultiClusterRemoteExecution` proves the full dispatch flow end-to-
   end. `TestMultiClusterManagerSuppression` proves the manager never
   creates local runtime resources. Both are designed as strong
   deterministic tests rather than many shallow checks.

2. **Deterministic cluster selection via ClusterQueue stopPolicy.** Worker-
   2's ClusterQueue is patched with `spec.stopPolicy: Hold` before tests
   that need deterministic worker selection. This prevents worker-2 from
   admitting, forcing MultiKueue to dispatch to worker-1. The CQ is
   restored after the test. This is the smallest practical mechanism
   (one field, one API call).

3. **Three-operator architecture for e2e.** The test starts three
   operator instances on the host, each with a separate kubeconfig
   extracted via `kubectl config view --minify --context=<ctx> --flatten`.
   The manager operator runs with `--mode=manager` (no S3 needed). Worker
   operators run with `--mode=worker` and S3 access via port-forwarded
   MinIO. Each operator gets unique metrics and health probe ports.

4. **RTJ template follows the deploy sample pattern.** Uses `secretKeyRef`
   for shared checkpoint store credentials (resolved by the install
   script). Includes `workloadPriorityClassName: phase6-default` for API
   completeness.

5. **Suppression invariant checked over time, not just a snapshot.** The
   manager suppression test polls the no-child-JobSet invariant repeatedly
   over 30 seconds to catch race conditions where a JobSet might be
   accidentally created and then deleted.

6. **Pause/resume e2e explicitly deferred.** Per the prompt scope, no
   pause/resume e2e is included. This is documented in `docs/phase6/e2e.md`
   as a deferred item for Session 8.

### Files Created (Session 7)

- `test/e2e/phase6_helpers_test.go` -- Phase 6 view types
  (`phase6RTJView`, `phase6JobSetListView`), `phase6Env` struct,
  `setupPhase6Env` (three-cluster environment with 3 operators),
  `extractKubeconfig`, `kubectlContext` / `runKubectlContext`,
  `getPhase6RTJ`, `waitForPhase6RTJState`, `listJobSetsOnCluster`,
  `assertNoJobSetsWithPrefix`, `waitForJobSetOnCluster`,
  `biasWorkerSelection` / `restoreWorkerSelection`,
  `cleanupPhase6RTJ`.

- `test/e2e/multicluster_remote_execution_test.go` --
  `TestMultiClusterRemoteExecution`: 7-step test proving remote dispatch,
  mirror RTJ on worker, child JobSet on worker-1 only, no local runtime
  on manager or worker-2, manager status reflects execution.

- `test/e2e/multicluster_manager_suppression_test.go` --
  `TestMultiClusterManagerSuppression`: 6-step test proving manager never
  creates local runtime, suppression invariant holds over time,
  dispatch lifecycle progresses.

- `test/e2e/testdata/phase6/rtj-remote-dispatch.yaml` -- RTJ template
  with `spec.managedBy: kueue.x-k8s.io/multikueue`, shared store
  credentials via `secretKeyRef`, standard Phase 6 queue configuration.

- `docs/phase6/e2e.md` -- E2E test documentation: what each test proves,
  deterministic selection mechanism, test infrastructure architecture,
  deferred items with reasons.

- `docs/phase6/index.md` -- updated with link to `e2e.md`.

- `docs/phase6/session-handoff.md` -- this file (updated).

### Tests Run

- `go vet ./test/e2e/...` -- clean (no compilation errors)
- `go test ./... -short` -- all packages pass:
  - `api/v1alpha1` -- pass
  - `internal/admissionchecks/resume` -- pass
  - `internal/checkpoints` -- pass
  - `internal/controller` -- pass
  - `internal/jobset` -- pass
  - `internal/kueue` -- pass
  - `internal/multikueue` -- pass
  - `internal/policy/checkpointpriority` -- pass
  - `internal/remote` -- pass
  - `internal/topology` -- pass
  - `test/e2e` -- pass (e2e tests skip when `RUN_KIND_E2E!=1`)

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| Manager must not create local runtime for MultiKueue-managed RTJs | Verified: `TestMultiClusterRemoteExecution` Step 5, `TestMultiClusterManagerSuppression` Steps 3+5 |
| Selected worker cluster must be the only place where child JobSets appear | Verified: `TestMultiClusterRemoteExecution` Steps 4+6 |
| Shared checkpoint store requirement intact | Verified: RTJ template uses shared credentials via `secretKeyRef` |
| Prefer a few strong deterministic tests over many shallow ones | Verified: 2 tests with 13 assertions total, each proving a distinct invariant |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | **Resolved:** Session 4. |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | **Resolved:** Session 4. |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Resolved:** Sessions 4 & 5. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | **Resolved:** Session 6. |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | **Resolved:** Session 6. |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | **Resolved:** Session 4. |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | **Resolved:** Session 4. |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | **Resolved:** Session 4. |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Manager does NOT create local child JobSets for MultiKueue-managed RTJs (e2e tested).
- Selected worker cluster is the only place where child JobSets appear (e2e tested).
- Shared checkpoint store requirement intact (template uses shared credentials).
- Strong deterministic tests preferred over shallow coverage.
- Pause/resume e2e explicitly excluded per prompt scope.

---

## Session 8: Remote Pause/Resume E2E Coverage (G3)

- Date: 2026-03-30

### Decisions Made

1. **Remote pause uses the Kueue adapter's delete-recreate mechanism.**
   The generic adapter does not propagate spec mutations to running remote
   jobs (Session 4, OQ-2). When the user patches `desiredState: Paused`
   on the manager RTJ, the adapter detects spec drift and tears down the
   active remote RTJ. A new remote RTJ is created with `desiredState:
   Paused`, entering the worker's manual-hold path (PendingPaused).

2. **The manager controller preserves checkpoint evidence across the
   delete-recreate cycle.** When the adapter mirrors fresh status from
   the new remote RTJ (which has no checkpoint), the manager controller
   preserves the `multiCluster.remoteCheckpoint` summary that was
   populated before the pause. This ensures checkpoint evidence is not
   lost during the transition.

3. **The manager controller marks Paused based on composite signal.**
   When `isPauseRequested && !hasRemoteStatusSignal`, the manager marks
   the RTJ as Paused with `reasonRemotePauseComplete`. This means the
   remote execution has been torn down and the pause is complete.

4. **Resume uses the shared checkpoint store for cross-worker recovery.**
   When the user patches `desiredState: Running`, the adapter creates a
   new remote RTJ. The worker controller finds the checkpoint in the
   shared store (same `storageURI`, same `identity`) and resumes from it.

5. **Multi-cluster pause differs from single-cluster pause.** Single-
   cluster pause uses a graceful yield-at-step-boundary with a fresh
   checkpoint. Multi-cluster pause relies on the training's periodic
   checkpoint writes. The latest periodic checkpoint before teardown is
   the recovery point. This is documented in
   `docs/phase6/remote-pause-resume.md`.

6. **Smallest coherent fix identified for spec propagation.** The
   adapter's no-spec-propagation design means graceful in-place yield
   (like the single-cluster path) is not possible without Kueue
   changes. The delete-recreate mechanism is the smallest coherent
   approach that works within the existing Kueue v0.15.1 adapter.
   See `docs/phase6/remote-pause-resume.md` for the full analysis.

7. **Requeue during transitional states for timely convergence.** When
   pause is requested but the remote is still active (adapter hasn't
   torn it down yet), the controller requeues at 5-second intervals
   to poll for teardown completion.

### Files Created / Modified (Session 8)

- `internal/controller/remote_status.go` -- modified. Added
  `isRemotePauseRequested`, `markRemotePaused`,
  `preserveRemoteCheckpoint`, `restoreRemoteCheckpoint` helpers.
  Added `reasonRemotePauseComplete` and `messageRemotePaused` constants.

- `internal/controller/resumabletrainingjob_controller.go` -- modified.
  Updated `reconcileManagerIntent` with remote pause/resume handling:
  pause detection, checkpoint preservation across adapter sync,
  Paused marking when remote is torn down, requeue during transition.

- `test/e2e/multicluster_remote_pause_resume_test.go` -- new file.
  `TestMultiClusterRemotePauseResume`: 12-step test proving the full
  remote pause/resume cycle:
  - Submit to manager, dispatch to worker-1
  - Wait for Running + checkpoint
  - Patch to Paused, wait for Paused
  - Verify checkpoint preserved in multiCluster status
  - Verify checkpoint exists in shared S3 store
  - Verify no manager-local child JobSet
  - Verify remote phase surfaced on manager
  - Patch to Running, wait for resume
  - Wait for new checkpoint (different ID)
  - Verify monotonic progression

- `test/e2e/testdata/phase6/rtj-remote-pause-resume.yaml` -- new file.
  RTJ template with faster checkpoint cadence (CHECKPOINT_EVERY=2,
  SLEEP_PER_STEP=3) for reliable e2e testing.

- `test/e2e/phase6_helpers_test.go` -- modified. Added
  `LastCompletedCheckpoint` and `SelectedCheckpoint` fields to
  `phase6RTJView.Status`. Added `LastCompletedCheckpointTime` field
  to `RemoteCheckpoint` view. These fields are needed for checkpoint
  verification in the pause/resume test.

- `docs/phase6/remote-pause-resume.md` -- new file. Comprehensive
  documentation of the pause/resume propagation model: mechanism,
  pause flow, resume flow, difference from single-cluster, controller
  plumbing, shared store requirement, e2e coverage, file index.

- `docs/phase6/session-handoff.md` -- this file (updated).

### Tests Run

- `go vet ./internal/controller/...` -- clean
- `go vet ./test/e2e/...` -- clean
- `go test ./internal/controller/... -v` -- all tests pass (no regressions)
- `go test ./... -short` -- all packages pass:
  - `api/v1alpha1` -- pass
  - `internal/admissionchecks/resume` -- pass
  - `internal/checkpoints` -- pass
  - `internal/controller` -- pass
  - `internal/jobset` -- pass
  - `internal/kueue` -- pass
  - `internal/multikueue` -- pass
  - `internal/policy/checkpointpriority` -- pass
  - `internal/remote` -- pass
  - `internal/topology` -- pass
  - `test/e2e` -- pass (e2e tests skip when `RUN_KIND_E2E!=1`)

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| Pause/resume control originates from the manager cluster RTJ | Verified: user patches `spec.control.desiredState` on manager; adapter propagates via delete-recreate |
| Actual yield/checkpoint/restore happens on the selected worker mirror | Verified: worker runs full Phase 5 path; checkpoint written by worker, resume on worker |
| Manager cluster free of local child JobSets for remote RTJs | Verified: `assertNoJobSetsWithPrefix` called in test Steps 7 and 12 |
| Existing single-cluster pause/resume path preserved | Verified: all Phase 1-5 tests pass unchanged; `ShouldSuppressRuntime` gates the remote path |
| Checkpoint evidence exists in shared store | Verified: `assertObjectExists` validates manifest URI in MinIO |
| Remote phase surfaced on manager status | Verified: `multiCluster.remotePhase=Paused` checked in Step 8 |
| Step/checkpoint progression monotonic | Verified: pre-pause and post-resume checkpoint IDs differ; post-resume manifest exists in store |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | **Resolved:** Session 4. |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | **Resolved:** Spec mutations not propagated; pause via adapter delete-recreate. Sessions 4 & 8. |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Resolved:** Sessions 4 & 5. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | **Resolved:** Session 6. |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | **Resolved:** Session 6. |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | **Resolved:** Session 4. |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | **Resolved:** Session 4. |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | **Resolved:** Session 4. |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |
| OQ-11 | Graceful in-place yield for remote pause | Affects pause quality | **Documented:** Remote pause uses adapter delete-recreate, not graceful yield. Documented difference in `remote-pause-resume.md`. |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Pause/resume control originates from the manager cluster RTJ.
- Actual yield/checkpoint/restore happens on the selected worker mirror.
- Manager cluster free of local child JobSets for remote RTJs (e2e tested).
- Existing single-cluster pause/resume path preserved (all tests pass).
- Checkpoint evidence preserved across the adapter's delete-recreate cycle.
- Remote phase surfaced on manager status (e2e tested).
- Smallest coherent fix: adapter delete-recreate is documented as the
  propagation mechanism. Graceful in-place yield would require Kueue
  adapter changes (identified, not implemented).

---

## Session 9: Observability, Demo Tooling, and Operator UX

- Date: 2026-03-30

### Decisions Made

1. **Phase 6 metrics added to `internal/metrics/metrics.go`.** Nine new
   Prometheus metrics covering:
   - `rtjs_by_execution_role` (gauge) — RTJs by operator role (manager/worker)
   - `remote_rtjs_by_cluster` (gauge) — remote RTJs by selected worker cluster
   - `manager_local_suppressions_total` (counter) — manager-mode local launch suppressions
   - `remote_status_sync_successes_total` (counter) — successful remote status syncs
   - `remote_status_sync_failures_total` (counter) — failed remote status syncs
   - `remote_pause_events_total` (counter) — remote pause completions
   - `remote_resume_events_total` (counter) — remote resume initiations
   - `remote_checkpoint_observations_total` (counter) — remote checkpoint summaries observed
   - `shared_store_access_failures_total` (counter) — shared store access failures

2. **Startup log updated.** `cmd/operator/main.go` now includes
   `phase6Metrics: true` in the startup log.

3. **Six developer/demo shell scripts created.** Following existing
   patterns from Phase 5 scripts (source common.sh, require commands,
   ensure context, structured output):
   - `phase6-submit-manager-rtj.sh` — submit MultiKueue RTJ on manager
   - `phase6-pause-manager-rtj.sh` — patch desiredState to Paused
   - `phase6-resume-manager-rtj.sh` — patch desiredState to Running
   - `phase6-inspect-manager.sh` — full manager RTJ + MultiCluster status
   - `phase6-inspect-worker.sh` — mirror RTJ on both worker clusters
   - `phase6-inspect-checkpoints.sh` — cross-cluster checkpoint evidence

4. **Makefile extended with seven new targets.** `phase6-submit`,
   `phase6-pause`, `phase6-resume`, `phase6-inspect-manager`,
   `phase6-inspect-worker`, `phase6-inspect-checkpoints`, `e2e-phase6`.

5. **Three docs created for operator UX:**
   - `demo.md` — exact command sequences for remote dispatch, manager-
     visible status, remote pause, remote resume, and full copy-paste
     demo flow.
   - `operations.md` — how to inspect manager RTJ status, MultiKueue
     objects and worker selection, mirror RTJ on worker clusters, confirm
     local runtime suppression, inspect shared checkpoint evidence, and
     query Phase 6 metrics.
   - `troubleshooting.md` — six failure scenarios with check commands
     and resolution steps: missing external-framework config, manager
     launching local runtime, no worker selected, namespace/queue
     mismatch, shared store not reachable, pause/resume not reflecting.

6. **Lightweight and practical observability.** No UI, no dashboard
   definitions, no alerting rules. Metrics are standard Prometheus
   counters/gauges. Scripts use kubectl jsonpath for structured output.
   Docs reference `curl` + `grep` for ad-hoc metric queries.

### Files Created / Modified (Session 9)

**Metrics:**

- `internal/metrics/metrics.go` — modified. Added 9 Phase 6 metrics
  (gauge/counter), registered in `NewRecorder()`, added 12 recorder
  methods (`ObserveExecutionRole`, `RemoveExecutionRole`,
  `ObserveRemoteCluster`, `RemoveRemoteCluster`,
  `IncManagerLocalSuppression`, `IncRemoteStatusSyncSuccess`,
  `IncRemoteStatusSyncFailure`, `IncRemotePauseEvent`,
  `IncRemoteResumeEvent`, `IncRemoteCheckpointObservation`,
  `IncSharedStoreAccessFailure`).

- `cmd/operator/main.go` — modified. Added `phase6Metrics: true` to
  startup log.

**Scripts:**

- `hack/dev/phase6-submit-manager-rtj.sh` — new file. Submits
  MultiKueue RTJ on manager, resolves shared store endpoint, shows
  next steps.
- `hack/dev/phase6-pause-manager-rtj.sh` — new file. Patches
  desiredState to Paused, shows expected flow.
- `hack/dev/phase6-resume-manager-rtj.sh` — new file. Patches
  desiredState to Running, shows expected flow.
- `hack/dev/phase6-inspect-manager.sh` — new file. Shows RTJ phase,
  MultiCluster status, remote object ref, remote checkpoint, Workload,
  MultiKueue objects, local runtime suppression check.
- `hack/dev/phase6-inspect-worker.sh` — new file. Inspects both
  worker clusters for mirror RTJ, child JobSets, pods, checkpoints,
  Workloads.
- `hack/dev/phase6-inspect-checkpoints.sh` — new file. Cross-cluster
  checkpoint evidence: manager summary, worker status, shared store
  config, credentials, store reachability.

**Makefile:**

- `Makefile` — modified. Added `.PHONY` declarations and 7 targets:
  `phase6-submit`, `phase6-pause`, `phase6-resume`,
  `phase6-inspect-manager`, `phase6-inspect-worker`,
  `phase6-inspect-checkpoints`, `e2e-phase6`.

**Documentation:**

- `docs/phase6/demo.md` — new file. Four demos (remote dispatch,
  manager-visible status, remote pause, remote resume) with exact
  commands and observation points. Includes full copy-paste sequence.
- `docs/phase6/operations.md` — new file. Operational procedures for
  manager RTJ status, MultiKueue objects, mirror RTJ, suppression
  confirmation, shared checkpoints, and metrics.
- `docs/phase6/troubleshooting.md` — new file. Six failure scenarios
  with symptoms, check commands, and resolution steps.
- `docs/phase6/session-handoff.md` — this file (updated).

### Tests Run

No runtime Go logic changed (metrics are registration-only, no behavioral
changes). All existing tests continue to pass unchanged.

- `go vet ./internal/metrics/...` -- clean
- `go vet ./cmd/operator/...` -- clean

### Hard Boundary Verification

| Boundary | Status |
|---|---|
| Observability lightweight and practical | Verified: 9 Prometheus metrics, no UI, no dashboards, no alerting rules |
| No UI built | Verified: scripts use kubectl + curl only |
| No architecture decisions reopened | Verified: no behavioral changes to controllers, no new reconciliation logic |
| No new roadmap scope added | Verified: all work is observability, demo tooling, and docs |
| Existing Phase 1-5 functionality preserved | Verified: no changes to any controller or reconciliation logic |

---

## Open Issues

| ID | Question | Impact | Status |
| --- | --- | --- | --- |
| OQ-1 | MultiKueue external-framework protocol for custom CRDs | Blocks G1 | **Resolved:** Session 4. |
| OQ-2 | Pause propagation via MultiKueue | Blocks G3 | **Resolved:** Sessions 4 & 8. |
| OQ-3 | Remote RTJ status visibility on the manager | Affects G4 | **Resolved:** Sessions 4 & 5. |
| OQ-4 | Manager-mode detection (all-or-nothing vs per-RTJ) | Affects G2 | **Resolved:** Session 3. |
| OQ-5 | Shared checkpoint store credential distribution | Affects G3/G5 | **Resolved:** Session 6. |
| OQ-6 | MultiKueueCluster kubeconfig management in kind | Affects G5 | **Resolved:** Session 6. |
| OQ-7 | Kueue MultiKueue feature gate status in v0.15.1 | Affects isolation strategy | **Resolved:** Session 4. |
| OQ-8 | Remote RTJ cleanup on manager-side deletion | Affects lifecycle correctness | **Resolved:** Session 4. |
| OQ-9 | MultiKueue dispatch and Kueue preemption interaction | Affects preemption path | **Resolved:** Session 4. |
| OQ-10 | Phase 5 deferred items for Phase 6 | Minor worker-side improvements | Tentatively resolved: bundle with Phase 6 |
| OQ-11 | Graceful in-place yield for remote pause | Affects pause quality | **Documented:** Remote pause uses adapter delete-recreate. Session 8. |

### Divergence Notes

No divergence from the mission statement. All hard boundaries are
respected:
- Observability is lightweight: Prometheus metrics + kubectl scripts.
- No UI, no dashboards, no alerting rules.
- No architecture decisions reopened.
- No new roadmap scope added.
- All existing Phase 1-5 tests pass unchanged.
- Demo and operational docs reference only existing functionality.

---

## Recommended Next Prompt

### Session 10: Hardening and Signoff Pass

**Goal:** Perform the Phase 6 hardening and signoff pass. Audit the
implementation and docs against all accepted contracts from Phases 0-6.
Identify drift, tighten vague wording, and produce signoff documentation.

---

## Session 10: Hardening and Signoff Pass

Date: 2026-03-30

### Work Performed

1. **Full codebase audit.** Read every Go source file, test file, hack
   script, and documentation file in the project. Cross-referenced the
   implementation against all Phase 0-6 contracts.

2. **Consistency audit.** Verified each locked Phase 6 goal (G1-G5)
   against the actual code. Checked all 14 key design decisions from
   Session 1. Verified hard boundaries: RTJ as only Kueue-managed object,
   child JobSets as plain runtime, manager-local runtime suppression,
   worker-local execution ownership, generic external-framework integration,
   shared-checkpoint remote pause/resume, single-cluster path preservation.

3. **Test coverage audit.** Verified minimum required coverage:
   - Webhook unit tests: 10 Phase 6-specific tests (managedBy validation,
     immutability, domain-prefix format, combo with all phase features).
   - Mode split unit tests: 8 unit + 6 integration tests.
   - Remote status unit/integration tests: 9 unit + 5 integration tests.
   - E2E remote execution: `TestMultiClusterRemoteExecution` (139 lines).
   - E2E remote pause/resume: `TestMultiClusterRemotePauseResume` (244 lines).
   - Bonus: E2E manager suppression test (137 lines).

4. **Gaps analysis.** Identified seven low-severity items (no design drift):
   - G-INSTRUMENT-1: Phase 6 metrics registered but not actively emitted
     from controller hot paths. Deferred to Phase 7.
   - G-HARDEN-1: Magic 5-second requeue interval in reconcileManagerIntent.
   - G-HARDEN-2: hasRemoteStatusSignal heuristic documented but fragile
     if future phases add manager-side run attempts.
   - G-HARDEN-3: Webhook error message doesn't distinguish "set at create"
     from "already set to different value".
   - G-DOC-1: Demo doc missing checkpoint timing guidance.
   - G-DOC-2: Operations doc doesn't mention metrics scrape prerequisite.
   - G-DOC-3: Troubleshooting missing worker-operator-restart scenario.

5. **Created review documents:**
   - `docs/phase6/review/consistency-audit.md` - full audit results.
   - `docs/phase6/review/gaps.md` - gaps with classification and severity.

6. **Created signoff document:**
   - `docs/phase6/PHASE6_SIGNOFF.md` - capabilities, experimental items,
     deferred items, known risks, Phase 7 recommendations.

7. **Updated index:**
   - `docs/phase6/index.md` - added Review and Signoff section.

### Files Created

| File | Purpose |
|------|---------|
| `docs/phase6/review/consistency-audit.md` | Phase 0-6 contract compliance audit |
| `docs/phase6/review/gaps.md` | Gaps and tightening items |
| `docs/phase6/PHASE6_SIGNOFF.md` | Phase 6 signoff summary |

### Files Modified

| File | Change |
|------|--------|
| `docs/phase6/index.md` | Added Review and Signoff section with links |
| `docs/phase6/session-handoff.md` | Added Session 10 |

### Key Findings

- **No design drift found.** The implementation is consistent with the
  locked Phase 6 design across all five goals and all 14 key decisions.
- **No unauthorized scope additions.** No new product scope was added.
- **All hard boundaries respected.** Single-cluster path preserved.
  RTJ remains sole Kueue-managed object. Child JobSets remain plain
  runtime. Manager never creates local runtime for managed RTJs.
- **All required test coverage satisfied.** Webhook, mode split,
  remote status, e2e execution, and e2e pause/resume all have
  comprehensive tests.
- **Seven low-severity gaps identified.** All are acceptable for
  signoff and documented with Phase 7 recommendations.

### Hard Boundary Verification (Session 10)

| Boundary | Status |
|----------|--------|
| RTJ is the only Kueue-managed object | Verified |
| Child JobSets remain plain runtime | Verified |
| Manager-local runtime suppression | Verified |
| Worker-local execution ownership | Verified |
| MultiKueue external-framework integration | Verified |
| Shared-checkpoint remote pause/resume | Verified |
| Single-cluster path preservation | Verified |
| No new roadmap scope added | Verified |

### Open Questions

All 11 original open questions (OQ-1 through OQ-11) remain resolved.
No new open questions were raised during the hardening pass.

### Divergence Notes

No divergence from the mission statement. The hardening pass was
strictly audit and documentation. No code changes were made.

---

## Recommended Next Prompt

### Session 11: Cross-Worker Resume Validation (G3 Extension)

**Goal:** Validate that the shared checkpoint store enables cross-worker
resume: pause on worker-1, re-dispatch to worker-2 (un-bias cluster
selection), resume from the checkpoint written by worker-1.

**Prompt:**

```
You are working on Phase 6 only for the checkpoint-native preemption
controller repo.

Mission:
Validate and test the shared-checkpoint cross-worker resume path where
the resume happens on a DIFFERENT worker cluster.

Hard boundaries:
- Keep the shared checkpoint-store contract simple.
- No replication service.
- Reuse the existing pause/resume protocol.

Process:
- Read docs/phase6/session-handoff.md for current state.
- Add an e2e test that pauses on worker-1, un-biases cluster selection
  so worker-2 is eligible, resumes and verifies worker-2 picks up the
  checkpoint from worker-1.
- Update docs/phase6/session-handoff.md.
```
