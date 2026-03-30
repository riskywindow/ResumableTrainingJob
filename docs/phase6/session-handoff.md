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

## Recommended Next Prompt

The operator mode split is implemented. The recommended next session is:

### Session 4: MultiKueue Protocol Investigation

**Goal:** Resolve OQ-1, OQ-2, OQ-3 (remaining), OQ-7, OQ-8, and OQ-9
by inspecting the Kueue v0.15.1 source code for MultiKueue's
external-framework protocol.

**Prompt:**

```
You are working on Phase 6 only for the checkpoint-native preemption
controller repo.

Mission:
Resolve the following open questions by inspecting the Kueue v0.15.1
source code in the Go module cache.

Hard boundaries:
- Do not implement controller logic yet.
- Do not modify Go source files.
- Update only docs/phase6/open-questions.md and
  docs/phase6/session-handoff.md with findings.

Process:
- Read docs/phase6/session-handoff.md for current state.
- Inspect the Kueue v0.15.1 source for MultiKueue implementation.
- For each OQ, document: finding, evidence (file path + relevant code),
  impact on Phase 6 design, recommended resolution.

Questions:
1. OQ-1: Does MultiKueue support dispatching custom external-framework
   CRDs? What interfaces must the RTJ adapter implement?
2. OQ-2: Does MultiKueue propagate spec mutations to remote objects?
3. OQ-3: How exactly does MultiKueue mirror remote status to the manager?
4. OQ-7: What is MultiKueue's feature gate status in Kueue v0.15.1?
5. OQ-8: Does MultiKueue clean up remote objects on manager-side deletion?
6. OQ-9: How does Kueue preemption interact with MultiKueue dispatch?
```
