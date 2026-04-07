# CRD Versioning and Storage Migration Guide

Phase 10 graduates the three core CRDs (ResumableTrainingJob, CheckpointPriorityPolicy, ResumeReadinessPolicy) from v1alpha1 to v1beta1. This document covers the upgrade path, storage migration, and rollback expectations.

## Version Layout

| CRD | v1alpha1 | v1beta1 |
|-----|----------|---------|
| ResumableTrainingJob | served, not storage | served, **storage** (hub) |
| CheckpointPriorityPolicy | served, not storage | served, **storage** (hub) |
| ResumeReadinessPolicy | served, not storage | served, **storage** (hub) |

**Conversion strategy**: Webhook (`/convert` on the operator webhook server, port 9443).

## Conversion Semantics

v1alpha1 and v1beta1 schemas are structurally identical at graduation time. The conversion uses JSON-roundtrip between identically-tagged Go structs, guaranteeing lossless bidirectional conversion.

- **No fields are dropped** during conversion in either direction.
- **No fields are reinterpreted** — all enum values, duration formats, and nested structures are preserved byte-for-byte through JSON.
- **Future divergence** between versions will be handled by explicit per-field logic in the ConvertTo/ConvertFrom methods with annotation-based preservation of version-specific data.

## Upgrade Order

Perform these steps in order:

### 1. Install updated CRDs

```bash
kubectl apply -f config/crd/bases/
```

This:
- Adds v1beta1 as a served version with `storage: true`.
- Changes v1alpha1 to `storage: false` (still served).
- Sets `conversion.strategy: Webhook` pointing at `/convert`.

**Critical**: The operator must be running and serving the conversion webhook before clients can read objects. Deploy the operator immediately after CRD update, or use a rolling update that deploys both together.

### 2. Deploy the updated operator

The updated operator binary:
- Registers both v1alpha1 and v1beta1 in the runtime scheme.
- Serves the conversion webhook at `/convert`.
- Continues to serve v1alpha1 mutating/validating webhooks (which also cover v1beta1 via `matchPolicy: Equivalent`).

### 3. Run the storage version migration

After the CRDs report v1beta1 as the storage version and the operator is healthy, migrate existing objects so they are re-written in the new storage format:

```bash
# Option A: Use the StorageVersionMigrator (recommended for production)
# Install: https://github.com/kubernetes-sigs/kube-storage-version-migrator
# It will automatically re-PUT all existing objects, triggering conversion to v1beta1.

# Option B: Manual migration for small clusters
kubectl get rtj --all-namespaces -o json | kubectl replace -f -
kubectl get cpp -o json | kubectl replace -f -
kubectl get rrp -o json | kubectl replace -f -
```

### 4. Verify storedVersions

After migration completes, confirm that the CRD status reflects only v1beta1 in `storedVersions`:

```bash
kubectl get crd resumabletrainingjobs.training.checkpoint.example.io \
  -o jsonpath='{.status.storedVersions}'
# Expected: ["v1beta1"]

kubectl get crd checkpointprioritypolicies.training.checkpoint.example.io \
  -o jsonpath='{.status.storedVersions}'
# Expected: ["v1beta1"]

kubectl get crd resumereadinesspolicies.training.checkpoint.example.io \
  -o jsonpath='{.status.storedVersions}'
# Expected: ["v1beta1"]
```

If storedVersions still shows `["v1alpha1","v1beta1"]`, some objects have not been migrated. Re-run the migration.

### 5. Clean up storedVersions (optional)

Once all objects are confirmed migrated, you can patch the CRD to remove v1alpha1 from storedVersions. This is optional but signals that no v1alpha1-encoded objects remain in etcd:

```bash
kubectl patch crd resumabletrainingjobs.training.checkpoint.example.io \
  --type=json -p='[{"op":"replace","path":"/status/storedVersions","value":["v1beta1"]}]' \
  --subresource=status
```

Repeat for the other two CRDs.

## Rollback Expectations and Limits

### Safe rollback window

You can safely roll back to the pre-Phase-10 operator (v1alpha1-only) **as long as**:

1. The CRDs are reverted to v1alpha1 as the storage version.
2. All objects have been re-migrated to v1alpha1 storage format.
3. The conversion webhook is still available during the reverse migration.

### Rollback procedure

```bash
# 1. Revert CRDs to v1alpha1 storage
kubectl apply -f <pre-phase10-crds>

# 2. Re-migrate objects to v1alpha1 format
kubectl get rtj --all-namespaces -o json | kubectl replace -f -
kubectl get cpp -o json | kubectl replace -f -
kubectl get rrp -o json | kubectl replace -f -

# 3. Deploy the old operator
```

### Limits

- **After storedVersions cleanup**: If you removed v1alpha1 from storedVersions and then need to roll back, you must re-add it before reverting the CRD.
- **Data loss risk**: None during rollback because v1alpha1 and v1beta1 are schema-identical. No fields exist in only one version.
- **Future versions**: Once v1beta1 adds fields not present in v1alpha1, downgrade conversion will need explicit handling (annotation preservation or field truncation). This is not yet the case.

## Webhook Configuration

The conversion webhook is shared across all three CRDs:

| Path | Purpose | Port |
|------|---------|------|
| `/convert` | CRD version conversion (v1alpha1 <-> v1beta1) | 9443 |
| `/mutate-training-*-v1alpha1-*` | Defaulting (covers both versions via matchPolicy) | 9443 |
| `/validate-training-*-v1alpha1-*` | Validation (covers both versions via matchPolicy) | 9443 |

TLS certificates for the webhook server are managed by cert-manager (or the cluster's webhook CA injection mechanism).

## Testing

Round-trip conversion tests verify:
- v1alpha1 -> v1beta1 -> v1alpha1 preserves all spec and status fields.
- v1beta1 -> v1alpha1 -> v1beta1 preserves all fields.
- Minimal (zero-value) objects survive conversion (old stored objects).
- Wrong-type conversions are rejected.
- CRD manifests have correct version/storage/conversion config.
- Webhook manifests cover both versions for all three resources.
- Both versions register correctly in the runtime scheme.

See `test/integration/conversion_roundtrip_test.go` and `test/integration/crd_versioning_test.go`.
