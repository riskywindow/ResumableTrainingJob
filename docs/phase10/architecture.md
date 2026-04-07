# Phase 10 Architecture

This document provides production architecture diagrams for the checkpoint-native
preemption controller. Phase 10 does not alter the runtime or scheduling
architecture established in Phases 0-9; it adds production infrastructure around
the existing system.

---

## 1. Production Single-Cluster Component Diagram

```
+-----------------------------------------------------------------------+
|  Kubernetes Cluster                                                    |
|                                                                        |
|  +---------------------------+    +-------------------------------+    |
|  | cert-manager              |    | Prometheus + Grafana          |    |
|  | - Issuer                  |    | - ServiceMonitor              |    |
|  | - Certificate (webhook)   |    | - PrometheusRule (alerts)     |    |
|  | - Certificate (metrics)   |    | - Dashboard JSON              |    |
|  +------------+--------------+    +------+------------------------+    |
|               |                          |                             |
|               | TLS certs                | scrape :8080/metrics        |
|               v                          v                             |
|  +---------------------------------------------------------------+    |
|  | checkpoint-native-operator Deployment (2 replicas, HA)        |    |
|  |                                                                |    |
|  |  +-------------------------+  +----------------------------+  |    |
|  |  | Replica A (leader)      |  | Replica B (standby)        |  |    |
|  |  | - RTJ Reconciler        |  | - healthz: ok              |  |    |
|  |  | - WorkloadObserver      |  | - readyz: not-ready        |  |    |
|  |  | - ResumeReadiness AC    |  | - waiting for lease        |  |    |
|  |  | - Webhook Server (:9443)|  |                            |  |    |
|  |  | - Metrics Server (:8080)|  +----------------------------+  |    |
|  |  | - Leader Lease holder   |                                  |    |
|  |  +-------------------------+                                  |    |
|  |                                                                |    |
|  |  Shared Config:                                                |    |
|  |  - LeaderElectionID: 203ef34d.training.checkpoint.example.io  |    |
|  |  - PodDisruptionBudget: minAvailable=1                        |    |
|  |  - Pod anti-affinity: spread across nodes                     |    |
|  |  - PSS: restricted profile                                    |    |
|  +---------------------------------------------------------------+    |
|         |            |            |              |                      |
|   create/watch   create/watch   watch       synthesize/watch           |
|   RTJ (v1beta1)  child JobSet   Checkpoints  Kueue Workload           |
|         |            |            |              |                      |
|         v            v            v              v                      |
|  +----------+  +----------+  +----------+  +------------------+       |
|  | RTJ CRDs |  | JobSet   |  | S3/MinIO |  | Kueue            |       |
|  | v1beta1  |  | (runtime)|  | Ckpt     |  | - ClusterQueue   |       |
|  | v1alpha1 |  |          |  | Store    |  | - Workload       |       |
|  | (served) |  |          |  |          |  | - AdmissionCheck |       |
|  +----------+  +----------+  +----------+  +------------------+       |
|       |                                                                |
|       | conversion webhook (v1alpha1 <-> v1beta1)                      |
|       v                                                                |
|  +---------------------------+                                         |
|  | CRD Conversion Webhook    |                                         |
|  | (served by operator pod)  |                                         |
|  +---------------------------+                                         |
+-----------------------------------------------------------------------+
```

### Component Responsibilities

| Component | Responsibility |
|-----------|---------------|
| cert-manager | Manage TLS certificates for webhook and metrics endpoints |
| Operator Deployment | Run RTJ reconciler, workload observer, webhook server, metrics server |
| Leader Election | Ensure single active reconciler across replicas |
| PodDisruptionBudget | Prevent simultaneous eviction of both replicas |
| CRD Conversion Webhook | Translate between v1alpha1 and v1beta1 on API server reads/writes |
| Kueue | Admission control, quota, preemption decisions |
| JobSet | Runtime execution primitive (plain resource, not Kueue-managed) |
| Checkpoint Store | S3-compatible persistent checkpoint storage |
| Prometheus/Grafana | Metrics collection, alerting, dashboards |

---

## 2. Production MultiKueue Manager/Worker Component Diagram

```
+------------------------------------------+     +-------------------------------------------+
| Manager Cluster                          |     | Worker Cluster(s)                         |
|                                          |     |                                           |
| +--------------------------------------+ |     | +---------------------------------------+ |
| | checkpoint-native-operator           | |     | | checkpoint-native-operator            | |
| | --mode=manager                       | |     | | --mode=worker                         | |
| | --leader-elect=true                  | |     | | --leader-elect=true                   | |
| |                                      | |     | |                                       | |
| | Reconciler:                          | |     | | Reconciler:                           | |
| | - Suppress local child JobSet (I-6)  | |     | | - Full runtime path (Phase 0-9)      | |
| | - Synthesize Workload for dispatch   | |     | | - Launch child JobSet                 | |
| | - Mirror remote RTJ status           | |     | | - Manage checkpoints                  | |
| | - No elastic plan eval (I-12)        | |     | | - Elastic resize (Phase 9)            | |
| | - No reclaim state (I-14)            | |     | | - Publish reclaimablePods (I-13)      | |
| +--------------------------------------+ |     | +---------------------------------------+ |
|       |              |                   |     |       |              |                    |
|       v              v                   |     |       v              v                   |
| +----------+  +-----------------+        |     | +----------+  +-----------------+       |
| | RTJ CRDs |  | Kueue           |        |     | | RTJ CRDs |  | Kueue           |      |
| | v1beta1  |  | - MultiKueue    |---dispatch-->| | v1beta1  |  | - ClusterQueue  |      |
| |          |  | - AdmissionCheck|        |     | |          |  | - Workload      |      |
| |          |  | - ClusterQueue  |        |     | |          |  |                 |      |
| +----------+  +-----------------+        |     | +----------+  +-----------------+       |
|                      |                   |     |                      |                   |
|                      | remote status     |     |                      | local Workload    |
|                      | mirroring         |     |                      |                   |
|                      +<---------------------------------------------- +                   |
|                                          |     |       |                                  |
|                                          |     |       v                                  |
|                                          |     | +----------+  +----------+               |
|                                          |     | | JobSet   |  | S3/MinIO |               |
|                                          |     | | (runtime)|  | Ckpt     |               |
|                                          |     | |          |  | Store    |               |
|                                          |     | +----------+  +----+-----+               |
+------------------------------------------+     +-------------------|------------------------+
                                                                     |
                                                          Shared checkpoint store
                                                          (required for correctness)
```

### Manager vs Worker Responsibilities

| Aspect | Manager Cluster | Worker Cluster |
|--------|----------------|----------------|
| RTJ CRD | Installed (v1beta1) | Installed (v1beta1) |
| Operator mode | `--mode=manager` | `--mode=worker` |
| Child JobSet | Never created (suppressed) | Created and managed |
| Workload | Synthesized for MultiKueue dispatch | Local, owns admission state |
| Elastic resize | Not evaluated (I-12) | Full path (in-place shrink + C&R) |
| reclaimablePods | Not published (I-14) | Published on local Workload (I-13) |
| Checkpoints | Not directly accessed | Managed via checkpoint store |
| Remote status | Mirrors from worker | Source of truth |

---

## 3. API Conversion During Upgrade (Sequence Diagram)

```
  Operator                API Server            etcd             Client
  (new version)                                 (stored v1beta1)
     |                       |                    |                 |
     |  1. Deploy new CRD    |                    |                 |
     |  (served: v1beta1,    |                    |                 |
     |   v1alpha1;           |                    |                 |
     |   stored: v1beta1;    |                    |                 |
     |   conversion: Webhook)|                    |                 |
     |---------------------->|                    |                 |
     |                       |                    |                 |
     |  2. Register          |                    |                 |
     |  conversion webhook   |                    |                 |
     |---------------------->|                    |                 |
     |                       |                    |                 |
     |                       |  3. Client GETs    |                 |
     |                       |  v1alpha1 RTJ      |                 |
     |                       |<------------------------------------|
     |                       |                    |                 |
     |                       |  4. Read from etcd |                 |
     |                       |  (stored as        |                 |
     |                       |   v1beta1)         |                 |
     |                       |------------------->|                 |
     |                       |<-------------------|                 |
     |                       |                    |                 |
     |  5. Conversion webhook|                    |                 |
     |  v1beta1 -> v1alpha1  |                    |                 |
     |<----------------------|                    |                 |
     |  (lossless downgrade) |                    |                 |
     |---------------------->|                    |                 |
     |                       |                    |                 |
     |                       |  6. Return         |                 |
     |                       |  v1alpha1 to client|                 |
     |                       |------------------------------------>|
     |                       |                    |                 |
     |                       |  7. Client CREATEs |                 |
     |                       |  v1alpha1 RTJ      |                 |
     |                       |<------------------------------------|
     |                       |                    |                 |
     |  8. Conversion webhook|                    |                 |
     |  v1alpha1 -> v1beta1  |                    |                 |
     |<----------------------|                    |                 |
     |  (lossless upgrade)   |                    |                 |
     |---------------------->|                    |                 |
     |                       |                    |                 |
     |                       |  9. Store as       |                 |
     |                       |  v1beta1 in etcd   |                 |
     |                       |------------------->|                 |
     |                       |                    |                 |
     |                       |  10. Client GETs   |                 |
     |                       |  v1beta1 RTJ       |                 |
     |                       |<------------------------------------|
     |                       |  (no conversion    |                 |
     |                       |   needed, direct   |                 |
     |                       |   read from etcd)  |                 |
     |                       |------------------->|                 |
     |                       |<-------------------|                 |
     |                       |------------------------------------>|
     |                       |                    |                 |
```

### Conversion Rules

| Direction | Action | Data Loss |
|-----------|--------|-----------|
| v1alpha1 -> v1beta1 | Copy all fields; set `apiVersion` | None |
| v1beta1 -> v1alpha1 | Copy all fields; set `apiVersion` | None |
| v1beta1 -> v1beta1 | Identity (no-op) | None |

The schema is identical between versions. The conversion webhook only changes
the `apiVersion` field. This ensures lossless round-tripping.

### StorageVersionMigration

After the conversion webhook is deployed and stable:

1. Run `kubectl storagemigration` (or equivalent) to migrate all stored
   v1alpha1 objects to v1beta1 in etcd
2. Once all objects are migrated, v1alpha1 can optionally be removed from
   the served versions list (Phase 11+ consideration)

---

## 4. Disaster Recovery / State Reconstruction (Sequence Diagram)

```
  Operator                API Server     Checkpoint Store     Kueue
  (after recovery)                       (S3/MinIO)
     |                       |                |                 |
     |  === DISASTER: etcd data loss (RTJ objects deleted) ===  |
     |                       |                |                 |
     |  1. List checkpoint   |                |                 |
     |  manifests in store   |                |                 |
     |-------------------------------------->|                  |
     |<--------------------------------------|                  |
     |  (manifests with RTJ  |                |                 |
     |   name, namespace,    |                |                 |
     |   step, world-size)   |                |                 |
     |                       |                |                 |
     |  2. Query Kueue       |                |                 |
     |  Workloads for RTJ    |                |                 |
     |  owner references     |                |                 |
     |------------------------------------------------>|        |
     |<------------------------------------------------|        |
     |  (admission state,    |                |                 |
     |   quota assignment,   |                |                 |
     |   priority class)     |                |                 |
     |                       |                |                 |
     |  3. Reconstruct RTJ   |                |                 |
     |  spec from:           |                |                 |
     |  - checkpoint manifest|                |                 |
     |    (storage URI,      |                |                 |
     |     world size,       |                |                 |
     |     device profile)   |                |                 |
     |  - Workload state     |                |                 |
     |    (queue, priority,  |                |                 |
     |     admission)        |                |                 |
     |  - Operator defaults  |                |                 |
     |                       |                |                 |
     |  4. Create RTJ in     |                |                 |
     |  Paused state         |                |                 |
     |  (safe starting point)|                |                 |
     |---------------------->|                |                 |
     |                       |                |                 |
     |  5. Validate          |                |                 |
     |  reconstructed state  |                |                 |
     |  (checkpoint compat,  |                |                 |
     |   queue exists,       |                |                 |
     |   priority class OK)  |                |                 |
     |---------------------->|                |                 |
     |                       |                |                 |
     |  6. Admin reviews     |                |                 |
     |  reconstructed RTJs   |                |                 |
     |  and sets             |                |                 |
     |  desiredState=Running |                |                 |
     |  (manual gate)        |                |                 |
     |                       |                |                 |
     |  7. Normal resume     |                |                 |
     |  flow begins          |                |                 |
     |  (Phase 1 path)       |                |                 |
     |---------------------->|                |                 |
     |                       |                |                 |
```

### State Reconstruction Sources

| State | Primary Source | Fallback |
|-------|--------------|----------|
| Checkpoint data | Checkpoint store (S3 manifests) | None (data loss = training restart) |
| Queue assignment | Kueue Workload (if surviving) | Operator default / admin input |
| Priority class | Kueue Workload / RTJ annotation | Admin input |
| Parallelism | Checkpoint manifest (world size) | Admin input |
| Topology | Not reconstructable | Must be re-evaluated on next admission |
| DRA devices | Not reconstructable | Must be re-evaluated on next admission |
| Elastic state | Checkpoint manifest (worker count) | Reset to disabled |

### Key Principle

State reconstruction creates RTJs in **Paused** state. The admin must
explicitly resume each RTJ after validating the reconstruction. This prevents
automated resumption of potentially inconsistent state.

---

## 5. Production HA Failover (Sequence Diagram)

```
  Replica A            Replica B          API Server        Lease Object
  (leader)             (standby)                            (Coordination/v1)
     |                    |                   |                  |
     |  Holding lease     |                   |                  |
     |  (renewing every   |                   |                  |
     |   10s, default)    |                   |                  |
     |------------------------------------------------------->|
     |                    |                   |                  |
     |  Reconciling RTJs  |  Watching lease   |                  |
     |  (active)          |  (passive)        |                  |
     |                    |                   |                  |
     |  === FAILURE: Replica A crashes ===    |                  |
     |  X                 |                   |                  |
     |                    |                   |                  |
     |                    |  Lease expires    |                  |
     |                    |  (LeaseDuration   |                  |
     |                    |   = 15s default)  |                  |
     |                    |                   |                  |
     |                    |  Acquire lease    |                  |
     |                    |------------------------------------->|
     |                    |<-------------------------------------|
     |                    |  (new leader)     |                  |
     |                    |                   |                  |
     |                    |  Start reconciling|                  |
     |                    |  RTJs             |                  |
     |                    |                   |                  |
     |                    |  1. List all RTJs |                  |
     |                    |------------------>|                  |
     |                    |<------------------|                  |
     |                    |                   |                  |
     |                    |  2. For each RTJ: |                  |
     |                    |  - Read status    |                  |
     |                    |  - Check Workload |                  |
     |                    |  - Check child    |                  |
     |                    |    JobSet         |                  |
     |                    |  - Reconcile to   |                  |
     |                    |    desired state  |                  |
     |                    |                   |                  |
     |                    |  3. Resume normal |                  |
     |                    |  operations       |                  |
     |                    |                   |                  |
     |  === Replica A     |                   |                  |
     |  restarts ===      |                   |                  |
     |                    |                   |                  |
     |  Try acquire lease |                   |                  |
     |------------------------------------------------------->|
     |  DENIED (B holds)  |                   |                  |
     |<-------------------------------------------------------|
     |                    |                   |                  |
     |  Enter standby     |                   |                  |
     |  (healthz: ok,     |                   |                  |
     |   readyz: not-ready)|                  |                  |
     |                    |                   |                  |
```

### HA Configuration Parameters

| Parameter | Default | Production Recommended | Description |
|-----------|---------|----------------------|-------------|
| `LeaseDuration` | 15s | 15s | Time a lease is held before expiry |
| `RenewDeadline` | 10s | 10s | Time leader tries to renew before giving up |
| `RetryPeriod` | 2s | 2s | Time between acquire/renew attempts |
| `replicaCount` | 1 | 2 | Number of operator replicas |
| PDB `minAvailable` | - | 1 | Minimum available replicas during disruption |

### Failover Safety Guarantees

1. **No duplicate child JobSets:** The new leader reads existing child JobSets
   via owner references before creating new ones (create-if-missing pattern,
   tracked by `duplicate_child_jobset_preventions_total` metric).

2. **No duplicate Workloads:** Workload synthesis uses deterministic naming
   based on RTJ UID. The new leader checks for existing Workloads before
   creating.

3. **In-flight operations:** If the old leader was mid-yield or mid-resize,
   the new leader reads the current RTJ status and resumes from the last
   persisted state. The RTJ status is the single source of truth.

4. **Webhook availability:** Both replicas serve the webhook endpoint. The
   webhook does not require leader election (stateless validation/mutation).
   The Service load-balances across both replicas.

---

## 6. Production Network Topology

```
                    +-----------+
                    | API Server|
                    +-----+-----+
                          |
             +------------+------------+
             |                         |
     (webhook: 9443)          (CRUD: 6443)
             |                         |
  +----------v----------+    +--------v--------+
  | Operator Service    |    | Kueue Service   |
  | (ClusterIP)         |    | (ClusterIP)     |
  +----------+----------+    +-----------------+
             |
     +-------+-------+
     |               |
  +--v--+         +--v--+
  | Pod |         | Pod |
  | (A) |         | (B) |
  +--+--+         +--+--+
     |               |
     +-------+-------+
             |
     (metrics: 8080)
             |
  +----------v----------+
  | Prometheus           |
  | ServiceMonitor       |
  +----------------------+
```

### NetworkPolicy Rules

| Direction | From | To | Port | Purpose |
|-----------|------|-----|------|---------|
| Ingress | API Server | Operator | 9443 | Webhook calls |
| Ingress | Prometheus | Operator | 8080 | Metrics scraping |
| Ingress | Operator | Operator | 8081 | Health probes (kubelet) |
| Egress | Operator | API Server | 6443 | Kubernetes API calls |
| Egress | Operator | Checkpoint Store | 443/9000 | S3 checkpoint access |
| Egress | Operator | DNS | 53 | Service discovery |
