# Phase 6 Architecture

## Component Diagram

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                                  MANAGER CLUSTER                                         │
│                                                                                          │
│  ┌──────────┐                                                                            │
│  │   User   │                                                                            │
│  └────┬─────┘                                                                            │
│       │ creates                                                                          │
│       v                                                                                  │
│  ┌──────────────────────────────────────────────────────┐                                 │
│  │  ResumableTrainingJob (CRD)                          │                                 │
│  │                                                      │                                 │
│  │  spec.suspend                   (Kueue gate)         │                                 │
│  │  spec.queueName                 → MultiKueue CQ      │                                 │
│  │  spec.control.desiredState      (manual pause/resume) │                                 │
│  │  spec.checkpoint.storageURI     (shared S3 URI)       │                                 │
│  │  spec.workloadPriorityClassName (base priority)       │                                 │
│  │  spec.priorityPolicyRef         (Phase 5, optional)   │                                 │
│  │                                                      │                                 │
│  │  status.phase                   (mirrored from worker)│                                 │
│  │  status.remoteStatus            ◄── NEW               │                                 │
│  │    .workerCluster               (which worker)        │                                 │
│  │    .remotePhase                 (worker-side phase)   │                                 │
│  │    .lastSyncTime                (last mirror time)    │                                 │
│  │  status.lastCompletedCheckpoint (mirrored)            │                                 │
│  │  status.conditions              (merged from worker)  │                                 │
│  └──────────────────┬───────────────────────────────────┘                                 │
│                     │                                                                    │
│        ┌────────────┴───────────────────┐                                                │
│        │                                │                                                │
│        v                                v                                                │
│  ┌──────────────────────────┐   ┌──────────────────────────────────────────────────┐     │
│  │ Kueue (with MultiKueue)  │   │ RTJ Operator (MANAGER MODE)                      │     │
│  │                          │   │                                                   │     │
│  │ ClusterQueue             │   │  ┌────────────────────────────────────────────┐   │     │
│  │  admissionChecks:        │   │  │ Manager RTJ Controller               NEW  │   │     │
│  │   - multikueue           │   │  │                                            │   │     │
│  │                          │   │  │  - Detects MultiKueue dispatch             │   │     │
│  │ MultiKueueCluster        │   │  │  - Does NOT create local child JobSets     │   │     │
│  │  - worker-1 (kubeconfig) │   │  │  - Mirrors remote RTJ status to local     │   │     │
│  │  - worker-2 (kubeconfig) │   │  │  - Propagates desiredState changes        │   │     │
│  │                          │   │  │  - Reflects worker cluster identity       │   │     │
│  │ MultiKueueConfig         │   │  └────────────────────────────────────────────┘   │     │
│  │  - clusters: [w1, w2]    │   │                                                   │     │
│  │                          │   │  No child JobSet created locally.                  │     │
│  │ AdmissionCheck           │   │  No checkpoint I/O on manager.                    │     │
│  │  multikueue controller   │   │  Manager is control-plane only.                   │     │
│  │                          │   │                                                   │     │
│  │ Workload ────────────────┤   │                                                   │     │
│  │  dispatched to worker    │   │                                                   │     │
│  │  via MultiKueue          │   │                                                   │     │
│  └──────────┬───────────────┘   └───────────────────────────────────────────────────┘     │
│             │                                                                            │
└─────────────┼────────────────────────────────────────────────────────────────────────────┘
              │ MultiKueue dispatch
              │ (creates remote RTJ + Workload on worker)
              │
     ┌────────┴────────────────────────────────┐
     │                                         │
     v                                         v
┌──────────────────────────────────────┐ ┌──────────────────────────────────────┐
│        WORKER CLUSTER 1              │ │        WORKER CLUSTER 2              │
│                                      │ │                                      │
│  ┌──────────────────────────────┐    │ │  ┌──────────────────────────────┐    │
│  │ RTJ (remote copy)            │    │ │  │ RTJ (remote copy)            │    │
│  │  created by MultiKueue       │    │ │  │  created by MultiKueue       │    │
│  └──────────────┬───────────────┘    │ │  └──────────────┬───────────────┘    │
│                 │                    │ │                 │                    │
│  ┌──────────────v───────────────┐    │ │  ┌──────────────v───────────────┐    │
│  │ RTJ Operator (WORKER MODE)   │    │ │  │ RTJ Operator (WORKER MODE)   │    │
│  │                              │    │ │  │                              │    │
│  │  Full Phase 5 path:          │    │ │  │  Full Phase 5 path:          │    │
│  │  - GenericJob adapter        │    │ │  │  - GenericJob adapter        │    │
│  │  - Launch gating (Phase 4)   │    │ │  │  - Launch gating (Phase 4)   │    │
│  │  - Topology rendering        │    │ │  │  - Topology rendering        │    │
│  │  - Priority shaping (Ph 5)   │    │ │  │  - Priority shaping (Ph 5)   │    │
│  │  - Graceful yield            │    │ │  │  - Graceful yield            │    │
│  │  - Checkpoint selection      │    │ │  │  - Checkpoint selection      │    │
│  │  - Resume orchestration      │    │ │  │  - Resume orchestration      │    │
│  └──────────────┬───────────────┘    │ │  └──────────────┬───────────────┘    │
│                 │                    │ │                 │                    │
│  ┌──────────────v───────────────┐    │ │  ┌──────────────v───────────────┐    │
│  │ Child JobSet (plain runtime) │    │ │  │ Child JobSet (plain runtime) │    │
│  │  - No Kueue metadata         │    │ │  │  - No Kueue metadata         │    │
│  │  - Local to worker           │    │ │  │  - Local to worker           │    │
│  └──────────────┬───────────────┘    │ │  └──────────────┬───────────────┘    │
│                 │                    │ │                 │                    │
│  ┌──────────────v───────────────┐    │ │  ┌──────────────v───────────────┐    │
│  │ Training Pods                │    │ │  │ Training Pods                │    │
│  │  PyTorch DDP/FSDP + DCP      │    │ │  │  PyTorch DDP/FSDP + DCP      │    │
│  └──────────────┬───────────────┘    │ │  └──────────────┬───────────────┘    │
│                 │ checkpoint write   │ │                 │ checkpoint write   │
│                 v                    │ │                 v                    │
│  ┌──────────────────────────────┐    │ │  ┌──────────────────────────────┐    │
│  │ Kueue (worker-local)        │    │ │  │ Kueue (worker-local)        │    │
│  │  RTJ external framework      │    │ │  │  RTJ external framework      │    │
│  │  Local ClusterQueue          │    │ │  │  Local ClusterQueue          │    │
│  └──────────────────────────────┘    │ │  └──────────────────────────────┘    │
│                                      │ │                                      │
└──────────────────┬───────────────────┘ └──────────────────┬───────────────────┘
                   │                                        │
                   └──────────────┬─────────────────────────┘
                                  │
                                  v
                   ┌──────────────────────────────┐
                   │ Shared Checkpoint Store       │
                   │ (S3-compatible / MinIO)        │
                   │                               │
                   │  manifests/                    │
                   │  checkpoints/                  │
                   │  yield-markers/                │
                   │                               │
                   │  Accessible from all workers   │
                   │  Manager does NOT do ckpt I/O  │
                   └──────────────────────────────┘
```

## Manager/Worker Mode Determination

```
                     Operator Startup
                           │
                           v
                  ┌────────────────┐
                  │  --mode flag?  │
                  └────┬──────┬───┘
                       │      │
                 manager│      │worker (default)
                       │      │
                       v      v
              ┌────────────┐  ┌─────────────┐
              │ Manager    │  │ Worker      │
              │ Mode       │  │ Mode        │
              │            │  │             │
              │ - Status   │  │ - Full      │
              │   mirror   │  │   Phase 5   │
              │ - No local │  │   path      │
              │   JobSets  │  │ - Creates   │
              │ - Pause    │  │   child     │
              │   propagate│  │   JobSets   │
              └────────────┘  └─────────────┘
```

The mode is determined at startup, not at runtime. The operator binary
accepts a `--mode=manager|worker` flag (default: `worker`). This is an
explicit boundary: the same binary can serve either role, but the role
is fixed for the lifetime of the process.

## Sequence Diagram 1: Manager Submit -> Worker Dispatch -> Worker Launch

This diagram shows the initial dispatch of an RTJ from the manager cluster
to a worker cluster via MultiKueue.

```
User            Manager RTJ       Manager Kueue      MultiKueue       Worker Kueue      Worker RTJ
                Controller        (+ MultiKueue)     Controller       (worker-local)    Controller
 │               │                 │                  │                 │                 │
 │  create RTJ   │                 │                  │                 │                 │
 │  on manager   │                 │                  │                 │                 │
 │──────────────>│                 │                  │                 │                 │
 │               │                 │                  │                 │                 │
 │               │  RTJ created    │                  │                 │                 │
 │               │  spec.suspend   │                  │                 │                 │
 │               │  = true (Kueue  │                  │                 │                 │
 │               │  gate)          │                  │                 │                 │
 │               │                 │                  │                 │                 │
 │               │  GenericJob     │                  │                 │                 │
 │               │  adapter creates│                  │                 │                 │
 │               │  Workload       │                  │                 │                 │
 │               │────────────────>│                  │                 │                 │
 │               │                 │                  │                 │                 │
 │               │                 │  CQ has          │                 │                 │
 │               │                 │  multikueue      │                 │                 │
 │               │                 │  AdmissionCheck  │                 │                 │
 │               │                 │                  │                 │                 │
 │               │                 │  dispatch        │                 │                 │
 │               │                 │  Workload to     │                 │                 │
 │               │                 │  worker cluster  │                 │                 │
 │               │                 │─────────────────>│                 │                 │
 │               │                 │                  │                 │                 │
 │               │                 │                  │  create remote  │                 │
 │               │                 │                  │  RTJ + Workload │                 │
 │               │                 │                  │  on worker      │                 │
 │               │                 │                  │────────────────>│                 │
 │               │                 │                  │                 │                 │
 │               │                 │                  │                 │  worker Kueue   │
 │               │                 │                  │                 │  admits the     │
 │               │                 │                  │                 │  remote RTJ     │
 │               │                 │                  │                 │  Workload       │
 │               │                 │                  │                 │  locally        │
 │               │                 │                  │                 │                 │
 │               │                 │                  │                 │  unsuspend RTJ  │
 │               │                 │                  │                 │  on worker      │
 │               │                 │                  │                 │────────────────>│
 │               │                 │                  │                 │                 │
 │               │                 │                  │                 │                 │  worker RTJ
 │               │                 │                  │                 │                 │  controller
 │               │                 │                  │                 │                 │  creates
 │               │                 │                  │                 │                 │  child JobSet
 │               │                 │                  │                 │                 │
 │               │                 │                  │                 │                 │  training
 │               │                 │                  │                 │                 │  pods start
 │               │                 │                  │                 │                 │
 │               │                 │                  │  status mirror  │                 │
 │               │                 │                  │  worker RTJ     │                 │
 │               │                 │                  │  status flows   │                 │
 │               │                 │                  │  back to        │                 │
 │               │                 │                  │  manager        │                 │
 │               │                 │                  │<────────────────────────────────────
 │               │                 │                  │                 │                 │
 │               │                 │  MultiKueue sets │                 │                 │
 │               │                 │  AdmissionCheck  │                 │                 │
 │               │                 │  = Ready on      │                 │                 │
 │               │                 │  manager Workload│                 │                 │
 │               │                 │<─────────────────│                 │                 │
 │               │                 │                  │                 │                 │
 │               │  manager RTJ    │                  │                 │                 │
 │               │  controller     │                  │                 │                 │
 │               │  mirrors remote │                  │                 │                 │
 │               │  status to      │                  │                 │                 │
 │               │  local RTJ      │                  │                 │                 │
 │               │                 │                  │                 │                 │
 │               │  status.phase   │                  │                 │                 │
 │               │  = Running      │                  │                 │                 │
 │               │  status.remote  │                  │                 │                 │
 │               │  .workerCluster │                  │                 │                 │
 │               │  = worker-1     │                  │                 │                 │
```

### Key invariants in the submit flow

1. **Manager does not create a local child JobSet.** The manager RTJ
   controller detects that the Workload has a MultiKueue AdmissionCheck
   and skips local launch entirely.

2. **MultiKueue creates the remote RTJ.** The standard MultiKueue
   external-framework protocol creates a remote copy of the RTJ on the
   selected worker cluster.

3. **Worker runs the full Phase 5 path.** The remote RTJ is reconciled
   by the worker-mode operator exactly as if the user had created it
   locally. Launch gating, topology, priority shaping all apply.

4. **Status flows back.** MultiKueue mirrors remote Workload status to
   the manager. The manager RTJ controller reads this and updates the
   local RTJ status.

## Sequence Diagram 2: Manager Pause -> Worker Yield/Checkpoint -> Manager Paused

This diagram shows the remote pause flow initiated from the manager cluster.

```
User            Manager RTJ       MultiKueue       Worker RTJ        Shared
                Controller        Controller       Controller        Checkpoint
                (manager mode)                     (worker mode)     Store
 │               │                 │                │                  │
 │  patch RTJ    │                 │                │                  │
 │  desiredState │                 │                │                  │
 │  = Paused     │                 │                │                  │
 │──────────────>│                 │                │                  │
 │               │                 │                │                  │
 │               │  manager RTJ    │                │                  │
 │               │  controller     │                │                  │
 │               │  observes       │                │                  │
 │               │  desiredState   │                │                  │
 │               │  = Paused       │                │                  │
 │               │                 │                │                  │
 │               │  propagate      │                │                  │
 │               │  pause to       │                │                  │
 │               │  remote RTJ     │                │                  │
 │               │  via MultiKueue │                │                  │
 │               │────────────────>│                │                  │
 │               │                 │                │                  │
 │               │                 │  patch remote  │                  │
 │               │                 │  RTJ spec      │                  │
 │               │                 │  .control      │                  │
 │               │                 │  .desiredState  │                  │
 │               │                 │  = Paused       │                  │
 │               │                 │───────────────>│                  │
 │               │                 │                │                  │
 │               │                 │                │  worker RTJ      │
 │               │                 │                │  controller      │
 │               │                 │                │  detects pause   │
 │               │                 │                │                  │
 │               │                 │                │  phase =         │
 │               │                 │                │  YieldRequested   │
 │               │                 │                │                  │
 │               │                 │                │  write control   │
 │               │                 │                │  ConfigMap:      │
 │               │                 │                │  desiredState    │
 │               │                 │                │  = Paused        │
 │               │                 │                │                  │
 │               │                 │                │  trainer detects │
 │               │                 │                │  pause at step   │
 │               │                 │                │  boundary        │
 │               │                 │                │                  │
 │               │                 │                │  phase =         │
 │               │                 │                │  Draining        │
 │               │                 │                │                  │
 │               │                 │                │  trainer writes  │
 │               │                 │                │  DCP checkpoint  │
 │               │                 │                │─────────────────>│
 │               │                 │                │  yield marker    │
 │               │                 │                │─────────────────>│
 │               │                 │                │  manifest        │
 │               │                 │                │  (published last)│
 │               │                 │                │─────────────────>│
 │               │                 │                │                  │
 │               │                 │                │  delete child    │
 │               │                 │                │  JobSet          │
 │               │                 │                │                  │
 │               │                 │                │  phase = Paused  │
 │               │                 │                │                  │
 │               │                 │                │  update RTJ      │
 │               │                 │                │  status on       │
 │               │                 │                │  worker          │
 │               │                 │                │                  │
 │               │                 │  mirror status │                  │
 │               │                 │  back to       │                  │
 │               │                 │  manager       │                  │
 │               │                 │<──────────────│                  │
 │               │                 │                │                  │
 │               │  manager RTJ    │                │                  │
 │               │  controller     │                │                  │
 │               │  updates local  │                │                  │
 │               │  RTJ status:    │                │                  │
 │               │  phase = Paused │                │                  │
 │               │  lastCompleted  │                │                  │
 │               │  Checkpoint =   │                │                  │
 │               │  (from worker)  │                │                  │
 │               │<────────────────│                │                  │
```

### Key invariants in the pause flow

1. **The worker executes the existing Phase 2-5 graceful yield path.**
   No new yield protocol is introduced. The same step-boundary yield,
   DCP checkpoint, yield marker, manifest-last publication, and bounded
   drain timer apply.

2. **The checkpoint is written to the shared store.** The shared
   `spec.checkpoint.storageURI` points to S3-compatible storage
   accessible from all clusters.

3. **Manager sees Paused only after the worker completes the yield.**
   The status mirror is eventual-consistency. The manager does not
   short-circuit to Paused before the worker has actually completed
   the checkpoint and teardown.

4. **Manager does not do checkpoint I/O.** The manager reflects
   checkpoint metadata (step, timestamp, URI) from the worker status.
   It does not read or validate checkpoint artifacts directly.

## Sequence Diagram 3: Manager Resume -> Worker Restore

This diagram shows the resume flow after a pause. The RTJ may resume
on a different worker cluster than where it was paused.

```
User            Manager RTJ       MultiKueue       Worker-B Kueue    Worker-B RTJ      Shared
                Controller        Controller       (different from   Controller        Checkpoint
                (manager mode)                     pause worker)     (worker mode)     Store
 │               │                 │                │                 │                  │
 │  patch RTJ    │                 │                │                 │                  │
 │  desiredState │                 │                │                 │                  │
 │  = Running    │                 │                │                 │                  │
 │──────────────>│                 │                │                 │                  │
 │               │                 │                │                 │                  │
 │               │  manager RTJ    │                │                 │                  │
 │               │  controller     │                │                 │                  │
 │               │  observes       │                │                 │                  │
 │               │  desiredState   │                │                 │                  │
 │               │  = Running      │                │                 │                  │
 │               │                 │                │                 │                  │
 │               │  RTJ transitions│                │                 │                  │
 │               │  to Queued      │                │                 │                  │
 │               │  (Kueue         │                │                 │                  │
 │               │  re-queues      │                │                 │                  │
 │               │  the Workload)  │                │                 │                  │
 │               │                 │                │                 │                  │
 │               │                 │  MultiKueue    │                 │                  │
 │               │                 │  selects       │                 │                  │
 │               │                 │  Worker-B      │                 │                  │
 │               │                 │  (may differ   │                 │                  │
 │               │                 │  from Worker-A │                 │                  │
 │               │                 │  where paused) │                 │                  │
 │               │                 │                │                 │                  │
 │               │                 │  create remote │                 │                  │
 │               │                 │  RTJ on        │                 │                  │
 │               │                 │  Worker-B      │                 │                  │
 │               │                 │───────────────>│                 │                  │
 │               │                 │                │                 │                  │
 │               │                 │                │  Worker-B Kueue  │                  │
 │               │                 │                │  admits the      │                  │
 │               │                 │                │  Workload locally │                  │
 │               │                 │                │                 │                  │
 │               │                 │                │  unsuspend RTJ   │                  │
 │               │                 │                │────────────────>│                  │
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  worker RTJ      │
 │               │                 │                │                 │  controller      │
 │               │                 │                │                 │  detects         │
 │               │                 │                │                 │  unsuspend       │
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  select latest   │
 │               │                 │                │                 │  compatible      │
 │               │                 │                │                 │  checkpoint from │
 │               │                 │                │                 │  shared store    │
 │               │                 │                │                 │────────────────>│
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  checkpoint      │
 │               │                 │                │                 │  found (written  │
 │               │                 │                │                 │  by Worker-A)    │
 │               │                 │                │                 │<────────────────│
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  verify compat:  │
 │               │                 │                │                 │  - manifest ok   │
 │               │                 │                │                 │  - identity match│
 │               │                 │                │                 │  - sharding match│
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  create child    │
 │               │                 │                │                 │  JobSet with     │
 │               │                 │                │                 │  RESTORE_FROM    │
 │               │                 │                │                 │  env pointing to │
 │               │                 │                │                 │  shared ckpt     │
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  phase =         │
 │               │                 │                │                 │  Restoring       │
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  trainer loads   │
 │               │                 │                │                 │  checkpoint from │
 │               │                 │                │                 │  shared store    │
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  phase =         │
 │               │                 │                │                 │  Running         │
 │               │                 │                │                 │                  │
 │               │                 │                │                 │  global step     │
 │               │                 │                │                 │  monotonic       │
 │               │                 │                │                 │                  │
 │               │                 │  mirror status │                 │                  │
 │               │                 │<────────────────────────────────│                  │
 │               │                 │                │                 │                  │
 │               │  manager RTJ    │                │                 │                  │
 │               │  status:        │                │                 │                  │
 │               │  phase = Running│                │                 │                  │
 │               │  remoteStatus   │                │                 │                  │
 │               │  .workerCluster │                │                 │                  │
 │               │  = worker-2     │                │                 │                  │
 │               │<────────────────│                │                 │                  │
```

### Key invariants in the resume flow

1. **The worker runs the existing Phase 2-5 resume path.** Checkpoint
   selection uses the `LatestCompatibleComplete` policy from the shared
   store. Compatibility checking (identity, sharding, optimizer, world
   size) applies unchanged.

2. **Cross-worker resume is enabled by the shared checkpoint store.**
   The checkpoint was written by Worker-A's training pods and is read by
   Worker-B's training pods. No checkpoint copying or migration is needed
   because both workers access the same S3-compatible store.

3. **Global step count is monotonic.** The trainer reads the global step
   from the checkpoint manifest and continues from there, regardless of
   which worker cluster it runs on.

4. **Manager reflects the new worker identity.** After resume,
   `status.remoteStatus.workerCluster` updates to reflect Worker-B.

5. **Priority shaping resets.** If priority shaping is active on
   Worker-B, the protection window restarts from the resume time (Phase 5
   invariant, applied locally on the worker).

## Detailed Design

### Manager Mode Behavior

When the operator starts in manager mode:

1. **RTJ reconciler watches RTJs and Workloads.** The reconciler detects
   whether an RTJ is MultiKueue-managed by checking the Workload's
   AdmissionCheck list for a MultiKueue check.

2. **Skip local launch.** When a MultiKueue AdmissionCheck is present,
   the reconciler does NOT create local child JobSets. The launch path is
   entirely bypassed.

3. **Status mirroring.** The manager reads the remote RTJ's status
   (propagated by MultiKueue) and updates the local RTJ status:
   - `status.phase` reflects the remote phase.
   - `status.remoteStatus.workerCluster` identifies the worker.
   - `status.remoteStatus.remotePhase` is the raw remote phase.
   - `status.remoteStatus.lastSyncTime` is the last mirror time.
   - `status.lastCompletedCheckpoint` reflects the remote value.
   - `status.conditions` are merged from the remote RTJ.

4. **Pause propagation.** When the user patches `spec.control.desiredState`
   to `Paused` on the manager-side RTJ, the manager controller propagates
   this to the remote RTJ via MultiKueue's remote object management.

5. **Resume orchestration.** When the user patches `desiredState` to
   `Running`, the manager transitions the RTJ to Queued, which triggers
   Kueue/MultiKueue to re-dispatch the Workload (potentially to a
   different worker).

### Worker Mode Behavior

When the operator starts in worker mode:

1. **Full Phase 5 reconciliation path.** The worker runs the exact same
   code path as the Phase 5 single-cluster operator. No modifications
   to launch gating, topology rendering, priority shaping, graceful
   yield, checkpoint selection, or resume.

2. **Shared checkpoint store.** The `spec.checkpoint.storageURI` on the
   remote RTJ points to the shared S3-compatible store. The worker reads
   and writes checkpoints to this shared store.

3. **No awareness of manager.** The worker operator does not know or care
   that it was dispatched by a manager cluster. It treats the remote RTJ
   as a locally-created RTJ. This is the key isolation property: worker
   mode is identical to Phase 5 single-cluster mode.

### Status Mirroring Model

```
Worker RTJ Status                    Manager RTJ Status
┌─────────────────────┐              ┌─────────────────────────────┐
│ phase: Running      │──mirror──>   │ phase: Running              │
│ conditions:         │              │ conditions: (merged)        │
│  - Admitted: True   │              │  - Admitted: True           │
│  - Running: True    │              │  - Running: True            │
│  - CheckpointReady  │              │  - CheckpointReady          │
│ lastCompleted       │              │ lastCompletedCheckpoint:    │
│  Checkpoint:        │              │  (from worker)              │
│  step: 5000         │              │ remoteStatus:          NEW  │
│  timestamp: T       │              │  workerCluster: worker-1    │
│  storageURI: s3://  │              │  remotePhase: Running       │
│ priorityShaping:    │              │  lastSyncTime: T+1          │
│  (if policy active) │              │                             │
└─────────────────────┘              └─────────────────────────────┘
```

### Shared Checkpoint Store Requirements

The shared checkpoint store MUST satisfy:

1. **S3-compatible API.** Same interface as Phase 0-5 checkpoint storage.
2. **Accessible from all worker clusters.** All workers use the same
   endpoint and bucket/prefix.
3. **Consistent reads.** A checkpoint written by Worker-A must be
   immediately readable by Worker-B after the manifest is published.
   (S3 provides strong read-after-write consistency.)
4. **Same manifest format.** The checkpoint manifest schema from Phase 0
   is unchanged. No multi-cluster extensions to the manifest.
5. **No manager I/O.** The manager does not read or write checkpoint
   artifacts. It only reflects metadata from the worker's status.

### What MultiKueue Provides vs What the RTJ Operator Provides

| Concern | MultiKueue | RTJ Operator (Manager) | RTJ Operator (Worker) |
| --- | --- | --- | --- |
| Worker cluster selection | Authoritative | Not authoritative | Not authoritative |
| Remote RTJ creation | Authoritative | Not authoritative | Not authoritative |
| Remote Workload creation | Authoritative | Not authoritative | Not authoritative |
| Remote status mirroring | Provides raw data | Interprets for RTJ status | Produces status |
| Pause propagation | Transport layer | Initiates pause | Executes yield |
| Resume dispatch | Transport layer | Initiates resume | Executes restore |
| Child JobSet creation | Not authoritative | MUST NOT create | Authoritative |
| Checkpoint I/O | Not authoritative | MUST NOT do | Authoritative |
| Launch gating | Not authoritative | Not authoritative | Authoritative |
| Priority shaping | Not authoritative | Not authoritative | Authoritative (local) |

## Invariants

All Phase 0 through Phase 5 invariants remain in force. Phase 6 adds:

| Invariant | Description |
| --- | --- |
| **RTJ is the only Kueue-managed admission object** | Unchanged. Child JobSets remain plain runtime. |
| **Manager does not create local child JobSets** | For MultiKueue-managed RTJs, the manager is control-plane only. No local runtime. |
| **Worker runs the full Phase 5 path** | The worker does not know it was dispatched by a manager. It runs the existing Phase 5 reconciliation unchanged. |
| **Shared checkpoint store enables cross-worker resume** | The same S3-compatible store is used by all workers. Checkpoints written by one worker are readable by another. |
| **Phase 5 preserved when MultiKueue is not configured** | Worker mode is the default. When no MultiKueue AdmissionCheck exists, behavior is identical to Phase 5. |
| **Manager does not do checkpoint I/O** | The manager reflects checkpoint metadata from the worker status. It does not read or validate checkpoint artifacts. |
| **Mode is determined at startup, not runtime** | The `--mode` flag sets the operator's role for its lifetime. No runtime mode switching. |
| **Status mirroring is eventual-consistency** | The manager status may lag one reconciliation cycle behind the worker. |
| **Kueue and MultiKueue own dispatch and scheduling** | No custom cross-cluster scheduling or preemption in the RTJ operator. |
