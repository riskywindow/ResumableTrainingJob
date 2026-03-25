# Yield and Resume Protocol

This document defines the conceptual Phase 0 control protocol for controlled yield and resume in `v1`.
It is transport-neutral by design.
It does not choose a concrete wire format, sidecar API, RPC mechanism, CRD subresource, or Pod-local signaling path.

## Protocol Goals

The `v1` protocol MUST support:

- operator-accepted manual yield
- operator-observed Kueue-driven yield
- explicit runtime acknowledgement
- step-boundary-only checkpointing
- bounded drain and restore windows
- idempotent retries across operator restarts and repeated reconciliation

The protocol MUST remain consistent with ADR 0002 and ADR 0003:
the operator owns lifecycle coordination and checkpoint selection, while the SDK or agent owns in-process checkpoint and restore execution.

## Conceptual Control Exchanges

The protocol is expressed as conceptual exchanges between the operator and the SDK or agent.
The implementation MAY realize them through annotations, files, RPC, HTTP, gRPC, or another mechanism in a later phase.

The minimum conceptual exchanges are:

- `YieldRequest`
- `YieldAck`
- `RuntimeHeartbeat`
- `DrainComplete`
- `DrainFailed`
- `RestoreRequest`
- `RestoreAck`
- `RestoreComplete`
- `RestoreFailed`

Each exchange SHOULD carry stable identifiers sufficient for idempotency:

- RTJ lineage identity
- run attempt number
- request identifier
- timestamp
- controller generation or equivalent intent epoch

## Yield Triggers

`v1` supports exactly two yield-trigger sources:

- manual yield accepted by the operator
- Kueue-driven preemption intent observed by the operator

Regardless of source, once the operator accepts the yield intent it MUST normalize both flows into one protocol path:

1. Publish `YieldRequest`
2. Wait for `YieldAck`
3. Wait for step-boundary drain and checkpoint completion
4. Converge the RTJ to `Paused`

The runtime MUST NOT self-authorize yield without an operator-issued or operator-normalized yield request.

## Yield Acknowledgement Semantics

`YieldAck` means only that the runtime has observed the current yield request and accepted responsibility to begin controlled drain processing.
It does not mean the checkpoint is complete.
It does not mean a safe point has been reached.

The ack MUST be scoped to:

- the current RTJ lineage
- the current run attempt
- the latest yield request identifier

The runtime MUST treat duplicate delivery of the same `YieldRequest` as idempotent and MUST return the same effective ack state.
If the runtime receives a stale yield request for an older attempt or older request identifier, it MUST ignore it and SHOULD report the staleness in diagnostics.

## Safe-Point Semantics

For `v1`, `safePointMode` is fixed to `StepBoundary`.
That means:

- checkpoint creation MUST begin only at a user-defined training step boundary
- the runtime MUST NOT claim `DrainComplete` before checkpoint completion evidence exists
- the operator MUST treat any non-step-boundary completion claim as invalid

The runtime MAY continue forward progress until the next supported training step boundary after acknowledging yield.
The runtime MUST NOT reinterpret "step boundary" at restore time in a way that weakens the accepted compatibility rules.

## Drain Semantics

After `YieldAck`, the runtime enters controlled drain behavior:

1. Continue until the next safe training step boundary.
2. Write the DCP checkpoint to the configured S3-compatible storage URI.
3. Publish checkpoint metadata and completion evidence.
4. Stop active training execution so the operator can converge the RTJ to `Paused`.

The operator MUST treat drain as incomplete until it has both:

- runtime-reported completion for the current yield request
- persisted checkpoint evidence sufficient for the `v1` completeness and compatibility contract

If `maxDrainTime` expires before those conditions are met, the operator MUST fail closed and transition the RTJ to `Failed`.

## Restore Semantics

Resume in `v1` uses controlled restore, not arbitrary restart.

Before issuing `RestoreRequest`, the operator MUST:

- ensure the RTJ is currently `Paused`
- obtain valid Kueue admission for the new attempt
- select the latest compatible complete checkpoint under ADR 0003

`RestoreRequest` MUST identify:

- the new run attempt
- the selected checkpoint reference
- the restore deadline

`RestoreAck` means only that the runtime has observed the restore intent and begun restore processing.
`RestoreComplete` means the runtime has loaded the selected checkpoint and is ready to resume training for the current attempt.

If restore evidence is missing, incompatible, or late, the operator MUST fail closed.

## Runtime Heartbeats

The runtime SHOULD emit a bounded heartbeat while `Starting`, `Running`, `Draining`, and `Restoring`.
Heartbeats SHOULD include enough information for the operator to detect liveness and stale control state, including:

- run attempt
- last observed request identifier
- runtime sub-state such as `Running`, `WaitingForSafePoint`, `Checkpointing`, or `Restoring`
- last safe-point observation time when available
- last completed checkpoint identifier when available

Heartbeat loss alone does not prove failure, but the operator MUST use bounded staleness rules rather than indefinite waiting.

## Idempotency Expectations

The protocol MUST remain correct across retries, duplicated delivery, and operator restarts.

The operator MUST:

- reuse the same yield request identifier when replaying the same unresolved intent
- avoid creating a second active JobSet for the same RTJ lineage
- derive current protocol state from persisted RTJ status and checkpoint metadata after restart

The runtime MUST:

- treat duplicate `YieldRequest` and `RestoreRequest` messages for the same request identifier as idempotent
- avoid writing a second logical completion record for the same completed request unless the transport requires replay
- report the latest known state for a request when asked again after restart

Object storage presence alone MUST NOT be treated as idempotent proof of success.
The operator still MUST require the expected completion metadata for the current request and attempt.

## Required Timers

The protocol uses bounded timers so `v1` does not wait indefinitely on ambiguous runtime state.

| Timer | Default | Meaning | Rationale |
| --- | --- | --- | --- |
| `heartbeatInterval` | `15s` | Recommended cadence for runtime liveness reports while active | Short enough for prompt stale-state detection without creating excessive control-plane churn |
| `heartbeatStaleAfter` | `45s` | Operator SHOULD treat heartbeat state as stale after three missed intervals | Keeps failure detection bounded while tolerating transient delays |
| `yieldAckTimeout` | `30s` | Maximum time from `YieldRequest` publication to valid `YieldAck` | Distinguishes "request observed" from "drain completed" and prevents silent indefinite waiting |
| `maxDrainTime` | `900s` | Maximum time from accepted yield to completed checkpoint and paused runtime | Matches the narrow `v1` goal of bounded graceful yield while allowing step-boundary delay plus DCP write time |
| `restoreAckTimeout` | `30s` | Maximum time from `RestoreRequest` publication to valid `RestoreAck` | Makes restore initiation explicit and bounded |
| `restoreTimeout` | `1200s` | Maximum time from `RestoreRequest` to `RestoreComplete` and running heartbeat | Allows checkpoint discovery, DCP restore, and runtime convergence without indefinite waiting |

These values are conceptual Phase 0 defaults.
A later implementation MAY make them configurable, but the actual chosen values MUST remain explicit and bounded.

`spec.checkpoint.maxDrainTime` is user-authored in the RTJ contract.
If a later transport allows omission, the platform SHOULD default it to `900s` unless policy requires a stricter bound.

## Failure Handling Rules

- Missing `YieldAck` by `yieldAckTimeout` MUST transition the RTJ to `Failed`.
- Missing drain completion by `maxDrainTime` MUST transition the RTJ to `Failed`.
- Missing `RestoreAck` by `restoreAckTimeout` SHOULD be treated as restore failure for the current attempt.
- Missing `RestoreComplete` by `restoreTimeout` MUST fail the current restore attempt and MUST honor `maxResumeRetries`.
- Incomplete or incompatible checkpoint evidence MUST fail closed, even if object paths exist.

## PreStop Constraint

Kubernetes `PreStop` MAY be used only for lightweight coordination such as:

- setting an in-process flag
- nudging the runtime to flush final lightweight metadata
- helping the runtime notice that termination is imminent

`PreStop` MUST NOT be the primary mechanism for heavy checkpoint creation in `v1`.
Heavy DCP checkpoint creation MUST occur through the normal yield protocol while the runtime is still fully active, because `PreStop` timing is too constrained and too termination-coupled to serve as the main checkpoint path reliably.

## What This Protocol Does Not Yet Standardize

- the concrete Kueue signal consumed by the operator
- the concrete operator-to-runtime transport
- per-worker versus leader-only acknowledgement shape
