# Phase 3 Architecture

## Component Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          Kubernetes Cluster                              │
│                                                                          │
│  ┌──────────┐                                                            │
│  │   User   │                                                            │
│  └────┬─────┘                                                            │
│       │ creates                                                          │
│       v                                                                  │
│  ┌──────────────────────────────────────┐                                │
│  │  ResumableTrainingJob (CRD)          │                                │
│  │                                      │                                │
│  │  spec.suspend           (Kueue gate) │                                │
│  │  spec.identity.worldSize (requested) │                                │
│  │  spec.resume.worldSizePolicy         │  ◄── NEW: Fixed | Flexible    │
│  │  spec.resume.minWorldSize            │  ◄── NEW: partial-admission   │
│  │  spec.runtime.template  (JobSet)     │      floor (experimental)     │
│  │  spec.checkpoint.*                   │                                │
│  │                                      │                                │
│  │  status.admittedWorldSize            │  ◄── NEW: effective world     │
│  │  status.admittedFlavors              │  ◄── NEW: flavor visibility   │
│  └────────────────┬─────────────────────┘                                │
│                   │                                                      │
│          ┌────────┴────────┐                                             │
│          │                 │                                             │
│          v                 v                                             │
│  ┌───────────────┐  ┌─────────────────────────────────────────────────┐  │
│  │ Kueue         │  │ RTJ Controller                                  │  │
│  │               │  │                                                 │  │
│  │ ClusterQueue  │  │  ┌─────────────────────────────────────────┐    │  │
│  │ LocalQueue    │  │  │ Admission-Aware Launch Planner          │    │  │
│  │ ResourceFlavor│  │  │  - reads podSetAssignments              │    │  │
│  │ Workload ─────┼──┤  │  - extracts nodeSelector, tolerations   │    │  │
│  │               │  │  │  - computes admitted world size         │    │  │
│  │ PodSet ───────┼──┤  │  - adjusts replica counts if partial   │    │  │
│  │  .Count       │  │  └─────────────────┬───────────────────────┘    │  │
│  │  .MinCount ◄──┼──┤                    │                            │  │
│  │  (experimental)  │  ┌─────────────────v───────────────────────┐    │  │
│  │               │  │  │ Flavor-Aware JobSet Renderer             │    │  │
│  │ PodSetAssign- │  │  │  - applies per-podSet nodeSelector      │    │  │
│  │ ments ────────┼──┤  │  - applies per-podSet tolerations       │    │  │
│  │  .Flavors     │  │  │  - adjusts replicatedJob replica counts │    │  │
│  │  .NodeSelector│  │  │  - strips Kueue management metadata     │    │  │
│  │  .Tolerations │  │  │  - injects admitted world size env vars │    │  │
│  │  .Count       │  │  └─────────────────┬───────────────────────┘    │  │
│  │               │  │                    │                            │  │
│  └───────────────┘  │  ┌─────────────────v───────────────────────┐    │  │
│                     │  │ Flexible Checkpoint Selector             │    │  │
│                     │  │  - Fixed mode: exact world-size match   │    │  │
│                     │  │  - Flexible mode: skip world-size check │    │  │
│                     │  │  - All other dimensions remain strict   │    │  │
│                     │  └─────────────────────────────────────────┘    │  │
│                     │                                                 │  │
│                     │  (Unchanged from Phase 2:)                      │  │
│                     │  - Graceful yield coordinator                   │  │
│                     │  - Kueue GenericJob adapter                     │  │
│                     │  - Workload observer                            │  │
│                     └────────────────────┬────────────────────────────┘  │
│                                          │                               │
│                                          v                               │
│  ┌──────────────────────────────────────────────────────────────┐        │
│  │  Child JobSet (plain runtime resource, no Kueue metadata)    │        │
│  │                                                              │        │
│  │  replicatedJobs[i]:                                          │        │
│  │    replicas = admitted count (may differ from requested)     │        │
│  │    template.spec.nodeSelector = from admitted flavor          │        │
│  │    template.spec.tolerations  = from admitted flavor          │        │
│  │    env: YIELD_SDK_WORLD_SIZE  = admitted world size           │        │
│  │    env: YIELD_SDK_ORIGINAL_WORLD_SIZE = requested world size  │        │
│  │    env: YIELD_SDK_ADMITTED_FLAVOR = flavor name               │        │
│  └────────────────────────────┬─────────────────────────────────┘        │
│                               │                                          │
│                               v                                          │
│  ┌──────────────────────────────────────────────────────────────┐        │
│  │  Training Pods                                                │        │
│  │  - PyTorch DDP / FSDP                                         │        │
│  │  - DCP checkpoint with world size metadata                    │        │
│  │  - DCP resharding on world-size-flexible restore              │        │
│  └──────────────────────────────────────────────────────────────┘        │
│                                                                          │
│  ┌───────────────────────────┐                                           │
│  │  S3-Compatible Storage    │                                           │
│  │  manifests/               │                                           │
│  │  checkpoints/             │                                           │
│  │  yield-markers/           │                                           │
│  └───────────────────────────┘                                           │
└──────────────────────────────────────────────────────────────────────────┘
```

## Sequence Diagram 1: Create → Queue → Admit → Launch at Admitted Shape

This is the happy-path admission-aware launch.

```
User            RTJ              Webhook         Kueue             RTJ Controller       Child JobSet
 │               │                │               │                    │                    │
 │  create RTJ   │                │               │                    │                    │
 │──────────────>│                │               │                    │                    │
 │               │  default()     │               │                    │                    │
 │               │───────────────>│               │                    │                    │
 │               │   suspend=true │               │                    │                    │
 │               │   labels sync  │               │                    │                    │
 │               │<───────────────│               │                    │                    │
 │               │                                │                    │                    │
 │               │     GenericJob.PodSets()        │                    │                    │
 │               │────────────────────────────────>│                    │                    │
 │               │     PodSet[]{name, template,    │                    │                    │
 │               │       count, minCount*}         │                    │                    │
 │               │<────────────────────────────────│                    │                    │
 │               │                                 │                    │                    │
 │               │     create Workload             │                    │                    │
 │               │     (RTJ-owned, podSets from    │                    │                    │
 │               │      embedded JobSet template)  │                    │                    │
 │               │                                 │                    │                    │
 │               │     ─── queue/schedule ───      │                    │                    │
 │               │                                 │                    │                    │
 │               │     admit: PodSetAssignments     │                    │                    │
 │               │     [{name: "workers",           │                    │                    │
 │               │       flavors: {gpu: "a100"},    │                    │                    │
 │               │       nodeSelector: {pool: a100},│                    │                    │
 │               │       tolerations: [...],        │                    │                    │
 │               │       count: 8}]                 │                    │                    │
 │               │                                 │                    │                    │
 │               │     RunWithPodSetsInfo()         │                    │                    │
 │               │────────────────────────────────>│                    │                    │
 │               │     (applies nodeSelector,      │                    │                    │
 │               │      tolerations, labels to     │                    │                    │
 │               │      RTJ template; sets         │                    │                    │
 │               │      suspend=false)             │                    │                    │
 │               │<────────────────────────────────│                    │                    │
 │               │                                                      │                    │
 │               │   reconcile: admitted, not suspended                  │                    │
 │               │─────────────────────────────────────────────────────>│                    │
 │               │                                                      │                    │
 │               │   1. compute admitted world size from counts          │                    │
 │               │   2. record admittedWorldSize, admittedFlavors        │                    │
 │               │   3. render child JobSet with:                        │                    │
 │               │      - nodeSelector from admission                   │                    │
 │               │      - tolerations from admission                    │                    │
 │               │      - replica counts from admission                 │                    │
 │               │      - YIELD_SDK_WORLD_SIZE = admitted world size    │                    │
 │               │      - YIELD_SDK_ADMITTED_FLAVOR = flavor name       │                    │
 │               │                                                      │ create             │
 │               │                                                      │──────────────────>│
 │               │                                                      │                    │
 │               │   phase = Starting → Running                         │    pods scheduled   │
 │               │                                                      │    on flavor nodes  │
 │               │                                                      │                    │
```

`*minCount` is set only when the experimental `PartialAdmission` feature gate
is enabled and `spec.resume.minWorldSize` is specified.

## Sequence Diagram 2: Suspend/Preempt → Checkpoint → Re-Admit → Resume at Different Shape

This diagram shows the world-size-flexible resume path when an RTJ is
preempted and re-admitted at a different shape.

```
Kueue           RTJ Controller         Trainer          S3 Storage       Child JobSet
 │                    │                   │                  │                │
 │  set suspend=true  │                   │                  │                │
 │───────────────────>│                   │                  │                │
 │                    │                   │                  │                │
 │                    │  record stop      │                  │                │
 │                    │  request id       │                  │                │
 │                    │                   │                  │                │
 │                    │  write control    │                  │                │
 │                    │  ConfigMap:       │                  │                │
 │                    │  desiredState=    │                  │                │
 │                    │  Paused           │                  │                │
 │                    │──────────────────>│                  │                │
 │                    │                   │                  │                │
 │                    │                   │  step boundary   │                │
 │                    │                   │  DCP checkpoint  │                │
 │                    │                   │  (world size=8)  │                │
 │                    │                   │─────────────────>│                │
 │                    │                   │  yield marker    │                │
 │                    │                   │─────────────────>│                │
 │                    │                   │  manifest        │                │
 │                    │                   │  (worldSize=8)   │                │
 │                    │                   │─────────────────>│                │
 │                    │                   │                  │                │
 │                    │  poll: yield      │                  │                │
 │                    │  marker + manifest│                  │                │
 │                    │──────────────────────────────────────>                │
 │                    │  evidence found   │                  │                │
 │                    │                   │                  │                │
 │                    │                                                delete │
 │                    │──────────────────────────────────────────────────────>│
 │                    │                                                       │
 │                    │  phase = Queued                                       │
 │                    │  (RTJ still Kueue-suspended)                          │
 │                    │                                                       │
 │  ── time passes, quota freed ──                                            │
 │                                                                            │
 │  re-admit with                                                             │
 │  different shape:                                                          │
 │  PodSetAssignments                                                         │
 │  [{count: 4,                                                               │
 │    flavors: {gpu: h100},                                                   │
 │    nodeSelector:                                                           │
 │    {pool: h100}}]                                                          │
 │                    │                                                       │
 │  RunWithPodSetsInfo│                                                       │
 │───────────────────>│                                                       │
 │                    │                                                       │
 │                    │  1. compute admitted world size = 4                    │
 │                    │  2. worldSizePolicy = Flexible?                        │
 │                    │     yes → skip world-size check                        │
 │                    │  3. select latest compatible checkpoint                │
 │                    │     (worldSize=8, all other fields match)              │
 │                    │  4. record admittedWorldSize=4,                        │
 │                    │     selectedCheckpoint (worldSize=8)                   │
 │                    │                                                       │
 │                    │  render child JobSet:                      create      │
 │                    │  - replicas = 4                            ──────────>│
 │                    │  - nodeSelector = {pool: h100}                        │
 │                    │  - YIELD_SDK_WORLD_SIZE = 4                           │
 │                    │  - YIELD_SDK_ORIGINAL_WORLD_SIZE = 8                  │
 │                    │  - YIELD_SDK_RESTORE_MANIFEST_URI = ...               │
 │                    │                                                       │
 │                    │                   │                                    │
 │                    │                   │  DCP load with                     │
 │                    │                   │  resharding:                       │
 │                    │                   │  8 ranks → 4 ranks                 │
 │                    │                   │                                    │
 │                    │  phase = Restoring → Running                           │
 │                    │  training continues with monotonic step                │
```

## Sequence Diagram 3: Flavor-Aware Launch

This diagram focuses on how ResourceFlavor admission info flows through
the system into the child JobSet.

```
                     Kueue Admission                  RTJ Template
                     ┌──────────────┐                (in RTJ spec)
                     │              │                ┌──────────────┐
                     │ PodSetAssign-│   RunWithPod-  │              │
                     │ ments[i]:    │   SetsInfo()   │ replicatedJob│
                     │              │──────────────> │ [i].template:│
                     │  name        │   podset.Merge │              │
                     │  flavors:    │   applies:     │  nodeSelector│ ◄── from flavor
                     │   gpu: a100  │                │  tolerations │ ◄── from flavor
                     │  nodeSelector│                │  labels      │ ◄── from Kueue
                     │   pool: a100 │                │  annotations │ ◄── from Kueue
                     │  tolerations │                │              │
                     │   [...]      │                └──────┬───────┘
                     │  count: 8    │                       │
                     │              │                       │
                     └──────────────┘                       │
                                                            │ RenderChildJobSet()
                                                            │ reads mutated
                                                            │ template from
                                                            │ RTJ spec
                                                            v
                                                   ┌──────────────────┐
                                                   │ Child JobSet     │
                                                   │ (plain runtime)  │
                                                   │                  │
                                                   │ replicatedJobs:  │
                                                   │  [i]:            │
                                                   │   replicas: 8    │ ◄── from admitted count
                                                   │   template:      │
                                                   │    nodeSelector: │ ◄── carried through
                                                   │     pool: a100   │
                                                   │    tolerations:  │ ◄── carried through
                                                   │     [...]        │
                                                   │                  │
                                                   │ NO kueue.x-k8s. │
                                                   │ io/* labels      │ ◄── stripped
                                                   └──────────────────┘
```

### Flavor Flow Detail

1. **Kueue admits** the RTJ Workload and sets
   `Workload.Status.Admission.PodSetAssignments`. Each assignment carries a
   `Flavors` map (resource type → flavor name) and the flavor's
   `nodeSelector` and `tolerations`.

2. **Kueue calls `RunWithPodSetsInfo`** on the RTJ GenericJob adapter.
   `podset.PodSetInfo` carries `NodeSelector`, `Tolerations`, `Labels`,
   `Annotations`, and `Count` (admitted count). `podset.Merge` applies these
   onto the corresponding `replicatedJobs[i].template.spec.template` in the
   RTJ's embedded JobSet spec.

3. **The mutated RTJ spec is persisted** (Kueue's generic reconciler updates
   the RTJ object after `RunWithPodSetsInfo` returns).

4. **The RTJ controller reads the mutated template** during its own reconcile.
   `RenderChildJobSet` copies the embedded spec (now carrying admission
   mutations) into the child JobSet. The renderer:
   - Strips any Kueue management labels/annotations (Phase 2 invariant).
   - Adjusts replica counts to match the admitted count (Phase 3 addition).
   - Injects `YIELD_SDK_WORLD_SIZE`, `YIELD_SDK_ADMITTED_FLAVOR`, and other
     environment variables.

5. **The child JobSet creates pods** that land on the correct node pool
   because `nodeSelector` and `tolerations` from the flavor are present in
   the pod templates.

## Data Flow: Admitted Count → Child JobSet Replicas → World Size

Phase 2 does not adjust child JobSet replica counts based on admitted counts.
The admission mutations apply only `nodeSelector`, `tolerations`, and labels.

Phase 3 adds this flow:

```
Workload.Status.Admission.PodSetAssignments[i].Count
    │
    │  (Kueue generic reconciler builds PodSetInfo)
    v
podset.PodSetInfo.Count
    │
    │  (RunWithPodSetsInfo stores on RTJ)
    v
RTJ controller reads admitted template
    │
    │  (Phase 3 logic)
    v
admittedWorldSize = sum(admittedCount[i] * podsPerReplica[i])
    │
    ├──> status.admittedWorldSize
    ├──> child JobSet replicatedJobs[i].replicas = admittedCount[i] / podsPerReplica[i]
    └──> env YIELD_SDK_WORLD_SIZE = admittedWorldSize
```

When `PartialAdmission` is disabled (default), the admitted count always
equals the requested count, and the world size does not change. This path
degenerates to Phase 2 behavior.

## API Surface Changes

### New Spec Fields

```go
// ResumePolicy defines restore selection and bounded retries.
type ResumePolicy struct {
    SourcePolicy     ResumeSourcePolicy `json:"sourcePolicy,omitempty"`
    MaxResumeRetries int32              `json:"maxResumeRetries"`

    // WorldSizePolicy controls whether resume requires exact world-size
    // match or allows DCP resharding across world sizes.
    // Default: Fixed (preserves Phase 2 behavior).
    // +optional
    WorldSizePolicy WorldSizePolicy `json:"worldSizePolicy,omitempty"`

    // MinWorldSize is the minimum acceptable world size for partial
    // admission. Only effective when the PartialAdmission feature gate
    // is enabled. Must be <= spec.identity.worldSize.
    // +optional
    MinWorldSize *int32 `json:"minWorldSize,omitempty"`
}

// WorldSizePolicy controls world-size flexibility on resume.
// +kubebuilder:validation:Enum=Fixed;Flexible
type WorldSizePolicy string

const (
    WorldSizePolicyFixed    WorldSizePolicy = "Fixed"
    WorldSizePolicyFlexible WorldSizePolicy = "Flexible"
)
```

### New Status Fields

```go
type ResumableTrainingJobStatus struct {
    // ... existing fields ...

    // AdmittedWorldSize is the effective world size derived from the
    // admitted PodSetAssignment counts. Zero when not yet admitted.
    // +optional
    AdmittedWorldSize int32 `json:"admittedWorldSize,omitempty"`

    // AdmittedFlavors records the ResourceFlavor names assigned by Kueue
    // for each pod set, keyed by pod set name.
    // +optional
    AdmittedFlavors map[string]string `json:"admittedFlavors,omitempty"`
}
```

### New Environment Variables

| Variable | Purpose |
| --- | --- |
| `YIELD_SDK_WORLD_SIZE` | Admitted (effective) world size for this run attempt. Already exists but Phase 3 ensures it reflects the admitted count, not just the requested count. |
| `YIELD_SDK_ORIGINAL_WORLD_SIZE` | The world size recorded in the checkpoint being restored. Set only when restoring and the values differ. Trainer uses this to configure DCP resharding. |
| `YIELD_SDK_ADMITTED_FLAVOR` | Comma-separated list of `podSetName=flavorName` entries. Informational; for trainer logging and metrics. |

### Feature Gate

```go
const (
    // PartialAdmission enables experimental partial-admission support
    // for RTJ. When enabled, RTJ can declare spec.resume.minWorldSize
    // and Kueue may admit fewer replicas than requested.
    // Default: disabled.
    FeatureGatePartialAdmission = "PartialAdmission"
)
```

## Compatibility Checker Changes

### Fixed Mode (Default, Phase 2 Behavior)

No change. The `CheckManifestCompatibility` function requires exact world-size
match:

```
manifest.WorldSize != request.WorldSize → reject
```

### Flexible Mode (Phase 3)

When `worldSizePolicy=Flexible`, the compatibility checker skips the
world-size equality check:

```go
if request.WorldSizePolicy == WorldSizePolicyFlexible {
    // Skip world-size check; DCP resharding handles the difference.
    // Still record original world size for audit trail.
} else {
    if manifest.WorldSize != request.WorldSize {
        return false, "world size mismatch"
    }
}
```

All other compatibility dimensions remain strict and unchanged:

- Cluster identity (exact match)
- RTJ lineage identity (exact match)
- Runtime mode (exact match)
- GPU shape (exact match)
- Image identity (exact match)
- Code version identity (exact match)
- Optimizer mode (exact match)
- Sharding mode (exact match)
- Format version (supported set)

## PodSet MinCount Synthesis (Experimental)

When the `PartialAdmission` feature gate is enabled and
`spec.resume.minWorldSize` is set:

```go
func PodSetsFromRTJTemplate(job *trainingv1alpha1.ResumableTrainingJob) ([]kueuev1beta2.PodSet, error) {
    // ... existing logic ...

    for i, replicatedJob := range spec.ReplicatedJobs {
        podSets[i] = kueuev1beta2.PodSet{
            Name:     kueuev1beta2.NewPodSetReference(replicatedJob.Name),
            Template: *replicatedJob.Template.Spec.Template.DeepCopy(),
            Count:    podsCount(&replicatedJob),
        }

        // Phase 3: set MinCount when partial admission is enabled
        if featuregate.Enabled(FeatureGatePartialAdmission) && job.Spec.Resume.MinWorldSize != nil {
            minCount := scaleCount(
                podsCount(&replicatedJob),
                job.Spec.Identity.WorldSize,
                *job.Spec.Resume.MinWorldSize,
            )
            podSets[i].MinCount = ptr.To(minCount)
        }
    }
    return podSets, nil
}
```

The `scaleCount` helper proportionally scales the per-pod-set count from the
requested world size down to the minimum world size.

## Invariants

All Phase 0 through Phase 2 invariants remain in force. Phase 3 adds:

| Invariant | Description |
| --- | --- |
| **Admitted Shape Faithfulness** | The child JobSet MUST reflect the admitted nodeSelector, tolerations, and replica counts. The controller MUST NOT create a child JobSet with a shape that diverges from the admission decision. |
| **World-Size Audit Trail** | Checkpoint manifests always record the world size at which they were created. The controller and trainer MUST NOT overwrite or suppress this metadata. |
| **Flexible Resume Requires Explicit Opt-In** | World-size-flexible resume MUST NOT activate unless `spec.resume.worldSizePolicy=Flexible`. The default is `Fixed`, preserving Phase 2 exact-match behavior. |
| **Partial Admission Requires Feature Gate** | `spec.resume.minWorldSize` and PodSet `MinCount` MUST NOT be set unless the `PartialAdmission` feature gate is enabled. The gate is off by default. |
| **DCP Resharding Is Trainer Responsibility** | The controller tells the trainer the original and current world sizes. The trainer is responsible for invoking PyTorch DCP's resharding path. The controller does not verify resharding correctness; it verifies that training resumes with monotonically increasing global step. |
