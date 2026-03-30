# Phase 6 E2E Test Coverage

## Overview

Phase 6 e2e tests validate the MultiKueue remote dispatch and execution
path across a three-cluster environment (manager + 2 workers). They
exercise the full dispatch flow: RTJ submission on the manager, MultiKueue
dispatch to a worker, child JobSet creation on the worker, and
manager-side status reflection.

The tests are designed as a few strong deterministic tests that prove the
core hard boundaries rather than many shallow checks.

## Prerequisites

```bash
# 1. Set up the three-cluster environment
make phase6-up

# 2. Verify infrastructure
make phase6-smoke

# 3. Load trainer image into worker clusters
make phase6-load-images

# 4. Run Phase 6 e2e tests
RUN_KIND_E2E=1 PHASE6_TRAINER_IMAGE=<image> \
  go test ./test/e2e/ -run TestMultiCluster -v -timeout 20m
```

## Deterministic Cluster Selection

Both worker clusters have identical ClusterQueue quotas (500m CPU /
512Mi). Without intervention, MultiKueue dispatches to whichever worker
admits first, which is non-deterministic.

**Mechanism:** Before each test that requires deterministic selection,
the test patches worker-2's ClusterQueue with `spec.stopPolicy: Hold`.
This prevents worker-2's Kueue from admitting new Workloads. MultiKueue
creates remote Workloads on both workers, but only worker-1 can admit.
After the test, worker-2's CQ is restored to `spec.stopPolicy: None`.

Why this is the smallest practical mechanism:

| Alternative | Rejected because |
|---|---|
| Different quotas on workers | Requires changing persistent deploy manifests |
| Separate queue per worker | More resources, more complexity |
| Worker-2 offline | Too coarse, affects other tests |
| **CQ stopPolicy (chosen)** | **One field, one cluster, one API call** |

## Tests

### TestMultiClusterRemoteExecution

**File:** `test/e2e/multicluster_remote_execution_test.go`

**What it proves:**

| Step | Assertion | Hard boundary |
|---|---|---|
| 1 | RTJ with `spec.managedBy` is accepted on manager | API contract |
| 2 | Manager reports `multiCluster.dispatchPhase` | Manager status plumbing |
| 3 | Mirror RTJ exists on worker-1 with `spec.managedBy` stripped | Adapter strips field |
| 4 | Child JobSet `<name>-run-1` exists on worker-1 | Worker runs full Phase 5 path |
| 5 | No child JobSet on manager | **Manager must not create local runtime** |
| 6 | No child JobSet on worker-2 | **Selected worker is the only place** |
| 7 | Manager shows `executionCluster` and `localExecutionSuppressed=true` | Remote status reflection |

**Cluster selection:** Deterministic (worker-2 CQ paused).

### TestMultiClusterManagerSuppression

**File:** `test/e2e/multicluster_manager_suppression_test.go`

**What it proves:**

| Step | Assertion | Hard boundary |
|---|---|---|
| 1 | RTJ submitted to manager | API contract |
| 2 | `localExecutionSuppressed=true` immediately | Manager mode active |
| 3 | No child JobSets on manager (initial) | **No local runtime** |
| 4 | `dispatchPhase` progresses beyond Pending | Dispatch lifecycle works |
| 5 | No child JobSets on manager (repeated over 30s) | **Invariant, not snapshot** |
| 6 | Final state: suppressed=true, dispatchPhase set | Status correctness |

**Cluster selection:** Not biased (both workers available). The test
only verifies the manager side and does not depend on which worker is
selected.

**Design note:** Step 5 polls repeatedly over a 30-second window to
prove the no-local-runtime invariant holds continuously, not just at a
single point in time. This catches race conditions where the manager
might accidentally create a JobSet and then delete it.

## Test Infrastructure

### Three-Operator Architecture

Each e2e test starts three operator instances on the host:

| Operator | Cluster | Mode | Metrics | Health | S3 |
|---|---|---|---|---|---|
| Manager | `kind-phase6-manager` | `--mode=manager` | `:8090` | `:8091` | No |
| Worker-1 | `kind-phase6-worker-1` | `--mode=worker` | `:8092` | `:8093` | Yes |
| Worker-2 | `kind-phase6-worker-2` | `--mode=worker` | `:8094` | `:8095` | Yes |

Each operator gets a standalone kubeconfig extracted via
`kubectl config view --minify --context=<ctx> --flatten` and passed via
the `KUBECONFIG` environment variable.

### MinIO Access

MinIO runs on worker-1 (NodePort 30900). The host-side operator
processes reach it via `kubectl port-forward` from worker-1 to
`localhost:9002`. Trainer pods inside worker clusters reach it via the
Docker-internal IP stored in the `checkpoint-storage-credentials` Secret.

### Cleanup

Each test deletes the RTJ from all three clusters at test end. Child
JobSets are cleaned up via owner-reference garbage collection and
best-effort explicit deletion.

## What Remains Deferred

| Topic | Reason |
|---|---|
| Pause/resume e2e across workers | Explicitly excluded per prompt scope. Covered in Session 7 (cross-worker resume). |
| Cross-worker resume (checkpoint on worker-A, resume on worker-B) | Requires pause/resume flow. |
| Manager-visible remote checkpoint mirroring e2e | Requires the worker to complete a checkpoint cycle. Deferred to avoid time-dependent flakiness. |
| MultiKueue preemption-triggered re-dispatch e2e | Complex scenario (manager preempts → remote cleanup → re-dispatch). |
| Phase 5 backward compatibility e2e | Worker-mode without MultiKueue is Phase 5. Covered by existing Phase 5 e2e tests. |
| Concurrent multi-RTJ scheduling | Interaction between multiple MultiKueue RTJs. |

## File Index

| File | Purpose |
|---|---|
| `test/e2e/phase6_helpers_test.go` | Phase 6 view types, environment setup, kubectl/RTJ/JobSet helpers, cluster selection bias, cleanup |
| `test/e2e/multicluster_remote_execution_test.go` | Remote dispatch/execution end-to-end test |
| `test/e2e/multicluster_manager_suppression_test.go` | Manager-side local runtime suppression test |
| `test/e2e/testdata/phase6/rtj-remote-dispatch.yaml` | RTJ template with `spec.managedBy` and shared credentials |
| `docs/phase6/e2e.md` | This file |
