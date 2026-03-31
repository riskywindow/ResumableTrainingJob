# Phase 6 Demo: MultiKueue Remote Dispatch

This document provides the exact command sequence for demonstrating
Phase 6 multi-cluster checkpoint-native spillover.

## Prerequisites

1. Three kind clusters running (manager + 2 workers):

```bash
make phase6-up
```

2. Verify infrastructure:

```bash
make phase6-smoke
```

3. Build and load the trainer image into all three clusters:

```bash
# Build your trainer image, then:
make phase6-load-images IMAGES=your-trainer:latest
```

## Demo 1: Remote Dispatch

Submit a MultiKueue-managed RTJ on the manager. Kueue dispatches it to
a worker cluster.

```bash
# 1. Submit the RTJ on the manager cluster.
make phase6-submit PHASE6_TRAINER_IMAGE=your-trainer:latest

# 2. Watch the RTJ on the manager. The dispatch phase progresses:
#    Pending -> Dispatched -> Active
make phase6-inspect-manager

# 3. Verify the mirror RTJ appears on a worker cluster.
make phase6-inspect-worker

# 4. Confirm no local child JobSet exists on the manager.
#    The inspect-manager output includes a suppression check.
#    Expected: "PASS: no local child JobSets found"
make phase6-inspect-manager
```

### What to observe

- `status.multiCluster.dispatchPhase` transitions from `Pending` to
  `Dispatched` to `Active`.
- `status.multiCluster.executionCluster` shows which worker was selected.
- `status.multiCluster.localExecutionSuppressed` is `true`.
- The worker cluster shows a running child JobSet with trainer pods.
- The manager cluster has zero child JobSets for this RTJ.

## Demo 2: Manager-Visible Remote Status

After the remote RTJ is running, the manager reflects remote status.

```bash
# 1. Wait for the worker to produce a checkpoint (depends on trainer config).
#    With CHECKPOINT_EVERY=3 and SLEEP_PER_STEP=5, first checkpoint at ~15s.
sleep 20

# 2. Inspect remote checkpoint evidence on the manager.
make phase6-inspect-checkpoints

# 3. Inspect the full MultiCluster status.
make phase6-inspect-manager
```

### What to observe

- `status.multiCluster.remotePhase` mirrors the worker's current phase.
- `status.multiCluster.remoteCheckpoint` shows the latest checkpoint ID,
  completion time, and storage URI.
- `status.lastCompletedCheckpoint` is mirrored from the worker.
- `status.multiCluster.remoteObjectRef` shows the remote RTJ coordinates.

## Demo 3: Manager-Driven Remote Pause

Pause the remote RTJ from the manager cluster.

```bash
# 1. Pause the RTJ.
make phase6-pause

# 2. Wait for the adapter to tear down the remote (5-10 seconds).
sleep 10

# 3. Check manager status — should show Paused.
make phase6-inspect-manager

# 4. Check worker — the original mirror RTJ should be gone or recreated
#    with Paused spec.
make phase6-inspect-worker

# 5. Verify checkpoint evidence is preserved on the manager.
make phase6-inspect-checkpoints
```

### What to observe

- `status.phase` on the manager transitions to `Paused`.
- `status.multiCluster.remotePhase` shows `Paused`.
- `status.multiCluster.remoteCheckpoint` is preserved (not cleared by
  the adapter's delete-recreate cycle).
- The worker's child JobSet is torn down.

## Demo 4: Manager-Driven Remote Resume

Resume the remote RTJ from the manager cluster. The worker resumes from
the shared checkpoint store.

```bash
# 1. Resume the RTJ.
make phase6-resume

# 2. Wait for the adapter to create the new Running remote (5-15 seconds).
sleep 15

# 3. Check manager status — should show Active again.
make phase6-inspect-manager

# 4. Check worker — new child JobSet should be running.
make phase6-inspect-worker

# 5. Verify the worker resumed from the shared checkpoint.
make phase6-inspect-checkpoints
```

### What to observe

- `status.phase` on the manager transitions back to a running phase.
- `status.multiCluster.dispatchPhase` returns to `Active`.
- The worker creates a new child JobSet and resumes from the last
  checkpoint in the shared store.
- `status.currentRunAttempt` is incremented on the worker.

## Full Sequence (Copy-Paste)

```bash
# Setup
make phase6-up
make phase6-smoke

# Submit
make phase6-submit PHASE6_TRAINER_IMAGE=your-trainer:latest
sleep 5
make phase6-inspect-manager

# Wait for checkpoint
sleep 20
make phase6-inspect-checkpoints

# Pause
make phase6-pause
sleep 10
make phase6-inspect-manager
make phase6-inspect-checkpoints

# Resume
make phase6-resume
sleep 15
make phase6-inspect-manager
make phase6-inspect-worker

# Cleanup
make phase6-down
```

## E2E Tests

Run the automated e2e tests that exercise the same flows:

```bash
make e2e-phase6
```

This runs `TestMultiClusterRemoteExecution`, `TestMultiClusterManagerSuppression`,
and `TestMultiClusterRemotePauseResume`.
