# Tenancy and Admission Guardrails

This document describes the production tenancy model for the checkpoint-native preemption controller.  It covers namespace management, queue enforcement, bypass prevention, and user-facing RBAC.

> **Architecture Decision:** See [ADR-0005](adr/0005-namespace-and-queue-guardrails.md) for rationale and alternatives.

## Overview

The guardrail profile is **opt-in per namespace**.  Namespaces labeled `rtj.checkpoint.example.io/managed: "true"` receive:

1. **Queue enforcement** — RTJs must specify `spec.queueName`.
2. **Bypass prevention** — Direct creation of JobSets and Kueue Workloads by end users is blocked.
3. **Quota scoping** — ClusterQueues only accept Workloads from managed namespaces.

Unlabeled namespaces (dev, test, personal sandboxes) are completely unaffected.

## Managed Namespace Setup

### 1. Label the Namespace

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: training-prod
  labels:
    rtj.checkpoint.example.io/managed: "true"
    pod-security.kubernetes.io/enforce: restricted
```

The `rtj.checkpoint.example.io/managed: "true"` label activates all three ValidatingAdmissionPolicies.  The PSS restricted label is recommended but independent.

### 2. Create a LocalQueue

Each managed namespace needs at least one Kueue LocalQueue pointing to a ClusterQueue:

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  name: training-queue
  namespace: training-prod
spec:
  clusterQueue: training-cluster-queue
```

### 3. Configure the ClusterQueue

The ClusterQueue should use `namespaceSelector` to restrict which namespaces may submit:

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: training-cluster-queue
spec:
  namespaceSelector:
    matchLabels:
      rtj.checkpoint.example.io/managed: "true"
  resourceGroups:
    - coveredResources: ["cpu", "memory", "nvidia.com/gpu"]
      flavors:
        - name: default
          resources:
            - name: cpu
              nominalQuota: "1000"
            - name: memory
              nominalQuota: "4Ti"
            - name: "nvidia.com/gpu"
              nominalQuota: "64"
```

## Admission Policies

All policies use Kubernetes [ValidatingAdmissionPolicy](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/) (GA in Kubernetes 1.30).

### rtj-require-queue-assignment

| Field | Value |
|-------|-------|
| Scope | RTJ CREATE/UPDATE in managed namespaces |
| Rule | `spec.queueName` must be non-empty |
| Failure policy | Fail (deny if policy evaluation errors) |

**Example rejection:**

```
admission webhook denied the request: ResumableTrainingJobs in managed
namespaces must specify spec.queueName
```

### rtj-deny-direct-jobset

| Field | Value |
|-------|-------|
| Scope | JobSet CREATE/UPDATE in managed namespaces |
| Rule | Always deny |
| Exemptions | Service accounts in `rtj-system` namespace |
| Failure policy | Fail |

This ensures JobSets are only created by the RTJ controller as runtime children.  Users must submit RTJs; the controller renders and manages the JobSet lifecycle.

### rtj-deny-direct-workload

| Field | Value |
|-------|-------|
| Scope | Workload CREATE in managed namespaces |
| Rule | Always deny |
| Exemptions | Service accounts in `rtj-system` and `kueue-system` namespaces |
| Failure policy | Fail |

This prevents direct Kueue Workload creation that would consume quota without RTJ lifecycle management.

## RBAC

### Controller Service Account

The controller (`controller-manager` in `rtj-system`) has the minimum permissions required:

- **RTJ:** get, list, watch, update, patch (no `create` — controller reconciles user-created RTJs)
- **RTJ status/finalizers:** get, update, patch
- **Child resources (JobSets, ConfigMaps, Workloads):** full lifecycle management
- **Read-only:** ResourceFlavors, WorkloadPriorityClasses, PriorityClasses, AdmissionChecks
- **Events:** create, patch (no `update`)

### User-Facing Roles

| ClusterRole | Purpose | Key Permissions |
|-------------|---------|----------------|
| `rtj-editor` | Team members who create and manage training jobs | Full RTJ CRUD + read policies/workloads |
| `rtj-viewer` | Observers, dashboards, CI/CD pipelines | Read-only on all RTJ resources |

Both roles aggregate into the standard `edit` and `view` ClusterRoles via `rbac.authorization.k8s.io/aggregate-to-*` labels.

**Binding example (namespace-scoped):**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: team-alpha-rtj-editor
  namespace: training-prod
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: rtj-editor
subjects:
  - kind: Group
    name: team-alpha
    apiGroup: rbac.authorization.k8s.io
```

## Installation

### Kustomize

Use the tenancy overlay to deploy the operator with all guardrails:

```bash
kubectl apply -k deploy/prod/overlays/tenancy/
```

This composes:
- Production base (CRDs, operator, controller RBAC, webhooks)
- All three ValidatingAdmissionPolicies + Bindings
- User-facing RBAC roles (rtj-editor, rtj-viewer)
- Example managed namespace

Combine with other overlays as needed:

```bash
# Tenancy + HA + cert-manager
kustomize build deploy/prod/overlays/tenancy/ \
  | kustomize build deploy/prod/overlays/ha/ \
  | kubectl apply -f -
```

### Helm

The Helm chart includes user-facing RBAC roles by default.  The admission policies are applied separately since they are cluster-admin resources:

```bash
# Install operator
helm install rtj-operator charts/rtj-operator/ \
  --namespace rtj-system --create-namespace

# Apply admission policies
kubectl apply -f deploy/prod/policies/
```

## Customization

### Non-Default Operator Namespace

If the operator runs in a namespace other than `rtj-system`, update the `matchConditions` in the deny-direct-* policies:

```yaml
matchConditions:
  - name: not-controller
    expression: >-
      !request.userInfo.username.startsWith('system:serviceaccount:YOUR-NAMESPACE:')
```

### Additional Protected Resources

To block direct creation of other resources in managed namespaces (e.g., ResourceClaimTemplates), create additional VAP+Binding pairs following the `deny-direct-jobset.yaml` pattern.

### Relaxing Policies for Specific Users

Use `ValidatingAdmissionPolicyBinding.spec.matchResources.excludeResourceRules` or additional `matchConditions` to exempt specific service accounts or groups.
