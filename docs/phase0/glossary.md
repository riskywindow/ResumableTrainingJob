# Glossary

## Checkpoint-Native Preemption Controller

The product being defined in Phase 0.
For `v1`, it coordinates graceful yield and same-identity resume for JobSet-managed PyTorch DDP or FSDP workloads in a single Kubernetes cluster.

## Phase 0

The documentation and product-definition stage that precedes implementation.
Phase 0 defines scope, decisions, assumptions, risks, and unresolved questions.

## v1

The first intentionally narrow product scope accepted for implementation planning.
In these docs, `v1` is bound to one cluster, Kueue, JobSet, PyTorch DDP or FSDP, PyTorch DCP, and S3-compatible object storage.

## Kubernetes Cluster

The single cluster boundary within which `v1` yield and resume are supported.
Cross-cluster resume is out of scope.

## Kueue

The authority for queueing, admission, and preemption intent in `v1`.
The product does not replace Kueue with a custom scheduler.

## ResumableTrainingJob

The conceptual user-facing workload object for the accepted `v1` lifecycle.
It declares queue placement, runtime identity, checkpoint policy, resume policy, and the JobSet-compatible runtime template the operator should reconcile.

## ResumableTrainingJob Operator

The product-specific control-plane component that orchestrates graceful yield and resume.
It accepts manual yield requests, reconciles Kueue-driven intent, validates checkpoint compatibility, selects checkpoints, and creates or updates the JobSet used to run the workload.

## Admission

The decision that a workload may run in the cluster under Kueue control.
In these docs, admission authority belongs to Kueue.

## Preemption Intent

The signal that a running workload should yield capacity.
For `v1`, this intent may come from Kueue or from a manual operator action, but Kueue remains authoritative for queueing and admission policy.

## Manual Yield

An operator-initiated request to gracefully yield a running in-scope workload.
Manual yield is part of the `v1` scope.

## Kueue-Driven Yield

A graceful yield triggered by Kueue-originated preemption intent.
Kueue-driven yield is part of the `v1` scope.

## Graceful Yield

The act of stopping an in-scope training workload only after it reaches a safe training step boundary and writes the required checkpoint data.
`v1` supports graceful yield only; it does not promise arbitrary transparent suspension.

## Training Step Boundary

An application-defined point between training steps where state can be checkpointed safely.
`v1` only allows yield at these boundaries.

## Resume

Restarting a previously yielded workload from a valid checkpoint.
For `v1`, resume is supported only when the cluster, image identity, declared code version, world size, GPU shape, optimizer mode, and sharding mode match the declared request.

## Compatible Checkpoint

A checkpoint that satisfies the `v1` resume rules from ADR 0003.
At minimum, it must be a readable DCP checkpoint in S3-compatible storage whose metadata matches the same cluster, `ResumableTrainingJob` lineage, image identity, code version, world size, GPU shape, optimizer mode, sharding mode, and supported metadata format.

## Active JobSet

The single JobSet instance that currently represents the live runtime for an RTJ.
The Phase 0 invariants require at most one active JobSet per RTJ at a time.

## JobSet

The only supported runtime or orchestration primitive for distributed workloads in `v1`.
Other workload primitives are out of scope.

## Training SDK or Agent

The in-pod runtime component that turns yield and resume intent into DCP checkpoint and restore actions.
It does not own admission, checkpoint selection, or policy decisions.

## PyTorch DDP

PyTorch Distributed Data Parallel training.
It is one of the two supported distributed training modes in `v1`.

## PyTorch FSDP

PyTorch Fully Sharded Data Parallel training.
It is one of the two supported distributed training modes in `v1`.

## PyTorch DCP

PyTorch Distributed Checkpoint.
It is the only supported checkpoint mechanism and format in `v1`.

## S3-Compatible Object Storage

The only supported storage target for checkpoint artifacts in `v1`.
The docs use this term for services that expose an S3-compatible object API.

## World Size

The declared total number of distributed training processes participating in the job.
For `v1`, world size MUST match on resume.

## GPU Shape

The declared GPU resource shape expected by the training job at resume time.
For `v1`, GPU shape MUST match on resume.

## Image or Code Version

The declared runtime identity of the training workload used to determine resume compatibility.
For `v1`, resume requires the same recorded image identity and the same declared code version.

## Optimizer Mode or Sharding Mode

The declared optimizer-state mode and sharding mode of the training workload used to determine strict restore compatibility.
For `v1`, resume requires the same recorded optimizer mode and the same recorded sharding mode.

## Paused

A lifecycle state meaning the RTJ has no active training Pods and is not currently executing training work.
In Phase 0, `Paused` implies there is no active JobSet-backed training runtime for the RTJ.

## Running

A lifecycle state meaning the RTJ has valid admission and one active JobSet that is executing training work.

## Reference Environment

The canonical environment used by Phase 0 documentation and later Phase 1 benchmarks to evaluate the `v1` contract consistently.
