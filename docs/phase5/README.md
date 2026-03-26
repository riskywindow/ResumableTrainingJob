# Phase 5: Checkpoint-Aware Priority Shaping

Phase 5 adds checkpoint-aware priority shaping and yield budgets for
ResumableTrainingJob (RTJ), using Kueue's mutable Workload priority as the
control lever. The operator derives an "effective priority" from checkpoint
freshness and yield budgets, then writes it to the Workload to influence
Kueue's existing preemption behaviour.

## What Phase 5 Delivers

1. **Checkpoint-aware effective priority.** The operator computes an effective
   Workload priority by combining the user-declared base priority with
   checkpoint-freshness telemetry. A job whose checkpoint is stale (older than
   the configured freshness threshold) has its effective priority lowered,
   making it a natural preemption candidate; a job still within its protection
   window or with a fresh checkpoint keeps its full base priority.

2. **Yield budgets / protection windows.** A configurable per-RTJ protection
   window shields a job from priority shaping for a fixed duration after it
   starts or resumes. During this window the effective priority equals the
   base priority regardless of checkpoint state.

3. **Controller-derived effective priority on Workloads.** The RTJ operator
   owns the derivation of `Workload.Spec.Priority`. Kueue sees only the
   resulting integer and applies its standard within-ClusterQueue preemption
   logic. No custom scheduling, no custom victim selection.

4. **Deterministic within-ClusterQueue preemption profile.** In local
   development, Phase 5 targets a single ClusterQueue where a lower-effective-
   priority workload is preempted in favour of a higher-priority pending
   workload. Cohort and fair-sharing policy innovation is deferred.

## What Does Not Change

- RTJ remains the **only** Kueue-managed admission object.
- The child JobSet remains a **plain runtime resource** with no Kueue metadata.
- All Phase 0 through Phase 4 invariants are preserved.
- When no Phase 5 priority policy is attached, Phase 4 behaviour is preserved
  exactly.
- Kueue remains the queueing, admission, and preemption authority.
- The RTJ operator remains the lifecycle owner for yield, checkpoint, and resume.
- Pinned versions: Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4.

## What Remains Out of Scope

- MultiKueue or multi-cluster admission.
- Elastic Workloads or in-place scaling.
- Custom scheduling algorithms or custom victim-selection engines.
- Transparent CUDA or container snapshots.
- Cohort-level or fair-sharing priority innovation.
- Cross-ClusterQueue preemption policy.

## Quick Navigation

See [index.md](index.md) for the full document map.
