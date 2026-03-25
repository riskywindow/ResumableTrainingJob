# Non-Goal Boundaries

This document defines the boundary between the in-scope `v1` controlled-preemption guarantee and out-of-scope best-effort recovery behavior.
The purpose is to keep product claims precise before implementation begins.

## Controlled Preemption Guarantee

For `v1`, the product guarantees a narrow controlled-preemption path only when all of the following remain true:

- the workload is in the accepted `v1` scope
- Kueue or manual operator action expresses yield intent through the accepted control path
- the active runtime remains alive long enough to acknowledge the request and reach a safe training step boundary
- the runtime writes a complete, valid checkpoint through the supported DCP path
- the operator can verify checkpoint completeness and compatibility from persisted evidence
- resume later occurs only with valid admission and a complete, valid, compatible checkpoint

Within that boundary, `v1` MUST:

- normalize manual yield and Kueue-driven yield into one lifecycle
- use bounded timers for ack, drain, and restore
- fail closed when checkpoint or restore evidence is ambiguous
- preserve the single-active-runtime invariant
- recover safely across operator restarts from persisted truth

## Best-Effort Recovery Is Not The Same Thing

The following behavior MAY still happen operationally, but it is not part of the controlled-preemption guarantee:

- a runtime crashes and an older completed compatible checkpoint still exists
- a node is lost and the operator later attempts to resume from a previously completed checkpoint
- a restore attempt is retried within `maxResumeRetries`
- the operator surfaces enough diagnostics for a human or later automation to decide what to do next

Those are best-effort recovery paths.
They MUST NOT be presented as proof that `v1` guarantees generalized crash recovery.

## Boundary Table

| Scenario | Classification | What `v1` MUST do | What `v1` MUST NOT claim |
| --- | --- | --- | --- |
| Yield is requested, the runtime acknowledges, reaches a step boundary, writes a complete valid checkpoint, and the operator converges `Paused`. | In-scope controlled preemption | Treat the yield as successful and surface the completed checkpoint. | It MUST NOT imply portability beyond the strict compatibility contract. |
| The operator restarts during yield or restore, but persisted state is intact. | In-scope controlled preemption | Recover idempotently from persisted state and keep one active runtime. | It MUST NOT create a duplicate runtime or duplicate logical request. |
| The newest checkpoint is bad but an older compatible complete checkpoint exists. | In-scope controlled restore fallback | Skip the bad checkpoint, surface degradation, and select only an older compatible complete checkpoint. | It MUST NOT resume from the bad checkpoint or let the user override compatibility. |
| A Pod, JobSet, or node crashes before the current drain finishes. | Out-of-scope best-effort crash recovery | Surface failure clearly, preserve any older completed checkpoint references, and stop unsafe progress. | It MUST NOT report the current controlled yield as successful. |
| The platform hard-terminates the runtime after drain deadline expiry. | Out-of-scope forced termination | Surface that graceful yield did not complete and preserve only previously completed checkpoints. | It MUST NOT report the termination as a successful checkpoint-and-yield. |
| A checkpoint is incomplete, corrupt, unsupported, or incompatible. | In-scope fail-closed rejection | Reject it for resume and either fall back safely or fail closed. | It MUST NOT guess through missing data or relax compatibility. |
| Storage retention or outage removes required checkpoint evidence. | Out-of-scope for successful resume, but in-scope for fail-closed handling | Surface the blocking condition and refuse restore until valid evidence exists. | It MUST NOT invent missing objects or infer success from path shape alone. |
| Resume is attempted with different cluster identity, image, code version, world size, GPU shape, optimizer mode, or sharding mode. | Out-of-scope portability | Reject the resume as incompatible. | It MUST NOT attempt adaptive or reshaped resume in `v1`. |
| User code or an external script performs ad hoc restore outside the supported SDK or agent path. | Out-of-scope execution path | Treat the product guarantee as not applicable to that restore path. | It MUST NOT claim product-managed safety for a bypassed runtime path. |

## Explicit Non-Goals Reaffirmed

The following remain non-goals for `v1`:

- generalized node-failure recovery as a product guarantee
- transparent process, container, or CUDA snapshot behavior
- multi-cluster resume
- dynamic world-size change on resume
- adaptive resume across different GPU shapes
- non-PyTorch frameworks
- non-DCP checkpoint formats
- replacing Kueue with a custom scheduler

## Practical Review Rule

If a proposed Phase 1 design relies on the system succeeding after unexpected runtime crash, node loss, forced Pod termination, or missing checkpoint evidence, that design is crossing out of the controlled-preemption contract.
It MUST either:

- be rejected as out of scope for `v1`, or
- be introduced by a later ADR that explicitly expands the product boundary.
