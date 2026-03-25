# Phase 1 Launch Path

## Scope

This document describes the Phase 1 RTJ launch path that is now implemented in the operator.
It covers only first-run launch.
It does not yet implement manual yield, pause, restore orchestration, or resume selection.

## Launch Rule

When an RTJ wants to be `Running` and there is no active child `JobSet`, the operator now:

1. computes the next run-attempt number,
2. creates a control `ConfigMap` for that attempt,
3. renders a child `JobSet` from the embedded RTJ template,
4. injects the Kueue queue label and workload-priority label,
5. injects the Phase 1 runtime env vars,
6. mounts the control `ConfigMap` into the training pods,
7. mounts an `emptyDir` staging volume for local DCP filesystem checkpoints,
8. sets owner references on both child resources,
9. records the active child names and run attempt in RTJ status.

## Child Names

The current deterministic naming scheme is:

- child `JobSet`: `<rtj-name>-run-<attempt>`
- control `ConfigMap`: `<rtj-name>-run-<attempt>-control`

Those names are stored in RTJ status:

- `status.currentRunAttempt`
- `status.activeJobSetName`
- `status.activeControlConfigMapName`

## Injected Labels

The renderer injects these labels onto the child `JobSet` metadata:

- `kueue.x-k8s.io/queue-name=<spec.queueName>`
- `kueue.x-k8s.io/priority-class=<spec.workloadPriorityClassName>` when configured
- controller-owned bookkeeping labels for RTJ name and run attempt

Phase 1 keeps label injection at the child `JobSet` metadata layer because Kueue is still managing the child `JobSet` through its built-in integration.

## Injected Runtime Env Vars

The renderer injects these env vars into every container in each replicated job template:

- `YIELD_SDK_STORAGE_URI`
- `YIELD_SDK_CONTROL_FILE`
- `YIELD_SDK_RUN_ATTEMPT`
- `YIELD_SDK_RESTORE_MANIFEST_URI` when a selected checkpoint manifest exists
- `YIELD_SDK_STAGING_ROOT`
- `YIELD_SDK_RESTORE_ROOT`
- `YIELD_SDK_YIELD_MARKER_PATH`
- `YIELD_SDK_RTJ_IDENTITY`
- `YIELD_SDK_CLUSTER_IDENTITY`

The first four are the required Phase 1 launch-path contract.
The additional paths are injected so the current Python runtime actually uses the mounted staging volume and the shared local filesystem layout.

## Mounted Paths

The renderer mounts:

- control `ConfigMap` at `/var/run/yield-sdk/control`
- local staging `emptyDir` at `/var/lib/yield-sdk`

The injected runtime paths then resolve to:

- control file: `/var/run/yield-sdk/control/control.json`
- staging root: `/var/lib/yield-sdk/staging`
- restore root: `/var/lib/yield-sdk/restore`
- yield-complete marker: `/var/lib/yield-sdk/yield-complete.json`

## Current Status Behavior

The operator now distinguishes these launch-path phases:

- `Pending`
- `Starting`
- `Running`
- `Failed`

Current behavior is intentionally minimal:

- `Starting` means the controller created the attempt resources and recorded their names.
- `Running` means the RTJ status points at a still-existing active child `JobSet`.
- `Failed` means the launch path could not render or create required child resources safely.

Phase 1 does not yet inspect Pod readiness or trainer heartbeats before publishing `Running`.
That richer runtime-state handling is deferred to the next controller step.
