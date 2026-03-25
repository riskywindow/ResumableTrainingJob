# ADR 0003: v1 Resume Compatibility

- Status: Accepted
- Date: 2026-03-19

## Context

The product promise depends on a strict definition of what checkpoint data may be resumed safely.
Without a concrete compatibility contract, the operator would either reject too much or, worse, resume workloads under ambiguous conditions.
`v1` therefore needs a fail-closed definition of a compatible checkpoint.

## Decision

For `v1`, a checkpoint is compatible only if all of the following are true:

1. The checkpoint was written by the supported training SDK or agent using PyTorch DCP.
2. The checkpoint artifacts are stored in the configured S3-compatible object store and the required manifest or metadata objects are present and readable.
3. The checkpoint metadata identifies the same Kubernetes cluster as the current resume request.
4. The checkpoint metadata identifies the same logical `ResumableTrainingJob` lineage as the current resume request.
5. The checkpoint metadata records the same distributed execution mode as the resume request: DDP or FSDP.
6. The checkpoint metadata records the same declared world size as the resume request.
7. The checkpoint metadata records the same declared GPU shape as the resume request.
8. The checkpoint metadata records the same training image identity and the same declared code version as the resume request.
9. The checkpoint metadata records the same optimizer mode and the same sharding mode as the resume request.
10. The checkpoint metadata format version is one the operator and SDK or agent support.

If any required condition is missing, unreadable, or mismatched, the operator MUST treat the checkpoint as incompatible and MUST fail closed.

## What v1 Must Record

To make the compatibility decision concrete, `v1` checkpoint metadata MUST record at least:

- Cluster identity
- `ResumableTrainingJob` lineage identity
- Checkpoint creation time
- DDP or FSDP mode
- Declared world size
- Declared GPU shape
- Training image identity
- Declared code version
- Declared optimizer mode
- Declared sharding mode
- DCP metadata or manifest version
- SDK or agent version that produced the checkpoint
- Object locations needed to restore the checkpoint

## World-Size Rule

`v1` does not support world-size changes on resume.
An attempted resume with a different declared world size MUST be rejected as incompatible.

Even though world-size changes are not supported in `v1`, the system SHOULD still record world-size-related metadata that would matter for a later release, including:

- Total world size
- Per-worker rank layout
- Replica count
- Sharding-related metadata needed to reason about future compatibility

Recording that metadata does not imply support for elastic or reshaped resume in `v1`.

## Explicit Exclusions

For `v1`, compatibility does not mean:

- Compatible across clusters
- Compatible across different image identities
- Compatible across different user-declared code versions
- Compatible across different world sizes
- Compatible across different GPU shapes
- Compatible across different optimizer modes
- Compatible across different sharding modes
- Compatible across arbitrary non-DCP checkpoint formats

`v1` also does not promise transparent adaptation when user code behavior has changed in a way the declared code version does not capture.

## Rationale

This contract deliberately prefers false negatives over unsafe resumes.
The first release needs a compatibility check that is deterministic and auditable.
Requiring exact matches for cluster, lineage, image, code version, world size, GPU shape, optimizer mode, and sharding mode is the narrowest rule that still supports a useful same-environment resume workflow.

## Consequences

- Resume requests become predictable and reviewable.
- Operators can explain clearly why a checkpoint was rejected.
- Future support for world-size changes or broader portability will require a new ADR and an explicit compatibility-model expansion.
