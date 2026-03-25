# Phase 1 Pause Flow

Phase 1 manual pause is a bounded drain of the active child `JobSet`.
It does not use Kueue-driven preemption.
Pause completion records the checkpoint produced by the active attempt.
Resume selection later re-discovers manifests from object storage instead of trusting `status.selectedCheckpoint` alone.

## Trigger

The pause path starts when:

- `spec.control.desiredState` changes from `Running` to `Paused`
- `status.activeJobSetName` still points at a live child `JobSet`

## Controller Sequence

The controller performs this sequence:

1. Record a stable pause request id in `status.pauseRequestID`.
2. Record the request time in `status.transitionTimestamps.yieldRequestedAt`.
3. Rewrite the mounted control `ConfigMap` so the trainer sees:

```json
{
  "desiredState": "Paused",
  "requestId": "<stable-request-id>",
  "updatedAt": "<rfc3339>"
}
```

4. Poll object storage for two artifacts newer than the recorded request time:
   - the yield-complete marker for the current run attempt
   - the checkpoint manifest referenced by that marker
5. Publish the completed checkpoint to:
   - `status.lastCompletedCheckpoint`
   - `status.selectedCheckpoint`
6. Delete the active child `JobSet`.
7. Mark the RTJ `Paused` once the child `JobSet` is gone.

The path is idempotent across controller restarts because the request id and request timestamp live in RTJ status, and the controller only accepts artifacts newer than that stored timestamp.

## Storage-Side Contract

The Phase 1 trainer now writes two pause artifacts on rank 0:

- `s3://<checkpoint-root>/yield-markers/run-<attempt>.json`
- `s3://<checkpoint-root>/manifests/<checkpoint-id>.manifest.json`

The yield marker is a small coordination record.
The manifest remains the completion boundary for the checkpoint itself.

The controller requires:

- marker `requestID` matches `status.pauseRequestID`
- marker `completionTimestamp` is newer than `yieldRequestedAt`
- manifest exists
- manifest `completionTimestamp` is newer than `yieldRequestedAt`

## Status Progression

The expected dominant phases are:

- `Running`: active child `JobSet` exists
- `YieldRequested`: pause request id and request time recorded
- `Draining`: control artifact is updated and the controller is waiting for storage artifacts
- `Paused`: checkpoint recorded and child `JobSet` removed

If the drain exceeds `spec.checkpoint.maxDrainTime`, the controller:

- deletes the child `JobSet`
- sets `status.phase=Failed`
- sets the `Degraded` condition with reason `DrainTimedOut`
- preserves the pause request id, request timestamp, and latest checkpoint references for debugging

## Current Phase 1 Limits

- The controller only uses the simplest storage-side catalog check needed for pause completion.
- The pause path is still polling-based and storage-driven. It does not use a richer runtime heartbeat contract yet.
- Pause completion currently depends on the trainer uploading the yield marker to object storage in addition to writing the local marker file.
