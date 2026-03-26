# ADR 0001: Checkpoint-Aware Priority Shaping

- **Status:** Accepted
- **Date:** 2026-03-25
- **Context:** Phase 5 design lock for the checkpoint-native preemption controller.

## Decision

Phase 5 extends the RTJ Kueue integration with checkpoint-aware priority
shaping:

1. **Checkpoint-aware effective priority derivation.**
2. **Yield budgets / protection windows.**
3. **Controller-derived effective priority written to Kueue Workloads.**
4. **Deterministic within-ClusterQueue preemption profile for local/e2e.**
5. **PriorityShapingPolicy CRD for configuration.**

These capabilities compose with Phase 3's flavor-aware rendering, Phase 4's
topology-aware pipeline, and all prior phase features without replacing them.

## Context

Phase 4 delivered topology-aware admission and a custom ResumeReadiness
AdmissionCheck. However, Kueue's preemption decisions are based entirely on
the static `WorkloadPriorityClass` value declared by the user. This creates
two gaps:

1. **No signal for checkpoint-aware preemption.** Two jobs at the same
   priority level are equally preemptable, even if one has a fresh checkpoint
   (safe to preempt) and the other has no checkpoint (expensive to preempt).
   The operator has no way to influence Kueue's victim selection.

2. **No yield budget / protection window.** A newly-started job should not
   have its checkpoint-dependent priority reduced immediately — it has had
   no time to produce a checkpoint. A configurable protection window is
   needed to shield new or recently-resumed jobs.

Phase 5 addresses these gaps by deriving an effective Workload priority from
checkpoint freshness and yield budget state. Kueue's existing preemption
engine consumes this effective priority without modification.

## Decisions

### D1: RTJ Remains the Only Kueue-Managed Admission Object

**Decision:** RTJ is the only object that Kueue manages for admission.
The child JobSet remains a plain runtime resource with no Kueue metadata.

**Rationale:** Unchanged from Phase 2. The single-admission-object invariant
is preserved.

### D2: Kueue Remains the Preemption Authority

**Decision:** The operator does NOT implement preemption logic, victim
selection, or custom scheduling. It shapes the `Workload.Spec.Priority`
integer that Kueue reads. Kueue decides when and what to preempt.

**Rationale:** Building a custom preemption engine would duplicate Kueue's
functionality, create ordering conflicts, and violate the Phase 0 authority
boundary where Kueue owns queueing, admission, and preemption.

**Alternative considered:** Operator-initiated yield (preempt without
Kueue involvement). Rejected because it bypasses Kueue's quota accounting
and priority ordering, potentially admitting workloads out of order.

### D3: Effective Priority Is a Derived Value on the Workload

**Decision:** Effective priority is derived by the operator from:
- Base priority (WorkloadPriorityClass.Value)
- Checkpoint freshness (latest manifest age from S3)
- Yield budget state (protection window)
- PriorityShapingPolicy parameters

The derived value is written to `Workload.Spec.Priority`.

**Rationale:** Kueue reads `Workload.Spec.Priority` for preemption decisions.
This is the only field the operator can influence. The PriorityClassRef is
immutable once quota is reserved. The Priority integer is the mutable lever.

**Alternative considered:** Create per-RTJ WorkloadPriorityClasses with
dynamic values. Rejected because WorkloadPriorityClass is cluster-scoped
and shared, and changing its value does not affect existing Workloads.

**Alternative considered:** Use a Kueue annotation or label for priority
hints. Rejected because Kueue does not read annotations for preemption;
it reads `Spec.Priority`.

### D4: Fail-Safe Is Keep Base Priority

**Decision:** When checkpoint telemetry is unavailable (S3 read failure,
catalog not configured, no manifests found), the effective priority remains
equal to the base priority. No penalty is applied.

**Rationale:** Silent demotion on telemetry failure would cause unexpected
preemption. Keeping the base priority is the safe default: the job is no
more preemptable than its user-declared priority implies. If the checkpoint
catalog is not configured (Phase 1/2 deployment), the job behaves as if
priority shaping is disabled.

**Alternative considered:** Fail to Retry (re-evaluate later). Rejected
because the re-evaluation interval already handles retries. The fail-safe
decision is about the interim value during failure, not whether to retry.

### D5: Protection Window Resets on Resume

**Decision:** When an RTJ resumes from a checkpoint, its protection window
restarts from the resume time, not the original start time.

**Rationale:** A resumed job needs time to produce a new checkpoint before
its freshness is meaningful. Using the original start time would cause the
protection window to have already expired on resume, immediately subjecting
the job to penalty from a (now-old) checkpoint.

### D6: Priority Shaping Only for Running Workloads

**Decision:** The Priority Shaping Controller only evaluates and adjusts
effective priority for RTJs in Running, Starting, or Restoring phases. When
an RTJ transitions to Queued (after preemption), its Workload priority is
reset to the base value.

**Rationale:** Queued RTJs should compete for admission at their declared
base priority. Checkpoint staleness from a previous run should not penalise
the re-admission ordering. The job will produce a fresh checkpoint after it
starts running.

**Alternative considered:** Carry forward the penalty from the previous run.
Rejected because the job's checkpoint will be revalidated during the
ResumeReadiness check, and the staleness that triggered preemption is already
"paid for" by the preemption itself.

### D7: PriorityShapingPolicy Is Cluster-Scoped

**Decision:** `PriorityShapingPolicy` is a cluster-scoped CRD, similar to
`ResumeReadinessPolicy` and `WorkloadPriorityClass`.

**Rationale:** Priority shaping parameters are infrastructure policy, not
per-namespace workload configuration. Cluster-scoped allows operators to
define organisation-wide shaping policies that workload authors reference
by name.

### D8: Phase 4 Behaviour Preserved When Shaping Disabled

**Decision:** When `spec.priorityShapingRef` is nil, no priority shaping is
applied. Effective priority equals the base priority. All Phase 4 behaviour
is preserved exactly. No regression.

**Rationale:** Phase 5 features are opt-in. Existing RTJs must not break.

### D9: Priority Shaping Controller Is a Separate Controller

**Decision:** The Priority Shaping Controller is a separate controller
(separate reconciliation loop) wired into the existing operator binary.
It is not part of the main RTJ reconciler or the ResumeReadiness controller.

**Rationale:**
- Priority evaluation runs on a timer (every `evaluationInterval`), not
  event-driven. A separate controller with its own requeueAfter is cleaner
  than coupling timer logic to the main reconciler.
- Readiness evaluation (pre-admission) and priority shaping (post-admission,
  during running) are different lifecycle stages with different triggers.
- A separate controller has its own error handling and does not block the
  main RTJ reconciliation on checkpoint catalog I/O.

### D10: Deterministic Local Preemption Profile

**Decision:** Phase 5 delivers a local development profile with:
- A single ClusterQueue with `preemption.withinClusterQueue: LowerPriority`.
- Limited quota (4 CPU / 4 Gi).
- Two WorkloadPriorityClasses: `training-low` (100), `training-high` (1000).
- A PriorityShapingPolicy with short durations for fast iteration.
- Sample RTJs that exercise the full preemption cycle.

**Rationale:** Deterministic local testing is required for development
velocity. The single-CQ profile isolates Phase 5 from cohort and
fair-sharing complexity.

### D11: Pinned Versions Unchanged

**Decision:** Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4.
No version bumps in Phase 5.

**Risk:** If Kueue v0.15.1 does not allow mutation of `Workload.Spec.Priority`
on admitted Workloads (e.g., webhook rejects the update), the effective-
priority write path fails. In this case, the operator must either:
- Write priority before admission (during Workload creation/sync).
- Use an alternative mechanism (e.g., delete and recreate the Workload with
  new priority, triggering re-admission).

This risk is captured as OQ-1 and must be resolved during implementation.

## Phase 5 Must-Ship Demo

The must-ship demo exercises the full priority-shaping preemption cycle in
the local `kind` dev environment:

### Setup

1. Single ClusterQueue with 4 CPU / 4 Gi quota and
   `preemption.withinClusterQueue: LowerPriority`.
2. Two WorkloadPriorityClasses: `training-low` (100), `training-high` (1000).
3. PriorityShapingPolicy `fast-shaping`:
   - `protectionDuration: 2m`
   - `freshnessThreshold: 1m`
   - `penaltyStepSize: 200`
   - `maxPenalty: 600`
   - `evaluationInterval: 15s`

### Scenario

1. Submit RTJ-A with `training-low` priority and `fast-shaping` policy.
2. RTJ-A runs, produces a checkpoint, protection window expires.
3. RTJ-A's checkpoint freshness degrades past 1m.
4. Priority Shaping Controller lowers RTJ-A's effective priority to -100
   (100 - 200).
5. Submit RTJ-B with `training-low` priority (effective = 100, no shaping)
   or `training-high` priority.
6. Kueue sees RTJ-B (100 or 1000) > RTJ-A (-100) → preempts RTJ-A.
7. RTJ-A gracefully yields, saves checkpoint, re-queues.
8. RTJ-B completes (or is deleted).
9. RTJ-A resumes from checkpoint, protection window restarts.

### Observable Assertions

- `status.effectivePriority.penalty > 0` before preemption.
- `Workload.Spec.Priority < basePriority` before preemption.
- Kueue suspends RTJ-A (not RTJ-B or any other workload).
- Graceful yield completes with checkpoint.
- Resume produces a fresh child JobSet from the saved checkpoint.
- Global step counter is monotonic across the preemption boundary.

## Consequences

### Positive

- Checkpoint-aware preemption: stale-checkpoint jobs are naturally preempted
  before fresh-checkpoint jobs.
- Protection windows prevent premature preemption of newly-started jobs.
- No custom scheduling or victim selection — fully delegated to Kueue.
- Phase 4 behaviour preserved for existing RTJs.
- Observable: effective priority, penalty, and protection state surfaced in
  RTJ status and Prometheus metrics.

### Negative

- Adds a new reconciliation loop (Priority Shaping Controller) with periodic
  S3 reads for checkpoint manifest freshness.
- Effective-priority ownership model must be coordinated with Kueue's
  GenericJob reconciler to prevent clobbering (OQ-1).
- Effective-priority changes are visible in the Workload spec, which may
  confuse users who expect priority to match the WorkloadPriorityClass.

### Neutral

- The child JobSet remains plain runtime. No change to the JobSet
  controller or JobSet CRD.
- Checkpoint compatibility checking is unchanged. Priority shaping reads
  manifest timestamps, not checkpoint content.
- The graceful yield protocol is unchanged. Priority shaping influences
  when preemption occurs, not how it proceeds.

## Verification

| Decision | Verification |
| --- | --- |
| D1 | Unit test: child JobSet has no `kueue.x-k8s.io/*` labels. |
| D2 | Design review: no preemption logic in operator code. |
| D3 | Unit test: effective priority = base - penalty, written to Workload. |
| D4 | Unit test: telemetry failure → effective = base, no penalty. |
| D5 | Unit test: protection window restarts on resume. |
| D6 | Unit test: queued RTJ → Workload priority reset to base. |
| D7 | CRD manifest: PriorityShapingPolicy is cluster-scoped. |
| D8 | Unit test: nil priorityShapingRef → no shaping, Phase 4 behaviour. |
| D9 | Code review: separate controller with own reconciliation loop. |
| D10 | E2E test: full preemption cycle in kind dev environment. |
| D11 | `go.mod` inspection: pinned versions unchanged. |
