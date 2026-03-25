# Reference Workloads

This document defines the benchmark workloads used to evaluate the `v1` success targets from [../metrics-success.md](../metrics-success.md).
The workload set is intentionally narrow and stays inside the accepted Phase 0 scope.

## Purpose

The required workload set exists to prevent benchmark drift.
Later implementation work MUST NOT claim `v1` success using only easy or non-representative workloads.

## Benchmark Invariants

All required workloads MUST preserve these invariants:

- one Kubernetes cluster only
- one RTJ under test at a time for the canonical benchmark path
- JobSet as the only runtime primitive
- PyTorch `DDP` or `FSDP` only
- PyTorch `DCP` as the only checkpoint mechanism
- one fixed declared world size for the lifetime of a cycle
- one fixed GPU shape for the lifetime of a cycle
- one fixed image identity and code version per workload revision
- one fixed optimizer mode and sharding mode per workload
- one stable S3-compatible checkpoint prefix per RTJ lineage

## Required Workload Set

| ID | Workload name | Runtime mode | World size | GPU shape | Optimizer mode | Sharding mode | Target steady-state step band | Checkpoint interval | Freshness budget | Max drain time | Why it exists |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `RW-1` | `ddp-short-step-8xa100` | `DDP` | `8` | `nvidia-a100-80gb` | `adamw` | `replicated-optimizer-state` | `1s` to `3s` | `300s` | `900s` | `300s` | Exercises short step boundaries, low-latency yield, and overhead sensitivity. |
| `RW-2` | `ddp-medium-step-8xa100` | `DDP` | `8` | `nvidia-a100-80gb` | `adamw` | `replicated-optimizer-state` | `8s` to `20s` | `600s` | `1200s` | `600s` | Represents a typical in-scope DDP training workload with material checkpoint state. |
| `RW-3` | `fsdp-long-step-8xa100` | `FSDP` | `8` | `nvidia-a100-80gb` | `adamw` | `fsdp-full-shard` | `20s` to `60s` | `600s` | `1200s` | `900s` | Represents the heaviest required `v1` drain and restore path while remaining within the default world-size-8 reference environment. |

The workload names are benchmark identities, not standardized product APIs.
Phase 1 MAY substitute an equivalent training program if and only if it preserves the same execution mode, state-shape class, step-time band, and checkpoint profile.

## Workload Intent

### `RW-1`

`RW-1` MUST behave like a short-step DDP workload whose primary value is exposing:

- fast safe-point availability
- high sensitivity to checkpoint overhead
- frequent opportunities to verify low-latency yield handling

### `RW-2`

`RW-2` MUST behave like a medium-step DDP workload whose primary value is exposing:

- representative same-cluster DDP restore behavior
- non-trivial checkpoint size
- meaningful but still bounded drain latency

### `RW-3`

`RW-3` MUST behave like a longer-step FSDP workload whose primary value is exposing:

- the strict optimizer-mode and sharding-mode compatibility rules
- the slowest accepted in-scope drain path
- the most demanding required restore path for `v1`

## Required Benchmark Profiles

| Profile | Purpose | Required workloads | Minimum repetitions |
| --- | --- | --- | --- |
| `BP-1 Baseline` | Establish no-checkpoint throughput and step-time baseline | `RW-1`, `RW-2`, `RW-3` | `3` runs per workload |
| `BP-2 PeriodicCheckpoint` | Measure steady-state checkpoint overhead | `RW-1`, `RW-2`, `RW-3` | `3` runs per workload |
| `BP-3 ControlledCycles` | Measure drain, lost progress, and repeated-cycle resume reliability | `RW-1`, `RW-2`, `RW-3` | `40` cycles per workload |
| `BP-4 NegativeRestore` | Verify fail-closed behavior for incomplete, corrupt, and incompatible checkpoints | `RW-1`, `RW-2`, `RW-3` | As defined in [../test-plan.md](../test-plan.md) |

## Yield Injection Rules

The `ControlledCycles` profile MUST distribute yield requests across three timing windows:

- early in step execution
- mid-step
- near the next expected step boundary

No workload may be benchmarked only at the easiest timing window.
This rule exists because `v1` promises controlled yield at step boundaries, not idealized instant checkpointing.

## Required Measurement Outputs

Each workload run MUST produce enough evidence to compute or validate:

- training-step time distribution
- checkpoint completion timestamps
- yield-request acceptance timestamps
- `Paused` convergence timestamps
- selected checkpoint ID and `completionTimestamp`
- restored run-attempt timestamps
- post-resume progress for at least `5` completed steps

## Non-Required Stretch Workloads

Phase 1 MAY run additional stretch workloads, including larger FSDP models or different GPU shapes, but those runs MUST NOT replace the required workload set when claiming `v1` target attainment.

Stretch workloads are useful for exploration.
They are not part of the minimum acceptance bar for Phase 0 completeness.
