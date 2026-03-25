# Decision Gaps

This file records the real remaining decisions that are intentionally deferred beyond Phase 0.
It MUST stay short.
If a gap reopens the locked `v1` scope, it does not belong here and MUST instead block Phase 0 signoff.

## Deferred Gaps

### 1. Concrete Kueue Intent Surface

- Gap: Phase 0 locks Kueue as the authority for queue-driven preemption intent, but it does not yet choose the exact signal, event, or object handoff the operator MUST consume.
- Why deferred: this is a concrete integration-shape decision, not a scope decision.
- Phase 1 constraint: any chosen signal MUST preserve Kueue authority and MUST NOT invent a competing scheduler or preemption policy path.

### 2. Concrete Operator-To-Runtime Signaling Mechanism

- Gap: Phase 0 defines the conceptual yield and restore protocol, but it does not yet standardize the concrete transport between the operator and the SDK or agent.
- Why deferred: this is a transport and implementation-shape choice.
- Phase 1 constraint: any chosen mechanism MUST preserve request identity, bounded timers, restart-safe idempotency, and fail-closed completion rules.

### 3. Final Manual Yield Control Surface

- Gap: the conceptual `spec.control.desiredState` field is accepted only as a Phase 0 review surface; it is not yet the final transport decision for manual yield.
- Why deferred: this is an API-shape decision that may require a later ADR.
- Phase 1 constraint: any chosen surface MUST preserve the accepted lifecycle semantics and MUST NOT create a second manual-control path with different behavior from Kueue-driven yield.

### 4. Standardized Reason, Metric, And Event Names

- Gap: Phase 0 locks the semantics for failure, degradation, and benchmarks, but it does not yet freeze the exact machine-readable reason names, metric names, or event names that implementations MUST emit.
- Why deferred: this is an observability-shape and API-detail decision.
- Phase 1 constraint: any chosen names SHOULD align closely with the existing failure and benchmark vocabulary and MUST NOT hide or reinterpret the accepted semantics.

## Non-Gaps

The following are not open decisions and MUST NOT be reopened in Phase 1 without a new ADR:

- single-cluster scope
- Kueue authority
- JobSet-only runtime support
- PyTorch `DDP` and `FSDP` only
- PyTorch `DCP` only
- S3-compatible storage only
- step-boundary-only graceful yield
- strict same-identity resume compatibility
- fail-closed incomplete-checkpoint handling
- the required benchmark workload set and numeric success targets
