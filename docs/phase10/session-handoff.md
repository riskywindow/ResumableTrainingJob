# Phase 10 Session Handoff

**Last updated:** 2026-04-07
**Phase:** 10 - Production Hardening & API Beta
**Status:** Conversion webhook wired; v1beta1 is storage version; all tests pass

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

## 2. Files Created

| File | Purpose |
|------|---------|
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

## 3. Files Changed

| File | Change |
|------|--------|
| `cmd/operator/main.go` | Import v1beta1; register in scheme; wire conversion webhook at `/convert` |
| `config/crd/bases/training.checkpoint.example.io_resumabletrainingjobs.yaml` | conversion.strategy=Webhook; v1beta1 storage=true; v1alpha1 storage=false |
| `config/crd/bases/training.checkpoint.example.io_checkpointprioritypolicies.yaml` | conversion.strategy=Webhook; v1beta1 storage=true; v1alpha1 storage=false |
| `config/crd/bases/training.checkpoint.example.io_resumereadinesspolicies.yaml` | conversion.strategy=Webhook; v1beta1 storage=true; v1alpha1 storage=false |
| `config/crd/kustomization.yaml` | Added CPP and RRP CRD bases to resources |
| `config/webhook/manifests.yaml` | Added CPP+RRP webhooks; expanded apiVersions to include v1beta1 |

## 4. Tests Run

| Suite | Tests | Result |
|-------|-------|--------|
| `test/integration/...` | 12 | All PASS |
| `api/v1beta1/...` | 23 | All PASS |
| `api/v1alpha1/...` | All existing | All PASS (no regressions) |
| Full project (`go test ./...`) | All | All PASS |
| Full build (`go build ./...`) | — | Clean |

## 5. What Was NOT Done (Deferred)

| Item | Reason | Next Step |
|------|--------|-----------|
| StorageVersionMigration job | Requires running cluster | Run after deploying updated operator |
| storedVersions cleanup | Requires migration completion | Patch CRD status after migration |
| Controller migration to v1beta1 types | Controllers still use v1alpha1 internally | Separate prompt; conversion handles runtime translation |
| v1alpha1 deprecation header | Needs API server integration | Add in future phase |
| Helm chart update | Out of scope for this prompt | Add conversion webhook and cert-manager config to chart |
| HA deployment profile | Not blocking conversion | Separate Phase 10 workstream |

## 6. Open Issues

| ID | Summary | Priority | Status |
|----|---------|----------|--------|
| OQ-10.01 | Conversion webhook deployment strategy | P1 | **Resolved**: same pod, `/convert` path |
| OQ-10.02 | v1alpha1 deprecation timeline | P2 | Open |
| OQ-10.03 | Helm vs Kustomize as primary | P2 | Open |
| OQ-10.04 | Metrics TLS default | P2 | Open |
| OQ-10.05 | DR automation level | P2 | Open |
| OQ-10.06 | Phase 9 deferred metrics wiring | P1 | Open |
| OQ-10.07 | Soak test infrastructure | P3 | Open |
| OQ-10.08 | Feature gate persistence | P3 | Open |
| OQ-10.09 | StorageVersionMigration automation | P1 | Open (post-deploy) |

## 7. Recommended Next Prompt

```
You are working on Phase 10 only for the checkpoint-native preemption controller repo.

Mission: Deploy and validate the storage version migration.

Context:
- Conversion webhook is wired and tested
- v1beta1 is the storage version for all 3 CRDs
- v1alpha1 remains served for backward compatibility
- See docs/phase10/session-handoff.md and docs/phase10/crd-versioning-and-migration.md

Tasks:
1. Create a StorageVersionMigration manifest for each CRD
2. Add a Makefile target for running the migration
3. Add a post-migration verification script
4. Begin HA deployment profile (2 replicas, leader election)
5. Wire Phase 9 deferred metrics
6. Update docs/phase10/session-handoff.md

Hard boundaries:
- Do NOT modify controller reconciliation logic
- Do NOT change field names, types, or semantics
- Preserve all Phase 0-9 invariants
- Keep v1alpha1 as a served version
```
