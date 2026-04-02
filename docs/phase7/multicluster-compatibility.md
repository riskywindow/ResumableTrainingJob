# Phase 7 -- Multi-Cluster Compatibility

> Session 8: integrating Phase 7 capacity guarantees into the existing
> Phase 6 manager/worker MultiKueue path.

## Overview

Phase 7 (capacity-guaranteed launch) is fully compatible with the Phase 6
multi-cluster architecture. The core principle is:

- **Worker clusters**: run the complete Phase 7 path (launch gating,
  provisioning-aware gates, topology second-pass, waitForPodsReady,
  podSetUpdates). This is identical to single-cluster mode.
- **Manager clusters**: continue to suppress local runtime. Phase 7
  worker status is surfaced transparently via the existing adapter
  status mirror.

No new multi-cluster dispatch policy is introduced. The existing
MultiKueue dispatch + adapter mirror path is unchanged.

## What Phase 7 changes on worker clusters

When a worker cluster has Phase 7 provisioning configured (i.e., the
worker's ClusterQueue has a ProvisioningRequest AdmissionCheck), the
following Phase 7 gates apply to RTJs dispatched via MultiKueue:

| Gate | Behavior |
|---|---|
| **Provisioning AC** | Worker does not create child JobSet until the ProvisioningRequest AC is Ready. |
| **All ACs Ready** | Worker waits for ALL AdmissionChecks (provisioning, resume readiness, etc.) to be Ready. |
| **Topology second-pass** | If topology is enabled, worker waits for the topology assignment even after ACs are Ready. |
| **podSetUpdates** | Worker applies AC-suggested pod mutations (labels, annotations, nodeSelector, tolerations) to the rendered JobSet. Conflicts are fail-fast. |
| **waitForPodsReady** | If Kueue's waitForPodsReady is enabled on the worker, startup/recovery timeouts trigger the existing graceful yield path. |

These gates run in the normal `Reconcile()` path, which executes on the
worker when `ShouldSuppressRuntime()` returns `false`. Since the worker
operator runs in `ModeWorker`, `ShouldSuppressRuntime` always returns
`false`, and the full Phase 7 path executes regardless of whether the
RTJ was dispatched via MultiKueue or submitted directly.

### Worker status fields populated

When Phase 7 features are active on the worker, the RTJ status includes:

- `status.launchGate` — aggregate gate state with per-AC summary
- `status.provisioning` — provisioning-specific state with PR reference
- `status.startupRecovery` — startup/recovery lifecycle with eviction reasons
- `status.capacity` — derived capacity guarantee indicator

These fields are part of the worker's RTJ status and are mirrored to the
manager via the Kueue adapter's full-status mirror.

## What remains the same on manager clusters

The manager cluster behavior is **unchanged** from Phase 6:

| Invariant | Preserved |
|---|---|
| Local child JobSet suppression | Yes — `ShouldSuppressRuntime(ModeManager, job)` returns true |
| No local ProvisioningRequest creation | Yes — manager does not evaluate launch gates |
| No local checkpoint I/O | Yes — manager never does checkpoint operations |
| Remote status mirroring | Yes — adapter copies full `.status` from worker |
| Remote pause/resume propagation | Yes — adapter handles spec drift → teardown + recreate |
| Execution cluster resolution | Yes — `ClusterResolver` resolves from Workload admission |

### Phase 7 status surfacing on manager

The Kueue adapter performs an unstructured status patch that copies the
**entire** `.status` from the remote worker RTJ to the manager-side RTJ.
This means Phase 7 status fields are automatically available on the
manager-side RTJ:

```
# On the manager cluster:
kubectl get rtj <name> -o jsonpath='{.status.launchGate}'
kubectl get rtj <name> -o jsonpath='{.status.provisioning}'
kubectl get rtj <name> -o jsonpath='{.status.capacity}'
kubectl get rtj <name> -o jsonpath='{.status.startupRecovery}'
```

The `reconcileManagerIntent` function logs Phase 7 remote state when
the adapter has mirrored Phase 7 fields:

```
manager mode: remote Phase 7 launch status (from worker)
  remoteLaunchGateState=Blocked
  remoteProvisioningState=Pending
  remoteCapacityGuarantee=false
  remoteStartupState=Starting
```

The `status.multiCluster` section continues to carry the dispatch phase,
execution cluster, remote phase, and remote checkpoint summary. It does
**not** duplicate Phase 7 fields — those are read directly from the
mirrored status sections.

### Phase 6 backward compatibility

When a worker cluster does **not** have Phase 7 provisioning configured:

- `ProvisioningACNames` is empty on the worker reconciler
- `provisioning.BuildView()` returns `AllChecksReady: true`
- Launch gates pass through (fail-open)
- Phase 6 behavior is preserved exactly
- Phase 7 status fields are nil on the worker → nil on the manager

The `hasPhase7RemoteStatus()` function returns false, and the manager
skips Phase 7 logging. No change in manager behavior.

## Worker mode: Phase 7 gate flow for dispatched RTJs

The Phase 7 gate evaluation order on the worker is:

```
1. RTJ admitted by Kueue (not suspended)
2. Workload found with quota reserved
3. All AdmissionChecks Ready
   - ProvisioningRequest AC: waits for backend to confirm capacity
   - ResumeReadiness AC: waits for checkpoint validation (Phase 4)
   - Any future ACs: waits for Ready state
4. Topology second-pass (if topology enabled)
   - Waits for topology assignment even after ACs Ready
5. Topology assignment parsed (if topology enabled)
6. podSetUpdate conflict check (dry-run before launch)
7. Launch: create control ConfigMap + child JobSet
```

This is the same flow as single-cluster mode. The dispatched RTJ copy
on the worker goes through the same `Reconcile()` path.

## Test coverage

### Covered by unit/integration tests

| Test | What it proves |
|---|---|
| `TestManagerModeReflectsPhase7WorkerLaunchGateStatus` | Manager preserves Phase 7 launch gate state mirrored from worker |
| `TestManagerModeReflectsPhase7WorkerProvisionedAndRunning` | Manager reflects Phase 7 capacity guarantee from provisioned worker |
| `TestManagerModePhase6WorkerHasNoPhase7Fields` | Manager works correctly with Phase 6 workers (no Phase 7 status) |
| `TestBuildRemoteLaunchSummaryFullState` | Phase 7 summary extraction from mirrored status |
| `TestBuildRemoteLaunchSummaryEmptyStatus` | Nil-safe summary for Phase 6 workers |
| `TestBuildRemoteLaunchSummaryProvisionedAndRunning` | Summary with active capacity guarantee |
| `TestHasPhase7RemoteStatus` | Detection of Phase 7 fields in mirrored status |

### Covered by e2e smoke test

| Test | What it proves |
|---|---|
| `TestMultiClusterCapacityGateSmoke` | Manager suppression preserved with Phase 7 codebase; worker launches correctly in Phase 6 environment |

### Not covered (deferred)

| Item | Reason | Prerequisite |
|---|---|---|
| Worker-side Phase 7 provisioning e2e in multi-cluster | Requires provisioning infrastructure on worker clusters | Phase 7 dev profile on worker kind clusters |
| Manager observing worker provisioning pending → provisioned transition | Requires live adapter + provisioning backend on worker | Three-cluster setup with Phase 7 profile on workers |
| Cross-worker resume with Phase 7 provisioning | Requires provisioning + shared checkpoint store + two workers | Full Phase 6+7 combined environment |
| Topology + provisioning in multi-cluster | TAS + ProvisioningRequest on worker clusters | Environment-dependent |

To achieve full multi-cluster Phase 7 e2e coverage, the Phase 6
three-cluster environment would need:
1. ProvisioningRequest CRD installed on worker clusters
2. Fake provisioner deployed on worker clusters
3. Kueue configured with ProvisioningACC feature gate on workers
4. Worker operator started with `--provisioning-ac-names` flag

This is documented for a future session.

## Architecture diagram

```
Manager Cluster                    Worker Cluster
┌────────────────────┐             ┌────────────────────────────────────┐
│ RTJ (spec)         │             │ RTJ (copy from adapter)            │
│   managedBy: MK    │  adapter    │   managedBy: MK (or stripped)      │
│   desiredState: R  │────────────>│   desiredState: R                  │
│                    │             │                                    │
│ RTJ (status)       │  adapter    │ RTJ Controller (ModeWorker)        │
│   multiCluster:    │<────────────│   ├── evaluateLaunchGates()        │
│     dispatch: Act  │  full       │   │   ├── quota reserved?          │
│     remote: Run    │  status     │   │   ├── all ACs Ready?           │
│     exec: worker-1 │  mirror     │   │   ├── provisioning AC Ready?   │
│   launchGate: Open │             │   │   ├── topology assigned?       │
│   provisioning: OK │             │   │   └── podSetUpdate conflicts?  │
│   capacity:        │             │   ├── reconcileLaunch/Resume()     │
│     guarantee: T   │             │   │   └── create child JobSet      │
│   startupRecovery: │             │   └── detectAndRecordEviction()    │
│     state: Running │             │       └── waitForPodsReady timeout │
└────────────────────┘             └────────────────────────────────────┘
```

## Key design decisions

1. **No API changes for multi-cluster Phase 7.** Phase 7 status fields
   pass through the adapter mirror transparently. No new MultiClusterStatus
   fields are needed.

2. **Manager does not evaluate Phase 7 launch gates.** Launch gating is
   worker-local. The manager only observes the outcome via status mirroring.

3. **Phase 7 logging on manager is conditional.** The manager only logs
   Phase 7 remote state when `hasPhase7RemoteStatus()` returns true,
   avoiding noise when connected to Phase 6 workers.

4. **Worker Phase 7 path is identical to single-cluster.** No multi-cluster
   special cases in the launch gate, provisioning observation layer, or
   startup recovery code.
