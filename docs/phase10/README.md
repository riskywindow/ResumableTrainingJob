# Phase 10 - Production Hardening & API Beta

Phase 10 promotes the checkpoint-native preemption controller from a working
prototype (Phases 0-9) to a production-grade system with stable APIs.

## Quick Links

| Document | Purpose |
|----------|---------|
| [index.md](index.md) | Phase overview and scope |
| [goals.md](goals.md) | Numbered deliverables and acceptance criteria |
| [architecture.md](architecture.md) | Production component, upgrade, DR, and HA diagrams |
| [migration-from-phase9.md](migration-from-phase9.md) | What changes for existing Phase 9 deployments |
| [open-questions.md](open-questions.md) | Unresolved design questions |
| [session-handoff.md](session-handoff.md) | Session state for continuation |
| [adr/0001-production-hardening-and-api-beta.md](adr/0001-production-hardening-and-api-beta.md) | Architectural decision record |

## Core Themes

1. **API Graduation** - Promote `ResumableTrainingJob` to `v1beta1`; keep
   optional policy CRDs at `v1alpha1`.
2. **Production Install** - Helm chart, Kustomize production overlays, HA
   deployment, leader election, webhook + metrics TLS, cert-manager.
3. **Security & Tenancy** - RBAC hardening, namespace-scoped tenancy, network
   policies, Pod Security Standards enforcement.
4. **Observability** - SLI/SLO definitions, Prometheus alerting rules,
   Grafana dashboards, structured logging, runbooks.
5. **Upgrade Safety** - CRD conversion webhook, storage version migration,
   rollback procedures, feature gates.
6. **Disaster Recovery** - Backup/restore procedures, state reconstruction
   from checkpoints + Kueue Workloads, etcd snapshot integration.
7. **Validation** - Soak testing, chaos engineering, upgrade integration tests.

## Invariants Preserved

All Phase 0-9 invariants (I-1 through I-14) are preserved. Phase 10 does not
modify the runtime, scheduling, or control-plane architecture. See
[migration-from-phase9.md](migration-from-phase9.md) for the full list.
