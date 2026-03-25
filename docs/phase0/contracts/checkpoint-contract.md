# Checkpoint Contract

This document defines the conceptual Phase 0 checkpoint contract for `v1`.
It specifies what a checkpoint is, when it is considered complete, how validity differs from compatibility, how freshness affects preemptibility, and what minimum manifest data the operator MUST rely on.

This contract is intentionally strict.
`v1` MUST prefer false negatives over unsafe resume behavior.

## Purpose

For `v1`, a checkpoint MUST be evaluated along four separate dimensions:

- completeness
- validity
- compatibility
- resumability

These terms are related, but they are not interchangeable.

## Definitions

### Complete

A checkpoint is **complete** only if all of the following are true:

1. All required checkpoint artifacts for the checkpoint ID have been written to the configured S3-compatible storage prefix.
2. The checkpoint manifest exists and is readable.
3. The manifest was committed last, after every required artifact for that checkpoint ID was durably written.
4. The manifest declares a `completionTimestamp`.
5. Every artifact listed in the manifest has integrity metadata and a readable object at the declared location.

If any required artifact is missing, unreadable, omitted from the manifest, or written after the manifest commit point, the checkpoint MUST be treated as incomplete.

### Incomplete

A checkpoint is **incomplete** if the operator cannot prove completeness from persisted objects and metadata.

Examples include:

- artifacts exist but the manifest is missing
- the manifest exists but has no completion timestamp
- the manifest exists but one or more listed artifacts are missing
- objects are still arriving after the manifest was written
- integrity metadata is missing or unreadable

`v1` MUST NOT resume from an incomplete checkpoint.

### Valid

A checkpoint is **valid** only if it is complete and all of the following are true:

- it was produced by the supported SDK or agent using PyTorch DCP
- the manifest format version is supported by the operator and runtime
- the manifest content is syntactically well-formed and semantically coherent
- required identity and runtime fields are present
- artifact integrity verification succeeds for the artifacts required to prove checkpoint integrity

### Invalid

A checkpoint is **invalid** if it is complete but fails validation, or if the manifest itself is malformed or contradictory.

Examples include:

- unsupported format version
- duplicate artifact entries with conflicting metadata
- checksum mismatch
- manifest field values that contradict the RTJ lineage or run attempt structure
- unsupported runtime mode or checkpoint producer version

`v1` MUST treat invalid checkpoints as unusable.

### Compatible

A checkpoint is **compatible** only if it is valid and matches the current resume request under the strict `v1` compatibility rules in [checkpoint-selection-and-compatibility.md](checkpoint-selection-and-compatibility.md) and ADR 0003.

### Resumable

A checkpoint is **resumable** only if it is:

- complete
- valid
- compatible
- selected by the operator as the active restore source for the current run attempt

A checkpoint MAY be compatible without being the currently selected restore source.

## Manifest Commit Rule

The manifest MUST be committed last.

That means:

- artifact objects MUST be written before the manifest becomes visible as the completion record
- the manifest is the only storage-side record that can move a checkpoint from "possibly still being written" to "complete"
- the operator MUST NOT infer completion from object count, prefix listing, or timing alone

This rule exists so the operator has a single persisted completion boundary that survives retries and restarts.

## Minimum Manifest Fields

Every complete `v1` checkpoint manifest MUST record at least:

- `checkpointID`
- `rtjIdentity`
- `runAttempt`
- `globalStep`
- `wallClockTimestamp`
- `imageIdentity`
- `codeVersionIdentity`
- `worldSize`
- `optimizerMode`
- `shardingMode`
- `formatVersion`
- `artifacts`
- `completionTimestamp`

The minimum shape of those fields is:

| Field | Meaning |
| --- | --- |
| `checkpointID` | Stable identifier for this checkpoint within the RTJ lineage |
| `rtjIdentity` | Stable lineage identity for the RTJ that owns the checkpoint |
| `runAttempt` | The runtime attempt that produced the checkpoint |
| `globalStep` | Training step at which the checkpoint was taken |
| `wallClockTimestamp` | Timestamp associated with checkpoint creation |
| `imageIdentity` | Training image or build identity used for compatibility checks |
| `codeVersionIdentity` | Declared training code version used for compatibility checks |
| `worldSize` | Declared world size at checkpoint time |
| `optimizerMode` | Declared optimizer-state mode required for strict `v1` resume compatibility |
| `shardingMode` | Declared sharding mode required for strict `v1` resume compatibility |
| `formatVersion` | Manifest or metadata format version |
| `artifacts` | The required artifact list with integrity metadata |
| `completionTimestamp` | The commit time after all required artifacts were durably written |

## Artifact List Requirements

The manifest `artifacts` list MUST include one entry per required artifact.
Each artifact entry MUST include at least:

- logical artifact name
- object URI or object key
- size in bytes
- content digest algorithm
- content digest value

The operator MAY allow additional artifact metadata, but it MUST require enough integrity metadata to detect missing or corrupted data.

## Freshness

Checkpoint freshness is measured as:

`now - completionTimestamp`

using the most recent complete checkpoint for the RTJ lineage.

Freshness is policy-relevant even when the checkpoint is valid and compatible.

### Fresh

A checkpoint is **fresh** if its age is less than or equal to the RTJ's declared `spec.checkpoint.freshnessBudget`.

### Stale

A checkpoint is **stale** if its age exceeds the RTJ's declared `spec.checkpoint.freshnessBudget`.

Staleness does not make a checkpoint invalid.
It does affect preemptibility.

## Freshness and Preemptibility

For `v1`, freshness influences whether an RTJ MAY be treated as safely preemptible without first taking a new checkpoint.

- If the latest complete compatible checkpoint is fresh, the operator MAY consider the RTJ to have a recent recoverable point, but it SHOULD still prefer an explicit controlled yield when time allows.
- If the latest complete compatible checkpoint is stale, the operator SHOULD treat the RTJ as requiring a fresh controlled drain before it is considered safely preemptible.
- Freshness alone MUST NOT override the step-boundary-only yield rule.
- Freshness alone MUST NOT override completeness, validity, or compatibility requirements.

This means freshness is an operational signal, not a compatibility shortcut.

## Corruption Handling

If integrity verification fails for any required artifact, the checkpoint MUST be treated as invalid.

Corruption handling rules for `v1` are:

- the operator MUST NOT select a corrupted checkpoint for resume
- corruption of one checkpoint MUST NOT cause unrelated checkpoints in the same RTJ lineage to be discarded automatically
- the operator SHOULD retain corruption diagnostics in status, logs, or events
- the operator MAY mark corrupted checkpoints so they are skipped on later selection passes

## Retention Assumptions

`v1` assumes:

- checkpoints are stored under a stable per-RTJ lineage prefix
- retention policy is operated by the platform, not dynamically inferred by the operator at restore time
- manifests and required artifacts for checkpoints that remain eligible for resume are retained together

The operator MUST assume that deletion of either the manifest or any required artifact makes that checkpoint unusable.

## Metadata For Future World-Size Change Support

`v1` does not support world-size changes on resume.
Even so, the manifest SHOULD record metadata that would help a later release reason about reshaped restore, including:

- replica count
- rank-to-worker mapping
- shard count
- shard ownership map
- optimizer partition metadata
- tensor or state partition metadata

Recording that metadata does not imply support for elastic or reshaped resume in `v1`.
