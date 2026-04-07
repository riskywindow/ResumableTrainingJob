# Phase 10 - Production Hardening & API Beta

**Status:** Design locked
**Depends on:** Phase 9 (signed off 2026-04-06)
**API group:** `training.checkpoint.example.io`

---

## 1. Phase Intent

Phase 10 transforms the checkpoint-native preemption controller from a
functionally complete system (Phases 0-9) into a production-ready platform
with a stable API contract and operational maturity.

This phase does **not** add new runtime features, scheduling capabilities,
or queueing models. It hardens the existing system for production use.

## 2. Scope

### 2.1 In Scope

| Area | Deliverables |
|------|-------------|
| API graduation | `ResumableTrainingJob` CRD promoted to `v1beta1` with conversion webhook |
| Production install | Helm chart (primary), Kustomize production overlays |
| HA deployment | Multi-replica operator with leader election enabled by default |
| TLS hardening | Webhook serving TLS via cert-manager; metrics endpoint TLS |
| Security | RBAC least-privilege review, Pod Security Standards, NetworkPolicy templates |
| Tenancy | Namespace-scoped RBAC templates, resource quota interaction validation |
| Observability | SLI/SLO definitions, PrometheusRule alerting, Grafana dashboard JSON, runbooks |
| Upgrade safety | CRD conversion webhook (v1alpha1 <-> v1beta1), StorageVersionMigration, rollback runbook |
| Disaster recovery | Backup/restore procedures, state reconstruction from checkpoint store + Kueue Workloads |
| Chaos/soak validation | Soak test harness, chaos scenarios (leader failover, node drain, etcd partition) |

### 2.2 Explicitly Out of Scope

- New scheduling features, runtimes, or queueing models
- Graduation of `CheckpointPriorityPolicy` or `ResumeReadinessPolicy` to v1beta1
- Automatic metric-driven resize (ElasticityMode: Auto)
- In-place grow (pending upstream Kueue support)
- Multi-cluster reclaimablePods mirroring
- Core scheduling architecture changes

## 3. CRD Graduation Plan

| CRD | Current | Phase 10 Target | Rationale |
|-----|---------|-----------------|-----------|
| `ResumableTrainingJob` | `v1alpha1` | `v1beta1` (served + stored) | Core production resource; API surface stable since Phase 3, extended in Phases 4-9 with backward-compatible additions |
| `CheckpointPriorityPolicy` | `v1alpha1` | `v1alpha1` (no change) | Optional policy CRD; priority shaping is an advanced tuning surface not required for the production path |
| `ResumeReadinessPolicy` | `v1alpha1` | `v1alpha1` (no change) | Optional admission check policy; topology-aware gating is an advanced feature not required for baseline production |

### 3.1 v1beta1 RTJ API Contract

The `v1beta1` RTJ API will be a **direct copy** of the current `v1alpha1`
schema with these changes:

- API version string: `training.checkpoint.example.io/v1beta1`
- No field removals or renames (full backward compatibility)
- Experimental fields gated behind annotations or feature flags:
  - `spec.parallelism.enablePartialAdmission` - remains experimental
  - `spec.devices` (DRA mode) - remains experimental
  - `spec.elasticity` - remains experimental
- A conversion webhook handles round-tripping between `v1alpha1` and `v1beta1`

### 3.2 What "v1beta1" Means

- The API is **suitable for production use**
- Breaking changes will follow a deprecation policy (minimum 2 releases)
- Backward-compatible additions are allowed
- Experimental sub-fields may change without notice (documented as such)

## 4. Architectural Invariants (Preserved)

All Phase 0-9 invariants are carried forward without modification:

| ID | Invariant | Source |
|----|-----------|--------|
| I-1 | RTJ is the only Kueue-managed admission object | Phase 2 |
| I-2 | Child JobSets are plain runtime resources | Phase 2 |
| I-3 | Kueue is sole authority for admission, preemption, quota | Phase 2 |
| I-4 | RTJ operator is lifecycle owner for launch/yield/resize/checkpoint/rendering | Phase 2, extended Phase 9 |
| I-5 | Checkpoint compatibility is fail-closed | Phase 0 |
| I-6 | Manager/worker split is transparent to single-cluster use | Phase 6 |
| I-7 | DRA disabled = Phase 7 behavior | Phase 8 |
| I-8 | Resume uses latest-compatible-complete checkpoint | Phase 0 |
| I-9 | Elasticity disabled = Phase 8 behavior | Phase 9 |
| I-10 | Scale-up always goes through checkpoint-and-relaunch | Phase 9 |
| I-11 | reclaimablePods is the only quota-release signal | Phase 9 |
| I-12 | Manager never evaluates elastic plans for remote RTJs | Phase 9 |
| I-13 | reclaimablePods published only on executing worker-side Workload | Phase 9 |
| I-14 | Manager never creates reclaim helper state for remote RTJs | Phase 9 |

## 5. Dependencies

| Dependency | Version | Notes |
|------------|---------|-------|
| Kubernetes | >= 1.30 | CRD conversion webhook requires `CustomResourceConversionWebhook` |
| Kueue | >= 0.15.1 | External framework, MultiKueue, PartialAdmission |
| JobSet | >= 0.10.1 | Transitive via Kueue |
| cert-manager | >= 1.14 | Webhook and metrics TLS certificate lifecycle |
| controller-runtime | >= 0.22.4 | Leader election, webhook serving, metrics server |

## 6. Linked Documents

- [goals.md](goals.md) - Detailed deliverables and acceptance criteria
- [architecture.md](architecture.md) - Production architecture diagrams
- [migration-from-phase9.md](migration-from-phase9.md) - Migration guide
- [open-questions.md](open-questions.md) - Unresolved design questions
- [adr/0001-production-hardening-and-api-beta.md](adr/0001-production-hardening-and-api-beta.md) - ADR
