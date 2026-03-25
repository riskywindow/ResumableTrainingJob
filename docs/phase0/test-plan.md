# Test Plan

This document defines the Phase 0 validation and acceptance plan for the `v1` contract.
It maps the major Phase 0 contracts to explicit validation tests, acceptance tests, and docs-only chaos scenarios.

Phase 0 does not execute controller code.
This plan exists so later implementation work can be measured against a concrete and reviewable test matrix.

## Test Categories

| Category | Meaning |
| --- | --- |
| `DV` | Design-validation review performed against the Phase 0 documents themselves. |
| `FT` | Functional acceptance scenario for the in-scope happy path. |
| `CT` | Compatibility or checkpoint-integrity rejection scenario. |
| `RT` | Restore-path validation scenario. |
| `RC` | Repeated controlled-cycle validation against numeric targets. |
| `CH` | Chaos-style docs-only scenario used to validate failure and degraded semantics. |
| `OB` | Operability and status-surfacing validation. |

## Contract-To-Test Matrix

| Contract or artifact | Required tests |
| --- | --- |
| `prd-v1.md` | `DV-01`, `FT-01`, `FT-02`, `RC-01`, `RC-02` |
| `adr/0001-v1-scope.md` | `DV-01`, `CH-03` |
| `adr/0002-authority-boundaries.md` | `DV-02`, `FT-01`, `FT-02` |
| `adr/0003-v1-resume-compatibility.md` | `CT-01`, `RT-01`, `RT-02` |
| `system-context.md` | `DV-02`, `FT-01`, `FT-02` |
| `contracts/actor-responsibilities.md` | `DV-02` |
| `contracts/assumptions-and-invariants.md` | `CH-01`, `RC-01`, `RC-02` |
| `contracts/reference-environment.md` | `DV-03`, `RC-01`, `RC-02` |
| `contracts/resumabletrainingjob-api.md` | `DV-04` |
| `contracts/resumabletrainingjob-status.md` | `OB-01`, `FT-01`, `FT-02`, `CH-02` |
| `contracts/lifecycle-state-machine.md` | `FT-01`, `FT-02`, `RT-01`, `CH-01` |
| `contracts/yield-resume-protocol.md` | `FT-01`, `FT-02`, `CH-01`, `CH-02` |
| `contracts/checkpoint-contract.md` | `CT-02`, `CT-03`, `RT-01` |
| `contracts/checkpoint-storage-layout.md` | `CT-02`, `CT-03` |
| `contracts/checkpoint-selection-and-compatibility.md` | `CT-01`, `CT-03`, `RT-01`, `RT-02` |
| `contracts/failure-semantics.md` | `CH-01`, `CH-02`, `CH-03`, `RT-02` |
| `contracts/degraded-behavior.md` | `OB-01`, `CH-02` |
| `contracts/non-goal-boundaries.md` | `DV-01`, `CH-03` |
| `metrics-success.md` | `RC-01`, `RC-02`, `CT-02` |
| `benchmarks/reference-workloads.md` | `DV-03`, `RC-01`, `RC-02` |
| `review/design-review-checklist.md` | `DV-05` |
| `exit-criteria.md` | `DV-05` |

Every major contract in the Phase 0 set MUST appear in at least one test row.

## Test Catalog

### Design Validation

| ID | Type | Objective | Primary evidence | Pass criteria |
| --- | --- | --- | --- | --- |
| `DV-01 Scope and Non-Goal Review` | `DV` | Confirm the benchmark and test plan stay inside the narrow `v1` boundary. | PRD, ADR 0001, non-goal boundaries, reference workloads. | No required test assumes multi-cluster resume, elastic resize, non-PyTorch runtime, non-DCP format, or generalized crash recovery. |
| `DV-02 Authority and Ownership Review` | `DV` | Confirm test scenarios do not assign scheduler, checkpoint-selection, or training-semantics authority to the wrong actor. | ADR 0002, system context, actor responsibilities. | Every scenario preserves Kueue admission authority, operator lifecycle authority, SDK or agent checkpoint authority, and user-code safe-point authority. |
| `DV-03 Benchmark Conformance Review` | `DV` | Confirm the benchmark workloads and profiles follow the reference environment. | Reference environment, reference workloads, metrics-success. | The required workload set stays within one cluster, world size `8`, fixed GPU shape, fixed identity, and S3-backed DCP restore. |
| `DV-04 RTJ Surface Review` | `DV` | Confirm the API and status contracts expose enough fields to support the required measurements and acceptance scenarios. | RTJ API contract, RTJ status contract, schemas, example YAML. | The docs support queueing, identity, runtime mode, checkpoint policy, resume policy, phase, conditions, checkpoint references, timestamps, and degraded surfacing needed by the tests. |
| `DV-05 Exit Review` | `DV` | Confirm the Phase 0 artifact set and review outcomes satisfy the formal exit gate. | Design review checklist, exit criteria, session handoff. | Every required artifact exists, every checklist item is accepted or tracked, and no unresolved contradiction remains in the authoritative Phase 0 set. |

### Functional Acceptance

| ID | Type | Workloads | Objective | Pass criteria |
| --- | --- | --- | --- | --- |
| `FT-01 Manual Controlled Yield Happy Path` | `FT` | `RW-1`, `RW-2` | Validate manual yield from `Running` through `YieldRequested`, `Draining`, `Paused`, `Admitted`, `Restoring`, and back to `Running`. | The RTJ follows only allowed lifecycle transitions, records the expected timestamps and conditions, and restores from the selected compatible complete checkpoint. |
| `FT-02 Kueue-Driven Controlled Yield Happy Path` | `FT` | `RW-2`, `RW-3` | Validate the same controlled path for Kueue-driven yield. | Manual and Kueue-driven flows converge on the same lifecycle and checkpoint rules, with no special-case compatibility or restore behavior. |

### Compatibility And Checkpoint Integrity

| ID | Type | Workloads | Objective | Pass criteria |
| --- | --- | --- | --- | --- |
| `CT-01 Strict Compatibility Rejection Matrix` | `CT` | `RW-1`, `RW-2`, `RW-3` | Verify resume rejection for mismatched cluster identity, image identity, code version, world size, GPU shape, optimizer mode, and sharding mode. | Every mismatch is rejected before a resumed runtime is treated as valid, and the RTJ does not reach `Running` from an incompatible checkpoint. |
| `CT-02 Incomplete Checkpoint Rejection` | `CT` | `RW-1`, `RW-2`, `RW-3` | Verify that manifest-missing, artifact-missing, and manifest-before-artifacts cases are fail-closed. | `0` incomplete-checkpoint resumes reach `Running` or `Succeeded`; the RTJ remains `Paused` or transitions to `Failed` with diagnosable reasons. |
| `CT-03 Corrupt Or Newest-Bad Checkpoint Handling` | `CT` | `RW-2`, `RW-3` | Verify that a corrupt or otherwise unusable newest checkpoint is skipped safely. | The newest bad checkpoint is marked unusable, a valid older compatible checkpoint is selected if present, and no known-bad checkpoint is retried indefinitely. |

### Restore Validation

| ID | Type | Workloads | Objective | Pass criteria |
| --- | --- | --- | --- | --- |
| `RT-01 Latest Compatible Complete Restore` | `RT` | `RW-1`, `RW-2`, `RW-3` | Validate that restore uses the latest compatible complete checkpoint and not merely the newest artifact subtree. | Selection comes from committed manifests, restore-start validation succeeds, and the resumed attempt reaches `Running` from the chosen compatible checkpoint. |
| `RT-02 Bounded Restore Retry And Exhaustion` | `RT` | `RW-2`, `RW-3` | Validate restore failure handling when the selected checkpoint fails restore or restore timeout occurs. | The controller retries only within `maxResumeRetries`, preserves diagnostics, and transitions to `Failed` when retries are exhausted. |

### Repeated Controlled Cycles

| ID | Type | Workloads | Objective | Pass criteria |
| --- | --- | --- | --- | --- |
| `RC-01 Repeated Controlled Cycles DDP` | `RC` | `RW-1`, `RW-2` | Validate the numeric success targets on required DDP workloads over repeated cycles. | The workloads meet the `metrics-success.md` targets for p95 lost progress, p95 drain time, resume success rate, and checkpoint overhead. |
| `RC-02 Repeated Controlled Cycles FSDP` | `RC` | `RW-3` | Validate the same targets for the heaviest required in-scope FSDP workload. | `RW-3` meets its explicit numeric targets, including `>= 95%` resume success over `40` cycles and `<= 15%` checkpoint overhead. |

### Operability And Status Surfacing

| ID | Type | Workloads | Objective | Pass criteria |
| --- | --- | --- | --- | --- |
| `OB-01 Phase Condition And Degraded Surfacing` | `OB` | `RW-1`, `RW-2`, `RW-3` | Validate that operators can distinguish healthy, degraded, paused, restoring, and failed states from RTJ-visible status. | `phase`, `conditions`, `reason`, `message`, checkpoint references, and transition timestamps are sufficient to explain the current lifecycle edge and blocker. |

### Chaos-Style Docs-Only Scenarios

These scenarios are tabletop or document-trace validations in Phase 0.
They MUST be executable later, but Phase 0 accepts them as docs-only scenario reviews.

| ID | Type | Objective | Pass criteria |
| --- | --- | --- | --- |
| `CH-01 Controller Restart During Yield Or Restore` | `CH` | Validate the restart-recovery cases from `failure-semantics.md` against the persisted-truth and single-active-runtime invariants. | The scenario trace never requires a second active JobSet, duplicated logical request, or in-memory-only recovery assumption. |
| `CH-02 Stale Telemetry And Blocked Storage While Safe To Wait` | `CH` | Validate degraded behaviors for stale heartbeats during `Running` or `Draining` and storage-read blockage while `Paused`. | The trace stays in a bounded degraded state, surfaces `Degraded=True`, and either clears on decisive evidence or transitions to `Failed` without guessing. |
| `CH-03 Node Loss And Forced Termination Boundary` | `CH` | Validate that out-of-scope crash-recovery and forced-termination scenarios are surfaced clearly but not reported as successful controlled yield. | The trace ends in fail-closed or blocked behavior consistent with `non-goal-boundaries.md` and never reclassifies the event as an in-scope success. |

## Minimum Execution Set For Later Acceptance

Before a later implementation may claim `v1` conformance, it MUST complete at least:

- all `DV` reviews
- both `FT` scenarios
- all `CT` scenarios
- both `RT` scenarios
- both `RC` scenarios
- `OB-01`
- all `CH` scenarios

No subset smaller than this may be used to claim that the accepted Phase 0 contract has been satisfied.
