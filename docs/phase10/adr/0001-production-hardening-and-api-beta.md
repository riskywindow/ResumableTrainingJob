# ADR-0001: Production Hardening and API Beta

**Status:** Accepted
**Date:** 2026-04-06
**Deciders:** Project maintainers
**Phase:** 10

---

## Context

The checkpoint-native preemption controller has been under active development
for 10 phases (Phase 0 through Phase 9). The system is functionally complete:
it supports manual and Kueue-driven pause/resume, admission-aware launch with
flavor and topology awareness, checkpoint-aware priority shaping, multi-cluster
dispatch via MultiKueue, capacity-guaranteed launch, DRA device management, and
hybrid elastic resize.

All three CRDs (`ResumableTrainingJob`, `CheckpointPriorityPolicy`,
`ResumeReadinessPolicy`) are currently at `v1alpha1`. The system has no
production install story (no Helm chart, no production Kustomize overlays),
no HA deployment configuration, no TLS automation, no alerting, and no
upgrade safety mechanism.

Phase 10 must bridge the gap between "functionally complete prototype" and
"production-ready system."

## Decision

### 1. Graduate ResumableTrainingJob to v1beta1

The `ResumableTrainingJob` CRD will be promoted to
`training.checkpoint.example.io/v1beta1`.

**Rationale:**
- RTJ is the core resource that users interact with directly
- The API surface has been stable since Phase 3; Phases 4-9 added new
  optional sub-fields without breaking changes
- The spec and status schema will be **identical** between v1alpha1 and v1beta1
  (apiVersion-only conversion)
- v1beta1 signals "suitable for production use" with a deprecation policy

**What v1beta1 means:**
- Breaking changes require a deprecation period (minimum 2 releases)
- Backward-compatible additions are allowed at any time
- Fields marked "experimental" (partialAdmission, devices, elasticity) may
  change without notice

### 2. Keep CheckpointPriorityPolicy and ResumeReadinessPolicy at v1alpha1

**Rationale (CPP):** Priority shaping is an advanced tuning surface. The
priority model (checkpoint freshness scoring, yield budgets, protection
windows, cooldown periods) may evolve significantly based on production
workload feedback. Graduating CPP prematurely would constrain future
optimization of the priority algorithm.

**Rationale (RRP):** The ResumeReadinessPolicy is tightly coupled to Kueue's
AdmissionCheck pipeline. As Kueue evolves its AdmissionCheck surface (and
potentially absorbs resume-readiness checking natively), RRP may need
significant changes. Keeping it at v1alpha1 preserves flexibility.

### 3. Helm Chart as Primary Install Path

**Rationale:** Helm provides:
- Parameterized installation (production vs dev values)
- Upgrade/rollback lifecycle management
- Dependency management (cert-manager)
- Community standard for Kubernetes operator distribution

Kustomize production overlays will be maintained as a secondary path for
teams that prefer Kustomize.

### 4. HA with Leader Election by Default

**Rationale:**
- Production deployments require high availability
- controller-runtime's leader election is battle-tested
- The operator already supports `--leader-elect` (added in Phase 1)
- 2-replica deployment with PDB ensures zero-downtime upgrades

### 5. cert-manager for TLS Lifecycle

**Rationale:**
- cert-manager is the de facto standard for Kubernetes certificate management
- Handles automatic rotation, renewal, and CA bundle injection
- Already required by many Kueue production deployments
- Eliminates manual certificate management burden

### 6. Manual Disaster Recovery (Phase 10)

**Rationale:**
- Automated state reconstruction has high risk of incorrect state propagation
- The checkpoint store + Kueue Workload state provides sufficient information
  for manual reconstruction
- Admin-gated reconstruction ensures human review of recovered state
- Automated reconstruction can be explored in Phase 11+ with production
  operational experience

### 7. Conversion Webhook in Same Pod

**Rationale:**
- The operator already runs a webhook server on port 9443
- Adding a conversion handler is a single function registration
- No additional deployment, service, or certificate needed
- Conversion is lightweight (apiVersion field change only)
- Downside (brief unavailability during restart) is mitigated by HA deployment

## Consequences

### Positive

- Users get a stable API contract for the core RTJ resource
- Production deployments have a clear, tested installation path
- HA deployment prevents single point of failure
- cert-manager integration eliminates TLS management burden
- Observability stack (alerts + dashboards) reduces MTTR
- Upgrade safety via conversion webhook prevents data loss

### Negative

- Conversion webhook adds a dependency on operator availability for API server
  reads (mitigated by HA + PDB)
- cert-manager becomes a soft dependency for production installs (alternative:
  manual certificate management)
- Helm chart maintenance overhead alongside Kustomize overlays
- Phase 9 experimental features (elasticity, DRA, partial admission) are
  included in v1beta1 schema but marked experimental, creating a potentially
  confusing "stable API with unstable sub-fields" situation

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Conversion webhook downtime blocks API server | Low (HA + PDB) | High | PDB minAvailable=1, webhook timeout, failurePolicy |
| v1alpha1 deprecation breaks existing tooling | Medium | Medium | 2-release deprecation window, both versions served |
| cert-manager unavailability blocks webhook | Low | High | Support manual certificate path as fallback |
| Experimental fields change after v1beta1 | Medium | Low | Clear documentation, annotation-gated features |

## Alternatives Considered

### Alternative A: Skip v1beta1, Go Directly to v1

**Rejected because:** The experimental sub-fields (devices, elasticity,
partial admission) are not mature enough for a v1 stability commitment. v1beta1
provides a middle ground with production suitability and room for experimental
evolution.

### Alternative B: Graduate All CRDs to v1beta1

**Rejected because:** CPP and RRP are optional policy CRDs with surfaces that
may need significant changes. Graduating them prematurely would constrain
future design. Users of CPP/RRP can continue using v1alpha1 without any
impact.

### Alternative C: Operator-per-Namespace Model

**Rejected because:** The current cluster-scoped operator model aligns with
Kueue's cluster-scoped ClusterQueue and AdmissionCheck resources. A
namespace-scoped operator would require significant architectural changes
and would not integrate cleanly with Kueue's tenancy model.

### Alternative D: Automated Disaster Recovery

**Rejected for Phase 10** (not permanently). Manual reconstruction is safer
for the initial production release. Automated DR can be explored once
operators have production experience with the system and the failure modes
are better understood.

## References

- [Phase 10 Index](../index.md)
- [Phase 10 Goals](../goals.md)
- [Phase 10 Architecture](../architecture.md)
- [Phase 10 Migration Guide](../migration-from-phase9.md)
- [Kubernetes API Versioning](https://kubernetes.io/docs/reference/using-api/#api-versioning)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Kueue MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/)
