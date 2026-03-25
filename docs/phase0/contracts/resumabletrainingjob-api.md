# ResumableTrainingJob API Contract

This document defines the conceptual Phase 0 API contract for a `ResumableTrainingJob` (`RTJ`) object.
It is intentionally CRD-shaped so Phase 0 reviewers can reason about the user-facing surface area, but it MUST be treated as a conceptual artifact only.
It does not finalize a production Kubernetes API, controller behavior, or generated types.

## Contract Intent

For `v1`, the `RTJ` contract MUST stay inside the accepted Phase 0 scope:

- One Kubernetes cluster only
- Kueue as the authority for queueing, admission, and queue-driven preemption intent
- JobSet as the only supported runtime primitive
- PyTorch DDP or FSDP only
- PyTorch DCP to S3-compatible object storage only
- Graceful yield only at training step boundaries
- Resume only from the latest compatible complete checkpoint under ADR 0003
- Resume only when image identity, code version identity, world size, GPU shape, optimizer mode, and sharding mode still match

This contract exists to make that scope reviewable in an RTJ-shaped object model.

## Top-Level Shape

The conceptual resource shape is:

- `apiVersion`
- `kind`
- `metadata`
- `spec`
- `status`

`spec` is the user-authored desired state.
`status` is the controller-published observed state.

## Authorship Model

- User-authored fields: `metadata.name`, `metadata.namespace`, user-provided labels and annotations, and all of `spec`
- Controller-authored fields: `status.conditions`, `status.currentRunAttempt`, `status.reason`, `status.message`, `status.observedGeneration`
- Derived fields: `status.phase`, `status.selectedCheckpoint`, `status.lastCompletedCheckpoint`, and `status.transitionTimestamps`

Derived fields are still published by the controller, but they are computed from persisted cluster state, checkpoint metadata, and the fixed `v1` policy rules rather than copied directly from user input.

## `spec` Contract

### Scheduling

| Field | Authorship | Required | v1 Rule |
| --- | --- | --- | --- |
| `spec.queueName` | User-authored | Yes | MUST identify the Kueue queue the RTJ targets. |
| `spec.workloadPriorityClassName` | User-authored | Yes | MUST identify the workload priority class used for admission and preemption policy. |

### Workload Identity

| Field | Authorship | Required | v1 Rule |
| --- | --- | --- | --- |
| `spec.identity.image` | User-authored | Yes | MUST declare the training image identity used for resume compatibility. |
| `spec.identity.codeVersion` | User-authored | Yes | MUST declare the code version used for resume compatibility. |
| `spec.identity.worldSize` | User-authored | Yes | MUST be a fixed positive integer and MUST match on resume. |
| `spec.identity.gpuShape` | User-authored | Yes | MUST declare the GPU shape and MUST match on resume. |

### Runtime

| Field | Authorship | Required | v1 Rule |
| --- | --- | --- | --- |
| `spec.runtime.mode` | User-authored | Yes | MUST be `DDP` or `FSDP`. |
| `spec.runtime.optimizerMode` | User-authored | Yes | MUST declare the optimizer-state mode and MUST match on resume. |
| `spec.runtime.shardingMode` | User-authored | Yes | MUST declare the sharding mode and MUST match on resume. |
| `spec.runtime.templateRef` | User-authored | Conditionally | MUST reference a JobSet-compatible runtime template when used. |
| `spec.runtime.template` | User-authored | Conditionally | MUST embed a conceptual JobSet-compatible runtime template when used. |

Exactly one of `spec.runtime.templateRef` or `spec.runtime.template` MUST be present.
The effective runtime template MUST resolve to a JobSet workload, because JobSet is the only supported runtime primitive in `v1`.

### Checkpoint Policy

| Field | Authorship | Required | v1 Rule |
| --- | --- | --- | --- |
| `spec.checkpoint.storageURI` | User-authored | Yes | MUST be an `s3://` URI or equivalent S3-compatible object-store URI. |
| `spec.checkpoint.interval` | User-authored | Yes | MUST define the nominal periodic checkpoint cadence while the RTJ is running. |
| `spec.checkpoint.freshnessBudget` | User-authored | Yes | MUST define the maximum acceptable age of the most recent completed checkpoint while the RTJ is healthy. |
| `spec.checkpoint.maxDrainTime` | User-authored | Yes | MUST define a bounded graceful-yield window from accepted yield intent to a paused runtime. |
| `spec.checkpoint.safePointMode` | User-authored | Yes | MUST be `StepBoundary` in `v1`. |

For Phase 0, `checkpoint.freshnessBudget` is a policy declaration, not a promise that an RTJ can always avoid taking a fresh checkpoint during yield.
The accepted `v1` product behavior still assumes graceful yield occurs at a training step boundary and that the supported runtime writes DCP checkpoint data through the in-pod SDK or agent.

### Resume Policy

| Field | Authorship | Required | v1 Rule |
| --- | --- | --- | --- |
| `spec.resume.sourcePolicy` | User-authored | Yes | MUST be `LatestCompatibleComplete` in `v1`. |
| `spec.resume.maxResumeRetries` | User-authored | Yes | MUST bound how many controller-managed resume attempts are allowed before the RTJ is treated as failed. |

`spec.resume.sourcePolicy` is fixed to `LatestCompatibleComplete` so `v1` does not expose user-directed checkpoint selection.
The controller MUST reject or ignore any attempt to resume from an incompatible or incomplete checkpoint.

### Optional Manual Control Surface

| Field | Authorship | Required | v1 Rule |
| --- | --- | --- | --- |
| `spec.control.desiredState` | User-authored | No | MAY be used to express declarative manual intent. If present, it MUST be `Running` or `Paused`. |

For this conceptual Phase 0 pack, `spec.control.desiredState: Paused` represents manual yield intent and `Running` represents run or resume intent.
This field does not finalize a future subresource, CLI, or admission workflow.

## Validation Rules

- `spec.identity.worldSize` MUST be greater than zero.
- `spec.checkpoint.interval`, `spec.checkpoint.freshnessBudget`, and `spec.checkpoint.maxDrainTime` MUST be bounded duration strings.
- `spec.checkpoint.freshnessBudget` SHOULD be greater than or equal to `spec.checkpoint.interval`.
- `spec.runtime.mode` plus the effective runtime template MUST describe a PyTorch DDP or FSDP JobSet workload.
- `spec.runtime.optimizerMode` and `spec.runtime.shardingMode` MUST be explicit because strict `v1` compatibility requires exact matches for both.
- `spec.resume.maxResumeRetries` SHOULD be small and explicit so failure semantics remain reviewable.
- User-authored fields MUST NOT override the accepted compatibility rules from ADR 0003.

## What This Contract Deliberately Does Not Define

- The concrete Kueue signal used to express queue-driven preemption intent
- The concrete operator-to-runtime signaling mechanism for yield
- The detailed S3 object layout or retention contract
- The full nested JobSet schema carried by `spec.runtime.template`
- Generated CRD manifests, controller code, or admission webhooks

Those details remain Phase 0 follow-up work or later implementation work.
