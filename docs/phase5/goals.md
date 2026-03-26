# Phase 5 Goals

## Mission

Implement checkpoint-aware priority shaping and yield budgets for
ResumableTrainingJob, using Kueue's mutable Workload priority as the control
lever. The operator derives an effective priority from checkpoint freshness and
yield budget state, then writes it to the Workload so Kueue's existing
within-ClusterQueue preemption selects the right victim without any custom
scheduling.

## Goals

### G1: Checkpoint-Aware Effective Priority Derivation

The RTJ operator MUST compute an effective Workload priority for each running
RTJ by combining:

- **Base priority:** the integer value from the WorkloadPriorityClass
  referenced by `spec.workloadPriorityClassName`.
- **Checkpoint freshness signal:** how recently the job completed a
  successful DCP checkpoint (read from checkpoint manifests in S3-compatible
  storage).
- **Yield budget state:** whether the job is still within its protection
  window.

The effective priority is a single `int32` that the operator writes to
`Workload.Spec.Priority`. Kueue reads this value for its standard preemption
comparisons.

**Acceptance:** An RTJ whose latest checkpoint is older than the configured
freshness threshold has its effective Workload priority lowered below its base
priority. An RTJ still within its yield budget retains its full base priority
regardless of checkpoint state.

### G2: Yield Budgets / Protection Windows

The operator MUST support a per-RTJ protection window (yield budget) that
shields a job from effective-priority reduction for a configurable duration
after it begins running or resumes from a checkpoint.

During the protection window:
- Effective priority equals the base priority.
- No checkpoint-freshness penalty is applied.
- Kueue sees the job at full declared priority.

After the protection window expires:
- The operator evaluates checkpoint freshness.
- If the freshness threshold is exceeded, the effective priority drops.
- The drop magnitude is a configurable function of staleness.

**Acceptance:** A low-priority RTJ that has been running for less than
`yieldBudget.protectionDuration` retains its full base priority — no
checkpoint-staleness penalty is applied. The protection window prevents
*additional* priority reduction from checkpoint freshness degradation. It does
NOT prevent Kueue's standard `LowerPriority` preemption by strictly higher-
priority workloads. After the protection window expires and the checkpoint is
stale, the effective priority drops further, making the job a preemption
candidate even against same-base-priority pending workloads.

### G3: Effective Priority Written to Kueue Workload

The operator MUST write the computed effective priority to the
`Workload.Spec.Priority` field on the Kueue Workload associated with the RTJ.
The operator MUST own this field and prevent Kueue's GenericJob reconciler from
clobbering it during normal sync.

The ownership model must ensure:
1. Base priority class reference (`PriorityClassRef`) is set at Workload
   creation and is immutable (Kueue enforces this).
2. The `Priority` integer field is mutable and owned by the RTJ operator's
   priority-shaping controller.
3. If checkpoint telemetry is unavailable, the fail-safe is to keep the base
   priority (no penalty applied).

**Acceptance:** The `Workload.Spec.Priority` field reflects the operator-
computed effective priority within one reconciliation cycle of the triggering
event (checkpoint completion or protection window expiry).

### G4: Deterministic Within-ClusterQueue Preemption Profile

Phase 5 MUST deliver a local development profile where preemption is
deterministic and observable:

- A single ClusterQueue with limited quota (e.g., 4 CPU / 4 Gi).
- Two RTJs: one low-priority (long-running), one high-priority (pending).
- The low-priority RTJ's effective priority drops after its protection window
  expires and checkpoint freshness degrades.
- Kueue preempts the low-priority RTJ.
- The operator initiates graceful yield, saves a checkpoint, and the RTJ
  re-queues.
- After the high-priority RTJ finishes, the low-priority RTJ resumes from its
  checkpoint.

**Acceptance:** The full cycle (run -> priority drop -> preemption -> yield ->
checkpoint -> re-queue -> resume) is exercised in a local `kind` e2e test
with deterministic timing.

### G5: PriorityShapingPolicy CRD

A new cluster-scoped CRD, `PriorityShapingPolicy`, parameterises the
priority-shaping behaviour:

- `protectionDuration` (duration): yield budget / protection window length.
- `freshnessThreshold` (duration): how old a checkpoint can be before
  priority penalty applies.
- `penaltyStepSize` (int32): how much to reduce effective priority per
  penalty step.
- `maxPenalty` (int32): maximum cumulative priority reduction.
- `evaluationInterval` (duration): how often the operator re-evaluates
  effective priority.

The policy is referenced from the RTJ spec or from the ResumeReadiness
AdmissionCheck parameters (design decision in open questions).

**Acceptance:** A `PriorityShapingPolicy` CR configures priority shaping for
an RTJ. Default values produce sensible behaviour in the local dev profile.

## Non-Goals (Explicitly Out of Scope)

| Non-Goal | Reason |
| --- | --- |
| MultiKueue or multi-cluster admission | Phase 0 contract: single cluster only. |
| Elastic Workloads or in-place scaling | Deferred; Phase 3 handles resume-time shape changes only. |
| Custom scheduling algorithms | Kueue owns scheduling; the operator shapes priority, not scheduling decisions. |
| Custom victim-selection engine | Kueue owns victim selection; the operator influences it through effective priority. |
| Transparent CUDA or container snapshots | Phase 0 contract: DCP checkpointing only. |
| Cohort-level or fair-sharing priority | Deferred; Phase 5 targets within-ClusterQueue preemption only. |
| Cross-ClusterQueue preemption policy | Requires Kueue upstream features not yet available. |
| Scheduling-gate support for per-pod topology | Deferred from Phase 4; orthogonal to priority shaping. |
| Automatic yield-before-preemption | Kueue preemption triggers yield; the operator does not preemptively yield based on predicted preemption. |

## Success Criteria

### Must-Ship

1. Effective priority is derived from base priority + checkpoint freshness +
   yield budget state and written to `Workload.Spec.Priority`.
2. Protection window prevents priority reduction for configurable duration
   after job start or resume.
3. Checkpoint freshness degradation causes effective priority drop.
4. Priority drop triggers Kueue preemption of the RTJ in favour of a
   higher-priority pending workload.
5. Graceful yield, checkpoint save, re-queue, and resume work end-to-end
   after priority-driven preemption.
6. Phase 4 behaviour is preserved when no PriorityShapingPolicy is attached.

### Should-Ship

7. Status surfaces effective priority, protection window state, and freshness
   evaluation results.
8. Metrics track priority evaluations, penalty applications, and
   protection-window transitions.
9. PriorityShapingPolicy CRD with sensible defaults for local development.
10. Local dev profile with two-RTJ preemption scenario.

### Exit Criteria

- All must-ship acceptance criteria pass in the local `kind` dev environment.
- Unit tests cover: effective priority computation, yield budget state
  machine, checkpoint freshness evaluation, Workload priority write path,
  fail-safe when telemetry is unavailable.
- E2E test covers: priority-driven preemption with graceful yield and resume.
- Documentation updated: architecture, migration, ADR, session handoff.
- Phase 4 regression: all Phase 4 tests continue to pass.
