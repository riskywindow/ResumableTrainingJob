# Phase 10 Goals & Deliverables

Each goal has an identifier (G-10.xx), deliverables, and acceptance criteria.

---

## G-10.01 - RTJ CRD Graduation to v1beta1

**Deliverables:**
- `api/v1beta1/` package with RTJ types mirroring current v1alpha1 schema
- Conversion webhook for round-tripping v1alpha1 <-> v1beta1
- `v1beta1` set as the served and stored version
- `v1alpha1` remains served but not stored (conversion on read)
- CRD manifest updated with `conversion.strategy: Webhook`
- Unit tests for lossless round-trip conversion
- Fuzz tests for conversion edge cases

**Acceptance Criteria:**
- `kubectl get rtj` returns v1beta1 objects by default
- Existing v1alpha1 RTJs are served correctly via conversion
- `kubectl apply` of a v1alpha1 manifest succeeds and is stored as v1beta1
- Round-trip conversion loses no fields (validated by fuzz testing)
- Experimental fields preserved through conversion with no silent drops

---

## G-10.02 - Helm Chart (Primary Install Path)

**Deliverables:**
- `charts/checkpoint-native-operator/` Helm chart with:
  - `values.yaml` with production defaults
  - Templates for: Deployment, ServiceAccount, ClusterRole, ClusterRoleBinding,
    Service, MutatingWebhookConfiguration, ValidatingWebhookConfiguration,
    cert-manager Certificate + Issuer, ServiceMonitor, PrometheusRule,
    NetworkPolicy, PodDisruptionBudget, ConfigMap (operator config)
  - Support for `--set operator.mode=manager` or `worker`
  - HA mode: `replicaCount: 2` with leader election enabled
  - `values.production.yaml` overlay for production defaults
  - `values.dev.yaml` overlay for single-replica dev/test
- Chart version `0.10.0`
- Helm chart unit tests (helm-unittest)
- Install/upgrade/rollback smoke test

**Acceptance Criteria:**
- `helm install` deploys a working operator in < 60 seconds
- `helm upgrade` from dev to production values succeeds without downtime
- `helm rollback` restores previous configuration
- Chart passes `helm lint` and `helm template` validation

---

## G-10.03 - Kustomize Production Overlays

**Deliverables:**
- `config/production/` overlay with:
  - 2-replica HA deployment with leader election
  - Resource limits and requests
  - Pod anti-affinity for spread across nodes
  - PodDisruptionBudget (minAvailable: 1)
  - cert-manager Certificate resources for webhook + metrics TLS
  - NetworkPolicy restricting ingress to webhook port (9443) and metrics (8080)
  - Pod Security Standards: `restricted` profile enforcement
- `config/production/multikueue-manager/` overlay extending production for
  manager mode
- `config/production/multikueue-worker/` overlay extending production for
  worker mode

**Acceptance Criteria:**
- `kustomize build config/production` produces valid manifests
- Manifests pass `kubectl apply --dry-run=server` on a v1.30+ cluster
- All pods run under `restricted` PSS profile

---

## G-10.04 - HA Controller Deployment & Leader Election

**Deliverables:**
- Leader election enabled by default in production profiles
- `--leader-elect=true` set in production Helm values and Kustomize overlays
- Leader election lease tuning (LeaseDuration, RenewDeadline, RetryPeriod)
  exposed as configuration
- Health check endpoints (`/healthz`, `/readyz`) correctly reflect leader status
- Documentation for HA deployment topology

**Acceptance Criteria:**
- Two-replica deployment with only one active reconciler at any time
- Leader failover completes within 30 seconds (configurable)
- Non-leader replica correctly reports not-ready until it acquires the lease
- Failover does not cause duplicate child JobSet creation (I-4 preserved)

---

## G-10.05 - Webhook & Metrics TLS via cert-manager

**Deliverables:**
- cert-manager `Certificate` resource for webhook serving certificate
- cert-manager `Certificate` resource for metrics endpoint TLS (optional)
- Webhook configuration `caBundle` injection via cert-manager's `inject-ca-from`
  annotation
- Support for user-provided certificates (non-cert-manager path)
- Metrics endpoint TLS configuration flag (`--metrics-tls-cert-file`,
  `--metrics-tls-key-file`)
- Documentation for certificate rotation behavior

**Acceptance Criteria:**
- Webhook serves TLS with cert-manager-managed certificate
- Certificate rotation occurs without operator restart
- Metrics endpoint optionally serves HTTPS
- Non-cert-manager path works with manually provisioned certificates

---

## G-10.06 - RBAC Hardening & Tenancy Guardrails

**Deliverables:**
- Least-privilege ClusterRole review and tightening
- Namespace-scoped Role + RoleBinding templates for multi-tenant deployments
- Separate RBAC for:
  - Operator service account (cluster-scoped, minimal)
  - RTJ authors (namespace-scoped, create/update/delete RTJ)
  - RTJ viewers (namespace-scoped, read-only)
  - Cluster admin (manage policies, admission checks, cluster queues)
- Pod Security Standards enforcement (`restricted` profile for operator pods)
- NetworkPolicy templates (ingress: webhook 9443 from API server, metrics 8080
  from monitoring; egress: API server, checkpoint store)
- Documentation for multi-tenant deployment patterns

**Acceptance Criteria:**
- Operator runs successfully with tightened RBAC
- RTJ author cannot modify cluster-scoped resources (CPP, RRP, ClusterQueue)
- RTJ viewer cannot create or modify RTJs
- All RBAC manifests pass `kubectl auth can-i` validation

---

## G-10.07 - Observability: SLIs, SLOs, Alerting, Dashboards

**Deliverables:**
- SLI definitions document:
  - Reconcile latency (p50, p95, p99)
  - Checkpoint-to-resume latency
  - Yield-to-paused latency
  - Workload admission latency (measured from RTJ creation)
  - Error rate (reconcile errors / total reconciles)
- SLO targets document (recommended starting points)
- PrometheusRule manifest with alerting rules:
  - `RTJReconcileLatencyHigh` (p99 > threshold)
  - `RTJResumeFailureRateHigh` (resume failures > N in window)
  - `RTJStuckInPhase` (RTJ in non-terminal phase > threshold)
  - `RTJWorkloadPatchFailures` (SSA patch failures)
  - `RTJLeaderElectionLost` (leader election loss events)
  - `RTJWebhookErrors` (webhook rejection rate)
  - `RTJCheckpointStorageUnreachable` (shared store failures)
- Grafana dashboard JSON:
  - RTJ lifecycle overview (phase distribution, transitions)
  - Checkpoint & resume performance
  - Elastic resize operations
  - Multi-cluster dispatch status
  - Operator health (reconcile rate, error rate, queue depth)
- Structured logging audit (ensure all log lines have consistent keys)
- Runbook stubs for each alert

**Acceptance Criteria:**
- PrometheusRule deploys and Prometheus scrapes rules successfully
- Grafana dashboard imports and renders with sample data
- All existing metrics are represented in at least one dashboard panel
- Runbooks cover root cause analysis and remediation steps for each alert

---

## G-10.08 - Upgrade Safety: Conversion Webhook & Migration

**Deliverables:**
- CRD conversion webhook handler:
  - `v1alpha1 -> v1beta1` conversion (upward)
  - `v1beta1 -> v1alpha1` conversion (downward, for rollback)
  - Identity conversion (same version, no-op)
- StorageVersionMigration trigger documentation
- Upgrade runbook:
  - Pre-upgrade checklist (backup CRDs, check Kueue version)
  - Rolling upgrade procedure
  - Post-upgrade validation steps
  - Rollback procedure
- Feature gate framework for Phase 10+ features:
  - `StableRTJAPI` (default: true) - serve v1beta1
  - `MetricsTLS` (default: false) - metrics endpoint TLS
  - `StrictPSS` (default: true) - enforce restricted Pod Security Standards
- Integration test: upgrade from v1alpha1-only to dual-version deployment

**Acceptance Criteria:**
- CRD with conversion webhook deploys successfully
- v1alpha1 RTJs are readable after upgrade (conversion on the fly)
- New RTJs created as v1beta1 are readable via v1alpha1 client
- Rollback to v1alpha1-only deployment succeeds without data loss
- StorageVersionMigration completes for all existing RTJ objects

---

## G-10.09 - Disaster Recovery & State Reconstruction

**Deliverables:**
- Backup procedure documentation:
  - etcd snapshot covering RTJ, CPP, RRP custom resources
  - Checkpoint store backup (S3 bucket versioning / replication)
  - Kueue Workload state backup
- State reconstruction runbook:
  - Reconstruct RTJ state from checkpoint manifests + Kueue Workloads
  - Re-create RTJ objects from checkpoint store metadata
  - Validate reconstructed state before resuming operations
- Disaster recovery test scenarios:
  - etcd data loss with checkpoint store intact
  - Checkpoint store data loss with etcd intact
  - Full cluster rebuild from scratch
- Controller restart resilience validation:
  - Operator restart during yield/drain flow
  - Operator restart during resize operation
  - Operator restart during checkpoint upload

**Acceptance Criteria:**
- State reconstruction from checkpoints produces valid RTJ objects
- Controller restart during any phase transition recovers correctly
- DR runbook tested end-to-end in dev environment
- No orphaned child JobSets after reconstruction

---

## G-10.10 - Chaos & Soak Validation

**Deliverables:**
- Soak test harness:
  - 24-hour continuous operation test profile
  - Multiple concurrent RTJs (10+) with mixed lifecycle operations
  - Periodic pause/resume/resize cycles
  - Memory leak detection (operator RSS monitoring)
  - Goroutine leak detection (pprof goroutine profile)
- Chaos scenarios:
  - Leader election failover (kill active leader pod)
  - Node drain during checkpoint upload
  - API server unreachable (network partition)
  - Webhook certificate expiry and rotation
  - Kueue controller restart during admission
  - S3/checkpoint store temporary unavailability
- Chaos test automation (scripts or CI job definitions)
- Results documentation template

**Acceptance Criteria:**
- 24-hour soak completes without OOM, goroutine leak, or stuck RTJs
- Leader failover recovers within 30 seconds, no duplicate child JobSets
- Node drain during checkpoint triggers clean yield-and-resume cycle
- All chaos scenarios have documented expected behavior and recovery
