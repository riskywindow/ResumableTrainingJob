# Consistency Audit

- Date: 2026-03-20
- Scope audited: the entire `docs/phase0` directory, including PRD, ADRs, contracts, schemas, examples, benchmark docs, review docs, and traceability artifacts
- Audit intent: detect contradictions, duplicate definitions, ambiguous language, and drift from the locked `v1` scope

## Audit Result

- Blocking contradictions in the authoritative Phase 0 set: `None`
- Duplicate definitions requiring immediate repair: `None`
- Scope drift from the locked `v1` contract in the authoritative Phase 0 set: `None`
- Real deferred gaps: `4`, all implementation-shaped and recorded in [decision-gaps.md](decision-gaps.md)

The authoritative Phase 0 set is internally consistent enough to support Phase 1 planning without reopening product-definition scope.

## Authoritative Boundary

The audit treats the following as authoritative for `v1`:

- `prd-v1.md`
- `adr/0001-v1-scope.md`
- `adr/0002-authority-boundaries.md`
- `adr/0003-v1-resume-compatibility.md`
- `system-context.md`
- all documents under `contracts/`
- `metrics-success.md`
- `benchmarks/reference-workloads.md`
- `test-plan.md`
- `exit-criteria.md`
- the `ResumableTrainingJob` schema and examples
- the review and handoff docs that summarize the accepted contract

The following are audited for traceability but are not authoritative:

- `adrs/0001-v1-scope-and-contract.md`
- `adrs/0002-v1-product-boundaries.md`
- `schemas/checkpoint-preemption-request.schema.json`
- `examples/checkpoint-preemption-request.example.yaml`

Those files intentionally preserve earlier Phase 0 exploration and MUST NOT be treated as the current `v1` contract direction.

## Locked v1 Contract Verified

The audit verified that the authoritative set consistently states all of the following:

- `v1` MUST support exactly one Kubernetes cluster
- Kueue MUST remain authoritative for queueing, admission, and queue-driven preemption intent
- JobSet MUST be the only supported runtime primitive
- PyTorch `DDP` and `FSDP` MUST be the only supported execution modes
- PyTorch `DCP` MUST be the only supported checkpoint mechanism and format
- S3-compatible object storage MUST be the only supported checkpoint storage target
- Graceful yield MUST occur only at training step boundaries
- Resume MUST be fail-closed and MUST require exact match on cluster identity, lineage, runtime mode, world size, GPU shape, image identity, code version, optimizer mode, and sharding mode
- Manual yield and Kueue-driven yield MUST converge on one lifecycle
- Incomplete, invalid, incompatible, or unreadable checkpoints MUST NOT be used for resume

## Terminology Normalization Verified

The following terms are used consistently across the authoritative set:

- `ResumableTrainingJob` or `RTJ`: the conceptual user-facing workload object for the accepted `v1` lifecycle
- `yield`: the controlled runtime action after accepted intent
- `Kueue-driven preemption intent`: the queue-authoritative trigger for a controlled yield path
- `manual yield`: the operator-initiated in-scope yield trigger
- `latest compatible complete checkpoint`: the fixed `v1` selection policy
- `world size`, `GPU shape`, `optimizer mode`, `sharding mode`: required compatibility dimensions
- `Pending`, `Queued`, `Admitted`, `Starting`, `Running`, `YieldRequested`, `Draining`, `Paused`, `Restoring`, `Succeeded`, `Failed`: the authoritative phase enum
- `Admitted`, `Running`, `YieldRequested`, `Draining`, `CheckpointReady`, `ResumeReady`, `Degraded`: the stable conceptual condition set

## Findings

### 1. No blocking contradiction remains in the authoritative Phase 0 set

The PRD, accepted ADRs, contracts, benchmark pack, schemas, examples, and exit criteria all align on the same narrow `v1` boundary.
No authoritative document reopens multi-cluster resume, elastic resize, non-PyTorch frameworks, non-DCP formats, or generalized crash recovery.

### 2. Earlier `CheckpointPreemptionRequest` artifacts remain intentionally non-authoritative

The older `CheckpointPreemptionRequest` schema, example, and superseded `adrs/` documents still describe an earlier exploration path.
That is acceptable because the index and handoff now make their traceability-only status explicit.
They are not a contradiction as long as Phase 1 does not treat them as the implementation baseline.

### 3. The benchmark pack is narrower than the conceptual API examples by design

The required benchmark pack fixes the evaluation environment to world-size-`8` `A100` workloads.
The conceptual `ResumableTrainingJob` examples include a larger `H100` FSDP shape to illustrate the API surface.
This is not contract drift because the examples are conceptual API artifacts, while the benchmark pack is the canonical measurement baseline.

### 4. Remaining gaps are implementation-shaped, not scope-shaped

The unresolved items all concern transport choice, signal shape, and naming standardization.
They do not reopen the locked `v1` contract and are appropriately deferred to Phase 1 or a later ADR.

## Ambiguity Pass

The audit checked normative language across the authoritative set.
Result:

- Normative product, lifecycle, compatibility, failure, and benchmark rules are already expressed predominantly with RFC 2119 language.
- Remaining lowercase narrative uses are descriptive rather than normative and do not weaken the accepted contract.
- New audit, gap, signoff, index, and handoff updates use `MUST`, `SHOULD`, and `MAY` where they establish requirements rather than explanation.

## Phase 1 Guidance From This Audit

Phase 1 MUST treat the following as settled and non-negotiable unless a new ADR explicitly changes them:

- the locked `v1` scope
- the authority boundaries
- the strict compatibility contract
- the fail-closed failure semantics
- the degraded-behavior bounds
- the benchmark workload set and numeric success targets
- the formal Phase 0 exit criteria

Phase 1 MAY choose concrete transports, message shapes, and implementation details only within those bounds.
