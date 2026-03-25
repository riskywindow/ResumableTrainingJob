# Exit Criteria

This document defines the exact gates for declaring Phase 0 complete for the `checkpoint-native preemption controller`.
Phase 0 is complete only when every required artifact exists and every required review outcome below is satisfied.

## Required Artifact Set

### Core Product Definition

The following files MUST exist and remain internally consistent:

- `docs/phase0/index.md`
- `docs/phase0/README.md`
- `docs/phase0/prd-v1.md`
- `docs/phase0/open-questions.md`
- `docs/phase0/glossary.md`
- `docs/phase0/system-context.md`

### Accepted ADR Set

The following ADRs MUST exist and remain the authoritative decision set:

- `docs/phase0/adr/0001-v1-scope.md`
- `docs/phase0/adr/0002-authority-boundaries.md`
- `docs/phase0/adr/0003-v1-resume-compatibility.md`

### Contract Set

The following contract documents MUST exist:

- `docs/phase0/contracts/actor-responsibilities.md`
- `docs/phase0/contracts/assumptions-and-invariants.md`
- `docs/phase0/contracts/reference-environment.md`
- `docs/phase0/contracts/resumabletrainingjob-api.md`
- `docs/phase0/contracts/resumabletrainingjob-status.md`
- `docs/phase0/contracts/lifecycle-state-machine.md`
- `docs/phase0/contracts/failure-semantics.md`
- `docs/phase0/contracts/degraded-behavior.md`
- `docs/phase0/contracts/non-goal-boundaries.md`
- `docs/phase0/contracts/yield-resume-protocol.md`
- `docs/phase0/contracts/checkpoint-contract.md`
- `docs/phase0/contracts/checkpoint-storage-layout.md`
- `docs/phase0/contracts/checkpoint-selection-and-compatibility.md`

### Conceptual Artifact Set

The following conceptual schema and example files MUST exist:

- `docs/phase0/schemas/checkpoint-preemption-request.schema.json`
- `docs/phase0/schemas/resumabletrainingjob-phase0.schema.json`
- `docs/phase0/examples/checkpoint-preemption-request.example.yaml`
- `docs/phase0/examples/resumabletrainingjob-minimal.yaml`
- `docs/phase0/examples/resumabletrainingjob-full.yaml`

### Review, Benchmark, And Exit Pack

The following planning and review artifacts MUST exist:

- `docs/phase0/review/design-review-checklist.md`
- `docs/phase0/review/risk-register.md`
- `docs/phase0/metrics-success.md`
- `docs/phase0/benchmarks/reference-workloads.md`
- `docs/phase0/test-plan.md`
- `docs/phase0/exit-criteria.md`
- `docs/phase0/session-handoff.md`

## Required Review Outcomes

### 1. Scope Outcome

Reviewers MUST accept that the authoritative Phase 0 set still describes exactly the narrow `v1` scope:

- one cluster only
- Kueue authority
- JobSet only
- PyTorch `DDP` or `FSDP` only
- PyTorch `DCP` only
- S3-compatible object storage only
- graceful yield only at step boundaries
- strict fail-closed same-identity resume

Any disagreement that reopens those boundaries blocks Phase 0 exit.

### 2. Authority And Lifecycle Outcome

Reviewers MUST accept that authority boundaries, lifecycle phases, status vocabulary, and invariants are specific enough to guide implementation planning.

At minimum, reviewers MUST agree that:

- phase and condition semantics are unambiguous
- the single-active-runtime invariant is explicit
- manual yield and Kueue-driven yield converge on one lifecycle
- the operator, Kueue, JobSet, SDK or agent, object storage, and user code each have clear responsibilities

### 3. Failure And Boundary Outcome

Reviewers MUST accept that the authoritative Phase 0 set makes failure and non-goal boundaries explicit.

At minimum, reviewers MUST agree that:

- ambiguous drain, checkpoint, and restore outcomes fail closed
- allowed degraded behaviors are bounded and surfaced explicitly
- best-effort crash recovery is not misrepresented as an in-scope guarantee
- forced termination and node-loss scenarios are clearly classified

### 4. Measurement Outcome

Reviewers MUST accept the explicit numeric targets in `metrics-success.md` and the required benchmark workload set in `benchmarks/reference-workloads.md`.

At minimum, reviewers MUST agree that:

- the targets are numeric and not left as `TBD`
- the required workload set is representative of the accepted `v1` scope
- sample sizes and benchmark profiles are specific enough for later execution

### 5. Test-Planning Outcome

Reviewers MUST accept that `test-plan.md` maps every major contract to at least one validation test or acceptance scenario.

At minimum, reviewers MUST agree that the plan includes:

- functional happy-path tests
- compatibility rejection tests
- restore validation tests
- repeated controlled-cycle tests
- chaos-style docs-only scenarios

### 6. Review Artifact Outcome

The following review conditions MUST be true:

- `review/design-review-checklist.md` has no untracked blocker
- `review/risk-register.md` has been revisited against the current contract set
- `session-handoff.md` reflects the latest decisions, files changed, open questions, and recommended next prompt

## Open-Question Policy

Phase 0 MAY exit with a small number of open questions only if all of the following are true:

- every open question is recorded in both `open-questions.md` and `session-handoff.md`
- no open question contradicts an accepted ADR
- no open question reopens the `v1` scope, strict compatibility rules, failure boundaries, benchmark workload set, or numeric success targets
- remaining questions are limited to concrete transport or implementation-shape choices

## Blockers

Any of the following MUST block Phase 0 completion:

- a missing required artifact from this document
- contradictory phase, condition, or compatibility language across authoritative docs
- a metric target, benchmark workload, or test requirement left as `TBD`
- an unresolved disagreement on what counts as in-scope controlled preemption versus out-of-scope crash recovery
- a missing update to `session-handoff.md` after a material decision change

## Phase 0 Completion Statement

Phase 0 is complete only when:

1. every required artifact in this document exists,
2. every required review outcome has been accepted,
3. all remaining open questions are implementation-shaped rather than scope-shaped, and
4. the authoritative Phase 0 set is specific enough to start implementation planning without reopening basic product-definition debates.
