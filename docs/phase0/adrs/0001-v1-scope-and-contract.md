# ADR 0001: Narrow v1 Scope and Request Contract

- Status: Accepted
- Date: 2026-03-19

## Context

The project needs a Phase 0 definition for a `checkpoint-native preemption controller`.
The main design risk is over-expanding `v1` into a broad workload orchestration platform before the minimal contract is clear.
Phase 0 therefore needs a narrow decision that constrains both scope and interface shape.

## Decision

The project will adopt the following `v1` decisions:

1. `v1` MUST center on a single conceptual request document named `CheckpointPreemptionRequest`.
2. The request contract MUST describe checkpoint-first preemption intent for exactly one workload target.
3. The Phase 0 contract MUST remain transport-neutral.
4. The request MAY later be carried by a CRD, API endpoint, or message, but Phase 0 will not standardize that transport.
5. The controller responsibility in `v1` MUST be limited to the conceptual lifecycle:
   `accept request -> validate eligibility -> request checkpoint -> wait for bounded readiness -> permit preemption or report failure`.
6. `v1` MUST assume the checkpoint mechanism itself is provided externally by an existing runtime, integration, or future subsystem.
7. `v1` MUST NOT include restore orchestration, migration, placement selection, or batch disruption coordination.
8. Only one active request per target SHOULD be allowed in `v1`.
9. The request SHOULD require an explicit checkpoint deadline and a preemption mode to make failure semantics reviewable.

## Rationale

This decision keeps the first contract reviewable and avoids binding the project to Kubernetes API machinery too early.
It also makes failure semantics visible: if checkpoint readiness is not achieved within a bounded deadline, the request can be rejected, expired, or marked failed without implying undefined preemption behavior.

## Consequences

Positive consequences:

- The API surface is small enough for design review.
- Transport neutrality preserves optionality for later integration choices.
- Runtime-specific checkpoint logic remains clearly out of scope for controller `v1`.

Negative consequences:

- The contract does not yet answer restore or resumption behavior.
- Some users may expect scheduler or eviction integration that is intentionally deferred.
- Additional ADRs will be required before any concrete API, status model, or multi-target semantics are accepted.

## Follow-Up

- Phase 0 SHOULD define a conceptual schema for `CheckpointPreemptionRequest`.
- Phase 0 SHOULD provide at least one example YAML document for design review.
- A later ADR MUST be added before adopting a concrete transport such as a CRD.
