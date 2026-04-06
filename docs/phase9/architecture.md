# Phase 9 — Architecture

## Component diagram

```
┌───────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                             │
│                                                                       │
│  ┌──────────────┐   patch targetWorkerCount                          │
│  │  Operator /   │──────────────────────────┐                        │
│  │  Platform     │                          │                        │
│  └──────────────┘                          ▼                        │
│                                   ┌──────────────────┐               │
│                                   │  ResumableTraining│               │
│                                   │  Job (RTJ)        │               │
│                                   │                  │               │
│                                   │ spec.elasticity: │               │
│                                   │   mode: Manual   │               │
│                                   │   target: 4      │               │
│                                   │   inPlaceShrink: │               │
│                                   │     IfSupported  │               │
│                                   └────────┬─────────┘               │
│                                            │                         │
│                                   ┌────────▼─────────┐               │
│                                   │  RTJ Controller   │               │
│                                   │  (resize logic)   │               │
│                                   │                   │               │
│                                   │ 1. Detect delta   │               │
│                                   │ 2. Choose path    │               │
│                                   │ 3. Execute resize │               │
│                                   └──┬─────┬──────┬──┘               │
│                          ┌───────────┘     │      └──────────┐       │
│                          ▼                 ▼                 ▼       │
│                ┌──────────────┐   ┌──────────────┐   ┌────────────┐  │
│                │ Child JobSet  │   │ Kueue         │   │ Checkpoint │  │
│                │ (runtime)     │   │ Workload      │   │ Catalog    │  │
│                │               │   │               │   │ (S3)       │  │
│                │ - patch       │   │ - suspend     │   │            │  │
│                │   replicas    │   │ - reclaimable │   │ - write    │  │
│                │ - delete +    │   │   Pods        │   │ - select   │  │
│                │   recreate    │   │ - PodSet      │   │ - restore  │  │
│                │               │   │   mutation    │   │            │  │
│                └──────────────┘   └──────┬───────┘   └────────────┘  │
│                                          │                           │
│                                 ┌────────▼───────┐                   │
│                                 │  Kueue          │                   │
│                                 │  Admission      │                   │
│                                 │  Controller     │                   │
│                                 │                 │                   │
│                                 │ - reads         │                   │
│                                 │   reclaimable   │                   │
│                                 │   Pods          │                   │
│                                 │ - releases      │                   │
│                                 │   quota         │                   │
│                                 │ - re-admits on  │                   │
│                                 │   grow          │                   │
│                                 └─────────────────┘                   │
│                                                                       │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Existing subsystems (unchanged)                                  │ │
│  │  Phase 4: Topology  │ Phase 5: Priority  │ Phase 7: Launch Gate  │ │
│  │  Phase 6: MultiKueue│ Phase 8: DRA       │                       │ │
│  └──────────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────┘
```

---

## Resize path decision tree

```
targetWorkerCount changed?
│
├── target == current → no-op
│
├── target < current (SHRINK)
│   │
│   ├── inPlaceShrinkPolicy == Never?
│   │   └── YES → checkpoint-and-relaunch shrink
│   │
│   ├── runtime annotation present?
│   │   │  (training.io/supports-in-place-shrink: "true")
│   │   │
│   │   ├── YES → in-place shrink
│   │   │         1. Patch JobSet replicas
│   │   │         2. Write reclaimablePods
│   │   │         3. Wait for pod termination
│   │   │         4. Clear reclaimablePods
│   │   │
│   │   └── NO → checkpoint-and-relaunch shrink
│   │             1. Pause → checkpoint → teardown
│   │             2. Mutate Workload PodSets
│   │             3. Re-admit → relaunch at smaller size
│   │
│   └── target < parallelism.minCount?
│       └── REJECT (validation error)
│
└── target > current (GROW)
    │
    └── checkpoint-and-relaunch grow
        1. Pause → checkpoint → teardown
        2. Mutate Workload PodSets to larger count
        3. Suspend Workload → request re-admission
        4. Kueue admits at larger size (if quota available)
        5. Relaunch at larger size with DCP restore
```

---

## Sequence diagram: Manual shrink — in-place path

```
Operator          RTJ Controller         Child JobSet       Kueue Workload       Kueue AC
  │                    │                      │                   │                  │
  │ PATCH target=4     │                      │                   │                  │
  │ (current=8)        │                      │                   │                  │
  │───────────────────>│                      │                   │                  │
  │                    │                      │                   │                  │
  │                    │ detect shrink        │                   │                  │
  │                    │ delta = 4 pods       │                   │                  │
  │                    │                      │                   │                  │
  │                    │ check annotation     │                   │                  │
  │                    │ supports-in-place    │                   │                  │
  │                    │ = true               │                   │                  │
  │                    │                      │                   │                  │
  │                    │ status.elasticity.   │                   │                  │
  │                    │ resizePath=InPlace   │                   │                  │
  │                    │                      │                   │                  │
  │                    │ PATCH replicas=4     │                   │                  │
  │                    │─────────────────────>│                   │                  │
  │                    │                      │                   │                  │
  │                    │ PATCH reclaimable    │                   │                  │
  │                    │ Pods=[{workers,4}]   │                   │                  │
  │                    │──────────────────────────────────────────>│                  │
  │                    │                      │                   │                  │
  │                    │                      │                   │ read             │
  │                    │                      │                   │ reclaimablePods  │
  │                    │                      │                   │────────────────> │
  │                    │                      │                   │                  │
  │                    │                      │                   │ release quota    │
  │                    │                      │                   │ for 4 pods       │
  │                    │                      │                   │<────────────────>│
  │                    │                      │                   │                  │
  │                    │ observe 4 pods       │                   │                  │
  │                    │ terminated           │                   │                  │
  │                    │<─────────────────────│                   │                  │
  │                    │                      │                   │                  │
  │                    │ PATCH clear          │                   │                  │
  │                    │ reclaimablePods=[]   │                   │                  │
  │                    │──────────────────────────────────────────>│                  │
  │                    │                      │                   │                  │
  │                    │ status.elasticity.   │                   │                  │
  │                    │ resizePhase=Idle     │                   │                  │
  │                    │ currentWorkerCount=4 │                   │                  │
  │                    │                      │                   │                  │
```

---

## Sequence diagram: Manual grow — checkpoint-and-relaunch

```
Operator          RTJ Controller         Child JobSet       Checkpoint       Kueue Workload
  │                    │                      │               Catalog              │
  │ PATCH target=6     │                      │                  │                  │
  │ (current=4)        │                      │                  │                  │
  │───────────────────>│                      │                  │                  │
  │                    │                      │                  │                  │
  │                    │ detect grow          │                  │                  │
  │                    │ delta = +2 pods      │                  │                  │
  │                    │                      │                  │                  │
  │                    │ status.elasticity.   │                  │                  │
  │                    │ resizePath=C&R       │                  │                  │
  │                    │ resizePhase=InProgress│                 │                  │
  │                    │                      │                  │                  │
  │                    │ desiredState=Paused  │                  │                  │
  │                    │ (write control CM)   │                  │                  │
  │                    │─────────────────────>│                  │                  │
  │                    │                      │                  │                  │
  │                    │                      │ trainer writes   │                  │
  │                    │                      │ checkpoint at    │                  │
  │                    │                      │ step boundary    │                  │
  │                    │                      │─────────────────>│                  │
  │                    │                      │                  │                  │
  │                    │                      │ pods exit        │                  │
  │                    │                      │ gracefully       │                  │
  │                    │<─────────────────────│                  │                  │
  │                    │                      │                  │                  │
  │                    │ record completed     │                  │                  │
  │                    │ checkpoint           │                  │                  │
  │                    │────────────────────────────────────────>│                  │
  │                    │                      │                  │                  │
  │                    │ delete child JobSet  │                  │                  │
  │                    │─────────────────────>│ (deleted)        │                  │
  │                    │                      │                  │                  │
  │                    │ mutate Workload      │                  │                  │
  │                    │ PodSet workers=6     │                  │                  │
  │                    │ suspend=true         │                  │                  │
  │                    │──────────────────────────────────────────────────────────> │
  │                    │                      │                  │                  │
  │                    │                      │                  │   Kueue evaluates│
  │                    │                      │                  │   admission at   │
  │                    │                      │                  │   new size       │
  │                    │                      │                  │                  │
  │                    │ admission granted    │                  │                  │
  │                    │ (6 workers)          │                  │                  │
  │                    │<──────────────────────────────────────────────────────────│
  │                    │                      │                  │                  │
  │                    │ select checkpoint    │                  │                  │
  │                    │ (world-size-flexible)│                  │                  │
  │                    │<───────────────────────────────────────│                  │
  │                    │                      │                  │                  │
  │                    │ create child JobSet  │                  │                  │
  │                    │ attempt N+1          │                  │                  │
  │                    │ replicas=6           │                  │                  │
  │                    │ restoreFrom=ckpt     │                  │                  │
  │                    │─────────────────────>│ (new)            │                  │
  │                    │                      │                  │                  │
  │                    │                      │ DCP reshards to  │                  │
  │                    │                      │ world_size=6     │                  │
  │                    │                      │                  │                  │
  │                    │ status.elasticity.   │                  │                  │
  │                    │ resizePhase=Idle     │                  │                  │
  │                    │ currentWorkerCount=6 │                  │                  │
  │                    │                      │                  │                  │
```

---

## Sequence diagram: Fallback shrink — checkpoint-and-relaunch (no in-place support)

```
Operator          RTJ Controller         Child JobSet       Checkpoint       Kueue Workload
  │                    │                      │               Catalog              │
  │ PATCH target=4     │                      │                  │                  │
  │ (current=8)        │                      │                  │                  │
  │───────────────────>│                      │                  │                  │
  │                    │                      │                  │                  │
  │                    │ detect shrink        │                  │                  │
  │                    │ delta = 4 pods       │                  │                  │
  │                    │                      │                  │                  │
  │                    │ check annotation     │                  │                  │
  │                    │ supports-in-place    │                  │                  │
  │                    │ = absent / false     │                  │                  │
  │                    │                      │                  │                  │
  │                    │ OR inPlaceShrinkPolicy│                 │                  │
  │                    │ = Never              │                  │                  │
  │                    │                      │                  │                  │
  │                    │ status.elasticity.   │                  │                  │
  │                    │ resizePath=C&R       │                  │                  │
  │                    │ resizePhase=InProgress│                 │                  │
  │                    │                      │                  │                  │
  │                    │ desiredState=Paused  │                  │                  │
  │                    │ (write control CM)   │                  │                  │
  │                    │─────────────────────>│                  │                  │
  │                    │                      │                  │                  │
  │                    │                      │ trainer writes   │                  │
  │                    │                      │ checkpoint at    │                  │
  │                    │                      │ step boundary    │                  │
  │                    │                      │─────────────────>│                  │
  │                    │                      │                  │                  │
  │                    │                      │ pods exit        │                  │
  │                    │                      │ gracefully       │                  │
  │                    │<─────────────────────│                  │                  │
  │                    │                      │                  │                  │
  │                    │ record completed ckpt│                  │                  │
  │                    │────────────────────────────────────────>│                  │
  │                    │                      │                  │                  │
  │                    │ delete child JobSet  │                  │                  │
  │                    │─────────────────────>│ (deleted)        │                  │
  │                    │                      │                  │                  │
  │                    │ mutate Workload      │                  │                  │
  │                    │ PodSet workers=4     │                  │                  │
  │                    │ suspend=false        │                  │                  │
  │                    │──────────────────────────────────────────────────────────> │
  │                    │                      │                  │                  │
  │                    │                      │                  │   Kueue re-admits│
  │                    │                      │                  │   at smaller size│
  │                    │                      │                  │   (always fits)  │
  │                    │                      │                  │                  │
  │                    │ admission granted    │                  │                  │
  │                    │ (4 workers)          │                  │                  │
  │                    │<──────────────────────────────────────────────────────────│
  │                    │                      │                  │                  │
  │                    │ select checkpoint    │                  │                  │
  │                    │ (resharding to 4)    │                  │                  │
  │                    │<───────────────────────────────────────│                  │
  │                    │                      │                  │                  │
  │                    │ create child JobSet  │                  │                  │
  │                    │ attempt N+1          │                  │                  │
  │                    │ replicas=4           │                  │                  │
  │                    │ restoreFrom=ckpt     │                  │                  │
  │                    │─────────────────────>│ (new)            │                  │
  │                    │                      │                  │                  │
  │                    │                      │ DCP reshards to  │                  │
  │                    │                      │ world_size=4     │                  │
  │                    │                      │                  │                  │
  │                    │ status.elasticity.   │                  │                  │
  │                    │ resizePhase=Idle     │                  │                  │
  │                    │ currentWorkerCount=4 │                  │                  │
  │                    │                      │                  │                  │
```

---

## Sequence diagram: Manager/worker path under Phase 6 compatibility

```
Operator       Manager RTJ       Manager         MultiKueue        Worker RTJ       Worker
               Controller        Workload         Adapter           Controller       JobSet
  │                │                 │                │                  │               │
  │ PATCH          │                 │                │                  │               │
  │ target=4       │                 │                │                  │               │
  │───────────────>│                 │                │                  │               │
  │                │                 │                │                  │               │
  │                │ propagate       │                │                  │               │
  │                │ spec.elasticity │                │                  │               │
  │                │ to worker RTJ   │                │                  │               │
  │                │────────────────────────────────>│                  │               │
  │                │                 │                │                  │               │
  │                │                 │                │ sync elasticity  │               │
  │                │                 │                │ to worker-side   │               │
  │                │                 │                │ RTJ spec         │               │
  │                │                 │                │─────────────────>│               │
  │                │                 │                │                  │               │
  │                │                 │                │                  │ (worker        │
  │                │                 │                │                  │  executes      │
  │                │                 │                │                  │  resize        │
  │                │                 │                │                  │  locally —     │
  │                │                 │                │                  │  same as       │
  │                │                 │                │                  │  single-       │
  │                │                 │                │                  │  cluster       │
  │                │                 │                │                  │  path)         │
  │                │                 │                │                  │               │
  │                │                 │                │                  │ status.        │
  │                │                 │                │                  │ elasticity     │
  │                │                 │                │                  │ updated        │
  │                │                 │                │                  │               │
  │                │                 │                │ mirror worker    │               │
  │                │                 │                │ status.elasticity│               │
  │                │                 │                │<────────────────│               │
  │                │                 │                │                  │               │
  │                │ read mirrored   │                │                  │               │
  │                │ status          │                │                  │               │
  │                │<───────────────────────────────│                  │               │
  │                │                 │                │                  │               │
  │                │ update manager  │                │                  │               │
  │                │ status.elasticity│               │                  │               │
  │                │ (mirror of      │                │                  │               │
  │                │  worker state)  │                │                  │               │
  │                │                 │                │                  │               │
```

---

## Controller state machine extension

```
                    ┌──────────────────────┐
                    │      Running         │
                    │  (currentWorkers = N)│
                    └──────────┬───────────┘
                               │
                    targetWorkerCount changed
                               │
                    ┌──────────▼───────────┐
                    │  ResizeEvaluating    │
                    │  - compute delta     │
                    │  - choose path       │
                    └──────────┬───────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
     target < current   target < current   target > current
     in-place OK        in-place NOT OK
              │                │                │
    ┌─────────▼────┐  ┌───────▼────────┐  ┌───▼──────────────┐
    │ InPlace      │  │ C&R Shrink     │  │ C&R Grow         │
    │ Shrinking    │  │                │  │                  │
    │              │  │ Pausing →      │  │ Pausing →        │
    │ patch JobSet │  │ Checkpointing →│  │ Checkpointing →  │
    │ reclaimable  │  │ Suspending →   │  │ Suspending →     │
    │ Pods         │  │ Re-admitting → │  │ Re-admitting →   │
    │              │  │ Relaunching    │  │ Relaunching      │
    └──────┬───────┘  └───────┬────────┘  └───┬──────────────┘
           │                  │               │
           └──────────────────┼───────────────┘
                              │
                    ┌─────────▼────────────┐
                    │      Running         │
                    │  (currentWorkers =   │
                    │   targetWorkerCount) │
                    └──────────────────────┘
```

---

## Workload mutation strategy

### In-place shrink (no Workload PodSet mutation)

The Workload's `spec.podSets[].count` is **not** mutated.  Instead, the
controller writes `status.reclaimablePods` to signal Kueue that some of the
admitted quota is no longer needed.  Kueue reads this field and releases the
corresponding quota from the ClusterQueue.  The Workload itself remains
admitted at the original size; the reclaimablePods entry represents the delta.

This avoids requiring Kueue to support in-flight Workload spec mutation.

### Checkpoint-and-relaunch (Workload PodSet mutation via suspend cycle)

1. Controller suspends the Workload (`spec.suspend = true`).
2. Kueue de-admits the Workload and releases all quota.
3. Controller mutates `spec.podSets[].count` to the new target.
4. Controller un-suspends (`spec.suspend = false`).
5. Kueue re-evaluates admission at the new size.

This leverages the existing suspend/un-suspend cycle that Kueue already
supports for external job frameworks, with no new Kueue API surface.

---

## Interaction with existing subsystems

### Phase 3 (Adaptive parallelism)

`spec.resume.allowWorldSizeChange` must be `true` for any resize to succeed,
since DCP resharding is required when the world size changes.  The controller
rejects resize requests if `allowWorldSizeChange` is false.

### Phase 4 (Topology)

Topology constraints apply to the relaunch step.  If topology requirements
change between sizes (e.g., 8 workers across 2 racks vs. 4 workers on 1
rack), the controller includes updated TopologyRequest in the Workload
PodSets before re-admission.

### Phase 5 (Priority)

Effective priority is recalculated after resize.  A resize that completes a
checkpoint resets the checkpoint-age component of the priority score.

### Phase 7 (Launch gating)

Launch gates (ProvisioningRequest, waitForPodsReady) apply to the relaunch
step of checkpoint-and-relaunch, identical to a normal resume.

### Phase 8 (DRA)

Device profile is immutable during resize.  The same ResourceClaimTemplates
are reused.  Checkpoint device-profile compatibility applies to the relaunch
step (fail-closed).
