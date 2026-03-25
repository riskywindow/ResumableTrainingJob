# Assumptions and Invariants

This document captures the assumptions that `v1` relies on and the invariants that MUST remain true across operator restarts, retries, and repeated reconciliation.
These rules are part of the Phase 0 contract and SHOULD be treated as non-negotiable unless a later ADR changes them.

## Product Assumptions

- The product scope remains the narrow `v1` scope from ADR 0001.
- The system supports exactly one Kubernetes cluster.
- Kueue remains the authority for queueing, admission, and queue-driven preemption intent.
- JobSet remains the only supported runtime or orchestration primitive.
- PyTorch DDP and FSDP remain the only supported distributed execution modes.
- PyTorch DCP remains the only supported checkpoint mechanism and format.
- S3-compatible object storage remains the only supported checkpoint storage target.
- Resume continues to require the compatibility contract from ADR 0003.

## Operational Assumptions

- Platform admins provision and operate Kueue, JobSet, the operator, and object storage before any RTJ is submitted.
- Every RTJ has a stable declared image identity, declared code version, world size, GPU shape, optimizer mode, and sharding mode.
- Object storage credentials and network access needed for checkpoint read or write are available to in-scope workloads.
- The operator can recover its decisions from Kubernetes objects and persisted checkpoint metadata rather than requiring in-memory continuity.
- External automation does not create or delete JobSets behind the operator's back for the same RTJ lineage.
- The platform keeps clock skew, credential rotation, and object-store access policy within operational bounds that do not break checkpoint write or resume flows.

## Runtime Assumptions

- User training code exposes a safe training step boundary that the SDK or agent can use for graceful yield.
- The SDK or agent is present in every in-scope training Pod and is the only runtime path used for DCP checkpoint and restore.
- DCP checkpoint metadata is written in a form the operator can use to evaluate compatibility and readiness.
- A resumed workload uses the same cluster, image identity, declared code version, world size, GPU shape, optimizer mode, and sharding mode as the checkpoint it restores from.
- User code does not mutate the meaning of checkpoint metadata after the checkpoint is written.

## Invariants Across Operator Restarts and Reconciliation

### Single Active Runtime Invariant

At most one active JobSet MAY exist for a given RTJ at any time.
The operator MUST NOT create a second active JobSet for the same RTJ lineage during retry, restart, or concurrent reconciliation.

### Incomplete Checkpoint Invariant

The system MUST never resume from an incomplete checkpoint.
If checkpoint completeness or compatibility cannot be proven from persisted metadata and required objects, the operator MUST treat the checkpoint as unusable.

### Paused State Invariant

`Paused` implies no active training Pods.
If an RTJ is reported as `Paused`, the operator MUST have converged the workload such that no active training JobSet Pods remain for that RTJ.

### Running State Invariant

`Running` implies an admitted workload plus an active JobSet.
If an RTJ is reported as `Running`, there MUST be both valid admission state and one active JobSet for that RTJ.

### Idempotent Transition Invariant

All lifecycle transitions MUST be idempotent.
Repeating the same reconcile, retrying the same API write, or replaying the same event MUST NOT create duplicate runtime instances or advance the RTJ into an impossible state.

### Persisted-Truth Invariant

Externally visible lifecycle truth MUST be recoverable after an operator restart.
The operator MUST derive resumed behavior from persisted Kubernetes state and checkpoint metadata, not from ephemeral in-memory assumptions.

### Checkpoint-Lineage Invariant

Any checkpoint selected for resume MUST belong to the same RTJ lineage and satisfy ADR 0003 compatibility rules at the time of selection.
The operator MUST NOT treat a checkpoint from another lineage as eligible merely because the object path looks similar.

### Admission Coupling Invariant

The operator MUST NOT treat an RTJ as `Running` or start a resume without valid Kueue admission.
Loss, absence, or revocation of admission MUST prevent creation of a new active runtime instance.

### Yield Convergence Invariant

Manual yield and Kueue-driven yield MUST converge on the same lifecycle invariants.
The source of yield intent MAY differ, but the operator MUST enforce the same single-runtime, safe-checkpoint, and idempotent-transition guarantees.
