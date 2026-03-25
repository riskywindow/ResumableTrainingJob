# Phase 1 Trainer Fixture

## Purpose

The `fixtures/pytorch_ddp_counter` workload is the first concrete runtime for Phase 1.
It is intentionally a toy trainer, but it still implements the Phase 0 and Phase 1 runtime responsibilities:

- run under `torch.distributed`
- use CPU plus `gloo` by default
- optionally use `NCCL` when CUDA is available
- checkpoint model, optimizer, step, RNG, and minimal trainer state with PyTorch DCP
- stage checkpoint data to local filesystem first
- upload artifacts to S3-compatible object storage
- publish the manifest last
- restore from a manifest URI and continue stepping

## Phase 1 Runtime Choice

Phase 0 deferred the concrete operator-to-runtime transport.
For this Phase 1 slice, the trainer uses the smallest working transport:
a mounted JSON control file that is polled at step boundaries.

That choice preserves the accepted semantics:

- the operator remains the source of pause intent
- the runtime does not self-authorize yield
- yield is observed only at step boundaries
- the file can carry stable request identity such as `requestId`

The Phase 1 control file shape is:

```json
{
  "desiredState": "Running | Paused",
  "requestId": "optional-stable-request-id",
  "updatedAt": "optional-rfc3339-timestamp"
}
```

The SDK accepts either `desiredState` or `desired_state` for convenience, but the documented Phase 1 shape is camelCase.

## Checkpoint Layout

The trainer treats `spec.checkpoint.storageURI` as the stable per-lineage root and writes this logical layout:

```text
s3://<bucket>/<lineage-root>/
  yield-markers/
    run-<attempt>.json
  manifests/
    <checkpoint-id>.manifest.json
  checkpoints/
    <checkpoint-id>/
      metadata/
        runtime.json
      data/
        <dcp-artifacts>
```

The trainer uploads artifacts first, the manifest last, and the yield marker only after the completed manifest exists.
The manifest remains the completion boundary for the checkpoint itself.

## Shared-Filesystem Assumption

Phase 1 keeps the default fixture single-node and single-container friendly.
The intended path is `torchrun --nproc-per-node=<world-size>` inside one container so all local ranks share:

- the same local staging directory
- the same local restore directory
- the same mounted control file

That keeps the first vertical slice CPU-first and `kind` friendly.
It also means the fixture does not yet solve multi-Pod shared-filesystem coordination.
That is acceptable for Phase 1 because the operator is not yet wired to the runtime.

## Environment Contract

The trainer and SDK consume these main variables:

| Variable | Meaning |
| --- | --- |
| `YIELD_SDK_S3_ENDPOINT` | S3-compatible endpoint such as MinIO. |
| `YIELD_SDK_S3_ACCESS_KEY` | Access key for object storage. |
| `YIELD_SDK_S3_SECRET_KEY` | Secret key for object storage. |
| `YIELD_SDK_S3_SESSION_TOKEN` | Optional session token. |
| `YIELD_SDK_S3_REGION` | Optional region. |
| `YIELD_SDK_S3_SECURE` | `true` or `false`; defaults to `false` for local MinIO. |
| `YIELD_SDK_STORAGE_URI` | Stable lineage root such as `s3://phase1-checkpoints/demo-rtj`. |
| `YIELD_SDK_CLUSTER_IDENTITY` | Cluster identity recorded in manifests. |
| `YIELD_SDK_RTJ_IDENTITY` | RTJ lineage identity recorded in manifests. |
| `YIELD_SDK_RUN_ATTEMPT` | Current runtime attempt number. |
| `YIELD_SDK_RUNTIME_MODE` | `DDP` or `FSDP`; Phase 1 fixture uses `DDP`. |
| `YIELD_SDK_WORLD_SIZE` | Declared world size recorded in the manifest. |
| `YIELD_SDK_GPU_SHAPE` | `cpu` by default in the local path. |
| `YIELD_SDK_IMAGE_IDENTITY` | Training image identity recorded in the manifest. |
| `YIELD_SDK_CODE_VERSION` | Declared code version recorded in the manifest. |
| `YIELD_SDK_OPTIMIZER_MODE` | Optimizer-mode compatibility field. |
| `YIELD_SDK_SHARDING_MODE` | Sharding-mode compatibility field. |
| `YIELD_SDK_CONTROL_FILE` | Mounted control-file path. |
| `YIELD_SDK_RESTORE_MANIFEST_URI` | Manifest URI used when starting a resumed attempt. |
| `YIELD_SDK_STAGING_ROOT` | Local staging root. |
| `YIELD_SDK_RESTORE_ROOT` | Local restore root. |
| `YIELD_SDK_YIELD_MARKER_PATH` | Local marker file written after a clean manual yield. |
| `YIELD_SDK_YIELD_MARKER_URI` | Object-store marker URI observed by the operator during the pause drain. |

## Yield Marker

On a successful manual yield, rank 0 writes a local JSON marker file and uploads the same payload to object storage.
The payload contains:

- `checkpointID`
- `manifestURI`
- `globalStep`
- `requestID`
- `completionTimestamp`

This is a thin Phase 1 bridge for the future operator integration and for smoke-test assertions.

## Current Limits

- The trainer is correctness-first, not performance-first.
- Checkpointing is synchronous.
- Rank 0 performs object-store upload and download.
- The fixture does not yet implement multi-Pod staging or restore.
- The operator is not yet wired to render or reconcile the child `JobSet` against this runtime.
