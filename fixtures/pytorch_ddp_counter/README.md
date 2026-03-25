# PyTorch DDP Counter Fixture

This fixture is the toy trainer for Phase 1-3.
It is intentionally small and deterministic enough for local `kind` smoke coverage while still exercising the real runtime contract:

- `torch.distributed` execution
- CPU with `gloo` by default
- optional `NCCL` when CUDA is present
- PyTorch DCP checkpoint and restore
- local filesystem staging
- S3-compatible upload
- manifest-last completion
- manual yield driven by a mounted control file
- **Phase 3:** world-size-flexible resume via DCP resharding

## Current Shape

The fixture is single-node and shared-filesystem friendly.
The intended default path is one container started with `torchrun --nproc-per-node=<world-size>`, so every local rank can share the same staging and restore directories inside the container.

That keeps the first slice correct on CPU in `kind` without introducing a distributed shared volume dependency yet.

## Control File

The trainer polls a mounted JSON control file at step boundaries.
The smallest working shape is:

```json
{
  "desiredState": "Paused",
  "requestId": "pause-001"
}
```

When `desiredState` becomes `Paused`, the trainer:

1. waits for the current step boundary,
2. barriers all local ranks,
3. writes a DCP checkpoint to local staging,
4. uploads artifacts to S3-compatible storage,
5. writes the manifest last,
6. writes a local yield-complete marker on rank 0,
7. exits cleanly.

## Phase 3: World-Size-Flexible Resume

When `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE=true` and the checkpoint was saved at a
different world size, DCP handles the resharding automatically. The restore mode
(`SameSize` or `Reshard`) is logged in the `trainer_start` event.

The manifest records `crossSizeRestoreSupported: true` for all DCP checkpoints,
indicating they can be restored at a different world size.

## Required Environment

The fixture reads these main environment variables:

- `YIELD_SDK_S3_ENDPOINT`
- `YIELD_SDK_S3_ACCESS_KEY`
- `YIELD_SDK_S3_SECRET_KEY`
- `YIELD_SDK_STORAGE_URI`
- `YIELD_SDK_RTJ_IDENTITY`
- `YIELD_SDK_CLUSTER_IDENTITY`
- `YIELD_SDK_RUN_ATTEMPT`
- `YIELD_SDK_CONTROL_FILE`
- `YIELD_SDK_RESTORE_MANIFEST_URI` for resume
- `YIELD_SDK_YIELD_MARKER_PATH`
- `YIELD_SDK_WORLD_SIZE` (current world size)
- `YIELD_SDK_ORIGINAL_WORLD_SIZE` (checkpoint world size, Phase 3)
- `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE` (`true` to enable resharding, Phase 3)

## Local Example

```bash
export PYTHONPATH=$PWD/sdk/python
export YIELD_SDK_S3_ENDPOINT=127.0.0.1:9000
export YIELD_SDK_S3_ACCESS_KEY=minioadmin
export YIELD_SDK_S3_SECRET_KEY=minioadmin
export YIELD_SDK_STORAGE_URI=s3://phase1-checkpoints/demo-rtj
export YIELD_SDK_RTJ_IDENTITY=demo-rtj
export YIELD_SDK_CLUSTER_IDENTITY=kind-phase1
export YIELD_SDK_RUN_ATTEMPT=1
export YIELD_SDK_CONTROL_FILE=$PWD/.tmp/control.json
export YIELD_SDK_YIELD_MARKER_PATH=$PWD/.tmp/yield-complete.json

mkdir -p .tmp
printf '{"desiredState":"Running"}\n' > .tmp/control.json
torchrun --standalone --nproc-per-node=2 fixtures/pytorch_ddp_counter/train.py --max-steps 12 --sleep-per-step 0.5
```

Patch the control file to `Paused` while the trainer is running to trigger a clean yield.
