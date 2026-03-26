# ResumeReadiness Decision Logic

This document describes the readiness decision logic implemented by the
ResumeReadiness AdmissionCheck controller. The controller evaluates whether
an RTJ workload is ready for Kueue admission from a checkpoint/resume
perspective.

## Decision Architecture

The logic is split into three layers:

| Layer | File | Responsibility |
|-------|------|---------------|
| **Reconciler** | `workload_reconciler.go` | I/O: find RTJ, load policy, query catalog |
| **Evaluator** | `evaluator.go` | Pure decision function, no I/O |
| **Policy** | `policy.go` | Resolve ResumeReadinessPolicy defaults |

The evaluator is a pure function (`Evaluate(EvaluatorInput) ReadinessDecision`)
that takes all inputs as parameters. This makes it trivially testable and easy
to reason about.

## Decision Tree

```
Evaluate(input)
│
├─ CatalogError != nil?
│  ├─ FailOpen  → Ready  (ReasonStorageUnavailable)
│  └─ FailClosed → Retry  (ReasonStorageUnavailable)
│
├─ CatalogQueried == false? (no catalog configured)
│  ├─ AllowInitialLaunchWithoutCheckpoint → Ready  (ReasonInitialLaunchReady)
│  ├─ FailOpen  → Ready  (ReasonStorageUnavailable)
│  └─ FailClosed → Retry  (ReasonStorageUnavailable)
│
├─ SelectedCheckpoint == nil? (no compatible checkpoint found)
│  ├─ AllowInitialLaunchWithoutCheckpoint → Ready  (ReasonInitialLaunchReady)
│  ├─ First launch (attempt=0, no prior checkpoint)
│  │  └─ Rejected (ReasonInitialLaunchBlocked)
│  └─ Prior run exists
│     └─ Rejected (ReasonNoCheckpointAvailable)
│
├─ MaxCheckpointAge set AND checkpoint exceeds limit?
│  └─ Rejected (ReasonCheckpointTooOld)
│
└─ Checkpoint found, complete, within age limits
   └─ Ready (ReasonCheckpointReady)
```

## AdmissionCheckState Mapping

| Decision | Kueue State | Reason | Meaning |
|----------|-------------|--------|---------|
| First launch allowed | `Ready` | `InitialLaunchReady` | No checkpoint needed; policy permits |
| Checkpoint valid | `Ready` | `CheckpointReady` | Compatible, complete, age-valid |
| Store error + FailOpen | `Ready` | `StorageUnavailable` | Optimistic pass |
| Store error + FailClosed | `Retry` | `StorageUnavailable` | Transient; requeue 30s |
| Policy load failed | `Retry` | `PolicyResolutionFailed` | Transient; requeue 30s |
| No checkpoint + blocked | `Rejected` | `InitialLaunchBlocked` | Permanent: no checkpoint, policy denies |
| No compatible checkpoint | `Rejected` | `NoCheckpointAvailable` | Permanent: incompatible checkpoints |
| Checkpoint too old | `Rejected` | `CheckpointTooOld` | Permanent: exceeds maxCheckpointAge |

## Pre-Launch vs Launch-Time Boundary

The AdmissionCheck runs **before Kueue admits the workload**. At this stage,
the exact admitted shape is not yet known (partial admission may change the
worker count). The evaluator validates what is knowable pre-launch:

**Pre-launch (AdmissionCheck validates):**
- Checkpoint existence in storage
- Checkpoint completeness (manifest-last semantics)
- Checkpoint age vs policy limit
- Checkpoint compatibility on non-shape dimensions (lineage, code version,
  runtime mode, GPU shape, optimizer mode, sharding mode, format version)
- World-size compatibility using `spec.identity.worldSize` as reference

**Launch-time (operator validates):**
- Exact world-size match after partial admission resolves
- DCP resharding feasibility with the final admitted worker count
- Topology assignment materialization into child JobSet

This boundary is intentional: the AdmissionCheck is conservative and avoids
blocking admission for shape-specific concerns that can only be resolved after
Kueue commits to an admission decision.

## Policy Fields

| Field | Default | Effect |
|-------|---------|--------|
| `requireCompleteCheckpoint` | `true` | Checkpoint must have a valid manifest |
| `maxCheckpointAge` | nil (no limit) | Maximum age for the selected checkpoint |
| `failurePolicy` | `FailClosed` | How to handle store/catalog errors |
| `allowInitialLaunchWithoutCheckpoint` | `true` | Allow first launch with no checkpoint |

## Phase 3 Backward Compatibility

When no ResumeReadiness AdmissionCheck is configured on the ClusterQueue,
this controller does not run and Phase 3 behavior is fully preserved.

When the AdmissionCheck IS configured but no catalog is available (S3
credentials not set), the default policy (`allowInitialLaunchWithoutCheckpoint=true`)
causes all checks to pass immediately — equivalent to the Phase 4 scaffold
behavior.

## Checkpoint Helpers

Two helpers were added to support the evaluator:

- `checkpoints.IsCheckpointTooOld(manifest, maxAge, now)` — age comparison
  in `internal/checkpoints/compatibility.go`
- `checkpoints.ResumeRequestFromRTJ(rtj, clusterIdentity)` — builds a
  `ResumeRequest` from RTJ spec fields in `internal/checkpoints/selector.go`

## Implementation Files

| File | Description |
|------|-------------|
| `internal/admissionchecks/resume/evaluator.go` | Pure decision function |
| `internal/admissionchecks/resume/evaluator_test.go` | 15 tests for all decision paths |
| `internal/admissionchecks/resume/policy.go` | Policy resolution and loading |
| `internal/admissionchecks/resume/workload_reconciler.go` | Reconciler with catalog integration |
| `internal/admissionchecks/resume/workload_reconciler_test.go` | 9 reconciler integration tests |
| `internal/admissionchecks/resume/constants.go` | Decision reason constants |
| `internal/admissionchecks/resume/setup.go` | Manager wiring (accepts optional catalog) |
| `internal/checkpoints/compatibility.go` | Added `IsCheckpointTooOld` |
| `internal/checkpoints/selector.go` | Added `ResumeRequestFromRTJ` |
