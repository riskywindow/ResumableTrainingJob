# Phase 9 — MultiCluster Compatibility

This document describes how Phase 9 elasticity integrates with the
Phase 6 manager/worker MultiKueue path.

## Design Principle

Phase 9 elasticity follows the same transparent-split model established in
Phase 6 and preserved in Phases 7 and 8:

- **Worker mode** runs the full Phase 9 resize path unchanged.
- **Manager mode** suppresses all resize execution for MultiKueue-managed
  RTJs and surfaces worker-side elasticity status for observability.
- **Single-cluster mode** is unaffected — Phase 9 behavior is identical
  whether or not MultiKueue is configured.

## What Changes on Workers

Workers run the complete Phase 9 elastic resize execution path, including:

| Capability | Worker behavior |
|---|---|
| Elastic plan evaluation | Worker evaluates `EvaluatePlan()` on every reconcile when elasticity is enabled |
| In-place shrink | Worker publishes `reclaimablePods` on the **worker-local** Workload via SSA |
| Checkpoint-and-relaunch | Worker executes the drain flow with `stopSourceResize`, checkpoints, and relaunches at new size |
| reclaimablePods lifecycle | Worker-local only — reclaimablePods are written to and cleared from the worker-side Workload |
| Resize completion detection | Worker detects `Restoring → Running` transition with `isResizeTriggeredStop()` |
| DRA during resize | Device profile is immutable during resize; compat checked on relaunch (Phase 8 preserved) |
| Launch gates during resize | ProvisioningRequest, waitForPodsReady apply to relaunch step (Phase 7 preserved) |
| Environment variable injection | `YIELD_SDK_TARGET_WORKER_COUNT` injected into all containers on the worker |

The worker does not know or care that it was dispatched by a manager.
The elastic resize path is identical to single-cluster Phase 9.

## What Remains the Same on Manager

The manager controller suppresses all local runtime for MultiKueue-managed
RTJs. Phase 9 adds no new manager-side runtime behavior:

| Concern | Manager behavior |
|---|---|
| Local child JobSet creation | **NEVER** — suppressed by `ShouldSuppressRuntime()` |
| Elastic plan evaluation | **NEVER** — suppression returns before the elastic plan block |
| reclaimablePods write | **NEVER** — no SSA patches to any Workload |
| Resize execution | **NEVER** — no stop flow, checkpoint, or relaunch triggered |
| Reclaim helper state | **NEVER** — no local reclaim state created |
| Elasticity status population | **NEVER** — the manager does not set `status.elasticity` fields |
| Elasticity status surfacing | **YES** — the adapter mirrors the worker's full `.status` including `status.elasticity`; the manager logs it for observability |

### Suppression Path

The existing `ShouldSuppressRuntime()` gate at `resumabletrainingjob_controller.go:139`
returns before the elastic plan evaluation block at line ~222. This means:

```
Manager Reconcile
  ↓
ShouldSuppressRuntime() == true
  ↓
reconcileManagerIntent()          ← manager path
  ↓
Log Phase 7 remote launch status  ← existing
Log Phase 8 remote DRA status     ← existing
Log Phase 9 remote elastic status ← NEW
  ↓
Return (never reaches elastic plan evaluation)
```

### Remote Elasticity Surfacing

When the adapter mirrors a Phase 9 worker's status, the manager detects
`hasPhase9RemoteStatus()` and logs a `remoteElasticitySummary`:

- `remoteResizeState` — worker's resize state (Idle, InProgress, Completed, etc.)
- `remoteResizePath` — InPlace or CheckpointAndRelaunch
- `remoteTargetWorkers` — worker's current resize target
- `remoteActiveWorkers` — worker's observed active pod count
- `remoteAdmittedWorkers` — worker's Kueue admission size
- `remoteReclaimablePodsPublished` — whether worker has published reclaimablePods
- `remoteInPlaceShrinkSupported` — worker's runtime capability
- `remoteExecutionMode` — Fixed or Elastic

This is observability only. The manager does not act on these values.

### Backward Compatibility

| Worker version | Manager behavior |
|---|---|
| Phase 6 (no Phase 9) | No `status.elasticity` mirrored; `hasPhase9RemoteStatus()` returns false |
| Phase 7 (no Phase 9) | Same as Phase 6 |
| Phase 8 (no Phase 9) | Same as Phase 6 |
| Phase 9 (elastic disabled) | `status.elasticity` has `executionMode=Fixed`; `hasPhase9RemoteStatus()` returns false |
| Phase 9 (elastic enabled) | Full elasticity summary logged |

## Test Coverage

### Unit Tests (internal/controller/remote_status_test.go)

| Test | Description |
|---|---|
| `TestBuildRemoteElasticitySummaryFullState` | All Phase 9 elasticity fields populated |
| `TestBuildRemoteElasticitySummaryEmptyStatus` | No Phase 9 fields (Phase 8 worker) |
| `TestBuildRemoteElasticitySummaryCheckpointAndRelaunch` | Grow via C&R scenario |
| `TestHasPhase9RemoteStatus` | Table-driven: elastic mode, fixed mode, empty, nil |
| `TestManagerModeReflectsPhase9WorkerElasticStatus` | Integration: manager preserves in-place shrink state from worker |
| `TestManagerModeReflectsPhase9WorkerGrowResize` | Integration: manager preserves grow-via-relaunch state from worker |
| `TestManagerModePhase8WorkerHasNoPhase9Fields` | Backward compat: Phase 8 worker produces no Phase 9 status |
| `TestManagerModeDoesNotExecuteElasticResize` | Integration: elastic spec on manager does not trigger local resize |
| `TestManagerModeResizeStateTransitionReflected` | Integration: manager reflects state transitions (InProgress → Completed) |

### E2E Smoke Test (test/e2e/multicluster_elastic_smoke_test.go)

| Test | Description |
|---|---|
| `TestMultiClusterElasticSmokeManagerSuppression` | Elastic RTJ dispatched via MultiKueue; manager suppresses local runtime; suppression invariant checked over 30s window |

This test requires the Phase 6 multi-cluster environment (`make phase6-up`)
and `RUN_KIND_E2E=1`. It uses the Phase 6 helper infrastructure.

### What Is NOT Covered (Deferred)

| Item | Reason |
|---|---|
| Full multi-cluster resize execution (shrink/grow) | Requires combined Phase 6 + 9 infrastructure; deferred to integration milestone |
| Multi-cluster reclaimablePods mirroring (OQ-3) | Stretch work per Phase 9 design; reclaimablePods is worker-local |
| Cross-cluster resize failover | Worker switch during resize is an advanced scenario; deferred |
| Manager-initiated resize target propagation | Requires adapter spec-propagation support; deferred |
| Combined DRA + elasticity in multi-cluster | Each is independently compatible; combined test deferred |

## Invariants

Phase 9 multi-cluster compatibility preserves all Phase 6-8 invariants
and adds:

| ID | Invariant |
|---|---|
| I-6 | Manager/worker split is transparent to single-cluster use (Phase 6, preserved) |
| I-9 | Elasticity disabled ≡ Phase 8 behavior (Phase 9, preserved across clusters) |
| I-12 | Manager never evaluates elastic plans for remote RTJs (NEW) |
| I-13 | reclaimablePods is published only on the executing worker-side Workload (NEW) |
| I-14 | Manager never creates reclaim helper state for remote RTJs (NEW) |

## Ownership Table (Phase 9 Extension)

| Concern | MultiKueue | Manager Controller | Worker Controller |
|---|---|---|---|
| Elastic plan evaluation | Not | MUST NOT evaluate | Authoritative |
| In-place shrink execution | Not | MUST NOT execute | Authoritative |
| reclaimablePods write | Not | MUST NOT write | Authoritative (worker Workload) |
| Checkpoint-and-relaunch resize | Not | MUST NOT execute | Authoritative |
| Resize completion detection | Not | MUST NOT detect | Authoritative |
| Elasticity status surfacing | Transport (mirrors status) | Logs for observability | Produces status |
