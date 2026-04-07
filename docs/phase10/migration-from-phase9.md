# Migration from Phase 9 to Phase 10

This document explains what changes when upgrading from a Phase 9 deployment to
Phase 10 (Production Hardening & API Beta).

---

## 1. What Stays the Same

Phase 10 preserves the entire Phase 0-9 runtime, scheduling, and control-plane
architecture. The following are **unchanged**:

### Runtime & Scheduling

- RTJ remains the only Kueue-managed admission object (I-1)
- Child JobSets remain plain runtime resources (I-2)
- Kueue remains sole authority for admission, preemption, quota (I-3)
- RTJ operator remains lifecycle owner for launch/yield/resize/checkpoint/rendering (I-4)
- Checkpoint compatibility remains fail-closed (I-5)
- Manager/worker split remains transparent to single-cluster use (I-6)
- DRA disabled = Phase 7 behavior (I-7)
- Resume uses latest-compatible-complete checkpoint (I-8)
- Elasticity disabled = Phase 8 behavior (I-9)
- Scale-up always goes through checkpoint-and-relaunch (I-10)
- reclaimablePods is the only quota-release signal (I-11)
- Manager never evaluates elastic plans for remote RTJs (I-12)
- reclaimablePods published only on executing worker-side Workload (I-13)
- Manager never creates reclaim helper state for remote RTJs (I-14)

### Controller Behavior

- Reconciliation logic: identical
- Workload synthesis: identical
- Child JobSet rendering: identical
- Checkpoint selection: identical
- Priority shaping: identical
- Elastic resize: identical
- Multi-cluster dispatch: identical

### Python SDK

- No changes to `yield_sdk`
- No changes to checkpoint manifest format
- No changes to elastic resize protocol

---

## 2. What Is Newly Stabilized

### RTJ CRD at v1beta1

The `ResumableTrainingJob` CRD is promoted to `v1beta1`:

| Aspect | Phase 9 | Phase 10 |
|--------|---------|----------|
| API version | `training.checkpoint.example.io/v1alpha1` | `training.checkpoint.example.io/v1beta1` (primary) |
| Stored version | `v1alpha1` | `v1beta1` |
| Served versions | `v1alpha1` | `v1beta1`, `v1alpha1` (via conversion) |
| Schema | Identical | Identical (no field changes) |

**Impact on existing deployments:**
- Existing v1alpha1 RTJ manifests continue to work (conversion webhook handles
  automatic translation)
- `kubectl get rtj` returns v1beta1 by default
- No manual migration required for existing objects (StorageVersionMigration
  handles background conversion)
- Clients using v1alpha1 API will continue to work indefinitely

### Stable API Contract

With v1beta1, these fields are considered **stable**:

| Field | Stable Since | Notes |
|-------|-------------|-------|
| `spec.control.desiredState` | Phase 1 | Running/Paused |
| `spec.checkpoint.*` | Phase 1 | storageURI, interval, freshnessBudget, maxDrainTime |
| `spec.resume.*` | Phase 1 | sourcePolicy, maxResumeRetries, allowWorldSizeChange |
| `spec.queueName` | Phase 2 | Required |
| `spec.workloadPriorityClassName` | Phase 2 | Required |
| `spec.suspend` | Phase 2 | Kueue integration |
| `spec.parallelism.preferredCount` | Phase 3 | Worker count |
| `spec.parallelism.minCount` | Phase 3 | Minimum workers |
| `spec.topology.*` | Phase 4 | mode, level |
| `spec.priorityPolicyRef` | Phase 5 | CPP reference |
| `spec.managedBy` | Phase 6 | MultiKueue |
| `status.phase` | Phase 1 | Full lifecycle |
| `status.currentRunAttempt` | Phase 1 | Attempt counter |
| `status.latestCheckpoint` | Phase 1 | Checkpoint evidence |
| `status.conditions` | Phase 2 | Standard conditions |

### Experimental Fields (within v1beta1)

These fields are included in v1beta1 but marked as **experimental** and may
change without deprecation notice:

| Field | Phase | Reason |
|-------|-------|--------|
| `spec.parallelism.enablePartialAdmission` | Phase 3 | Experimental, depends on Kueue feature gate |
| `spec.devices.*` | Phase 8 | DRA is evolving upstream (v1beta1 -> v1) |
| `spec.elasticity.*` | Phase 9 | Manual-only; metrics wiring not complete |

---

## 3. What Remains at v1alpha1 (Experimental)

### CheckpointPriorityPolicy (CPP)

- Stays at `training.checkpoint.example.io/v1alpha1`
- **Reason:** Priority shaping is an advanced tuning surface. The priority
  model (freshness scoring, yield budgets, protection windows) may evolve
  significantly based on production feedback. Graduating it prematurely would
  constrain future optimization.
- **Impact:** Users of CPP continue unchanged. No migration needed.

### ResumeReadinessPolicy (RRP)

- Stays at `training.checkpoint.example.io/v1alpha1`
- **Reason:** The resume readiness gate is tightly coupled to the topology and
  admission check pipeline. As Kueue evolves its AdmissionCheck surface (and
  potentially absorbs some of this functionality), RRP may need to change.
- **Impact:** Users of RRP continue unchanged. No migration needed.

---

## 4. What Changes Operationally

### 4.1 Installation

| Aspect | Phase 9 | Phase 10 |
|--------|---------|----------|
| Primary install | `kustomize build config/` + `kubectl apply` | `helm install` (Helm chart) |
| Kustomize | Dev overlays only | Dev + production overlays |
| HA mode | Manual (`--leader-elect=true`) | Default in production profile |
| TLS | Self-signed or manual | cert-manager managed (default) |
| Metrics TLS | Plaintext | Optional TLS via cert-manager |

### 4.2 Deployment Topology

| Aspect | Phase 9 | Phase 10 |
|--------|---------|----------|
| Replicas | 1 (default) | 2 (production default) |
| Leader election | Opt-in | On by default (production) |
| PodDisruptionBudget | None | minAvailable: 1 |
| Pod anti-affinity | None | Spread across nodes |
| Pod Security Standards | Not enforced | `restricted` profile |
| NetworkPolicy | None | Webhook + metrics ingress restricted |

### 4.3 Observability

| Aspect | Phase 9 | Phase 10 |
|--------|---------|----------|
| Metrics | Defined, partially wired | Fully wired + alerting rules |
| Dashboards | None | Grafana JSON dashboard |
| Alerting | None | PrometheusRule with runbooks |
| SLIs/SLOs | Not defined | Defined and documented |
| Logging | Structured (zap) | Audited for consistency |

### 4.4 Security

| Aspect | Phase 9 | Phase 10 |
|--------|---------|----------|
| RBAC | Broad ClusterRole | Least-privilege, per-persona roles |
| Tenancy | Namespace isolation (implicit) | Explicit RBAC templates |
| Webhook TLS | Self-signed / manual | cert-manager lifecycle |
| Network | Open | NetworkPolicy enforced |

### 4.5 Upgrade Path

| Aspect | Phase 9 | Phase 10 |
|--------|---------|----------|
| CRD upgrade | Replace CRD manifest | Conversion webhook + StorageVersionMigration |
| Rollback | Re-apply old CRD | Documented rollback procedure |
| Feature gates | `--enable-experimental-partial-admission` | Extended gate framework |

---

## 5. Migration Checklist

For teams upgrading from Phase 9 to Phase 10:

### Pre-Upgrade

- [ ] Verify Kubernetes >= 1.30
- [ ] Verify Kueue >= 0.15.1
- [ ] Install cert-manager >= 1.14 (if not already present)
- [ ] Backup existing CRD definitions
- [ ] Backup RTJ objects (`kubectl get rtj -A -o yaml > rtj-backup.yaml`)
- [ ] Verify all RTJs are in a stable phase (Running or Paused, not mid-transition)

### Upgrade

- [ ] Apply new CRD manifests (with conversion webhook strategy)
- [ ] Deploy Phase 10 operator (Helm or Kustomize production overlay)
- [ ] Verify conversion webhook is serving (check webhook endpoint health)
- [ ] Verify existing RTJs are accessible via both v1alpha1 and v1beta1

### Post-Upgrade

- [ ] Run StorageVersionMigration to convert all stored objects to v1beta1
- [ ] Verify Prometheus scrapes new alerting rules
- [ ] Import Grafana dashboard
- [ ] Test leader failover (delete active leader pod)
- [ ] Test webhook certificate rotation
- [ ] Update client tooling to use v1beta1 (optional, v1alpha1 continues to work)

### Rollback (if needed)

- [ ] Scale operator to 0 replicas
- [ ] Re-apply Phase 9 CRD (v1alpha1-only, no conversion webhook)
- [ ] Re-deploy Phase 9 operator
- [ ] Verify all RTJs are accessible
- [ ] Note: RTJs stored as v1beta1 will need manual conversion if v1beta1 CRD
  is removed (StorageVersionMigration back to v1alpha1 first)
