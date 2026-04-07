# ADR-0005: Namespace and Queue Guardrails

| Field        | Value |
|--------------|-------|
| Status       | Accepted |
| Date         | 2026-04-07 |
| Phase        | 10 — Production Hardening & API Beta |
| Supersedes   | — |
| Superseded by | — |

## Context

The RTJ operator manages the full lifecycle of ML training jobs: Kueue admission, JobSet rendering, checkpoint-aware preemption, and elastic resize.  In production clusters, multiple teams share GPU capacity through Kueue ClusterQueues.  Without guardrails:

1. Users could create RTJs without a `queueName`, bypassing Kueue quota entirely.
2. Users could create JobSets or Kueue Workloads directly, circumventing the RTJ lifecycle and defeating quota enforcement.
3. No standard RBAC exists for team-level RTJ access.
4. The controller service-account carries permissions it does not need (e.g. `create` on RTJ objects), violating least-privilege.

The project targets Kubernetes >= 1.30, where `ValidatingAdmissionPolicy` is GA.  This provides a declarative, CEL-based alternative to custom webhooks for cluster-enforced validation.

## Decision

### 1. Opt-in Managed Namespace Model

Guardrails activate only in namespaces labeled `rtj.checkpoint.example.io/managed: "true"`.  Unlabeled namespaces (dev, test, personal) remain unrestricted.  This preserves Phase 0–9 behavior in non-production contexts.

### 2. ValidatingAdmissionPolicy for Cluster-Enforced Rules

Three `ValidatingAdmissionPolicy` + `ValidatingAdmissionPolicyBinding` pairs enforce:

| Policy | Guard Against | Exemptions |
|--------|--------------|------------|
| `rtj-require-queue-assignment` | RTJs without `spec.queueName` in managed namespaces | None (all RTJs must specify a queue) |
| `rtj-deny-direct-jobset` | Direct JobSet create/update in managed namespaces | Operator SA (`system:serviceaccount:rtj-system:*`) |
| `rtj-deny-direct-workload` | Direct Workload creation in managed namespaces | Operator SA and Kueue SA (`system:serviceaccount:kueue-system:*`) |

All policies use `matchConditions` to scope activation to managed namespaces and exempt controller service-accounts.  Policies set `failurePolicy: Fail` (fail-closed).

### 3. ClusterQueue namespaceSelector

Production ClusterQueues should use `namespaceSelector.matchLabels` with `rtj.checkpoint.example.io/managed: "true"` to restrict which namespaces may consume quota.  An example is provided; actual quota values are cluster-specific.

### 4. User-Facing RBAC Roles

Two aggregation-ready ClusterRoles:

| Role | Permissions | Aggregates To |
|------|-------------|--------------|
| `rtj-editor` | Full RTJ lifecycle (CRUD) + read policies + read Workloads | `edit` |
| `rtj-viewer` | Read-only on RTJs, CPPs, RRPs, Workloads | `view` |

Neither role grants write access to JobSets, Workloads, or ConfigMaps — those are controller-only paths.

### 5. Controller RBAC Minimization

Two over-permissions removed from the controller ClusterRole:

| Permission Removed | Reason |
|-------------------|--------|
| `create` on `resumabletrainingjobs` | Controller reconciles existing RTJs; never creates new ones |
| `update` on `events` | Events are created and patched, never updated in-place |

## Alternatives Considered

### Custom Admission Webhook

A dedicated webhook could enforce the same rules.  Rejected because:
- VAP is declarative (YAML + CEL), not code.  No new binary, no TLS management, no availability concern.
- VAP is evaluated in-process by the API server — lower latency and higher availability than a webhook.
- Aligns with the project's principle of preferring cluster-level policy where practical.

### OPA/Gatekeeper

OPA provides a powerful policy engine but requires deploying Gatekeeper as an additional component.  VAP covers all three guardrails natively with no external dependencies.

### Namespace-Scoped RBAC Only

RBAC alone cannot express "RTJs must have queueName set" or "only the controller may create JobSets."  Admission policies are required for field-level and identity-based enforcement.

## Consequences

- **Positive:** Quota bypass is blocked at the API server layer, independent of operator availability.
- **Positive:** Unmanaged namespaces are completely unaffected (opt-in model).
- **Positive:** User-facing RBAC enables standard Kubernetes RBAC binding patterns for team onboarding.
- **Negative:** Operators deploying to non-standard SA names or namespaces must customize the `matchConditions` CEL expressions in the deny-direct-* policies.
- **Negative:** VAP requires Kubernetes >= 1.30.  Clusters on older versions must use the custom webhook alternative (not provided here).

## References

- [KEP-3488: ValidatingAdmissionPolicy](https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/3488-cel-admission-control)
- [Kueue ClusterQueue namespaceSelector](https://kueue.sigs.k8s.io/docs/concepts/cluster_queue/#namespace-selector)
- Phase 0 contract: RTJ is the sole user-facing entrypoint; child JobSets are runtime-only.
- Phase 2 invariant: RTJ is the only Kueue-managed admission object.
