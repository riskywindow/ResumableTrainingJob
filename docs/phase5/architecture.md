# Phase 5 Architecture

## Component Diagram

```
┌────────────────────────────────────────────────────────────────────────────────────┐
│                            Kubernetes Cluster                                      │
│                                                                                    │
│  ┌──────────┐                                                                      │
│  │   User   │                                                                      │
│  └────┬─────┘                                                                      │
│       │ creates                                                                    │
│       v                                                                            │
│  ┌─────────────────────────────────────────────┐                                   │
│  │  ResumableTrainingJob (CRD)                 │                                   │
│  │                                             │                                   │
│  │  spec.suspend              (Kueue gate)     │                                   │
│  │  spec.workloadPriorityClassName (base)      │                                   │
│  │  spec.priorityShaping          ◄── NEW      │                                   │
│  │  spec.identity.worldSize       (requested)  │                                   │
│  │  spec.topology.*               (Phase 4)    │                                   │
│  │  spec.parallelism.*            (Phase 3)    │                                   │
│  │  spec.resume.*                 (Phase 3)    │                                   │
│  │  spec.checkpoint.*                          │                                   │
│  │  spec.runtime.template         (JobSet)     │                                   │
│  │                                             │                                   │
│  │  status.admission.*            (Phase 3)    │                                   │
│  │  status.restore.*              (Phase 3)    │                                   │
│  │  status.topology.*             (Phase 4)    │                                   │
│  │  status.effectivePriority.*    ◄── NEW      │                                   │
│  └──────────────────┬──────────────────────────┘                                   │
│                     │                                                              │
│        ┌────────────┴───────────────────┐                                          │
│        │                                │                                          │
│        v                                v                                          │
│  ┌──────────────────┐   ┌──────────────────────────────────────────────────────┐   │
│  │ Kueue            │   │ RTJ Operator                                         │   │
│  │                  │   │                                                       │   │
│  │ ClusterQueue     │   │  ┌─────────────────────────────────────────────────┐  │   │
│  │  admissionChecks:│   │  │ RTJ Controller (Phase 2-4 core, unchanged)      │  │   │
│  │   - resume-ready │   │  │  - Kueue GenericJob adapter                     │  │   │
│  │  preemption:     │   │  │  - Graceful yield coordinator                   │  │   │
│  │   withinCQ:      │   │  │  - Checkpoint selector                          │  │   │
│  │    LowerPriority │   │  │  - Admission-aware launch planner               │  │   │
│  │ LocalQueue       │   │  │  - Flavor-aware JobSet renderer                 │  │   │
│  │ ResourceFlavor   │   │  │  - Topology-aware gate evaluator                │  │   │
│  │ Topology         │   │  └─────────────┬───────────────────────────────────┘  │   │
│  │                  │   │                │                                       │   │
│  │ WorkloadPriority │   │  ┌─────────────v───────────────────────────────────┐  │   │
│  │  Class           │   │  │ Priority Shaping Controller             ◄── NEW │  │   │
│  │  name: low-pri   │   │  │                                                 │  │   │
│  │  value: 100      │   │  │  Inputs:                                        │  │   │
│  │  name: high-pri  │   │  │    - base priority (from WorkloadPriorityClass) │  │   │
│  │  value: 1000     │   │  │    - checkpoint freshness (from S3 manifests)   │  │   │
│  │                  │   │  │    - yield budget state (protection window)      │  │   │
│  │ PriorityShaping  │   │  │    - PriorityShapingPolicy parameters           │  │   │
│  │  Policy  ◄── NEW │   │  │                                                 │  │   │
│  │  (cluster-scoped)│   │  │  Output:                                        │  │   │
│  │                  │   │  │    - effective priority (int32)                  │  │   │
│  │ Workload ────────┼───┤  │    → writes to Workload.Spec.Priority           │  │   │
│  │  .spec:          │   │  │                                                 │  │   │
│  │   priorityClass- │   │  │  Fail-safe:                                     │  │   │
│  │   Ref (immutable)│   │  │    - telemetry unavailable → keep base priority │  │   │
│  │   priority ◄─────┼───┤  │    - policy not found → keep base priority      │  │   │
│  │    (operator-    │   │  │                                                 │  │   │
│  │     owned,       │   │  └─────────────────────────────────────────────────┘  │   │
│  │     mutable)     │   │                                                       │   │
│  │  .status:        │   │  ┌─────────────────────────────────────────────────┐  │   │
│  │   admission:     │   │  │ Existing Phase 4 Components (unchanged)          │  │   │
│  │    PodSetAssign- │   │  │  - Topology-Aware PodSet Synthesizer             │  │   │
│  │    ments         │   │  │  - Topology-Aware JobSet Renderer                │  │   │
│  │                  │   │  │  - ResumeReadiness AdmissionCheck Controller     │  │   │
│  └──────────────────┘   │  └─────────────────────────────────────────────────┘  │   │
│                         └──────────────────┬────────────────────────────────────┘   │
│                                            │                                       │
│                                            v                                       │
│  ┌──────────────────────────────────────────────────────────────────────────┐       │
│  │  Child JobSet (plain runtime resource, no Kueue metadata)                │       │
│  │                                                                          │       │
│  │  replicatedJobs[i]:                                                      │       │
│  │    replicas = admitted count                                             │       │
│  │    template.spec.nodeSelector = flavor + topology domain                 │       │
│  │    template.spec.tolerations  = from admitted flavor                     │       │
│  │    env: YIELD_SDK_WORLD_SIZE  = admitted world size                      │       │
│  │    env: YIELD_SDK_TOPOLOGY_DOMAIN = assigned domain                      │       │
│  └────────────────────────────────────┬─────────────────────────────────────┘       │
│                                       │                                            │
│                                       v                                            │
│  ┌──────────────────────────────────────────────────────────────────────────┐       │
│  │  Training Pods                                                           │       │
│  │  - PyTorch DDP/FSDP with DCP checkpoint                                  │       │
│  │  - periodic checkpoint → manifest published to S3                        │       │
│  │  - checkpoint freshness = time since last manifest                       │       │
│  └──────────────────────────────────────────────────────────────────────────┘       │
│                                                                                    │
│  ┌────────────────────────────┐                                                    │
│  │  S3-Compatible Storage     │                                                    │
│  │  manifests/                │   ◄── Priority Shaping Controller reads             │
│  │  checkpoints/              │       checkpoint manifests to determine              │
│  │  yield-markers/            │       freshness                                     │
│  └────────────────────────────┘                                                    │
│                                                                                    │
└────────────────────────────────────────────────────────────────────────────────────┘
```

## Effective Priority Derivation

The Priority Shaping Controller computes effective priority using this formula:

```
effective_priority = base_priority - penalty

where:
  base_priority     = WorkloadPriorityClass.Value (from spec.workloadPriorityClassName)
  penalty           = 0                            if within protection window
  penalty           = 0                            if checkpoint_age <= freshnessThreshold
  penalty           = min(staleness_penalty, maxPenalty)  otherwise
  staleness_penalty = penaltyStepSize * ceil((checkpoint_age - freshnessThreshold) / freshnessThreshold)
```

Protection window state:

```
protection_active = (now - last_start_or_resume_time) < protectionDuration
```

Fail-safe:

```
if checkpoint_telemetry_unavailable:
    effective_priority = base_priority  (no penalty)
if policy_not_found:
    effective_priority = base_priority  (no penalty)
```

## Sequence Diagram 1: Protected Low-Priority RTJ + High-Priority Pending RTJ

This diagram shows that a low-priority RTJ within its yield budget is NOT
preempted when a high-priority RTJ arrives, because its effective priority
equals its base priority.

```
User            Low-Pri RTJ         Priority Shaping      Kueue              High-Pri RTJ
 │               Controller          Controller            │                    │
 │                │                    │                    │                    │
 │  create low-pri RTJ                 │                    │                    │
 │  (base priority = 100)              │                    │                    │
 │───────────────>│                    │                    │                    │
 │                │                    │                    │                    │
 │                │  Workload created   │                    │                    │
 │                │  priority = 100     │                    │                    │
 │                │────────────────────────────────────────>│                    │
 │                │                    │                    │                    │
 │                │  admitted, running  │                    │                    │
 │                │<───────────────────────────────────────│                    │
 │                │                    │                    │                    │
 │                │  protection window  │                    │                    │
 │                │  starts (e.g. 10m) │                    │                    │
 │                │                    │                    │                    │
 │                │                    │  evaluate:          │                    │
 │                │                    │  protection active  │                    │
 │                │                    │  → effective = 100  │                    │
 │                │                    │  (no penalty)       │                    │
 │                │                    │                    │                    │
 │  create high-pri RTJ (priority = 1000)                  │                    │
 │──────────────────────────────────────────────────────────────────────────────>│
 │                │                    │                    │                    │
 │                │                    │                    │  Workload created   │
 │                │                    │                    │  priority = 1000    │
 │                │                    │                    │<───────────────────│
 │                │                    │                    │                    │
 │                │                    │                    │  preemption check:  │
 │                │                    │                    │  low-pri effective  │
 │                │                    │                    │  = 100              │
 │                │                    │                    │  high-pri = 1000    │
 │                │                    │                    │                    │
 │                │                    │                    │  CQ has no quota    │
 │                │                    │                    │  for high-pri       │
 │                │                    │                    │                    │
 │                │                    │                    │  BUT: preemption    │
 │                │                    │                    │  policy says        │
 │                │                    │                    │  LowerPriority only │
 │                │                    │                    │                    │
 │                │                    │                    │  100 < 1000: low-   │
 │                │                    │                    │  pri IS lower →     │
 │                │                    │                    │  Kueue preempts     │
 │                │                    │                    │                    │
 │  NOTE: without Phase 5 yield budget, Kueue would preempt immediately.       │
 │  WITH yield budget, we have a race: if the operator has already lowered     │
 │  the effective priority before Kueue's preemption check, the job may be     │
 │  preempted. If the protection window is still active and effective priority  │
 │  is the same as base priority, Kueue's standard LowerPriority preemption    │
 │  still applies (100 < 1000 = preempt). The protection window does NOT       │
 │  prevent preemption by strictly higher-priority workloads. It prevents the  │
 │  ADDITIONAL priority reduction from checkpoint staleness.                    │
 │                                                                              │
 │  Protection window protects against: checkpoint-staleness-driven demotion.   │
 │  Protection window does NOT protect against: Kueue's standard preemption    │
 │  of lower-priority workloads by higher-priority pending workloads.           │
```

## Sequence Diagram 2: Checkpoint Completion -> Effective Priority Drop -> Kueue Preemption -> Graceful Yield

This diagram shows the full priority-shaping preemption cycle. Two RTJs
with the SAME base priority compete for quota. The one with the staler
checkpoint gets its effective priority lowered, making it the preemption
victim when a new workload arrives.

```
Same-Pri RTJ-A    Same-Pri RTJ-B    Priority Shaping    Kueue           S3 Storage    Pending RTJ-C
(running, fresh    (running, stale    Controller                                       (same priority,
 checkpoint)        checkpoint)                                                         waiting)
 │                  │                  │                  │                │              │
 │                  │                  │                  │                │              │
 │  ── both running, protection windows expired ──        │                │              │
 │                  │                  │                  │                │              │
 │                  │                  │  periodic eval    │                │              │
 │                  │                  │  for RTJ-A:       │                │              │
 │                  │                  │──────────────────────────────────>│              │
 │                  │                  │  manifest age     │                │              │
 │                  │                  │  = 2 min (fresh)  │                │              │
 │                  │                  │<──────────────────────────────────│              │
 │                  │                  │  penalty = 0      │                │              │
 │                  │                  │  effective = 500   │                │              │
 │                  │                  │                  │                │              │
 │                  │                  │  periodic eval    │                │              │
 │                  │                  │  for RTJ-B:       │                │              │
 │                  │                  │──────────────────────────────────>│              │
 │                  │                  │  manifest age     │                │              │
 │                  │                  │  = 15 min (stale) │                │              │
 │                  │                  │<──────────────────────────────────│              │
 │                  │                  │  penalty = 200    │                │              │
 │                  │                  │  effective = 300   │                │              │
 │                  │                  │                  │                │              │
 │                  │                  │  write Workload   │                │              │
 │                  │                  │  RTJ-B priority   │                │              │
 │                  │                  │  = 300            │                │              │
 │                  │                  │─────────────────>│                │              │
 │                  │                  │                  │                │              │
 │                  │                  │                  │  RTJ-C pending  │              │
 │                  │                  │                  │  priority = 500 │              │
 │                  │                  │                  │<─────────────────────────────│
 │                  │                  │                  │                │              │
 │                  │                  │                  │  preemption:    │              │
 │                  │                  │                  │  RTJ-C (500) >  │              │
 │                  │                  │                  │  RTJ-B (300)    │              │
 │                  │                  │                  │  → suspend B    │              │
 │                  │                  │                  │                │              │
 │                  │  spec.suspend=   │                  │                │              │
 │                  │  true (Kueue)    │                  │                │              │
 │                  │<────────────────────────────────────│                │              │
 │                  │                  │                  │                │              │
 │                  │  RTJ controller: │                  │                │              │
 │                  │  detect Kueue    │                  │                │              │
 │                  │  suspension →    │                  │                │              │
 │                  │  graceful yield  │                  │                │              │
 │                  │                  │                  │                │              │
 │                  │  write control   │                  │                │              │
 │                  │  ConfigMap:      │                  │                │              │
 │                  │  desiredState    │                  │                │              │
 │                  │  = Paused        │                  │                │              │
 │                  │                  │                  │                │              │
 │                  │  trainer saves   │                  │                │              │
 │                  │  DCP checkpoint  │                  │                │              │
 │                  │───────────────────────────────────────────────────>│              │
 │                  │  yield marker    │                  │                │              │
 │                  │───────────────────────────────────────────────────>│              │
 │                  │  manifest        │                  │                │              │
 │                  │───────────────────────────────────────────────────>│              │
 │                  │                  │                  │                │              │
 │                  │  delete child    │                  │                │              │
 │                  │  JobSet          │                  │                │              │
 │                  │                  │                  │                │              │
 │                  │  phase = Queued  │                  │  RTJ-C now     │              │
 │                  │                  │                  │  admitted,      │              │
 │                  │                  │                  │  running        │              │
 │                  │                  │                  │─────────────────────────────>│
```

## Sequence Diagram 3: Later Resume After Priority-Driven Preemption

This diagram shows RTJ-B resuming after the higher-effective-priority RTJ-C
completes and releases quota.

```
Kueue           ResumeReadiness     Priority Shaping    RTJ-B            S3 Storage    Child JobSet
 │               Controller          Controller          Controller         │              │
 │                │                    │                  │                  │              │
 │  RTJ-C finishes, quota freed        │                  │                  │              │
 │                │                    │                  │                  │              │
 │  RTJ-B Workload re-queued           │                  │                  │              │
 │  (still has lowered priority = 300) │                  │                  │              │
 │                │                    │                  │                  │              │
 │                │                    │  re-evaluate     │                  │              │
 │                │                    │  RTJ-B: queued,  │                  │              │
 │                │                    │  not running     │                  │              │
 │                │                    │  → reset penalty │                  │              │
 │                │                    │  → effective =   │                  │              │
 │                │                    │    base (500)    │                  │              │
 │                │                    │  write Workload  │                  │              │
 │                │                    │  priority = 500  │                  │              │
 │                │                    │────────────────>│                  │              │
 │                │                    │                  │                  │              │
 │  schedule: quota available           │                  │                  │              │
 │  reserve quota + topology (TAS)      │                  │                  │              │
 │                │                    │                  │                  │              │
 │  admission checks:                  │                  │                  │              │
 │   resume-readiness: Pending         │                  │                  │              │
 │────────────────>│                    │                  │                  │              │
 │                │                    │                  │                  │              │
 │                │  1. validate       │                  │                  │              │
 │                │     topology       │                  │                  │              │
 │                │  2. select latest  │                  │                  │              │
 │                │     compatible     │                  │                  │              │
 │                │     checkpoint     │──────────────────────────────────>│              │
 │                │                    │                  │ (catalog scan)  │              │
 │                │  3. verify compat  │                  │                  │              │
 │                │  4. set check      │<──────────────────────────────────│              │
 │                │     = Ready        │                  │                  │              │
 │<───────────────│                    │                  │                  │              │
 │                │                    │                  │                  │              │
 │  all checks passed                  │                  │                  │              │
 │  → admit (RunWithPodSetsInfo)        │                  │                  │              │
 │  → set suspend=false                │                  │                  │              │
 │─────────────────────────────────────────────────────>│                  │              │
 │                │                    │                  │                  │              │
 │                │                    │                  │  1. read topology│              │
 │                │                    │                  │     assignment   │              │
 │                │                    │                  │  2. select ckpt  │              │
 │                │                    │                  │     (validated)  │              │
 │                │                    │                  │  3. render child │              │
 │                │                    │                  │     JobSet       │ create        │
 │                │                    │                  │──────────────────────────────>│
 │                │                    │                  │                  │              │
 │                │                    │                  │  phase = Restoring → Running   │
 │                │                    │                  │  training resumes from ckpt   │
 │                │                    │                  │  global step monotonic        │
 │                │                    │                  │                  │              │
 │                │                    │  protection       │                  │              │
 │                │                    │  window restarts  │                  │              │
 │                │                    │  (from resume     │                  │              │
 │                │                    │   time)           │                  │              │
```

## Detailed Design

### PriorityShapingPolicy CRD

Phase 5 introduces a new cluster-scoped CRD that parameterises the priority
shaping behaviour:

```go
// PriorityShapingPolicy defines how checkpoint freshness and yield budgets
// influence effective Workload priority.
type PriorityShapingPolicy struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec PriorityShapingPolicySpec `json:"spec"`
}

type PriorityShapingPolicySpec struct {
    // ProtectionDuration is the yield budget window. During this window
    // after a job starts or resumes, no checkpoint-freshness penalty is
    // applied. Effective priority equals base priority.
    // Default: 10m.
    // +optional
    ProtectionDuration *metav1.Duration `json:"protectionDuration,omitempty"`

    // FreshnessThreshold is the maximum age of a checkpoint before
    // priority penalty begins. Measured from the most recent manifest
    // timestamp in S3-compatible storage.
    // Default: 5m.
    // +optional
    FreshnessThreshold *metav1.Duration `json:"freshnessThreshold,omitempty"`

    // PenaltyStepSize is the priority reduction per penalty step.
    // Each step corresponds to one freshnessThreshold duration of
    // additional staleness.
    // Default: 100.
    // +optional
    PenaltyStepSize *int32 `json:"penaltyStepSize,omitempty"`

    // MaxPenalty is the maximum cumulative priority reduction.
    // Effective priority will not drop below (base - maxPenalty).
    // Default: 500.
    // +optional
    MaxPenalty *int32 `json:"maxPenalty,omitempty"`

    // EvaluationInterval is how often the controller re-evaluates
    // effective priority for running workloads.
    // Default: 30s.
    // +optional
    EvaluationInterval *metav1.Duration `json:"evaluationInterval,omitempty"`
}
```

### RTJ Spec Extension

Phase 5 adds an optional reference to a PriorityShapingPolicy:

```go
// PriorityShapingRef references a PriorityShapingPolicy that configures
// checkpoint-aware priority shaping for this RTJ.
// When nil, no priority shaping is applied and effective priority equals
// the base priority from the WorkloadPriorityClass. Phase 4 behaviour
// is preserved.
// +optional
PriorityShapingRef *string `json:"priorityShapingRef,omitempty"`
```

### RTJ Status Extension

Phase 5 adds effective priority status:

```go
// EffectivePriorityStatus records the current effective priority state.
type EffectivePriorityStatus struct {
    // BasePriority is the integer value from the WorkloadPriorityClass.
    BasePriority int32 `json:"basePriority"`

    // EffectivePriority is the current computed priority on the Workload.
    EffectivePriority int32 `json:"effectivePriority"`

    // Penalty is the current priority reduction applied.
    Penalty int32 `json:"penalty"`

    // ProtectionWindowActive indicates whether the yield budget window
    // is still shielding this job from priority reduction.
    ProtectionWindowActive bool `json:"protectionWindowActive"`

    // ProtectionWindowExpiresAt is when the protection window expires.
    // +optional
    ProtectionWindowExpiresAt *metav1.Time `json:"protectionWindowExpiresAt,omitempty"`

    // LastCheckpointAge is the age of the most recent checkpoint manifest.
    // +optional
    LastCheckpointAge *metav1.Duration `json:"lastCheckpointAge,omitempty"`

    // LastEvaluationTime is when the priority was last evaluated.
    // +optional
    LastEvaluationTime *metav1.Time `json:"lastEvaluationTime,omitempty"`

    // Reason is a human-readable explanation of the current effective priority.
    // +optional
    Reason string `json:"reason,omitempty"`
}
```

### Effective Priority Ownership Model

The key design challenge is preventing Kueue's GenericJob reconciler from
clobbering the operator-written `Workload.Spec.Priority` value. The
ownership model:

```
                     Workload.Spec
                    ┌──────────────────────────────────┐
                    │                                  │
                    │  PriorityClassRef  ← set at      │
                    │    (immutable)       creation by  │
                    │                     Kueue GJ      │
                    │                     reconciler    │
                    │                                  │
                    │  Priority (int32) ← owned by     │
                    │    (mutable)        RTJ Priority  │
                    │                     Shaping       │
                    │                     Controller    │
                    │                                  │
                    └──────────────────────────────────┘
```

1. **Kueue GenericJob reconciler** creates the Workload with
   `PriorityClassRef` pointing to the WorkloadPriorityClass and sets the
   initial `Priority` from the class value. This happens once at creation.

2. **RTJ Priority Shaping Controller** periodically updates
   `Workload.Spec.Priority` to reflect the effective priority. This is a
   PATCH operation on the Priority field only.

3. **Conflict resolution:** If Kueue's reconciler resets Priority on its
   sync path, the Priority Shaping Controller's next evaluation will overwrite
   it again. The evaluation interval (default 30s) bounds the staleness.

4. **If the Kueue sync clobbers:** The smallest coherent fix is to have the
   RTJGenericJob adapter's sync path skip writing the Priority field when a
   PriorityShapingPolicy is attached. This is documented as OQ-1 and must
   be verified during implementation.

### Priority Shaping Controller Loop

```
every evaluationInterval:
  for each RTJ with spec.priorityShapingRef != nil:
    if RTJ.status.phase not in {Running, Starting, Restoring}:
      if Workload.Spec.Priority != basePriority:
        write basePriority to Workload.Spec.Priority
        # non-running jobs always get base priority
      continue

    basePriority = resolve WorkloadPriorityClass value
    protectionStart = RTJ.status.lastStartTime or lastResumeTime

    if now - protectionStart < policy.protectionDuration:
      effectivePriority = basePriority
      reason = "within protection window"
    else:
      latestManifestAge = query checkpoint catalog for latest manifest age
      if latestManifestAge == unavailable:
        effectivePriority = basePriority
        reason = "checkpoint telemetry unavailable, fail-safe"
      else if latestManifestAge <= policy.freshnessThreshold:
        effectivePriority = basePriority
        reason = "checkpoint fresh"
      else:
        steps = ceil((latestManifestAge - freshnessThreshold) / freshnessThreshold)
        penalty = min(steps * penaltyStepSize, maxPenalty)
        effectivePriority = basePriority - penalty
        reason = "checkpoint stale, penalty applied"

    if Workload.Spec.Priority != effectivePriority:
      patch Workload.Spec.Priority = effectivePriority
    update RTJ.status.effectivePriority
```

### Data Flow: Base Priority -> Effective Priority -> Kueue Preemption

```
RTJ spec.workloadPriorityClassName = "training-low"
    │
    │  (Kueue resolves WorkloadPriorityClass)
    v
WorkloadPriorityClass "training-low" .value = 100
    │
    │  (GenericJob reconciler creates Workload)
    v
Workload.Spec.PriorityClassRef = {group: "kueue.x-k8s.io",
                                   kind: "WorkloadPriorityClass",
                                   name: "training-low"}
Workload.Spec.Priority = 100  (initial)
    │
    │  (Priority Shaping Controller evaluates)
    │  protection window expired, checkpoint age = 15m
    │  freshnessThreshold = 5m, penaltyStepSize = 100, maxPenalty = 500
    │  steps = ceil((15m - 5m) / 5m) = 2
    │  penalty = min(2 * 100, 500) = 200
    v
Workload.Spec.Priority = -100  (100 - 200)
    │
    │  (Kueue preemption engine reads Workload priority)
    v
Kueue: pending workload priority (500) > running workload priority (-100)
    → preempt running workload (suspend RTJ)
    │
    │  (RTJ controller detects Kueue suspension)
    v
Graceful yield → checkpoint → re-queue → eventual resume
```

### Phase 4 Backward Compatibility

When `spec.priorityShapingRef` is nil:

1. **Priority:** No Priority Shaping Controller evaluation. Workload
   `Spec.Priority` is set by Kueue's GenericJob reconciler from the
   WorkloadPriorityClass and never mutated. Identical to Phase 4.
2. **Preemption:** Standard Kueue preemption based on declared priority.
3. **Status:** `status.effectivePriority` is nil.
4. **All Phase 4 features:** Topology, readiness gates, admission pipeline
   — all unchanged.

Phase 4 behaviour is fully preserved.

## Invariants

All Phase 0 through Phase 4 invariants remain in force. Phase 5 adds:

| Invariant | Description |
| --- | --- |
| **RTJ is the only Kueue-managed admission object** | Unchanged. The child JobSet remains a plain runtime resource. |
| **Kueue is the preemption authority** | The operator does NOT preempt workloads. It shapes the priority input that Kueue uses for its preemption decisions. |
| **Effective priority is derived, not declared** | The user declares base priority via WorkloadPriorityClass. The operator derives effective priority from base + checkpoint freshness + yield budget. |
| **Fail-safe: no penalty on telemetry failure** | If checkpoint manifests cannot be read, the operator keeps the base priority. No silent demotion. |
| **Protection window resets on resume** | When an RTJ resumes from a checkpoint, its protection window restarts from the resume time. |
| **Phase 4 preserved when shaping disabled** | When `spec.priorityShapingRef` is nil, all Phase 4 behaviour is identical. |
| **Priority field ownership is explicit** | The Priority Shaping Controller owns `Workload.Spec.Priority`. The GenericJob adapter defers to the controller when shaping is active. |
