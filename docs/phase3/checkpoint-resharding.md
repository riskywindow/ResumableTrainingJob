# Phase 3: Checkpoint Resharding

## Overview

Phase 3 extends the checkpoint manifest and restore path to support resuming
a training job at a different worker count than the checkpoint was saved at.
This enables two key scenarios:

1. **Partial admission**: Kueue admits fewer workers than requested, and the
   job resumes from a checkpoint saved at the original (larger) world size.
2. **Scale-on-resume**: A job is intentionally rescheduled with more or fewer
   workers to adapt to cluster capacity.

All resharding is performed by PyTorch DCP (Distributed Checkpoint), which
natively supports loading checkpoints saved with a different number of ranks.

## Manifest Extensions

The checkpoint manifest gains four new optional fields:

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `leaderCount` | int | None | Number of leader/launcher pods (typically 0) |
| `workerCount` | int | None | Number of worker pods that participated in checkpoint |
| `checkpointFormatVersion` | string | None | Checkpoint data format (e.g., `"dcp/v1"`) |
| `crossSizeRestoreSupported` | bool | None | Whether this checkpoint supports cross-size restore |

### Backward Compatibility

- All new fields are optional. Phase 2 manifests decode with `None` defaults.
- The existing `worldSize` field remains the canonical world size for
  compatibility checking. The new `workerCount` provides finer granularity
  for multi-role setups (leader + workers).
- `crossSizeRestoreSupported=None` (Phase 2 manifest) is treated as `False`
  during cross-size restore validation. This means Phase 2 checkpoints cannot
  be restored at a different world size even with `allowWorldSizeChange=True`.

## Runtime Configuration

Two new environment variables control the reshape behavior:

| Variable | Type | Description |
| --- | --- | --- |
| `YIELD_SDK_ORIGINAL_WORLD_SIZE` | int | World size from the checkpoint being restored |
| `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE` | bool | Whether cross-size restore is permitted |

These map to `RuntimeConfig` fields:

```python
@dataclass(frozen=True)
class RuntimeConfig:
    # ... existing fields ...
    original_world_size: int | None = None
    allow_world_size_change: bool = False
```

The controller sets these when `spec.resume.allowWorldSizeChange=true` and the
admitted world size differs from the checkpoint world size.

## Restore Path

### Same-Size Restore (Phase 2 behavior)

When `manifest.world_size == runtime.world_size`:

1. All compatibility dimensions are checked (strict match).
2. DCP loads the checkpoint directly.
3. RNG state is restored.
4. `restore_mode = "SameSize"`.

### Cross-Size Restore (Phase 3)

When `manifest.world_size != runtime.world_size`:

1. All compatibility dimensions except `worldSize` are checked.
2. `runtime.allow_world_size_change` must be `True`.
3. `manifest.cross_size_restore_supported` must be `True`.
4. DCP loads the checkpoint with its native resharding path.
5. RNG state is **not** restored (it is world-size-specific).
6. `restore_mode = "Reshard"`.

### Rejection Cases

Cross-size restore is rejected when:

- `allow_world_size_change=False` (default): world size mismatch is treated
  as an incompatibility like any other dimension.
- `cross_size_restore_supported` is `False` or `None`: the checkpoint does
  not declare resharding support (e.g., Phase 2 manifest or non-DCP format).
- Any other compatibility dimension mismatches (cluster, RTJ lineage, runtime
  mode, GPU shape, image, code version, optimizer mode, sharding mode, format
  version).

## DCP Resharding Details

PyTorch DCP's `load()` function handles resharding transparently:

- **Fewer ranks → more ranks**: Each new rank reads the subset of tensor
  shards it needs from the stored checkpoint files. DCP's metadata tracks
  the original sharding plan.
- **More ranks → fewer ranks**: Each rank reads and combines multiple
  shard fragments.
- **Same ranks**: Direct load (no resharding).

The SDK does not need special DCP options for resharding. The standard
`dcp.load()` call works for all cases because DCP stores sufficient metadata
to reconstruct any sharding.

### RNG State

RNG state is world-size-specific (one state per rank). When resharding:

- The saved RNG states have a different count than the current ranks.
- Restoring mismatched RNG states would produce incorrect results.
- Therefore, RNG state is **skipped** during cross-size restore.
- Training continues with freshly initialized RNG state.

This means cross-size restore does not produce bit-identical training
trajectories compared to same-size restore. This is expected and acceptable
for the resharding use case.

## Observability

### Logs

The restore path logs the restore mode:

```
restoring checkpoint ckpt-xxx: manifest_world_size=8 runtime_world_size=4 restore_mode=Reshard
```

### Restore Result

`CheckpointRestoreResult` includes the `restore_mode` field:

```python
@dataclass
class CheckpointRestoreResult:
    manifest: CheckpointManifest
    restore_dir: Path
    step: int
    trainer_state: dict[str, Any]
    restore_mode: str  # "SameSize" or "Reshard"
```

### RTJ Status (Controller Side)

The controller surfaces the restore mode in `status.restore`:

```yaml
status:
  restore:
    lastCheckpointWorldSize: 8
    lastRestoreWorldSize: 4
    restoreMode: Reshard
```

## Test Coverage

Unit tests cover:

- **Same-size restore**: verify `restore_mode="SameSize"` and step continuity.
- **Different-size restore**: verify `restore_mode="Reshard"` when
  `allow_world_size_change=True` and `cross_size_restore_supported=True`.
- **Incompatible rejection**: verify error when world size differs without
  `allow_world_size_change`.
- **Manifest without cross-size support**: verify error when manifest has
  `cross_size_restore_supported=False` or `None` (Phase 2 backcompat).
- **Manifest completeness**: verify all Phase 3 fields round-trip through
  JSON serialization.
- **Phase 2 backward compatibility**: verify Phase 2 manifests decode with
  `None` defaults for new fields.
