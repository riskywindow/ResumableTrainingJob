# ADR-0004: Production Install, HA, and TLS Strategy

**Status:** Accepted
**Date:** 2026-04-07
**Deciders:** checkpoint-native-preemption-controller maintainers

## Context

Phase 10 requires a production-grade install path. The operator currently deploys via
Kustomize with a single replica, no TLS management, and no pod hardening.
Production deployments need:

- Multiple replicas with leader election
- Webhook TLS via cert-manager (not self-signed dev certs)
- Security-hardened pod spec (non-root, drop caps, seccomp, read-only FS)
- PodDisruptionBudget for safe node drains
- Anti-affinity / topology spread for fault tolerance
- Network policies restricting operator traffic
- Support for both single-cluster (worker mode) and MultiKueue (manager mode)

## Decision

### 1. Helm as Primary Production Install Path

We create `charts/rtj-operator/` as the primary production install path.
Kustomize overlays in `deploy/prod/` are provided for teams that prefer
Kustomize or need to compose with existing Kustomize pipelines.

**Rationale:** Helm is the de facto standard for production Kubernetes installs.
It provides parameterized templates, release management, rollback, and a
familiar UX for platform teams. Kustomize overlays complement but do not
replace Helm for production.

### 2. cert-manager for Webhook TLS

Webhook TLS is managed by cert-manager Certificate resources. The Helm chart
creates a self-signed Issuer by default, with an option to reference an
external Issuer or ClusterIssuer.

**Rationale:** cert-manager is the Kubernetes-native standard for certificate
lifecycle management. It handles rotation, renewal, and CA injection into
webhook configurations. Manual certificate management is error-prone and
does not support automatic rotation.

**Alternatives considered:**
- Manual TLS secret: rejected (no rotation, error-prone)
- kube-webhook-certgen: rejected (one-shot, no renewal)
- Operator-managed self-signed: rejected (reinvents cert-manager)

### 3. HA Profile: 2 Replicas with Leader Election

Production deploys 2 replicas by default. Leader election is enabled.
Only the leader runs reconciliation; the standby handles webhook traffic
and takes over if the leader fails.

**Rationale:** 2 replicas is the minimum for HA. Both replicas serve
webhooks (conversion + admission), so the API server can reach either.
Leader election ensures exactly-once reconciliation.

### 4. Security Hardening

All production deployments enforce:
- `runAsNonRoot: true` with UID 65532 (nonroot distroless user)
- `readOnlyRootFilesystem: true`
- `capabilities.drop: [ALL]`
- `allowPrivilegeEscalation: false`
- `seccompProfile.type: RuntimeDefault`
- `priorityClassName: system-cluster-critical`

**Rationale:** These are Kubernetes security best practices and are
required by most Pod Security Standards (Restricted profile).

### 5. Kustomize Overlays as Composable Layers

Production Kustomize overlays are organized as composable layers:
- `deploy/prod/base/` — hardened single-replica base
- `deploy/prod/overlays/ha/` — 2 replicas, PDB, anti-affinity
- `deploy/prod/overlays/cert-manager/` — cert-manager integration
- `deploy/prod/overlays/network-policy/` — network policy

Teams can combine overlays or use them individually.

### 6. Manager/Worker Mode via Values

The Helm chart supports `operatorMode: worker` (default) and
`operatorMode: manager` for MultiKueue deployments. The mode is passed
as a CLI flag to the operator binary.

## Consequences

### Positive

- Production users have a tested, parameterized Helm chart
- Webhook TLS is fully automated via cert-manager
- HA provides fault tolerance for both webhooks and reconciliation
- Security hardening meets Pod Security Standards Restricted profile
- Dev profiles (config/manager/ + deploy/dev/) remain untouched

### Negative

- Helm chart and Kustomize overlays are two install paths to maintain
- cert-manager becomes a required dependency for production TLS
- 2-replica HA requires at least 2 schedulable nodes

### Risks

- Helm template drift from Kustomize base: mitigated by tests that validate
  both paths produce structurally valid manifests
- cert-manager version incompatibility: mitigated by testing with cert-manager
  >= 1.14 and documenting the minimum version

## Related

- ADR-0003: Conversion and Storage Strategy (webhook TLS is a prerequisite)
- Phase 10 index.md: G-10.02, G-10.03, G-10.04, G-10.05
