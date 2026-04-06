# Phase 9 — Elastic Planning Model

## Overview

The controller-side elasticity planning model is a pure-function evaluator
that maps a snapshot of the current RTJ state to a deterministic plan output.
It drives the entire resize lifecycle without performing any mutations itself —
the controller integration layer reads inputs from Kubernetes objects and
applies plan outputs.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│ Controller Reconcile Loop                            │
│                                                     │
│  ┌──────────────────────┐   ┌────────────────────┐  │
│  │ buildElasticPlanInput│──▶│  EvaluatePlan()    │  │
│  │ (reads RTJ, Workload)│   │  (pure function)   │  │
│  └──────────────────────┘   └────────┬───────────┘  │
│                                      │              │
│                                      ▼              │
│                             ┌────────────────────┐  │
│                             │   PlanOutput       │  │
│                             │  {Kind, Delta,     │  │
│                             │   Checkpoint?,     │  │
│                             │   Relaunch?,       │  │
│                             │   Reason}          │  │
│                             └────────┬───────────┘  │
│                                      │              │
│             ┌────────────────────────┼──────┐       │
│             ▼                        ▼      ▼       │
│  ┌──────────────────┐  ┌──────────┐ ┌──────────┐   │
│  │syncElasticStatus │  │reclaim   │ │Workload  │   │
│  │(RTJ status)      │  │delta     │ │SSA patch │   │
│  └──────────────────┘  └──────────┘ └──────────┘   │
└─────────────────────────────────────────────────────┘
```

## Plan Kinds

| PlanKind | Meaning | Controller Action |
|---|---|---|
| `NoResize` | No resize needed | No-op |
| `ShrinkInPlace` | Reduce workers in-place | Patch JobSet replicas, write reclaimablePods |
| `ShrinkViaRelaunch` | Reduce workers via C&R | Checkpoint → suspend → mutate → re-admit |
| `GrowViaRelaunch` | Increase workers via C&R | Checkpoint → suspend → mutate → re-admit |
| `ResizeBlocked` | Resize cannot proceed | Log reason, set status |
| `ResizeInProgress` | Prior resize still executing | Wait |
| `ReclaimPublished` | reclaimablePods written | Wait for pod termination, then clear |

## Decision Tree

```
elasticity disabled?
  └─ YES → NoResize

workload not admitted?
  └─ YES (and target ≠ current) → ResizeBlocked
  └─ YES (and target = current) → NoResize

preemption in progress?
  └─ YES → ResizeBlocked (coalesce with preemption per OQ-4)

target == 0 or target == current?
  └─ YES → NoResize

target out of bounds?
  └─ YES → ResizeBlocked

reclaimablePods already published?
  └─ YES → ReclaimPublished

resize already InProgress?
  └─ YES → ResizeInProgress

DRA constraints block?
  └─ YES → ResizeBlocked

target < current (SHRINK)?
  ├─ inPlaceShrinkPolicy == Never → ShrinkViaRelaunch
  ├─ runtime supports in-place? → ShrinkInPlace (delta = current - target)
  └─ else → ShrinkViaRelaunch

target > current (GROW)?
  └─ GrowViaRelaunch (always requires checkpoint + new admission)
```

## Plan Inputs

| Field | Source | Description |
|---|---|---|
| `ElasticityEnabled` | `spec.elasticity.mode` | Whether elasticity is active |
| `TargetWorkerCount` | `spec.elasticity.targetWorkerCount` | Desired worker count |
| `CurrentWorkerCount` | `status.elasticity.admittedWorkerCount` | Currently admitted workers |
| `ActiveWorkerCount` | `status.elasticity.activeWorkerCount` | Observed running pods |
| `MinWorkerCount` | `parallelism.minCount` or 1 | Lower bound |
| `MaxWorkerCount` | `parallelism.preferredCount` or `worldSize` | Upper bound |
| `InPlaceShrinkPolicy` | `spec.elasticity.inPlaceShrinkPolicy` | `IfSupported` or `Never` |
| `RuntimeSupportsInPlaceShrink` | `status.elasticity.inPlaceShrinkSupported` | From JobSet annotation |
| `WorkloadAdmitted` | Workload.Status.Admission != nil | Kueue admission state |
| `CurrentResizeState` | `status.elasticity.resizeState` | Prior resize state |
| `ReclaimablePodsPublished` | `status.elasticity.reclaimablePodsPublished` | Whether reclaimablePods written |
| `CheckpointReady` | `status.lastCompletedCheckpoint` | Checkpoint availability |
| `PreemptionInProgress` | `spec.suspend` + phase | Concurrent preemption |
| `DRAConstraintsBlock` | DRA compatibility check | Device constraints |

## Plan Outputs

| Field | Type | Description |
|---|---|---|
| `Kind` | PlanKind | Discrete plan action |
| `ReclaimableWorkerDelta` | int32 | Pods to declare reclaimable (shrink in-place only) |
| `CheckpointRequired` | bool | Whether a checkpoint is needed |
| `RelaunchRequired` | bool | Whether a relaunch cycle is needed |
| `NewWorkerCount` | int32 | Target worker count after the plan |
| `Reason` | string | Machine-readable reason for status.elasticity.resizeReason |
| `Message` | string | Human-readable explanation |

## Reclaim Delta and reclaimablePods

The `ReclaimDelta` type bridges the plan output to the Kueue Workload
reclaimablePods field:

- `ComputeReclaimDelta(plan, podSetName)` → extracts the delta from a plan
- `BuildReclaimablePods(delta)` → constructs the Kueue `[]ReclaimablePod`
- `NeedsReclaimUpdate(desired, existing)` → idempotency guard

## Workload.status.reclaimablePods Patch Strategy

### Chosen approach: Server-Side Apply (SSA) with dedicated field manager

**Field manager:** `rtj-elastic-reclaim`

The controller uses SSA to patch `Workload.status.reclaimablePods` with a
dedicated field manager that is distinct from Kueue's field manager. This
ensures that:

1. **No clobbering of Kueue-owned fields:** The SSA patch payload contains
   only `reclaimablePods`. Our field manager claims ownership of only the
   reclaimablePods list entries we create. Kueue retains ownership of
   `admission`, `conditions`, `admissionChecks`, `requeueState`, and all
   other status fields.

2. **No read-modify-write races:** SSA is a single atomic operation that
   does not require reading the current status first (unlike merge-patch).
   The API server resolves field ownership conflicts at apply time.

3. **List-map compatibility:** `reclaimablePods` is declared with
   `+listType=map` and `+listMapKey=name` in the Kueue API. SSA treats
   each PodSet entry as an independently owned item, so we can add/update
   our worker PodSet entry without affecting other entries.

4. **Idempotency:** The `NeedsReclaimUpdate()` guard prevents unnecessary
   patches when the desired state already matches the current state.

### Why not merge-patch?

Merge-patch on a status subresource replaces the entire status object or
operates on top-level fields. Under the pinned Kueue v0.15.1:

- Kueue's internal reconcilers write `admission`, `conditions`, etc. via
  strategic merge patch on the status subresource.
- If we also use merge-patch, we risk overwriting Kueue's fields if our
  read-modify-write cycle races with Kueue's status updates.
- SSA eliminates this race entirely by operating at the field level with
  explicit ownership boundaries.

### Why not a feature gate?

SSA field ownership is a GA feature in Kubernetes and is the recommended
approach for multi-writer status fields. A feature gate would add
unnecessary complexity for a strictly safer approach.

### Kueue v0.15.1 compatibility

Kueue v0.15.1 reads `reclaimablePods` from the Workload status during
quota management. It does not write to `reclaimablePods` — the field is
intended for external writers (job frameworks). This confirms that our
SSA approach has no ownership conflicts with Kueue.

The Kueue documentation for `reclaimablePods` states:
> "reclaimablePods keeps track of the number of pods within a podset for
> which the resource reservation is no longer needed."

This is explicitly designed for the pattern we implement: the job framework
(RTJ controller) writes the count, Kueue reads it for quota release.

## File Layout

```
internal/elastic/
├── types.go          # PlanKind, PlanInput, PlanOutput, ReclaimDelta
├── plan.go           # EvaluatePlan() pure function
├── plan_test.go      # 22 tests
├── reclaim.go        # ComputeReclaimDelta, BuildReclaimablePods, NeedsReclaimUpdate
└── reclaim_test.go   # 13 tests

internal/controller/
├── elastic_plan.go              # buildElasticPlanInput, evaluateElasticPlan, syncElasticityStatus
├── elastic_plan_test.go         # 17 tests
├── workload_status_patch.go     # SSA patch for reclaimablePods
└── workload_status_patch_test.go # 10 tests
```

## Test Coverage Summary

| Area | Tests | Coverage |
|---|---|---|
| Shrink plan generation (in-place) | 2 | Delta calculation, policy gating |
| Shrink plan generation (relaunch) | 2 | Policy=Never, runtime unsupported |
| Grow plan generation | 2 | Standard grow, grow by one |
| No-op behavior | 3 | Disabled, target=current, target=0 |
| Blocked states | 5 | Not admitted, preemption, bounds, DRA |
| In-progress/published | 2 | ResizeInProgress, ReclaimPublished |
| Reclaim delta calculations | 8 | ShrinkInPlace, non-shrink, from existing, needs update |
| reclaimablePods build | 3 | With count, zero, clear |
| Status patch strategy safety | 4 | No admission fields, only reclaimablePods, field manager, clear vs set |
| Idempotency | 6 | Plan idempotency (x3), status idempotency, patch idempotency (x2) |
| Controller integration | 11 | Input building, plan evaluation, status sync |
| **Total** | **62** | |

## Invariants

1. **I-9**: Elasticity disabled ≡ Phase 8 behavior. `EvaluatePlan()` returns
   `NoResize` when `ElasticityEnabled=false`.

2. **I-10**: Scale-up always checkpoint-and-relaunch. `EvaluatePlan()` returns
   `GrowViaRelaunch` for all grow scenarios.

3. **I-11**: reclaimablePods is the only quota-release signal for in-place
   shrink. The SSA patch writes only to `reclaimablePods`.

4. **Idempotency**: Repeated calls with the same input produce the same
   output. `NeedsReclaimUpdate()` prevents redundant patches.

5. **Preemption coalesce (OQ-4)**: When preemption is in progress, the
   planner returns `ResizeBlocked`. The resize target is preserved and
   evaluated after re-admission.
