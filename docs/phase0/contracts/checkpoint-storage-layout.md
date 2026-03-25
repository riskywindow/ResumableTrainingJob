# Checkpoint Storage Layout

This document defines the conceptual Phase 0 storage layout contract for `v1` checkpoints in S3-compatible object storage.
It exists so checkpoint completeness, corruption handling, retention, and restore lookup do not depend on ad hoc prefix conventions.

## Scope

This layout is conceptual.
It defines the minimum structure and naming intent the operator and runtime MUST be able to reason about.
It does not prescribe a concrete bucket name, tenancy model, or implementation library.

## Layout Goals

The layout MUST support:

- stable per-RTJ lineage separation
- per-checkpoint isolation
- manifest-last completion
- deterministic restore lookup
- retention that preserves complete checkpoints as atomic units

## Logical Layout

The conceptual storage layout is:

```text
s3://<bucket>/<prefix>/<rtj-identity>/
  manifests/
    <checkpoint-id>.manifest.json
  checkpoints/
    <checkpoint-id>/
      metadata/
        runtime.json
      data/
        ...
```

The operator MAY use either the `manifests/` index plus per-checkpoint subtrees, or an equivalent layout that preserves the same semantics.
What matters for `v1` is not the exact path text, but the following invariants.

## Required Layout Invariants

- Each RTJ lineage MUST have a stable storage root.
- Each checkpoint ID MUST map to one logical checkpoint subtree.
- A manifest for checkpoint `X` MUST refer only to artifacts belonging to checkpoint `X`.
- The manifest object MUST be written last.
- Required artifacts for a checkpoint MUST NOT be spread across unrelated RTJ lineage roots.

## Manifest Location

The operator MUST be able to discover candidate checkpoints by listing manifests for one RTJ lineage.
That means the layout MUST support a bounded manifest lookup path per RTJ lineage.

The manifest URI SHOULD be stable enough to derive from:

- RTJ lineage identity
- checkpoint ID

without requiring a full recursive walk of every object under the lineage prefix.

## Artifact Location

Each artifact referenced by the manifest MUST be addressable by a stable URI or object key.
The manifest MUST be the source of truth for required artifact locations.

The operator MUST NOT infer artifact membership from directory shape alone.

## Commit Ordering

The runtime MUST write the storage layout in this order:

1. create or update required artifact objects for the checkpoint subtree
2. verify local completion conditions needed to publish the completion record
3. write the manifest as the final commit record

If the runtime crashes before step 3, the checkpoint MUST be treated as incomplete.

## Atomicity Assumption

`v1` does not assume multi-object transactional storage.
Instead, it relies on the manifest-last rule.

This means the platform MUST treat the manifest as the checkpoint atomicity boundary.
The operator MUST ignore partially written checkpoint subtrees that do not have a valid committed manifest.

## Corruption and Tombstones

If a checkpoint is found to be corrupted after completion:

- the platform MAY retain the corrupted checkpoint for forensics
- the operator SHOULD mark it unusable in control-plane state
- the platform SHOULD avoid deleting only some required artifacts while leaving the manifest intact

If retention deletes a checkpoint, it SHOULD delete the manifest and all required artifacts as one logical unit.
Partial deletion SHOULD be treated as corruption from the operator's perspective.

## Retention Assumptions

`v1` assumes retention is policy-driven and externalized.

Retention SHOULD preserve at least:

- the latest complete compatible checkpoint
- any checkpoint currently selected for restore
- any newer checkpoint that has not yet been classified as valid or invalid

Retention MAY delete older complete checkpoints according to platform policy, but it MUST NOT leave behind a manifest that points to deleted required artifacts.

## Garbage Collection Ownership

For Phase 0, the platform owns retention and garbage-collection policy.
The operator MAY surface which checkpoints are still referenced or protected, but it does not own general storage cleanup policy in `v1`.

## Restore Lookup Assumption

To restore an RTJ, the operator SHOULD:

1. list manifests under the RTJ lineage root
2. validate candidate manifests
3. order candidates by selection policy
4. choose the latest compatible complete checkpoint

The operator MUST NOT restore by scanning raw artifact prefixes without a manifest.
