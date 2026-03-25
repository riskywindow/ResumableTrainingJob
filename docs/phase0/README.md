# Checkpoint-Native Preemption Controller Phase 0

Phase 0 is the product-definition stage for the `checkpoint-native preemption controller`.
This directory exists to define the narrow `v1` problem, bind the first-release scope, capture accepted architecture decisions, and document unresolved questions before any controller or operator implementation begins.

Phase 0 artifacts MUST stay at the level of documentation, ADRs, schemas, example YAML, and review materials.
They MUST NOT introduce implementation code, controller logic, SDK runtime behavior, or production Kubernetes resources.

## What Phase 0 Covers

For `v1`, Phase 0 covers a deliberately narrow product:

- A single Kubernetes cluster only
- Kueue as the authority for queueing, admission, and preemption intent
- JobSet as the only runtime or orchestration primitive
- PyTorch DDP and FSDP only
- PyTorch Distributed Checkpoint (DCP) only
- S3-compatible object storage only
- Graceful yield only at training step boundaries
- Resume only when cluster, image identity, declared code version, world size, GPU shape, optimizer mode, and sharding mode all match the declared request
- Both manual yield and Kueue-driven yield

## What "Done" Means

Phase 0 is done when all of the following are true:

- The `v1` product boundary is explicit and internally consistent across the PRD, ADRs, glossary, and open questions.
- A new engineer can read the docs in order and understand the product promise, reference environment, and non-goals without reading code.
- Decisions that materially constrain `v1` are captured as accepted ADRs.
- Open questions contain only real unresolved items that matter to design review.
- Review artifacts are sufficient to start implementation planning without reopening basic scope debates.

Phase 0 is not done merely because a schema or example YAML exists.
It is done when the product definition is specific enough to guide implementation and narrow enough to reject out-of-scope work.

## Reading Order

Start with these documents:

1. `index.md`
2. `prd-v1.md`
3. `adr/0001-v1-scope.md`
4. `adr/0002-authority-boundaries.md`
5. `adr/0003-v1-resume-compatibility.md`
6. `system-context.md`
7. `contracts/actor-responsibilities.md`
8. `contracts/assumptions-and-invariants.md`
9. `contracts/reference-environment.md`
10. `open-questions.md`
11. `glossary.md`

Then review supporting artifacts as needed:

- `schemas/checkpoint-preemption-request.schema.json`
- `examples/checkpoint-preemption-request.example.yaml`
- `contracts/resumabletrainingjob-api.md`
- `contracts/resumabletrainingjob-status.md`
- `contracts/lifecycle-state-machine.md`
- `contracts/failure-semantics.md`
- `contracts/degraded-behavior.md`
- `contracts/non-goal-boundaries.md`
- `metrics-success.md`
- `benchmarks/reference-workloads.md`
- `test-plan.md`
- `exit-criteria.md`
- `contracts/yield-resume-protocol.md`
- `contracts/checkpoint-contract.md`
- `contracts/checkpoint-storage-layout.md`
- `contracts/checkpoint-selection-and-compatibility.md`
- `schemas/resumabletrainingjob-phase0.schema.json`
- `examples/resumabletrainingjob-minimal.yaml`
- `examples/resumabletrainingjob-full.yaml`
- `review/design-review-checklist.md`
- `review/risk-register.md`
- `session-handoff.md`

## RFC 2119 Usage

The Phase 0 artifacts use the key words `MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, and `MAY` as described in RFC 2119.
