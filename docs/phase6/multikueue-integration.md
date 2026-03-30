# MultiKueue Integration for ResumableTrainingJob

This document describes how to configure Kueue MultiKueue external-framework
dispatch for ResumableTrainingJob (RTJ) on a manager cluster, what must exist
on worker clusters, and the execution model.

## Overview

Kueue v0.15.1 ships a generic unstructured adapter for external-framework
CRDs (`externalframeworks.Adapter`). When RTJ is listed in the Kueue
Configuration's `integrations.externalFrameworks`, this adapter handles:

1. Checking eligibility via `spec.managedBy`
2. Creating a remote RTJ copy on the dispatched worker cluster
3. Mirroring the remote RTJ's `.status` back to the manager-side RTJ
4. Cleaning up remote objects on deletion, completion, or spec drift

The RTJ operator does NOT implement any MultiKueue-specific interfaces.
All dispatch logic lives in Kueue's generic adapter.

## What Must Be Enabled on the Manager Cluster

### 1. Kueue Installation with RTJ External Framework

The Kueue Configuration must include RTJ in `integrations.externalFrameworks`:

```yaml
integrations:
  externalFrameworks:
    - ResumableTrainingJob.v1alpha1.training.checkpoint.example.io
```

See: `deploy/multikueue/manager-config/kueue-controller-manager-config.yaml`

### 2. Feature Gates

Two Kueue feature gates must be enabled (both are Beta and default-on in
v0.15.1, so no explicit action is needed unless they were previously disabled):

| Feature Gate | Status in v0.15.1 | Default | Purpose |
|---|---|---|---|
| `MultiKueue` | Beta (since v0.9) | **true** | Core MultiKueue dispatch |
| `MultiKueueAdaptersForCustomJobs` | Beta (since v0.15) | **true** | Generic adapter for external CRDs |

### 3. MultiKueue Admission Check

An `AdmissionCheck` resource with `controllerName: kueue.x-k8s.io/multikueue`
that references a `MultiKueueConfig`:

```yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: AdmissionCheck
metadata:
  name: multikueue
spec:
  controllerName: kueue.x-k8s.io/multikueue
  parameters:
    apiGroup: kueue.x-k8s.io
    kind: MultiKueueConfig
    name: multikueue-config
```

See: `deploy/multikueue/manager-config/admissioncheck.yaml`

### 4. MultiKueueConfig and MultiKueueCluster

A `MultiKueueConfig` listing worker clusters, and a `MultiKueueCluster` per
worker with a kubeconfig Secret:

```yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: MultiKueueConfig
metadata:
  name: multikueue-config
spec:
  clusters:
    - worker-1
    - worker-2
---
apiVersion: kueue.x-k8s.io/v1beta2
kind: MultiKueueCluster
metadata:
  name: worker-1
spec:
  kubeConfig:
    locationType: Secret
    location: worker-1-kubeconfig
```

See: `deploy/multikueue/manager-config/multikueueconfig.yaml` and
`deploy/multikueue/manager-config/multikueuecluster-template.yaml`

### 5. ClusterQueue with MultiKueue Admission Check

A `ClusterQueue` referencing the `multikueue` AdmissionCheck:

```yaml
apiVersion: kueue.x-k8s.io/v1beta2
kind: ClusterQueue
metadata:
  name: multikueue-training-cq
spec:
  admissionChecks:
    - multikueue
```

See: `deploy/multikueue/manager-config/clusterqueue-multikueue.yaml`

### 6. RBAC for Kueue Controller Manager

The Kueue controller manager ServiceAccount needs RBAC to manage RTJ objects:

```yaml
rules:
  - apiGroups: ["training.checkpoint.example.io"]
    resources: ["resumabletrainingjobs"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["training.checkpoint.example.io"]
    resources: ["resumabletrainingjobs/status"]
    verbs: ["get", "update", "patch"]
```

See: `deploy/multikueue/manager-rbac/clusterrole-kueue-rtj.yaml`

### 7. RTJ Operator in Manager Mode

The RTJ operator must run with `--mode=manager`:

```bash
./operator --mode=manager
```

This ensures the operator suppresses local child JobSet creation for
MultiKueue-managed RTJs (Session 3 implementation).

### 8. RTJ CRD Installed

The RTJ CRD must be installed on the manager cluster so that Kueue's
external-framework adapter can work with RTJ objects.

## What Must Exist on Worker Clusters

### 1. Kueue Installation with RTJ External Framework

Same as manager: Kueue with RTJ in `externalFrameworks`. The worker Kueue
manages local Workload admission for the remote RTJ copy.

### 2. RTJ Operator in Worker Mode

The RTJ operator runs with `--mode=worker` (default):

```bash
./operator --mode=worker  # or just ./operator
```

The worker operator runs the full Phase 5 path: launch gating, topology,
priority shaping, graceful yield, checkpoint, resume.

### 3. RTJ CRD Installed

The RTJ CRD must be installed on worker clusters.

### 4. ClusterQueue with Local Resources

Worker clusters need ClusterQueues with actual resource quotas (GPU, CPU,
memory) to admit Workloads locally.

### 5. RBAC for MultiKueue Remote Client

The identity used in the MultiKueueCluster kubeconfig needs permissions
to manage Workload and RTJ resources on the worker:

```yaml
rules:
  - apiGroups: ["kueue.x-k8s.io"]
    resources: ["workloads"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["training.checkpoint.example.io"]
    resources: ["resumabletrainingjobs"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: ["training.checkpoint.example.io"]
    resources: ["resumabletrainingjobs/status"]
    verbs: ["get"]
```

See: `deploy/multikueue/manager-rbac/worker-kubeconfig-rbac.yaml`

### 6. Shared Checkpoint Store Access

Worker pods must have access to the shared S3-compatible checkpoint store
referenced in `spec.checkpoint.storageURI`. All workers must use the same
endpoint and credentials.

## Why `spec.managedBy` Matters

The `spec.managedBy` field is the core signal for MultiKueue dispatch:

1. **On the manager cluster:** When `spec.managedBy = "kueue.x-k8s.io/multikueue"`,
   Kueue's generic adapter recognizes the RTJ as eligible for MultiKueue
   dispatch. The `IsJobManagedByKueue` method checks this field.

2. **On the manager cluster (RTJ operator):** The operator's
   `ShouldSuppressRuntime` predicate checks both `--mode=manager` and
   `IsManagedByMultiKueue()` to suppress local child JobSet creation.

3. **On the worker cluster:** The generic adapter **strips `spec.managedBy`**
   from the remote copy before creating it. This means the worker-side RTJ
   has no `managedBy` field, and both the worker Kueue and the worker RTJ
   operator treat it as a normal local job.

4. **Immutability:** `spec.managedBy` is immutable once set (enforced by
   the RTJ webhook). This prevents runtime ambiguity about which controller
   owns the RTJ.

5. **Backward compatibility:** When `spec.managedBy` is empty (the default),
   the RTJ follows the single-cluster Phase 5 path. No MultiKueue dispatch
   occurs.

## Mirror-Copy Execution Model

MultiKueue uses a "mirror-copy" execution model:

### Dispatch Flow

```
Manager                          Worker
  RTJ (managedBy=multikueue)       (no RTJ yet)
  Workload created by Kueue
       |
       v
  MultiKueue creates remote      Remote RTJ created
  Workload on worker              (managedBy stripped)
                                  Remote Workload created
                                  (prebuilt-workload label)
                                       |
                                       v
                                  Worker Kueue admits locally
                                  Worker RTJ operator launches
                                  Child JobSet + training pods
```

### Status Mirror Flow

```
Manager                          Worker
  RTJ .status patched <--------  RTJ .status produced
  (full .status copy)            (phase, conditions, checkpoint)

  MultiCluster status populated
  by manager RTJ controller
  (dispatchPhase, executionCluster,
   remotePhase, remoteCheckpoint)
```

### Key Properties

1. **One authoritative copy at a time.** The remote RTJ on the worker is the
   authoritative runtime copy. The manager-side RTJ is a control-plane mirror.

2. **Full status flow.** The entire `.status` object is copied from remote to
   local. This includes all Phase 1-5 status fields (phase, conditions,
   checkpoint references, admission status, topology, priority shaping).

3. **No spec propagation.** Changes to the manager-side RTJ spec (other than
   `suspend`) do NOT automatically flow to the remote copy. If the spec
   drifts, the remote Workload is deleted and recreated.

4. **Pause via Kueue suspension.** To pause a remote RTJ, the manager sets
   `spec.suspend = true` on the manager-side RTJ. Kueue propagates this
   as a Workload suspension to the worker, which triggers the existing
   graceful yield path.

5. **Resume via re-dispatch.** After pause, setting `spec.suspend = false`
   on the manager-side RTJ causes Kueue to re-dispatch the Workload. The
   worker (same or different) selects the latest compatible checkpoint from
   the shared store and resumes training.

6. **Automatic cleanup.** When the manager-side RTJ or Workload is deleted,
   the remote copies are automatically cleaned up by the generic adapter.
   Periodic GC also removes orphaned remote objects.

## Deploy Artifact Reference

| File | Purpose |
|---|---|
| `deploy/multikueue/manager-config/kueue-controller-manager-config.yaml` | Kueue Configuration with RTJ external framework |
| `deploy/multikueue/manager-config/admissioncheck.yaml` | MultiKueue AdmissionCheck |
| `deploy/multikueue/manager-config/multikueueconfig.yaml` | MultiKueueConfig listing worker clusters |
| `deploy/multikueue/manager-config/multikueuecluster-template.yaml` | MultiKueueCluster templates (one per worker) |
| `deploy/multikueue/manager-config/clusterqueue-multikueue.yaml` | ClusterQueue with MultiKueue admission check |
| `deploy/multikueue/manager-rbac/clusterrole-kueue-rtj.yaml` | ClusterRole for Kueue to manage RTJ |
| `deploy/multikueue/manager-rbac/clusterrolebinding-kueue-rtj.yaml` | ClusterRoleBinding for Kueue ServiceAccount |
| `deploy/multikueue/manager-rbac/worker-kubeconfig-rbac.yaml` | RBAC for remote client on worker clusters |

## Kueue v0.15.1 Version Accuracy

This integration is verified against the pinned Kueue v0.15.1. Key
implementation details confirmed from the Kueue source:

| Aspect | Kueue v0.15.1 Behavior |
|---|---|
| Generic adapter location | `pkg/controller/admissionchecks/multikueue/externalframeworks/adapter.go` |
| `managedBy` check | `adapter.IsJobManagedByKueue()` checks `spec.managedBy == "kueue.x-k8s.io/multikueue"` |
| Remote creation | `adapter.createRemoteObject()` deep-copies, strips `managedBy`, adds labels |
| Status mirroring | `adapter.syncStatus()` copies entire `.status` from remote to local |
| Remote deletion | `adapter.DeleteRemoteObject()` uses `DeletePropagationBackground` |
| `KeepAdmissionCheckPending` | Returns `true` for external frameworks |
| Spec mutation handling | Deletes out-of-sync remote Workloads (no in-place update for non-elastic) |
| GC interval | Default 1 minute |

### Divergence Notes

No divergence from Kueue v0.15.1. All configuration and RBAC follow the
patterns established by the generic external-framework adapter. If future
Kueue versions change the adapter interface or behavior, the deploy artifacts
and documentation should be updated accordingly.
