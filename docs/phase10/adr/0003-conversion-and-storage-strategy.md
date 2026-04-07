# ADR-0003: Conversion Webhook and Storage Version Strategy

- **Status**: Accepted
- **Date**: 2026-04-07
- **Supersedes**: ADR-0002 (extends; ADR-0002 deferred the conversion webhook)

## Context

ADR-0002 graduated all three core CRDs (RTJ, CPP, RRP) to v1beta1 with structurally identical schemas and `conversion.strategy: None`. That was sufficient for the initial type scaffolding but does not support switching the storage version, because `strategy: None` only works when both versions share a single schema definition in the CRD (or are truly identical in a way the API server can handle without conversion logic).

To make v1beta1 the storage version and ensure the API server can serve v1alpha1 requests against v1beta1-stored objects, we need an explicit conversion webhook.

## Decision

### Hub/Spoke topology

- **v1beta1 is the hub** (storage version). All three CRD types in `api/v1beta1/` implement `conversion.Hub`.
- **v1alpha1 is the spoke**. All three CRD types in `api/v1alpha1/` implement `conversion.Convertible` with `ConvertTo` and `ConvertFrom` methods.

### Conversion mechanism

JSON-roundtrip between identically-tagged Go structs. This approach:

1. Is **lossless** for structurally identical schemas.
2. Is **zero-maintenance** when both versions evolve in lockstep (add a field to both packages, it flows automatically).
3. Has **acceptable overhead** — conversion webhooks are not in the request hot-path (only invoked when the requested version differs from the stored version).
4. Is **future-safe** — when schemas diverge, the JSON roundtrip can be replaced with explicit per-field logic without changing the interface contracts.

### Storage version

v1beta1 is the storage version for all three CRDs. v1alpha1 remains served for backward compatibility but is no longer stored.

### Webhook registration

A single conversion webhook handler is registered at `/convert` on the operator's webhook server (port 9443). The handler is created by `controller-runtime/pkg/webhook/conversion.NewWebhookHandler`, which discovers Hub and Convertible types from the runtime scheme.

All three CRDs share the same webhook endpoint. The handler dispatches by GVK.

### Defaulting and validation

Existing v1alpha1 mutating/validating webhooks continue to serve both versions. The webhook configurations use `matchPolicy: Equivalent`, so the API server sends v1alpha1-shaped objects to the v1alpha1 webhook handlers regardless of which version the client used. This avoids duplicating webhook logic.

## Alternatives Considered

### 1. Keep v1alpha1 as storage, add v1beta1 as served-only

Rejected. This inverts the graduation signal — v1beta1 should be the preferred, stable version. Keeping v1alpha1 as storage would require all new features to maintain v1alpha1 compatibility indefinitely.

### 2. Drop v1alpha1 immediately

Rejected. Violates the hard boundary of preserving v1alpha1 during Phase 10. Existing automation, scripts, and tooling may reference v1alpha1 resources.

### 3. Manual field-by-field conversion

Rejected for now. With 30+ spec fields, 26+ status fields, and deeply nested sub-types, manual conversion would be error-prone and a maintenance burden. JSON roundtrip is equivalent in behavior and immune to field additions.

### 4. Separate conversion webhook binary

Rejected. The conversion webhook shares the same TLS certificates and scheme as the operator. Running it in a separate process would add operational complexity with no benefit.

## Consequences

### Positive

- v1beta1 is the storage version, signaling graduation intent.
- v1alpha1 clients continue to work without changes.
- Zero-maintenance conversion for the current schema-identical state.
- Clear upgrade path documented in `crd-versioning-and-migration.md`.

### Negative

- The operator must be running for any API requests to succeed (conversion webhook is required). This is already true due to the existing mutating/validating webhooks.
- JSON roundtrip adds ~100µs per conversion. Acceptable for the webhook path.

### Risks

- If schemas diverge in a future phase, the JSON roundtrip must be replaced with explicit conversion logic. The `ConvertTo`/`ConvertFrom` interface makes this a localized change.
- If the webhook is unavailable, all CRD operations fail. Mitigated by HA operator deployment (Phase 10 scope).
