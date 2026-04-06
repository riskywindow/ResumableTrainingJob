# Phase 9 — Runtime-Side Elasticity Protocol

## Overview

This document describes the runtime-side (SDK + fixture) elasticity protocol
added in Phase 9.  The protocol enables manual target-based worker-count resize
through a narrow, deterministic control interface between the Kubernetes
controller and the training runtime.

The controller-side reclaim logic (reclaimablePods lifecycle, Workload PodSet
mutation, suspend/mutate/re-admit cycle) is **not** covered here — it will be
implemented in a separate session.

## Design Principles

1. **Deterministic and narrow** — the protocol has exactly one trigger (target
   worker count change) and exactly three outcomes.
2. **Backward compatible** — when `elasticity.mode == Disabled`, the runtime
   behaves identically to Phase 8.
3. **Fail-closed** — the default for any runtime that does not explicitly claim
   in-place shrink support is checkpoint-and-relaunch.
4. **Manifest-last** — resize metadata is appended to the checkpoint manifest
   *after* all artifacts are durable, preserving the Phase 0 invariant.

## Protocol Summary

```
Controller                        Runtime
    │                                │
    │  control file / env vars       │
    │  (targetWorkerCount = N)       │
    ├───────────────────────────────►│
    │                                │
    │                                │  evaluate_resize()
    │                                │  ┌──────────────────────────┐
    │                                │  │ direction = SHRINK/GROW  │
    │                                │  │ in_place? → yes/no       │
    │                                │  └──────────────────────────┘
    │                                │
    │  resize-signal.json            │
    │◄───────────────────────────────┤
    │                                │
    │  (checkpoint manifest with     │
    │   resize metadata, if needed)  │
    │◄───────────────────────────────┤
    │                                │
```

## Resize Decision Tree

```
evaluate_resize(config) →

  mode == Disabled?
  └─ yes → NONE / NOT_REQUESTED / no checkpoint

  target == current?
  └─ yes → NONE / NOT_REQUESTED / no checkpoint

  target > current?  (GROW)
  └─ yes → GROW / NOT_REQUESTED / checkpoint required
           reason: "grow always requires checkpoint-and-relaunch"

  target < current?  (SHRINK)
  ├─ supports_in_place_shrink?
  │  └─ yes → SHRINK / SUCCESS / no checkpoint
  └─ no  → SHRINK / FALLBACK_REQUIRED / checkpoint required
           reason: "runtime does not support in-place shrink"
```

## Components

### `yield_sdk.elastic` — Core Protocol Module

| Type | Purpose |
|------|---------|
| `ElasticityMode` | Enum: `Disabled`, `Manual` |
| `ResizeDirection` | Enum: `None`, `Shrink`, `Grow` |
| `ShrinkOutcome` | Enum: `Success`, `FallbackRequired`, `NotRequested` |
| `ElasticConfig` | Frozen dataclass: runtime-visible elasticity state |
| `ResizeOutcome` | Frozen dataclass: deterministic resize evaluation result |
| `ResizeCheckpointContext` | Metadata to embed in checkpoint manifests |
| `evaluate_resize()` | Core decision function |
| `build_resize_checkpoint_context()` | Factory for checkpoint context |
| `write_resize_signal()` / `read_resize_signal()` | Signal file I/O |

### `ElasticConfig` Construction

The elastic config is assembled from multiple sources (priority order):

1. **Control file** `targetWorkerCount` field — highest priority, allows
   runtime mutation of target.
2. **`YIELD_SDK_TARGET_WORKER_COUNT`** env var — set at launch by the operator.
3. **Current world size** — fallback when no target is specified.

Additional env vars:

| Variable | Default | Description |
|----------|---------|-------------|
| `YIELD_SDK_ELASTICITY_MODE` | `Disabled` | `Disabled` or `Manual` |
| `YIELD_SDK_TARGET_WORKER_COUNT` | current world size | Target worker count |
| `YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK` | `false` | Runtime annotation gate |
| `YIELD_SDK_SHRINK_BARRIER_TIMEOUT` | `30.0` | Barrier timeout in seconds |

### `ControlRecord` Extensions (Phase 9)

Two new optional fields on the control file record:

| Field | JSON Key | Type | Description |
|-------|----------|------|-------------|
| `target_worker_count` | `targetWorkerCount` | `int \| None` | Desired worker count |
| `resize_request_id` | `resizeRequestId` | `str \| None` | Request tracking ID |

These are backward compatible — a Phase 1-8 control file without these fields
will parse normally with `None` defaults.

### `RuntimeConfig` Extensions (Phase 9)

Three new fields:

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `elasticity_mode` | `YIELD_SDK_ELASTICITY_MODE` | `"Disabled"` | Elasticity mode |
| `target_worker_count` | `YIELD_SDK_TARGET_WORKER_COUNT` | `None` | Initial target |
| `supports_in_place_shrink` | `YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK` | `False` | Runtime capability |

### Checkpoint Manifest Extensions (Phase 9)

Five new optional fields, present only when a checkpoint is produced during a
resize event:

| Field | JSON Key | Type | Description |
|-------|----------|------|-------------|
| `resize_active_worker_count` | `resizeActiveWorkerCount` | `int \| None` | Workers at checkpoint time |
| `resize_target_worker_count` | `resizeTargetWorkerCount` | `int \| None` | Target worker count |
| `resize_direction` | `resizeDirection` | `str \| None` | `"Shrink"` or `"Grow"` |
| `resize_reason` | `resizeReason` | `str \| None` | Human-readable reason |
| `resize_in_place_shrink_supported` | `resizeInPlaceShrinkSupported` | `bool \| None` | Runtime claim |

All fields are `None` for non-resize checkpoints, preserving backward
compatibility with Phase 3-8 manifests.

### Resize Signal File

Written by the runtime to `$YIELD_SDK_RESIZE_SIGNAL_DIR/resize-signal.json`.

```json
{
  "checkpointID": "ckpt-20260405T120000Z-a1-s10",
  "currentWorkerCount": 4,
  "direction": "Shrink",
  "fallbackReason": "runtime does not support in-place shrink ...",
  "inPlaceShrinkSupported": false,
  "manifestURI": "s3://bucket/manifests/ckpt-20260405T120000Z-a1-s10.manifest.json",
  "outcome": "FallbackRequired",
  "requiresCheckpoint": true,
  "targetWorkerCount": 2
}
```

## Fixture Knobs

The `pytorch_ddp_counter` fixture adds these CLI arguments:

| Argument | Default | Description |
|----------|---------|-------------|
| `--shrink-barrier-timeout` | `30.0` | Cooperative shrink barrier timeout (seconds) |
| `--warmup-steps` | `0` | Steps to skip before checking for resize |
| `--resize-check-every` | `1` | Check for resize every N steps |
| `--resize-signal-dir` | `$YIELD_SDK_RESIZE_SIGNAL_DIR` | Directory for signal files |

## Fixture Behavior

When elasticity is enabled and a resize is detected:

1. All ranks barrier.
2. Checkpoint is saved with resize metadata.
3. Resize signal file is written (rank 0).
4. Trainer exits cleanly.

The DDP fixture always reports `supports_in_place_shrink=false` because DDP
requires process group reinitialization for rank changes.

When elasticity is disabled (the default), the fixture behaves exactly as
Phase 1-8: only the control file `desiredState: Paused` triggers a yield.

## Test Coverage

| Test File | Count | Covers |
|-----------|-------|--------|
| `test_elastic.py` | 27 | Config detection, evaluate_resize(), serialization, signals, backward compat |
| `test_control.py` | 10 | Control file parsing including Phase 9 fields, backward compat |
| `test_manifest.py` | 21 | Manifest serialization including Phase 9 fields, cross-phase coexistence |
| `test_resume.py` | 10 | Checkpoint save/restore including non-elastic backward compat |
| **Total** | **68** new + 14 existing = **82** |

## Invariants Preserved

- **I-9**: Elasticity disabled ≡ Phase 8 behavior — no resize detection, no
  signal files, no manifest metadata.
- **I-10**: Scale-up always goes through checkpoint-and-relaunch — grow outcome
  always sets `requires_checkpoint=true`.
- **Manifest backward compat**: Phase 3-8 manifests without Phase 9 fields
  decode with `None` defaults.
- **Control file backward compat**: Phase 1-8 control files without
  `targetWorkerCount` parse normally.
