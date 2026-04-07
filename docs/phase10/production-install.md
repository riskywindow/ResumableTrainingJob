# Production Install Guide

**Phase:** 10 - Production Hardening & API Beta
**Last updated:** 2026-04-07

---

## Prerequisites

| Component | Minimum Version | Notes |
|-----------|----------------|-------|
| Kubernetes | 1.30+ | Required for DRA (v1.33+ for Phase 8 features) |
| Kueue | 0.15.1+ | Required for RTJ external framework integration |
| JobSet | 0.7+ | Required for child JobSet management |
| cert-manager | 1.14+ | Required for webhook TLS in production |
| Helm | 3.12+ | Required for Helm install path |

---

## Option A: Helm (Recommended)

### 1. Install cert-manager

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true
```

### 2. Install the operator

```bash
helm install rtj-operator charts/rtj-operator \
  --namespace rtj-system \
  --create-namespace \
  --set image.repository=ghcr.io/example/checkpoint-native-preemption-controller \
  --set image.tag=0.10.0
```

### 3. Verify

```bash
kubectl -n rtj-system get pods
kubectl -n rtj-system get certificate
kubectl get crd resumabletrainingjobs.training.checkpoint.example.io
```

### Key Values

| Value | Default | Description |
|-------|---------|-------------|
| `replicaCount` | `2` | Number of operator replicas |
| `operatorMode` | `worker` | `worker` or `manager` for MultiKueue |
| `leaderElection.enabled` | `true` | Enable leader election |
| `certManager.enabled` | `true` | Enable cert-manager TLS |
| `certManager.issuerName` | `""` | External Issuer name (empty = self-signed) |
| `certManager.issuerKind` | `Issuer` | `Issuer` or `ClusterIssuer` |
| `podDisruptionBudget.enabled` | `true` | Enable PDB |
| `networkPolicy.enabled` | `false` | Enable NetworkPolicy |
| `experimentalPartialAdmission` | `false` | Enable experimental partial admission |
| `priorityClassName` | `system-cluster-critical` | Pod priority class |

### MultiKueue Manager Mode

For the manager cluster in a MultiKueue deployment:

```bash
helm install rtj-operator charts/rtj-operator \
  --namespace rtj-system \
  --create-namespace \
  --set operatorMode=manager
```

For worker clusters, use the default `operatorMode=worker`.

### Custom Issuer

To use an existing ClusterIssuer instead of self-signed:

```bash
helm install rtj-operator charts/rtj-operator \
  --namespace rtj-system \
  --create-namespace \
  --set certManager.issuerKind=ClusterIssuer \
  --set certManager.issuerName=my-ca-issuer
```

---

## Option B: Kustomize

### Base (single replica, hardened)

```bash
kubectl apply -k deploy/prod/base
```

### HA overlay (2 replicas, PDB, anti-affinity)

```bash
kubectl apply -k deploy/prod/overlays/ha
```

### cert-manager overlay

```bash
kubectl apply -k deploy/prod/overlays/cert-manager
```

### Network policy overlay

```bash
kubectl apply -k deploy/prod/overlays/network-policy
```

### Combining overlays

Create a custom kustomization.yaml that references multiple overlays:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - github.com/example/checkpoint-native-preemption-controller/deploy/prod/overlays/ha
  - github.com/example/checkpoint-native-preemption-controller/deploy/prod/overlays/cert-manager
  - github.com/example/checkpoint-native-preemption-controller/deploy/prod/overlays/network-policy
```

---

## Security Posture

All production profiles enforce the Kubernetes Pod Security Standards
**Restricted** profile:

| Control | Setting |
|---------|---------|
| `runAsNonRoot` | `true` |
| `runAsUser` | `65532` (distroless nonroot) |
| `readOnlyRootFilesystem` | `true` |
| `allowPrivilegeEscalation` | `false` |
| `capabilities.drop` | `[ALL]` |
| `seccompProfile.type` | `RuntimeDefault` |
| `priorityClassName` | `system-cluster-critical` |

---

## HA Architecture

```
                   ┌─────────────────┐
                   │  API Server     │
                   │  (webhook calls)│
                   └────────┬────────┘
                            │
              ┌─────────────┼─────────────┐
              │             │             │
        ┌─────▼─────┐ ┌─────▼─────┐
        │  Replica 0 │ │  Replica 1 │
        │  (leader)  │ │  (standby) │
        │            │ │            │
        │ reconcile  │ │ webhooks   │
        │ + webhooks │ │ only       │
        └────────────┘ └────────────┘
```

- Both replicas serve admission and conversion webhooks
- Only the leader runs reconciliation loops
- Leader election uses Kubernetes Leases (`coordination.k8s.io`)
- If the leader fails, the standby acquires the lease within ~15s
- PodDisruptionBudget ensures at least 1 replica during node drains

---

## TLS Architecture

```
cert-manager               Operator Pod
┌──────────┐              ┌──────────────┐
│ Issuer   │──creates──▶ │ Secret       │
│          │              │ (TLS cert)   │
└──────────┘              └──────┬───────┘
                                 │ mounted at
                                 ▼
                          /tmp/k8s-webhook-server/serving-certs/
                                 │
                          ┌──────▼───────┐
                          │ Webhook      │
                          │ Server :9443 │
                          └──────────────┘
                                 ▲
                          caBundle injected
                          via annotation:
                          cert-manager.io/inject-ca-from
```

cert-manager automatically:
1. Creates the TLS certificate and private key
2. Stores them in the referenced Secret
3. Injects the CA bundle into webhook configurations
4. Renews certificates before expiry (default: 30 days before)

---

## Upgrade Path

1. Update CRDs first (they are cluster-scoped):
   ```bash
   kubectl apply -f charts/rtj-operator/crds/
   ```

2. Upgrade the operator:
   ```bash
   helm upgrade rtj-operator charts/rtj-operator \
     --namespace rtj-system \
     --set image.tag=NEW_VERSION
   ```

3. Verify conversion webhook is healthy:
   ```bash
   kubectl get crd resumabletrainingjobs.training.checkpoint.example.io \
     -o jsonpath='{.status.conditions}'
   ```

---

## Related Documents

- [ADR-0004: Production Install, HA, and TLS Strategy](adr/0004-prod-install-ha-and-tls.md)
- [ADR-0003: Conversion and Storage Strategy](adr/0003-conversion-and-storage-strategy.md)
- [CRD Versioning and Migration](crd-versioning-and-migration.md)
