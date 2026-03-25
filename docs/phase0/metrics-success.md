# Metrics and Success Targets

This document defines the concrete numeric success targets for the narrow `v1` scope.
It turns the PRD's draft success language into explicit Phase 0 acceptance targets for later benchmark and implementation planning.

These targets apply only to the in-scope `v1` reference environment and the required benchmark workloads from [benchmarks/reference-workloads.md](benchmarks/reference-workloads.md).
They MUST be interpreted together with:

- [contracts/reference-environment.md](contracts/reference-environment.md)
- [contracts/failure-semantics.md](contracts/failure-semantics.md)
- [contracts/degraded-behavior.md](contracts/degraded-behavior.md)
- [contracts/checkpoint-contract.md](contracts/checkpoint-contract.md)
- [contracts/checkpoint-selection-and-compatibility.md](contracts/checkpoint-selection-and-compatibility.md)

## Counting Rules

- Only in-scope controlled cycles count toward the primary success targets.
- An in-scope controlled cycle begins when a manual or Kueue-driven yield request is accepted for a `Running` RTJ and ends only after the RTJ reaches `Paused` from a complete valid checkpoint and later reaches `Running` again from a selected compatible checkpoint.
- Out-of-scope scenarios from [contracts/non-goal-boundaries.md](contracts/non-goal-boundaries.md), such as node loss during drain or forced termination after timeout, MUST be tested separately and MUST NOT be counted as successful controlled cycles.
- Each required workload MUST contribute at least `40` controlled cycles:
  - `20` manual-yield cycles
  - `20` Kueue-driven-yield cycles
- Each required workload MUST also contribute at least `10` negative resume attempts from deliberately incomplete checkpoints.

## Measurement Definitions

| Metric | Definition |
| --- | --- |
| Lost progress after controlled yield | The number of already-completed training steps that must be replayed after resume relative to the `globalStep` recorded in the selected checkpoint manifest. |
| Drain time after yield request | Time from accepted yield intent to the RTJ reaching `Paused` with `CheckpointReady=True`. |
| Resume success rate across repeated controlled cycles | Fraction of eligible controlled cycles that return to `Running` within bounded restore policy and complete at least `5` post-resume training steps without violating invariants. |
| Checkpoint overhead | Relative increase in median training-step time when periodic checkpointing is enabled versus the no-checkpoint baseline for the same workload shape and image identity. |
| Resume from incomplete checkpoint | Any cycle in which an incomplete checkpoint candidate causes a resumed runtime to reach `Restoring`, `Running`, or `Succeeded` as if the checkpoint were valid. |

## Required Numeric Targets

| Workload ID | p95 lost progress after controlled yield | p95 drain time after yield request | Resume success rate across `40` controlled cycles | Checkpoint overhead target | Incomplete-checkpoint resume target |
| --- | --- | --- | --- | --- | --- |
| `RW-1` | `<= 1` completed step | `<= 180s` | `>= 95%` (`38/40`) | `<= 5%` median step-time inflation | `0 / 10` resumes allowed |
| `RW-2` | `<= 1` completed step | `<= 300s` | `>= 95%` (`38/40`) | `<= 8%` median step-time inflation | `0 / 10` resumes allowed |
| `RW-3` | `<= 1` completed step | `<= 600s` | `>= 95%` (`38/40`) | `<= 15%` median step-time inflation | `0 / 10` resumes allowed |

These targets are intentionally conservative.
They are strict enough to reject an ambiguous or operationally weak design, but they do not assume an aggressively optimized `v1`.

## Cross-Workload Guardrails

In addition to the per-workload targets above:

- No successful controlled cycle MAY exceed the workload's declared `spec.checkpoint.maxDrainTime`.
- Aggregate resume success across all required workloads SHOULD be at least `96%`.
- Aggregate p95 lost progress across all required workloads SHOULD remain `<= 1` completed step.
- Aggregate p95 drain time across all required workloads SHOULD remain `<= 420s`.
- No resume attempt from an incomplete checkpoint MAY ever reach `Running` or `Succeeded`.

## Benchmark Profiles Required To Measure The Targets

### Steady-State Baseline Profile

- Run each required workload for at least `30` minutes with checkpointing disabled or functionally bypassed.
- Collect step-time and throughput distributions.
- Repeat baseline measurement `3` times per workload.

### Periodic Checkpoint Overhead Profile

- Run each required workload for at least `30` minutes with periodic checkpointing enabled at the interval defined in [benchmarks/reference-workloads.md](benchmarks/reference-workloads.md).
- Collect the same step-time and throughput distributions as the baseline profile.
- Repeat the checkpoint-enabled measurement `3` times per workload.

### Controlled-Cycle Profile

- Execute `40` controlled yield and resume cycles per required workload.
- Split the cycles evenly between manual yield and Kueue-driven yield.
- Inject yield requests across early-step, mid-step, and late-step timing windows so drain latency is not measured only under ideal alignment.

### Negative Restore Profile

- Attempt at least `10` resumes per required workload from deliberately incomplete checkpoints.
- Those attempts MUST include at least:
  - manifest missing
  - missing required artifact
  - manifest committed before all required artifacts exist

## Interpretation Rules

- A checkpoint that is stale but complete does not count as incomplete.
- A corrupted or incompatible checkpoint does not count as an incomplete-checkpoint success; it is covered by separate compatibility and restore-fallback tests.
- A cycle that fails because of an out-of-scope crash-recovery event MUST be reported separately from controlled-cycle success metrics.
- A workload MAY exceed the targets during exploratory or stretch benchmarks, but only the required benchmark set may be used to claim `v1` target attainment.
