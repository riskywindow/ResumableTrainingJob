# Session Handoff

- Date: 2026-03-20
- Scope: Final Phase 0 handoff for `checkpoint-native preemption controller`

## Decisions Made

- The locked `v1` contract is accepted and unchanged: one cluster only, Kueue authority, JobSet-only runtime, PyTorch `DDP` or `FSDP` only, PyTorch `DCP` only, S3-compatible storage only, graceful yield only at training step boundaries, and strict fail-closed resume compatibility.
- The authoritative contract pack now includes the PRD, ADRs, lifecycle, checkpoint, failure, degraded-behavior, benchmark, test-plan, exit-criteria, consistency-audit, and signoff docs.
- The conceptual `ResumableTrainingJob` schema and examples remain accepted as review artifacts only and MUST NOT be treated as a finalized production CRD.
- The benchmark and acceptance bar is now locked: `RW-1`, `RW-2`, and `RW-3`, the explicit numeric targets in `metrics-success.md`, and the execution minimum in `test-plan.md`.
- The only remaining gaps are implementation-shaped and are captured in `review/decision-gaps.md`.

## Files Read

- `docs/phase0/index.md`
- `docs/phase0/README.md`
- `docs/phase0/prd-v1.md`
- `docs/phase0/adr/0001-v1-scope.md`
- `docs/phase0/adr/0002-authority-boundaries.md`
- `docs/phase0/adr/0003-v1-resume-compatibility.md`
- `docs/phase0/adrs/0001-v1-scope-and-contract.md`
- `docs/phase0/adrs/0002-v1-product-boundaries.md`
- `docs/phase0/system-context.md`
- `docs/phase0/contracts/actor-responsibilities.md`
- `docs/phase0/contracts/assumptions-and-invariants.md`
- `docs/phase0/contracts/failure-semantics.md`
- `docs/phase0/contracts/degraded-behavior.md`
- `docs/phase0/contracts/non-goal-boundaries.md`
- `docs/phase0/contracts/reference-environment.md`
- `docs/phase0/open-questions.md`
- `docs/phase0/glossary.md`
- `docs/phase0/schemas/checkpoint-preemption-request.schema.json`
- `docs/phase0/examples/checkpoint-preemption-request.example.yaml`
- `docs/phase0/review/design-review-checklist.md`
- `docs/phase0/review/risk-register.md`
- `docs/phase0/review/consistency-audit.md`
- `docs/phase0/review/decision-gaps.md`
- `docs/phase0/PHASE0_SIGNOFF.md`
- `docs/phase0/metrics-success.md`
- `docs/phase0/benchmarks/reference-workloads.md`
- `docs/phase0/test-plan.md`
- `docs/phase0/exit-criteria.md`
- `docs/phase0/session-handoff.md`
- `docs/phase0/contracts/resumabletrainingjob-api.md`
- `docs/phase0/contracts/resumabletrainingjob-status.md`
- `docs/phase0/contracts/lifecycle-state-machine.md`
- `docs/phase0/contracts/yield-resume-protocol.md`
- `docs/phase0/contracts/checkpoint-contract.md`
- `docs/phase0/contracts/checkpoint-storage-layout.md`
- `docs/phase0/contracts/checkpoint-selection-and-compatibility.md`
- `docs/phase0/schemas/resumabletrainingjob-phase0.schema.json`
- `docs/phase0/examples/resumabletrainingjob-minimal.yaml`
- `docs/phase0/examples/resumabletrainingjob-full.yaml`

## Newly Discovered Gaps Or Contradictions

- No blocking contradiction remains in the authoritative Phase 0 set.
- The older `CheckpointPreemptionRequest` artifacts and the superseded `adrs/` directory remain traceability-only material and MUST NOT drive Phase 1 implementation.
- The remaining gaps are implementation-shaped only and are captured in `review/decision-gaps.md`.

## Recommended Repairs Before Phase 1

- Resolve the concrete Kueue intent signal without weakening Kueue authority.
- Resolve the concrete operator-to-runtime signaling mechanism without weakening request identity, bounded timers, or idempotency rules.
- Choose the final manual-yield control surface through a Phase 1 design or ADR without creating a second semantic path.
- Standardize machine-readable reason, metric, and event names in a way that preserves the accepted failure and benchmark vocabulary.

## Files Changed

- `docs/phase0/index.md`
- `docs/phase0/review/consistency-audit.md`
- `docs/phase0/review/decision-gaps.md`
- `docs/phase0/PHASE0_SIGNOFF.md`
- `docs/phase0/session-handoff.md`

## Open Questions

- What exact Kueue signal or handoff SHOULD the operator consume as the authoritative queue-driven preemption intent?
- What exact operator-to-runtime signaling mechanism SHOULD carry `YieldRequest`, `RestoreRequest`, and their acknowledgements?
- What final manual-yield control surface SHOULD replace or formalize the conceptual `spec.control.desiredState` review field?
- Should Phase 1 freeze machine-readable reason, metric, and event names in an ADR, or leave those names implementation-defined while keeping the semantics fixed?

## Recommended Next Prompt

Use the locked Phase 0 contract pack to start Phase 1 planning: propose the concrete Kueue intent surface, the concrete operator-to-runtime signaling design, and the final manual-yield control surface, while preserving every accepted `v1` scope, lifecycle, compatibility, failure, and benchmark constraint from `PHASE0_SIGNOFF.md`.
