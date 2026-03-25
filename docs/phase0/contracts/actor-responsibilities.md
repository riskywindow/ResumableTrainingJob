# Actor Responsibilities

This document defines the cross-cutting responsibilities for each actor in the `v1` system.
It is intentionally concrete so later implementation work can be evaluated against a clear division of labor.

## Platform Admins

Platform admins are responsible for:

- Installing and operating the single supported Kubernetes cluster
- Installing and configuring Kueue, JobSet, and the `ResumableTrainingJob` operator
- Defining queues, admission policy, quotas, and preemption policy in Kueue
- Provisioning S3-compatible object storage, bucket or prefix policy, and access credentials
- Publishing approved training images and enforcing image identity discipline
- Defining how ML users declare code version, world size, GPU shape, optimizer mode, sharding mode, and checkpoint location
- Setting operational guardrails such as retention, observability, and incident response policy

Platform admins are not responsible for:

- Writing user training loops
- Deciding per-job safe training step boundaries
- Selecting checkpoints at runtime for a given RTJ instance

## ML Users

ML users are responsible for:

- Submitting a `ResumableTrainingJob` with a valid queue target and runtime specification
- Using only the supported `v1` workload model: JobSet plus PyTorch DDP or FSDP
- Providing the declared image identity, declared code version, world size, GPU shape, optimizer mode, sharding mode, and checkpoint storage location required by the product contract
- Integrating the training SDK or agent correctly so checkpoint and restore happen through the supported path
- Defining safe training step boundaries in user code
- Exposing the model, optimizer, and related training state needed for DCP checkpoint and restore

ML users are not responsible for:

- Overriding Kueue admission or queue-driven preemption intent
- Relaxing checkpoint compatibility rules
- Bypassing the operator with ad hoc restore logic while still expecting `v1` guarantees

## ResumableTrainingJob Operator

The operator is responsible for:

- Reconciling `ResumableTrainingJob` intent into a single lifecycle state
- Creating, updating, or removing the active JobSet in a way that preserves the invariants in `assumptions-and-invariants.md`
- Accepting manual yield requests and normalizing them with Kueue-driven yield intent
- Tracking checkpoint readiness from persisted metadata, not from in-memory process state
- Selecting only compatible checkpoints for resume under ADR 0003
- Ensuring lifecycle transitions are idempotent across retries, restarts, and repeated reconciliation
- Publishing user-visible status that explains why a workload is `Queued`, `Admitted`, `Starting`, `Running`, `YieldRequested`, `Draining`, `Paused`, `Restoring`, `Succeeded`, or `Failed`

The operator is not responsible for:

- Acting as a scheduler
- Deciding Kueue admission
- Determining user-code safe-point semantics
- Writing DCP checkpoints directly

## Kueue

Kueue is responsible for:

- Queueing workloads
- Admission decisions
- Queue-driven preemption intent

Kueue is not responsible for:

- Managing checkpoint readiness
- Selecting checkpoints
- Performing restore orchestration inside the training runtime

## JobSet

JobSet is responsible for:

- Materializing the distributed worker set for the active runtime instance
- Reconciling the worker Pods and related workload objects
- Reflecting runtime liveness through the existence and state of the active JobSet and its Pods

JobSet is not responsible for:

- Queue policy
- Checkpoint compatibility
- Checkpoint selection
- Resume policy

## Training SDK or Agent

The SDK or agent is responsible for:

- Receiving yield or resume intent from the product control path
- Waiting for the next supported safe training step boundary before checkpointing
- Writing DCP checkpoints to the configured S3-compatible object store
- Emitting checkpoint metadata and completion signals needed by the operator
- Loading the operator-selected checkpoint during resume

The SDK or agent is not responsible for:

- Self-authorizing yield or resume
- Selecting a checkpoint to resume from
- Accepting incompatible checkpoint metadata as valid

## Object Storage

Object storage is responsible for:

- Persisting checkpoint objects that are successfully written
- Serving persisted checkpoint objects during restore
- Enforcing its own availability, durability, and access-control guarantees

Object storage is not responsible for:

- Declaring semantic checkpoint completeness
- Deciding which checkpoint is the correct resume source
- Coordinating training lifecycle transitions

## Responsibility Boundaries That Must Stay True

- Platform admins own environment policy.
- ML users own training semantics and declared workload identity.
- The operator owns lifecycle coordination.
- Kueue owns admission and queue-driven preemption intent.
- JobSet owns runtime workload materialization.
- The SDK or agent owns in-process checkpoint and restore execution.
- Object storage owns durable object persistence, not checkpoint semantics.
