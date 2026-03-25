# Checkpoint Selection and Compatibility

This document defines the conceptual Phase 0 rules for checkpoint selection, strict `v1` compatibility, restore fallback, and corruption-aware resume behavior.

## Selection Rule

`v1` selection is fixed:

- choose the latest compatible complete checkpoint first

More explicitly, the operator MUST:

1. enumerate candidate checkpoints for the RTJ lineage from committed manifests
2. discard any checkpoint that is incomplete
3. discard any checkpoint that is invalid
4. evaluate strict `v1` compatibility for the remaining candidates
5. sort compatible candidates by `completionTimestamp` descending
6. select the newest compatible candidate

The user MUST NOT directly override this selection order in `v1`.

## Strict v1 Compatibility

A checkpoint is compatible for `v1` only if all of the following match the current RTJ intent and runtime request:

- Kubernetes cluster identity
- RTJ lineage identity
- runtime mode: DDP or FSDP
- world size
- GPU shape
- image identity or build identity
- training code version identity
- optimizer mode
- sharding mode
- supported manifest or format version

If any required field is missing, unreadable, or mismatched, the checkpoint MUST be treated as incompatible.

## Optimizer and Sharding Strictness

`v1` compatibility is intentionally strict for optimizer and sharding behavior.

- A checkpoint written with one optimizer mode MUST NOT be resumed under a different optimizer mode.
- A checkpoint written with one sharding mode MUST NOT be resumed under a different sharding mode.
- Exact-match recording for optimizer and sharding mode is required even when the runtime mode remains `FSDP` or `DDP`.

This rule exists because optimizer-state layout and sharded state structure are part of the effective restore contract, not merely incidental metadata.

## Complete, Valid, Compatible, Resumable

Selection MUST apply the terms from [checkpoint-contract.md](checkpoint-contract.md) in order:

- incomplete checkpoints are rejected first
- complete but invalid checkpoints are rejected second
- valid but incompatible checkpoints are rejected third
- only complete, valid, compatible checkpoints are resumable candidates

## Latest Compatible Complete Checkpoint First

The selection policy is intentionally conservative.

- Newer but incomplete checkpoints MUST be skipped.
- Newer but invalid checkpoints MUST be skipped.
- Newer but incompatible checkpoints MUST be skipped.
- The operator MUST fall back to the next newest checkpoint that is complete, valid, and compatible.

This means "latest" is evaluated only after compatibility and completeness filtering.

## Restore Validation

Before issuing restore for a selected checkpoint, the operator MUST revalidate at least:

- manifest readability
- required artifact presence
- integrity metadata
- strict compatibility fields
- that the checkpoint has not been superseded in control-plane intent by a newer selection cycle

If restore-start validation fails, the operator MUST treat the selected checkpoint as unusable for that attempt.

## Fallback Logic

Fallback behavior in `v1` is:

1. validate the newest compatible complete checkpoint
2. if restore validation fails, mark that checkpoint unusable for the current selection cycle
3. retry selection with the next newest compatible complete checkpoint
4. continue until a checkpoint restores successfully or no candidates remain

If no compatible complete checkpoint remains:

- the operator MUST fail closed
- the RTJ MUST transition to `Failed`
- the failure reason SHOULD identify whether exhaustion was caused by incompatibility, corruption, missing artifacts, or restore failure

The operator MAY retry restore attempts according to `spec.resume.maxResumeRetries`, but it MUST NOT loop indefinitely on one known-bad checkpoint.

## Corruption Handling

Corruption handling is part of selection, not a separate afterthought.

- A checksum or digest mismatch MUST mark the checkpoint invalid.
- Missing required artifacts MUST mark the checkpoint incomplete or invalid depending on what can be proven.
- A manifest that references objects outside the RTJ lineage root SHOULD be treated as invalid.
- The operator SHOULD keep enough diagnostic state to explain why a checkpoint was skipped.

Corrupted checkpoints MUST be skipped even if they are the newest checkpoints in the lineage.

## Retention Assumptions

Selection assumes that retention preserves at least one complete compatible checkpoint for any RTJ that is expected to resume successfully.
If retention deletes all compatible complete checkpoints, restore MUST fail closed.

The operator MUST NOT guess around missing retained data.

## Metadata Recorded For Future World-Size Change Support

`v1` does not support world-size changes on resume.
However, manifests SHOULD still record metadata that would matter for a later compatibility expansion, including:

- total world size
- replica count
- per-rank placement
- shard topology
- optimizer partition metadata
- tensor partition metadata

Recording this metadata is forward-looking only.
It MUST NOT be interpreted as permission to resume with a different world size in `v1`.
