# Phase 5 Open Questions

## OQ-1: Workload.Spec.Priority Mutability and GenericJob Sync Clobbering

**Question:** Does Kueue's GenericJob reconciler overwrite
`Workload.Spec.Priority` on every sync, or only at Workload creation? If it
overwrites on sync, will the operator's effective-priority write be clobbered
on the next reconciliation cycle?

**Impact:** This is the most critical design question for Phase 5. If Kueue's
GenericJob reconciler resets Priority on every sync, the operator's
effective-priority writes will be undone within seconds. The design must ensure
the operator's writes are durable.

**Resolution plan:**
1. Inspect the Kueue v0.15.1 `GenericJob` reconciler source to trace the
   Priority field's lifecycle (creation vs. sync vs. admission).
2. If Priority is only set at creation: no conflict. The operator can freely
   mutate it after creation.
3. If Priority is set on every sync: the operator needs to intercept the sync
   path. Options:
   - **Option A:** Modify `RTJGenericJob` adapter to skip writing Priority
     when `spec.priorityShapingRef` is set. The adapter defers to the
     Priority Shaping Controller.
   - **Option B:** Use a Workload annotation
     (e.g., `checkpoint-native.example.io/priority-owner: shaping-controller`)
     to signal that the Priority field is operator-managed.
   - **Option C:** Accept the race and rely on the Priority Shaping
     Controller's evaluation interval being shorter than the GenericJob
     reconciler's sync interval.

**Recommended:** Option A — smallest, most coherent ownership model.

## OQ-2: Kueue Preemption Responsiveness to Priority Changes

**Question:** When the operator lowers `Workload.Spec.Priority` on a running
Workload, how quickly does Kueue re-evaluate preemption? Does Kueue watch
for Priority changes on admitted Workloads, or does it only check priority
during scheduling of pending Workloads?

**Impact:** Determines the end-to-end latency between a priority drop and
actual preemption. If Kueue only checks priority when scheduling new
workloads, priority shaping is reactive (preemption happens when a new job
arrives) rather than proactive.

**Resolution plan:**
1. Review Kueue's preemption code path for how it handles priority
   comparisons on running vs. pending workloads.
2. If Kueue watches Priority changes: the operator's priority drop triggers
   re-evaluation promptly.
3. If Kueue only checks on scheduling: priority shaping influences victim
   selection when a new workload arrives. This is still useful — the
   stale-checkpoint job becomes the preferred victim over a fresh-checkpoint
   job at the same base priority.

**Expected:** Kueue's preemption is triggered by pending workloads, not by
priority changes on running workloads. The effective-priority drop makes the
job more preemptable when a higher-priority workload arrives, but does not
cause self-preemption.

## OQ-3: Checkpoint Manifest Timestamp Source

**Question:** Where does the Priority Shaping Controller read the checkpoint
manifest timestamp? The existing checkpoint catalog stores manifest metadata
in S3-compatible storage. Does the controller:

- Read the S3 manifest directly?
- Use the existing `checkpoints.Catalog` interface?
- Use a new RTJ status field that the reconciler updates?

**Impact:** Determines the I/O pattern and coupling between the Priority
Shaping Controller and the checkpoint subsystem.

**Resolution plan:**
1. Prefer reusing the existing `checkpoints.Catalog` interface, which
   already provides manifest listing and metadata.
2. The Priority Shaping Controller calls `Catalog.ListManifests` and reads
   the most recent manifest's timestamp.
3. If the Catalog is not configured (no S3 storage), the controller applies
   the fail-safe (keep base priority).

**Recommended:** Reuse `checkpoints.Catalog` — consistent with Phase 4's
ResumeReadiness evaluator, which already uses the same interface.

## OQ-4: Priority Shaping Controller Placement

**Question:** Should the Priority Shaping Controller be:

- **Option A:** A periodic loop inside the main RTJ reconciler that runs
  every `evaluationInterval` via requeueAfter.
- **Option B:** A separate controller (separate manager.Runnable) with its
  own reconciliation loop.
- **Option C:** An extension to the existing ResumeReadiness
  AdmissionCheck controller that evaluates priority alongside readiness.

**Impact:** Affects code organisation, reconciliation frequency, and
interaction with existing controllers.

**Resolution plan:**
1. **Option A** is simplest but couples priority evaluation to RTJ
   reconciliation events. Priority evaluation needs to run on a timer
   (every 30s), not event-driven. Using requeueAfter works but increases
   reconcile traffic.
2. **Option B** is cleanest — a dedicated controller watches RTJs with
   priority shaping enabled and runs on a timer. Separate reconciliation
   loop, separate error handling.
3. **Option C** conflates readiness (pre-admission) with priority shaping
   (post-admission, during running). These are different lifecycle stages.

**Recommended:** Option B — a separate controller with its own timer-based
reconciliation, wired into the existing operator binary.

## OQ-5: Negative Effective Priority Values

**Question:** The penalty formula can produce negative effective priorities
(e.g., base=100, penalty=200 → effective=-100). Does Kueue handle negative
`Workload.Spec.Priority` values correctly?

**Impact:** If Kueue treats negative priorities unexpectedly (e.g., wraps
to max int, or rejects the update), the operator must clamp effective
priority to zero or some minimum.

**Resolution plan:**
1. Inspect Kueue's priority comparison code to verify it handles negative
   values.
2. Kueue's Priority field is `*int32`, which natively supports negative
   values. Standard integer comparison should work.
3. If issues are found, clamp effective priority to
   `max(basePriority - maxPenalty, 0)` or a configured floor.

**Expected:** Negative priorities work correctly. Kubernetes PriorityClasses
support negative values, and Kueue follows the same convention.

## OQ-6: Protection Window Start Time

**Question:** What timestamp marks the start of the protection window? Options:

- **Option A:** `RTJ.Status.StartTime` — when the RTJ first transitioned to
  Running.
- **Option B:** The child JobSet's creation time — when the current run
  attempt started.
- **Option C:** A new timestamp recorded by the operator when the phase
  transitions to Running or Restoring.

**Impact:** Determines the protection window's accuracy. If using StartTime,
the protection window may have already expired by the time the job finishes
restoring from a checkpoint.

**Resolution plan:**
1. **Option C** is most accurate — the operator records the timestamp when
   the child JobSet is created (or when the phase transitions to Running
   after restore completes).
2. This timestamp is stored in `status.effectivePriority.protectionWindowExpiresAt`
   as `startTime + protectionDuration`.
3. On resume, the timestamp resets to the resume time, not the original
   start time.

**Recommended:** Option C — a new timestamp recorded at phase transition,
reset on each resume.

## OQ-7: Interaction with ResumeReadiness AdmissionCheck

**Question:** When an RTJ with priority shaping is re-admitted after
preemption, should the ResumeReadiness evaluator consider the effective
priority? Or are priority shaping and readiness evaluation independent?

**Impact:** Determines whether the readiness gate validates against the
current effective priority (which may differ from base).

**Resolution plan:**
1. Readiness evaluation and priority shaping are independent concerns:
   - Readiness evaluates checkpoint compatibility (can the job resume?).
   - Priority shaping evaluates checkpoint freshness (should the job
     be preemptable?).
2. The readiness gate does NOT consider effective priority. It validates
   the same dimensions as Phase 4 (checkpoint existence, completeness,
   age within maxCheckpointAge, compatibility).
3. Priority shaping resumes after the job starts running. The protection
   window starts fresh.

**Recommended:** Independent. Readiness does not consider effective priority.
Priority shaping starts after admission, not during readiness evaluation.

## OQ-8: Priority Shaping for Queued (Non-Running) RTJs

**Question:** Should the Priority Shaping Controller adjust effective
priority for Queued RTJs (waiting for admission), or only for Running RTJs?

**Impact:** If a queued RTJ has a stale checkpoint from a previous run, its
effective priority during re-admission might affect scheduling order.

**Resolution plan:**
1. For queued RTJs, the effective priority should be reset to the base
   priority. Rationale: the checkpoint staleness is a property of the
   previous run, not the pending admission. The job will create a fresh
   checkpoint after it starts running.
2. Priority shaping only applies to Running/Starting/Restoring phases.
3. On transition to Queued (after preemption), the controller resets
   `Workload.Spec.Priority` to the base value.

**Recommended:** Reset to base priority when queued. Shape only when running.
