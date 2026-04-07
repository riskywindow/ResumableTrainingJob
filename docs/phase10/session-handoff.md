# Phase 10 Session Handoff

**Last updated:** 2026-04-07
**Phase:** 10 - Production Hardening & API Beta
**Status:** Conversion webhook wired; v1beta1 is storage version; Helm chart created; production install profile complete; tenancy and admission guardrails complete; all tests pass

---

## 1. Decisions Made

| Decision | Choice | Rationale |
|----------|--------|-----------|
| RTJ graduates to v1beta1 | Yes | Core resource, stable since Phase 3, all extensions backward-compatible |
| CPP graduates to v1beta1 | Yes (changed from ADR-0001) | Smallest coherent production set; priority shaping is integral to safe preemption |
| RRP graduates to v1beta1 | Yes (changed from ADR-0001) | Smallest coherent production set; resume readiness gates prevent unsafe launches |
| v1beta1 schema | Identical to v1alpha1 | No field changes; conversion is apiVersion-only |
| Storage version | **v1beta1** (changed from v1alpha1) | Conversion webhook now wired; v1beta1 is the hub |
| Conversion strategy | **Webhook** (changed from None) | handler at `/convert` on :9443 |
| Conversion mechanism | JSON roundtrip | Lossless for identical schemas; zero-maintenance |
| Hub/spoke topology | v1beta1=Hub, v1alpha1=Spoke | Standard controller-runtime pattern |
| Experimental fields in v1beta1 | partialAdmission, devices, elasticity | Documented as experimental, may change without deprecation |
| v1alpha1 webhook coverage | matchPolicy: Equivalent | Existing v1alpha1 webhooks cover both versions |
| Conversion webhook location | Same operator pod | Shares existing webhook server on :9443 |
| **Helm as primary prod install** | **Yes** (ADR-0004) | De facto standard; parameterized templates, release management |
| **cert-manager for webhook TLS** | **Yes** (ADR-0004) | Kubernetes-native cert lifecycle; auto-rotation and CA injection |
| **HA profile: 2 replicas** | **Yes** (ADR-0004) | Minimum for fault tolerance; both replicas serve webhooks |
| **Pod security: Restricted profile** | **Yes** (ADR-0004) | Non-root, drop caps, seccomp, read-only FS |
| **Kustomize overlays as layers** | **Yes** (ADR-0004) | Composable: base + ha + cert-manager + network-policy + tenancy |
| **Manager/worker mode via values** | **Yes** | operatorMode flag passed as Helm value |
| **dev-mode flag for logging** | **Yes** | Defaults to structured JSON; dev-mode enables verbose |
| **Opt-in managed namespace model** | **Yes** (ADR-0005) | Label `rtj.checkpoint.example.io/managed: "true"` activates guardrails; unlabeled NS unaffected |
| **ValidatingAdmissionPolicy for enforcement** | **Yes** (ADR-0005) | Declarative CEL-based policy; evaluated in API server; no webhook required |
| **Queue assignment enforcement** | **Yes** (ADR-0005) | RTJs in managed NS must specify spec.queueName |
| **Direct bypass prevention** | **Yes** (ADR-0005) | Block direct JobSet/Workload creation by non-controller users in managed NS |
| **User-facing RBAC roles** | **Yes** (ADR-0005) | rtj-editor (full CRUD) and rtj-viewer (read-only) with aggregation labels |
| **Controller RBAC minimization** | **Yes** (ADR-0005) | Removed `create` on RTJ (controller never creates RTJs) and `update` on events |

## 2. Files Created (This Session)

| File | Purpose |
|------|---------|
| `deploy/prod/policies/require-queue-assignment.yaml` | VAP + Binding: RTJs in managed NS must have spec.queueName |
| `deploy/prod/policies/deny-direct-jobset.yaml` | VAP + Binding: block non-controller JobSet writes in managed NS |
| `deploy/prod/policies/deny-direct-workload.yaml` | VAP + Binding: block non-controller Workload creation in managed NS |
| `deploy/prod/namespaces/managed-namespace.yaml` | Example managed namespace with guardrail and PSS labels |
| `deploy/prod/namespaces/clusterqueue-example.yaml` | Example ClusterQueue with namespaceSelector + LocalQueue |
| `config/rbac/rtj_user_roles.yaml` | User-facing ClusterRoles: rtj-editor and rtj-viewer |
| `deploy/prod/overlays/tenancy/kustomization.yaml` | Tenancy overlay: base + policies + user roles + managed NS |
| `docs/phase10/adr/0005-namespace-and-queue-guardrails.md` | ADR: namespace model, VAP, RBAC, guardrail rationale |
| `docs/phase10/tenancy-and-admission.md` | Guide: managed NS setup, policy descriptions, RBAC, install |
| `test/integration/rbac_and_policy_validation_test.go` | 20 tests: policy structure, RBAC minimization, user roles, overlays |

## 3. Files Changed (This Session)

| File | Change |
|------|--------|
| `config/rbac/role.yaml` | Removed `create` on RTJ; removed `update` on events (RBAC minimization) |
| `charts/rtj-operator/templates/rbac.yaml` | Same RBAC minimization + added rtj-editor and rtj-viewer ClusterRoles |

## 4. Files Created (Previous Sessions)

| File | Purpose |
|------|---------|
| `charts/rtj-operator/Chart.yaml` | Helm chart metadata (v0.10.0, kubeVersion >=1.30) |
| `charts/rtj-operator/values.yaml` | Production defaults (2 replicas, LE, cert-manager, PDB, security) |
| `charts/rtj-operator/.helmignore` | Helm ignore patterns |
| `charts/rtj-operator/templates/_helpers.tpl` | Template helpers (names, labels, selectors) |
| `charts/rtj-operator/templates/deployment.yaml` | Operator Deployment with full production spec |
| `charts/rtj-operator/templates/serviceaccount.yaml` | ServiceAccount |
| `charts/rtj-operator/templates/rbac.yaml` | ClusterRole, ClusterRoleBinding, leader-election Role/RoleBinding, user roles |
| `charts/rtj-operator/templates/service-webhook.yaml` | Webhook Service |
| `charts/rtj-operator/templates/service-metrics.yaml` | Metrics Service |
| `charts/rtj-operator/templates/webhooks.yaml` | Mutating + Validating webhook configs with cert-manager CA injection |
| `charts/rtj-operator/templates/cert-manager.yaml` | Issuer + Certificate for webhook TLS |
| `charts/rtj-operator/templates/pdb.yaml` | PodDisruptionBudget |
| `charts/rtj-operator/templates/networkpolicy.yaml` | NetworkPolicy (ingress/egress restrictions) |
| `deploy/prod/base/kustomization.yaml` | Kustomize base: all resources, image override, common labels |
| `deploy/prod/base/namespace.yaml` | rtj-system Namespace |
| `deploy/prod/base/patches/manager-prod.yaml` | Security hardening, probes, resources, priority class |
| `deploy/prod/base/patches/webhook-service-prod.yaml` | Webhook service labels |
| `deploy/prod/overlays/ha/kustomization.yaml` | HA overlay composition |
| `deploy/prod/overlays/ha/pdb.yaml` | PodDisruptionBudget (minAvailable: 1) |
| `deploy/prod/overlays/ha/patches/ha-deployment.yaml` | 2 replicas, anti-affinity, topology spread |
| `deploy/prod/overlays/cert-manager/kustomization.yaml` | cert-manager overlay composition |
| `deploy/prod/overlays/cert-manager/issuer.yaml` | Self-signed Issuer |
| `deploy/prod/overlays/cert-manager/certificate.yaml` | Webhook TLS Certificate |
| `deploy/prod/overlays/cert-manager/patches/webhook-cainjection.yaml` | CA injection annotations |
| `deploy/prod/overlays/network-policy/kustomization.yaml` | NetworkPolicy overlay composition |
| `deploy/prod/overlays/network-policy/networkpolicy.yaml` | Ingress/Egress NetworkPolicy |
| `api/v1beta1/doc.go` | Package doc with kubebuilder markers |
| `api/v1beta1/groupversion_info.go` | GroupVersion, SchemeBuilder, AddToScheme |
| `api/v1beta1/resumabletrainingjob_types.go` | All RTJ types: enums, spec, status, defaults |
| `api/v1beta1/checkpointprioritypolicy_types.go` | CPP types: spec, status, PriorityShapingStatus, defaults |
| `api/v1beta1/resumereadinesspolicy_types.go` | RRP types: spec, status, defaults |
| `api/v1beta1/zz_generated.deepcopy.go` | Auto-generated DeepCopy methods (controller-gen) |
| `api/v1beta1/types_test.go` | 16 tests: scheme, round-trip, defaults, backward-compat, enum parity |
| `api/v1beta1/schema_parity_test.go` | 7 tests: JSON field parity and default parity vs v1alpha1 |
| `api/v1beta1/resumabletrainingjob_conversion.go` | Hub marker for RTJ |
| `api/v1beta1/checkpointprioritypolicy_conversion.go` | Hub marker for CPP |
| `api/v1beta1/resumereadinesspolicy_conversion.go` | Hub marker for RRP |
| `api/v1alpha1/resumabletrainingjob_conversion.go` | Spoke conversion (ConvertTo/ConvertFrom) + jsonRoundTrip helper |
| `api/v1alpha1/checkpointprioritypolicy_conversion.go` | Spoke conversion for CPP |
| `api/v1alpha1/resumereadinesspolicy_conversion.go` | Spoke conversion for RRP |
| `test/integration/conversion_roundtrip_test.go` | 8 tests: round-trip, type safety, minimal object, interface checks |
| `test/integration/crd_versioning_test.go` | 4 tests: CRD config, scheme registration, hub/spoke, webhook manifests |
| `docs/phase10/api-graduation.md` | Per-CRD field audit: user fields, status, experimental, deprecated |
| `docs/phase10/adr/0002-core-crds-to-v1beta1.md` | ADR for 3-CRD graduation (supersedes ADR-0001) |
| `docs/phase10/adr/0003-conversion-and-storage-strategy.md` | ADR for conversion webhook and storage version strategy |
| `docs/phase10/crd-versioning-and-migration.md` | Upgrade order, migration guidance, rollback expectations |
| `docs/phase10/adr/0004-prod-install-ha-and-tls.md` | ADR: Helm, cert-manager, HA, security strategy |
| `docs/phase10/production-install.md` | Production install guide (Helm + Kustomize) |
| `test/integration/prod_install_test.go` | 13 tests: chart structure, values, overlays, security, cert-manager, PDB, NetworkPolicy |

## 5. Files Changed (Previous Sessions)

| File | Change |
|------|--------|
| `config/manager/manager.yaml` | Added: pod securityContext (nonroot, seccomp), container securityContext (drop caps, read-only FS, no priv escalation), webhook cert volume mount, improved probes |
| `cmd/operator/main.go` | Import v1beta1; register in scheme; wire conversion webhook; `--dev-mode` flag |
| `config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml` | conversion.strategy=Webhook; v1beta1 storage=true; v1alpha1 storage=false |
| `config/crd/bases/training.checkpoint.example.io_checkpointprioritypolicies.yaml` | conversion.strategy=Webhook; v1beta1 storage=true; v1alpha1 storage=false |
| `config/crd/bases/training.checkpoint.example.io_resumereadinesspolicies.yaml` | conversion.strategy=Webhook; v1beta1 storage=true; v1alpha1 storage=false |
| `config/crd/kustomization.yaml` | Added CPP and RRP CRD bases to resources |
| `config/webhook/manifests.yaml` | Added CPP+RRP webhooks; expanded apiVersions to include v1beta1 |

## 6. Tests Run (This Session)

| Suite | Tests | Result |
|-------|-------|--------|
| `test/integration/...` (all) | 45 | All PASS |
| `go vet ./cmd/operator/ ./api/...` | — | Clean |

## 7. What Was NOT Done (Deferred)

| Item | Reason | Next Step |
|------|--------|-----------|
| StorageVersionMigration job | Requires running cluster | Run after deploying updated operator |
| storedVersions cleanup | Requires migration completion | Patch CRD status after migration |
| Controller migration to v1beta1 types | Controllers still use v1alpha1 internally | Separate prompt; conversion handles runtime translation |
| v1alpha1 deprecation header | Needs API server integration | Add in future phase |
| Helm template rendering test (helm template) | Requires helm CLI in CI | Add to CI pipeline |
| ServiceMonitor / PrometheusRule | Out of scope for this prompt | Monitoring/alerting workstream |
| Metrics endpoint TLS | cert-manager Certificate wired but --metrics-secure-serving not yet in main.go | Wire when controller-runtime supports it |
| Observability SLIs/SLOs/alerts | Separate workstream | Phase 10 observability prompt |
| Chaos / soak validation | Requires running cluster | Phase 10 validation prompt |
| VAP live-cluster validation | Policies are structurally tested; runtime enforcement needs a cluster | Phase 10 validation prompt |
| Helm chart VAP templates | Policies are Kustomize-managed; Helm templating is optional | Add if teams request it |

## 8. Open Issues

| ID | Summary | Priority | Status |
|----|---------|----------|--------|
| OQ-10.01 | Conversion webhook deployment strategy | P1 | **Resolved**: same pod, `/convert` path |
| OQ-10.02 | v1alpha1 deprecation timeline | P2 | Open |
| OQ-10.03 | Helm vs Kustomize as primary | P2 | **Resolved**: Helm primary, Kustomize for composition (ADR-0004) |
| OQ-10.04 | Metrics TLS default | P2 | **Partially resolved**: cert-manager Certificate wired; --metrics-secure-serving pending |
| OQ-10.05 | DR automation level | P2 | Open |
| OQ-10.06 | Phase 9 deferred metrics wiring | P1 | Open |
| OQ-10.07 | Soak test infrastructure | P3 | Open |
| OQ-10.08 | Feature gate persistence | P3 | Open |
| OQ-10.09 | StorageVersionMigration automation | P1 | Open (post-deploy) |
| OQ-10.10 | Non-default operator namespace in VAP | P3 | **Documented**: matchConditions must be customized; see tenancy-and-admission.md |

## 9. Tenancy and Admission Guardrails Summary

### Enforcement Model
- **Opt-in**: label `rtj.checkpoint.example.io/managed: "true"` activates guardrails per namespace
- **Unmanaged namespaces**: completely unaffected (preserves Phase 0-9 behavior)

### ValidatingAdmissionPolicies (`deploy/prod/policies/`)
| Policy | Guards Against | Exemptions |
|--------|---------------|------------|
| `rtj-require-queue-assignment` | RTJs without `spec.queueName` | None |
| `rtj-deny-direct-jobset` | Direct JobSet writes | `rtj-system` SAs |
| `rtj-deny-direct-workload` | Direct Workload creation | `rtj-system` and `kueue-system` SAs |

### User-Facing RBAC
| Role | Scope | Aggregates To |
|------|-------|--------------|
| `rtj-editor` | Full RTJ CRUD + read policies/workloads | `edit` |
| `rtj-viewer` | Read-only on all RTJ resources | `view` |

### Controller RBAC Minimization
- Removed `create` on `resumabletrainingjobs` (controller never creates RTJs)
- Removed `update` on `events` (events are created/patched, not updated)

### Kustomize Overlay
- `deploy/prod/overlays/tenancy/`: composes base + all policies + user roles + managed NS

## 10. Recommended Next Prompt

```
You are working on Phase 10 only for the checkpoint-native preemption controller repo.

Mission: Deploy and validate the storage version migration, then wire observability.

Context:
- Conversion webhook is wired and tested
- v1beta1 is the storage version for all 3 CRDs
- Helm chart and production Kustomize overlays are complete
- cert-manager TLS is wired for webhooks
- Tenancy and admission guardrails (VAP) are complete
- See docs/phase10/session-handoff.md

Tasks:
1. Create a StorageVersionMigration manifest for each CRD
2. Add a Makefile target for running the migration
3. Add a post-migration verification script
4. Wire Phase 9 deferred metrics
5. Add ServiceMonitor and PrometheusRule to Helm chart
6. Add Helm template rendering test to CI (helm template + kubeconform)
7. Update docs/phase10/session-handoff.md

Hard boundaries:
- Do NOT modify controller reconciliation logic
- Do NOT change field names, types, or semantics
- Preserve all Phase 0-9 invariants
- Keep v1alpha1 as a served version
```
