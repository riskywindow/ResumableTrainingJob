# Phase 0 Contract Pack

This index is the entry point for the Phase 0 contract pack for the `checkpoint-native preemption controller`.
A new engineer SHOULD be able to use this file to understand:

- what is authoritative for the locked `v1` contract
- what remains conceptual or traceability-only
- what benchmarks and tests define success
- what Phase 1 may build without reopening Phase 0

## Read This First

1. [PHASE0_SIGNOFF.md](PHASE0_SIGNOFF.md)
   Final signoff summary of the locked `v1` contract, non-goals, success metrics, risks, and Phase 1 build allowance.
2. [README.md](README.md)
   High-level explanation of what Phase 0 covers and what "done" means.
3. [prd-v1.md](prd-v1.md)
   Product problem, promise, goals, non-goals, assumptions, risks, and success framing.

## Accepted Decisions

4. [adr/0001-v1-scope.md](adr/0001-v1-scope.md)
   Locks the narrow `v1` scope and explicit non-goals.
5. [adr/0002-authority-boundaries.md](adr/0002-authority-boundaries.md)
   Defines crisp authority boundaries across the control plane, runtime, storage, and user code.
6. [adr/0003-v1-resume-compatibility.md](adr/0003-v1-resume-compatibility.md)
   Locks the strict fail-closed resume-compatibility contract.

## Core System Contracts

7. [system-context.md](system-context.md)
   Component diagram, happy path, and authority matrix.
8. [glossary.md](glossary.md)
   Canonical vocabulary used across the Phase 0 pack.
9. [contracts/actor-responsibilities.md](contracts/actor-responsibilities.md)
   Cross-cutting ownership model.
10. [contracts/assumptions-and-invariants.md](contracts/assumptions-and-invariants.md)
   Product assumptions and non-negotiable invariants.
11. [contracts/reference-environment.md](contracts/reference-environment.md)
   Canonical environment for evaluation and benchmarking.
12. [contracts/resumabletrainingjob-api.md](contracts/resumabletrainingjob-api.md)
   Conceptual `ResumableTrainingJob` surface.
13. [contracts/resumabletrainingjob-status.md](contracts/resumabletrainingjob-status.md)
   Conceptual status surface and condition vocabulary.
14. [contracts/lifecycle-state-machine.md](contracts/lifecycle-state-machine.md)
   Authoritative controller-visible phase enum and transitions.
15. [contracts/yield-resume-protocol.md](contracts/yield-resume-protocol.md)
   Transport-neutral control protocol.
16. [contracts/checkpoint-contract.md](contracts/checkpoint-contract.md)
   Definitions of checkpoint completeness, validity, compatibility, and resumability.
17. [contracts/checkpoint-storage-layout.md](contracts/checkpoint-storage-layout.md)
   Storage layout invariants and manifest-last semantics.
18. [contracts/checkpoint-selection-and-compatibility.md](contracts/checkpoint-selection-and-compatibility.md)
   Latest-compatible-complete selection and fallback rules.
19. [contracts/failure-semantics.md](contracts/failure-semantics.md)
   Failure matrix and fail-closed expectations.
20. [contracts/degraded-behavior.md](contracts/degraded-behavior.md)
   Allowed degraded states and their surfacing rules.
21. [contracts/non-goal-boundaries.md](contracts/non-goal-boundaries.md)
   Boundary between controlled preemption guarantees and out-of-scope crash recovery.

## Conceptual API Artifacts

- [schemas/resumabletrainingjob-phase0.schema.json](schemas/resumabletrainingjob-phase0.schema.json)
  Conceptual RTJ schema aligned to the accepted `v1` contract.
- [examples/resumabletrainingjob-minimal.yaml](examples/resumabletrainingjob-minimal.yaml)
  Minimal conceptual RTJ example focused on required user-authored fields.
- [examples/resumabletrainingjob-full.yaml](examples/resumabletrainingjob-full.yaml)
  Full conceptual RTJ example including controller-authored status.

These files are conceptual review artifacts.
They MUST NOT be mistaken for a finalized production CRD or generated implementation types.

## Evaluation And Exit Pack

- [metrics-success.md](metrics-success.md)
  Accepted numeric success targets for `v1`.
- [benchmarks/reference-workloads.md](benchmarks/reference-workloads.md)
  Required benchmark workload set used to evaluate the targets.
- [test-plan.md](test-plan.md)
  Contract-to-test mapping and required acceptance scenarios.
- [exit-criteria.md](exit-criteria.md)
  Exact artifact and review gates for Phase 0 completion.
- [review/consistency-audit.md](review/consistency-audit.md)
  Full-pack consistency and terminology audit.
- [review/decision-gaps.md](review/decision-gaps.md)
  Real deferred implementation-shaped decisions only.
- [review/design-review-checklist.md](review/design-review-checklist.md)
  Review checklist for Phase 0 approval.
- [review/risk-register.md](review/risk-register.md)
  Known risks to carry into implementation planning.
- [session-handoff.md](session-handoff.md)
  Short final handoff for Phase 1 planning.

## Open Questions

- [open-questions.md](open-questions.md)
  Remaining unresolved questions that are still allowed after Phase 0 signoff.

## Traceability Artifacts

- [schemas/checkpoint-preemption-request.schema.json](schemas/checkpoint-preemption-request.schema.json)
- [examples/checkpoint-preemption-request.example.yaml](examples/checkpoint-preemption-request.example.yaml)
- [adrs/0001-v1-scope-and-contract.md](adrs/0001-v1-scope-and-contract.md)
- [adrs/0002-v1-product-boundaries.md](adrs/0002-v1-product-boundaries.md)

These artifacts remain in the repo for Phase 0 traceability only.
They MUST NOT be treated as the current authoritative `v1` contract direction.

## Usage Rules

- The PRD plus the ADRs under `adr/` are the authoritative product-definition documents for `v1`.
- The contracts, benchmark pack, and exit pack refine those decisions into implementation-planning constraints.
- If a previously accepted Phase 0 decision changes, that change MUST be captured in a new ADR and reflected in [session-handoff.md](session-handoff.md).
