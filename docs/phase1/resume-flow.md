# Phase 1 Resume Flow

Phase 1 resume stays deliberately small.
It starts from a `Paused` RTJ, selects the newest compatible complete checkpoint from object storage, creates a new child `JobSet`, and injects the selected manifest URI into the trainer.

## Selection Rule

The controller now discovers resume candidates by listing:

```text
s3://<checkpoint-root>/manifests/
```

For each manifest candidate it:

1. reads and decodes the manifest,
2. rejects it if the manifest is incomplete,
3. rejects it if any required artifact object is missing,
4. rejects it if the compatibility fields do not exactly match the current RTJ intent,
5. sorts the remaining candidates by `completionTimestamp` descending,
6. selects the newest compatible complete checkpoint.

Phase 1 still fails closed.
It does not resume from raw checkpoint directories, and it does not guess around missing artifacts or metadata.

## Compatibility Checks

The current selection path enforces exact matches for:

- cluster identity
- RTJ lineage identity
- runtime mode
- world size
- GPU shape
- image identity
- code version identity
- manifest format version
- optimizer mode
- sharding mode

If any required field is missing or mismatched, the manifest is rejected.

## Controller Sequence

When `spec.control.desiredState` moves from `Paused` back to `Running`, the operator:

1. selects the latest compatible complete checkpoint,
2. increments the run attempt,
3. creates a new per-attempt control `ConfigMap`,
4. creates a new child `JobSet`,
5. injects `YIELD_SDK_RESTORE_MANIFEST_URI`,
6. records the selected checkpoint in `status.selectedCheckpoint`,
7. marks the RTJ `Restoring`,
8. returns the RTJ to `Running` once the new child `JobSet` exists again.

This keeps Kueue managing only the child `JobSet` through built-in integration.

## Trainer Restore Behavior

The toy trainer now treats restore as fail closed too.
Before loading DCP state, it verifies that the selected manifest still matches the runtime configuration for:

- cluster identity
- RTJ lineage identity
- runtime mode
- world size
- GPU shape
- image identity
- code version identity
- format version
- optimizer mode
- sharding mode

On success, it restores model, optimizer, RNG, and trainer state and then continues from `globalStep + 1`.

## Failure Path

If no compatible complete checkpoint exists, the controller does not create a new child `JobSet`.
It marks the RTJ `Failed` with reason `NoCompatibleCheckpoint`.
