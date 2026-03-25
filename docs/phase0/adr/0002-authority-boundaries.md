# ADR 0002: Authority Boundaries

- Status: Accepted
- Date: 2026-03-19

## Context

The `v1` design uses existing systems and a small amount of product-specific control-plane logic.
If authority boundaries are ambiguous, the implementation will drift into duplicated logic, unsafe overrides, or conflicting state transitions.
This ADR defines which component is authoritative for which decision.

## Decision

The `v1` authority boundaries are as follows.

### Kueue

Kueue is authoritative for:

- Queueing policy
- Admission
- Queue-driven preemption intent

Kueue is not authoritative for:

- Detecting training safe points
- Writing checkpoints
- Selecting a checkpoint for resume
- Orchestrating restore inside a resumed training process

### ResumableTrainingJob Operator

The `ResumableTrainingJob` operator is authoritative for:

- Translating user intent into a resumable training lifecycle
- Accepting manual yield requests
- Reconciling Kueue-driven and manual yield requests into a single desired state
- Validating whether a checkpoint is compatible for a proposed resume under ADR 0003
- Selecting which compatible checkpoint to resume from
- Orchestrating restore by creating or updating the resumed JobSet with the selected checkpoint reference

The operator is not authoritative for:

- Queue admission decisions
- Training-loop safe-point detection
- Byte-level checkpoint creation
- Executing model code

### JobSet

JobSet is authoritative for:

- Materializing and reconciling the distributed worker set
- Managing the Kubernetes workload topology for the in-scope training job
- Restarting or replacing member Pods according to its own reconciliation behavior

JobSet is not authoritative for:

- Queueing or admission policy
- Preemption intent policy
- Checkpoint compatibility
- Checkpoint selection

### Training SDK or Agent

The training SDK or in-pod agent is authoritative for:

- Converting a yield request into in-process runtime behavior
- Invoking DCP writes at supported safe points
- Emitting checkpoint metadata and completion status back to the control plane
- Loading the selected checkpoint during resume inside the training process

The SDK or agent is not authoritative for:

- Declaring that a workload should be admitted or preempted
- Choosing which checkpoint to resume from
- Relaxing compatibility rules

### Object Storage

S3-compatible object storage is authoritative for:

- Persisting checkpoint objects that are successfully written
- Serving persisted checkpoint objects back during restore

Object storage is not authoritative for:

- Determining whether a checkpoint is semantically complete for `v1`
- Choosing the active checkpoint
- Coordinating restore or training execution

### User Training Code

User training code is authoritative for:

- The actual training-step semantics
- Exposing the model, optimizer, and related training state to DCP
- Declaring where the safe training step boundaries exist in the application

User training code is not authoritative for:

- Queue admission
- Queue-driven preemption policy
- Checkpoint selection
- Resume compatibility overrides

## Control Rules

The implementation MUST preserve these control rules:

1. Kueue decisions MUST NOT be overridden by the operator, SDK, or user code.
2. The operator MUST NOT mark a resume as valid unless ADR 0003 compatibility checks pass.
3. JobSet MUST be treated as the runtime carrier, not the product's policy brain.
4. The SDK or agent MUST NOT self-authorize a yield or resume outside operator intent.
5. User code MAY decline to reach a safe point promptly, but it MUST NOT redefine compatibility rules at resume time.
6. Object storage presence alone MUST NOT be treated as proof of checkpoint compatibility.

## Rationale

These boundaries let each subsystem keep the job it is already good at.
Kueue remains the scheduling authority.
JobSet remains the workload reconciler.
The operator handles product-specific coordination.
The SDK or agent handles in-process checkpoint and restore mechanics.
User code retains control of training semantics.

## Consequences

- Implementations can be evaluated against a crisp ownership model.
- Cross-component state machines become simpler because each transition has a clear source of truth.
- Some potentially convenient shortcuts are forbidden, especially treating object existence as semantic success or letting the SDK bypass operator policy.
