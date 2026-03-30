# Phase 6: Shared Checkpoint Store Contract

## Overview

In MultiKueue mode, all clusters (manager and workers) must access the same
S3-compatible checkpoint store so that checkpoints written by one worker are
readable by another on cross-worker resume. This document defines the shared
store contract and the validation added in Phase 6.

## Contract

1. **One store, all clusters.** Every worker cluster and the manager cluster
   (if it needs checkpoint metadata for status) must be able to reach the same
   S3-compatible endpoint using the same bucket paths.

2. **No replication service in Phase 6.** Cross-cluster checkpoint access is
   achieved by pointing all clusters at a shared endpoint, not by replicating
   objects between per-cluster stores.

3. **Checkpoint semantics unchanged.** Manifest-last publication, completeness
   validation, latest-compatible-complete selection, and fail-closed resume
   compatibility all apply identically to Phases 1-5.

4. **Credential distribution is an operational concern.** Each cluster needs
   valid S3 credentials for the shared store. Phase 6 does not prescribe a
   specific secrets-management approach (Vault, ExternalSecrets, etc.).

## What Changed

### Previous (Phases 1-5)

- `NewStoreFromEnv()` reads `AWS_ENDPOINT_URL` / `YIELD_SDK_S3_ENDPOINT` /
  `S3_ENDPOINT` and credential environment variables.
- No validation that the endpoint is reachable from other clusters.
- Works correctly in single-cluster mode. In practice, cluster-local MinIO
  endpoints (e.g., `http://minio.minio-system.svc.cluster.local:9000`) were
  common in dev environments.

### Phase 6 Additions

| Addition | Purpose |
|---|---|
| `SharedStoreConfig` struct | Explicit configuration for shared store (endpoint, region, credentials) |
| `NewStoreFromConfig(cfg)` | Constructor from explicit config (preferred for MultiKueue) |
| `ValidateSharedEndpoint(endpoint)` | Rejects cluster-local endpoints (`.svc.cluster.local`, `.svc`, `localhost`, loopback) |
| `IsSharedStoreConfigured()` | Pre-flight check: env-var endpoint passes shared validation |

### Migration Path

For existing single-cluster deployments:
- No changes required. `NewStoreFromEnv()` continues to work.
- Cluster-local endpoints remain valid for single-cluster mode.

For MultiKueue deployments:
1. Replace cluster-local MinIO with a shared endpoint (e.g., external MinIO,
   AWS S3, GCS with S3 compatibility).
2. Set the same `AWS_ENDPOINT_URL` / credentials on all clusters.
3. Optionally use `NewStoreFromConfig` with `ValidateSharedEndpoint` for
   pre-flight validation.

### Endpoint Validation

`ValidateSharedEndpoint` rejects:

| Pattern | Example | Reason |
|---|---|---|
| `.svc.cluster.local` | `http://minio.ns.svc.cluster.local:9000` | Kubernetes-internal DNS |
| `.cluster.local` | `http://minio.cluster.local` | Kubernetes-internal DNS |
| `.svc` | `https://store.prod.svc` | Kubernetes-internal DNS |
| `localhost` | `http://localhost:9000` | Loopback |
| `127.0.0.1` | `http://127.0.0.1:9000` | Loopback |
| `0.0.0.0` | `http://0.0.0.0:9000` | Loopback |

This is a best-effort check. Operators are responsible for ensuring actual
network connectivity from all clusters.

## Files

| File | Purpose |
|---|---|
| `internal/checkpoints/store.go` | `SharedStoreConfig`, `NewStoreFromConfig`, `ValidateSharedEndpoint`, `IsSharedStoreConfigured` |
| `internal/checkpoints/store_test.go` | 10 tests: endpoint validation, config validation, S3 URI parsing |

## Design Decisions

1. **No new CRD for store configuration.** The shared store is configured via
   environment variables (existing path) or explicit `SharedStoreConfig`
   (new path). A CRD would add complexity without clear benefit for Phase 6.

2. **Validation is advisory, not blocking.** `ValidateSharedEndpoint` warns
   about likely-broken configurations but does not prevent the operator from
   starting. This avoids breaking single-cluster deployments that use
   cluster-local endpoints.

3. **Manager does not perform checkpoint I/O.** The manager reflects checkpoint
   metadata from the worker's mirrored status. Only workers need actual S3
   access. This preserves ADR-0001 D5 (manager does not do checkpoint I/O).
