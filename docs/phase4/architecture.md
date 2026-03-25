# Phase 4 Architecture

## Component Diagram

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                            Kubernetes Cluster                                 │
│                                                                               │
│  ┌──────────┐                                                                 │
│  │   User   │                                                                 │
│  └────┬─────┘                                                                 │
│       │ creates                                                               │
│       v                                                                       │
│  ┌────────────────────────────────────────┐                                   │
│  │  ResumableTrainingJob (CRD)            │                                   │
│  │                                        │                                   │
│  │  spec.suspend            (Kueue gate)  │                                   │
│  │  spec.identity.worldSize (requested)   │                                   │
│  │  spec.topology.required     ◄── NEW    │                                   │
│  │  spec.topology.preferred    ◄── NEW    │                                   │
│  │  spec.resume.*              (Phase 3)  │                                   │
│  │  spec.parallelism.*         (Phase 3)  │                                   │
│  │  spec.runtime.template    (JobSet)     │                                   │
│  │  spec.checkpoint.*                     │                                   │
│  │                                        │                                   │
│  │  status.admission.*         (Phase 3)  │                                   │
│  │  status.restore.*           (Phase 3)  │                                   │
│  │  status.topology.*          ◄── NEW    │                                   │
│  │  status.admissionCheck.*    ◄── NEW    │                                   │
│  └──────────────────┬─────────────────────┘                                   │
│                     │                                                         │
│        ┌────────────┴──────────────┐                                          │
│        │                           │                                          │
│        v                           v                                          │
│  ┌──────────────────┐   ┌──────────────────────────────────────────────────┐  │
│  │ Kueue            │   │ RTJ Operator                                     │  │
│  │                  │   │                                                   │  │
│  │ ClusterQueue     │   │  ┌─────────────────────────────────────────────┐  │  │
│  │  admissionChecks:│   │  │ RTJ Controller (Phase 2+3, unchanged core)  │  │  │
│  │   - resume-ready │   │  │  - Kueue GenericJob adapter                 │  │  │
│  │   - prov-req*    │   │  │  - Graceful yield coordinator               │  │  │
│  │ LocalQueue       │   │  │  - Checkpoint selector                      │  │  │
│  │ ResourceFlavor   │   │  │  - Admission-aware launch planner           │  │  │
│  │ Topology         │   │  │  - Flavor-aware JobSet renderer             │  │  │
│  │                  │   │  └─────────────┬───────────────────────────────┘  │  │
│  │ Workload ────────┼───┤               │                                   │  │
│  │  .podSets[i]:    │   │  ┌────────────v────────────────────────────────┐  │  │
│  │   TopologyRequest│   │  │ Topology-Aware PodSet Synthesizer  ◄── NEW  │  │  │
│  │  .admission:     │   │  │  - reads spec.topology                      │  │  │
│  │   PodSetAssign-  │   │  │  - sets TopologyRequest on worker PodSets   │  │  │
│  │   ments[i]:      │   │  │  - passes through to Kueue TAS             │  │  │
│  │    Topology-     │   │  └─────────────────────────────────────────────┘  │  │
│  │    Assignment    │   │                                                   │  │
│  │   .admissionChks:│   │  ┌─────────────────────────────────────────────┐  │  │
│  │    resume-ready  │   │  │ Topology-Aware JobSet Renderer     ◄── NEW  │  │  │
│  │    state: Ready  │   │  │  - reads TopologyAssignment from Workload   │  │  │
│  │                  │   │  │  - injects per-domain scheduling            │  │  │
│  │ AdmissionCheck   │   │  │    constraints into child JobSet            │  │  │
│  │  name: resume-   │   │  │  - preserves flavor nodeSelector/toleration │  │  │
│  │   readiness      │   │  └─────────────────────────────────────────────┘  │  │
│  │  controllerName: │   │                                                   │  │
│  │   checkpoint-    │   │  ┌─────────────────────────────────────────────┐  │  │
│  │   native.example │   │  │ ResumeReadiness AdmissionCheck     ◄── NEW  │  │  │
│  │   .io/resume-    │   │  │  Controller                                 │  │  │
│  │   readiness      │   │  │  - watches Workloads with resume-readiness  │  │  │
│  │                  │   │  │    admission check                          │  │  │
│  │ ProvisioningReq* │   │  │  - waits for topology assignment            │  │  │
│  │  (optional)      │   │  │  - validates renderability                  │  │  │
│  │                  │   │  │  - validates checkpoint compat (on resume)  │  │  │
│  └──────────────────┘   │  │  - marks check Ready → Kueue admits         │  │  │
│                         │  └─────────────────────────────────────────────┘  │  │
│                         └──────────────────┬───────────────────────────────┘   │
│                                            │                                  │
│                                            v                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐      │
│  │  Child JobSet (plain runtime resource, no Kueue metadata)           │      │
│  │                                                                     │      │
│  │  replicatedJobs[i]:                                                 │      │
│  │    replicas = admitted count                                        │      │
│  │    template.spec.nodeSelector = flavor + topology domain    ◄── NEW │      │
│  │    template.spec.tolerations  = from admitted flavor                │      │
│  │    template.spec.affinity     = topology affinity           ◄── NEW │      │
│  │    env: YIELD_SDK_WORLD_SIZE  = admitted world size                 │      │
│  │    env: YIELD_SDK_TOPOLOGY_DOMAIN = assigned domain         ◄── NEW │      │
│  └───────────────────────────────┬─────────────────────────────────────┘      │
│                                  │                                            │
│                                  v                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐      │
│  │  Training Pods                                                       │      │
│  │  - scheduled to topology-assigned nodes                              │      │
│  │  - PyTorch DDP/FSDP with NCCL optimized for topology                │      │
│  │  - DCP checkpoint with topology metadata                             │      │
│  └─────────────────────────────────────────────────────────────────────┘      │
│                                                                               │
│  ┌────────────────────────────┐                                               │
│  │  S3-Compatible Storage     │                                               │
│  │  manifests/                │                                               │
│  │  checkpoints/              │                                               │
│  │  yield-markers/            │                                               │
│  └────────────────────────────┘                                               │
│                                                                               │
│  * ProvisioningRequest and its controller are optional; not required for       │
│    local Phase 4 success.                                                     │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Sequence Diagram 1: Submit → Queue → Admission Check → Admission → Topology-Aware Launch

This is the happy-path topology-aware launch with the ResumeReadiness gate.

```
User            RTJ              Webhook         Kueue              ResumeReadiness     RTJ Controller       Child JobSet
 │               │                │               │                  Controller           │                    │
 │  create RTJ   │                │               │                    │                   │                    │
 │  with spec.   │                │               │                    │                   │                    │
 │  topology     │                │               │                    │                   │                    │
 │──────────────>│                │               │                    │                   │                    │
 │               │  default()     │               │                    │                   │                    │
 │               │───────────────>│               │                    │                   │                    │
 │               │   suspend=true │               │                    │                   │                    │
 │               │<───────────────│               │                    │                   │                    │
 │               │                                │                    │                   │                    │
 │               │  GenericJob.PodSets()           │                    │                   │                    │
 │               │────────────────────────────────>│                    │                   │                    │
 │               │  PodSet[]{name, template,       │                    │                   │                    │
 │               │    count, minCount*,            │                    │                   │                    │
 │               │    topologyRequest: {           │                    │                   │                    │
 │               │      required: "zone"}}         │                    │                   │                    │
 │               │<────────────────────────────────│                    │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │  create Workload                │                    │                   │                    │
 │               │  (RTJ-owned, podSets with       │                    │                   │                    │
 │               │   TopologyRequest)              │                    │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │  ─── queue / schedule ───        │                    │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │  1. reserve quota                │                    │                   │                    │
 │               │  2. TAS assigns topology:        │                    │                   │                    │
 │               │     PodSetAssignment[i]:         │                    │                   │                    │
 │               │       topologyAssignment:        │                    │                   │                    │
 │               │         levels: ["zone"]         │                    │                   │                    │
 │               │         domains:                 │                    │                   │                    │
 │               │           [{values:["zone-a"],   │                    │                   │                    │
 │               │             count: 4},           │                    │                   │                    │
 │               │            {values:["zone-b"],   │                    │                   │                    │
 │               │             count: 4}]           │                    │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │  3. check admission checks       │                    │                   │                    │
 │               │     resume-readiness: Pending    │                    │                   │                    │
 │               │─────────────────────────────────────────────────────>│                   │                    │
 │               │                                 │                    │                   │                    │
 │               │                                 │   watch: Workload  │                   │                    │
 │               │                                 │   has topology     │                   │                    │
 │               │                                 │   assignment and   │                   │                    │
 │               │                                 │   resume-readiness │                   │                    │
 │               │                                 │   check pending    │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │                                 │   1. validate      │                   │                    │
 │               │                                 │      topology is   │                   │                    │
 │               │                                 │      renderable    │                   │                    │
 │               │                                 │   2. first launch: │                   │                    │
 │               │                                 │      no checkpoint │                   │                    │
 │               │                                 │      compat needed │                   │                    │
 │               │                                 │   3. set admission │                   │                    │
 │               │                                 │      check state   │                   │                    │
 │               │                                 │      = Ready       │                   │                    │
 │               │                                 │<────────────────── │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │  all checks passed               │                    │                   │                    │
 │               │  → admit Workload               │                    │                   │                    │
 │               │                                 │                    │                   │                    │
 │               │  RunWithPodSetsInfo()            │                    │                   │                    │
 │               │────────────────────────────────>│                    │                   │                    │
 │               │  (applies nodeSelector,         │                    │                   │                    │
 │               │   tolerations, topology info,   │                    │                   │                    │
 │               │   sets suspend=false)           │                    │                   │                    │
 │               │<────────────────────────────────│                    │                   │                    │
 │               │                                                                          │                    │
 │               │  reconcile: admitted, not suspended                                      │                    │
 │               │─────────────────────────────────────────────────────────────────────────>│                    │
 │               │                                                                          │                    │
 │               │  1. read topology assignment from Workload                                │                    │
 │               │  2. compute topology-aware scheduling constraints                         │                    │
 │               │  3. record status.topology (assigned domains)                             │                    │
 │               │  4. render child JobSet with:                                             │                    │
 │               │     - nodeSelector from flavor + topology domain                          │                    │
 │               │     - tolerations from flavor                                             │                    │
 │               │     - topology affinity constraints                                       │                    │
 │               │     - replica counts from admission                                       │                    │
 │               │     - YIELD_SDK_WORLD_SIZE, YIELD_SDK_TOPOLOGY_DOMAIN                     │                    │
 │               │                                                                          │ create              │
 │               │                                                                          │────────────────────>│
 │               │                                                                          │                    │
 │               │  phase = Starting → Running                                               │  pods scheduled     │
 │               │                                                                          │  to topology        │
 │               │                                                                          │  domains            │
```

## Sequence Diagram 2: Suspend/Preempt → Checkpoint → Re-Admit → Topology-Aware Resume

This diagram shows the topology-aware resume path when an RTJ is preempted
and re-admitted with a potentially different topology assignment.

```
Kueue           ResumeReadiness     RTJ Controller         Trainer          S3 Storage       Child JobSet
 │               Controller           │                     │                  │                │
 │                 │                   │                     │                  │                │
 │  set suspend=true                  │                     │                  │                │
 │────────────────────────────────────>│                     │                  │                │
 │                 │                   │                     │                  │                │
 │                 │                   │  write control      │                  │                │
 │                 │                   │  ConfigMap:         │                  │                │
 │                 │                   │  desiredState=Paused│                  │                │
 │                 │                   │────────────────────>│                  │                │
 │                 │                   │                     │                  │                │
 │                 │                   │                     │  step boundary   │                │
 │                 │                   │                     │  DCP checkpoint  │                │
 │                 │                   │                     │─────────────────>│                │
 │                 │                   │                     │  yield marker    │                │
 │                 │                   │                     │─────────────────>│                │
 │                 │                   │                     │  manifest        │                │
 │                 │                   │                     │─────────────────>│                │
 │                 │                   │                     │                  │                │
 │                 │                   │  poll: yield marker  │                  │                │
 │                 │                   │  + manifest found    │                  │                │
 │                 │                   │                     │                  │                │
 │                 │                   │                                          delete         │
 │                 │                   │──────────────────────────────────────────────────────>│
 │                 │                   │                                                       │
 │                 │                   │  phase = Queued                                       │
 │                 │                   │  (RTJ still Kueue-suspended)                          │
 │                 │                   │                                                       │
 │  ── time passes, quota freed ──     │                                                       │
 │                 │                   │                                                       │
 │  re-admit with  │                   │                                                       │
 │  new topology:  │                   │                                                       │
 │  PodSetAssignment                   │                                                       │
 │  [{count: 8,    │                   │                                                       │
 │    topology:    │                   │                                                       │
 │    {levels:     │                   │                                                       │
 │     ["zone"],   │                   │                                                       │
 │     domains:    │                   │                                                       │
 │     [{values:   │                   │                                                       │
 │      ["zone-c"],│                   │                                                       │
 │      count:8}]}}│                   │                                                       │
 │  ]              │                   │                                                       │
 │                 │                   │                                                       │
 │  resume-readiness                   │                                                       │
 │  check: Pending │                   │                                                       │
 │────────────────>│                   │                                                       │
 │                 │                   │                                                       │
 │                 │  1. read new      │                                                       │
 │                 │     topology      │                                                       │
 │                 │     assignment    │                                                       │
 │                 │  2. validate      │                                                       │
 │                 │     topology      │                                                       │
 │                 │     renderable    │                                                       │
 │                 │  3. select latest │                                                       │
 │                 │     compatible    │──────────────────────────────────────>│                │
 │                 │     checkpoint    │  (catalog scan)                      │                │
 │                 │  4. verify compat │<──────────────────────────────────────│                │
 │                 │     with new      │                                                       │
 │                 │     shape         │                                                       │
 │                 │  5. set check     │                                                       │
 │                 │     = Ready       │                                                       │
 │<────────────────│                   │                                                       │
 │                 │                   │                                                       │
 │  all checks     │                   │                                                       │
 │  passed         │                   │                                                       │
 │  → admit        │                   │                                                       │
 │                 │                   │                                                       │
 │  RunWithPodSetsInfo                 │                                                       │
 │────────────────────────────────────>│                                                       │
 │                 │                   │                                                       │
 │                 │                   │  1. read topology assignment                          │
 │                 │                   │  2. select checkpoint (already validated)              │
 │                 │                   │  3. render topology-aware child JobSet   create        │
 │                 │                   │     - nodeSelector for zone-c            ──────────>   │
 │                 │                   │     - topology affinity                                │
 │                 │                   │     - YIELD_SDK_RESTORE_MANIFEST_URI                   │
 │                 │                   │     - YIELD_SDK_ORIGINAL_WORLD_SIZE                    │
 │                 │                   │     - YIELD_SDK_TOPOLOGY_DOMAIN                        │
 │                 │                   │                                                       │
 │                 │                   │  phase = Restoring → Running                          │
 │                 │                   │  training continues with monotonic step               │
```

## Sequence Diagram 3: Optional Cloud Path with ProvisioningRequest

This diagram shows the admission pipeline when ProvisioningRequest is
configured on the ClusterQueue alongside ResumeReadiness. This path is
optional and not required for local Phase 4 success.

```
User            RTJ              Kueue              Provisioning       Cloud            ResumeReadiness    RTJ Controller
 │               │                │                  Request Ctrl       Provider          Controller          │
 │  create RTJ   │                │                    │                  │                  │                │
 │──────────────>│                │                    │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  create Workload                    │                  │                  │                │
 │               │  (with TopologyRequest)             │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  ─── queue / reserve quota ───       │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  admission checks:                  │                  │                  │                │
 │               │   1. prov-req: Pending              │                  │                  │                │
 │               │   2. resume-readiness: Pending      │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  ── ProvisioningRequest flow ──     │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │                │  create             │                  │                  │                │
 │               │                │  ProvisioningReq    │                  │                  │                │
 │               │                │───────────────────>│                  │                  │                │
 │               │                │                    │  request nodes   │                  │                │
 │               │                │                    │─────────────────>│                  │                │
 │               │                │                    │                  │                  │                │
 │               │                │                    │  nodes ready     │                  │                │
 │               │                │                    │<─────────────────│                  │                │
 │               │                │                    │                  │                  │                │
 │               │                │  prov-req: Ready    │                  │                  │                │
 │               │                │<───────────────────│                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  ── TAS topology assignment ──      │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  PodSetAssignment with              │                  │                  │                │
 │               │  TopologyAssignment on              │                  │                  │                │
 │               │  provisioned nodes                  │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  ── ResumeReadiness flow ──         │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │                │  resume-readiness   │                  │                  │                │
 │               │                │  Pending with       │                  │                  │                │
 │               │                │  topology assigned  │                  │                  │                │
 │               │                │──────────────────────────────────────────────────────────>│                │
 │               │                │                    │                  │                  │                │
 │               │                │                    │                  │  1. validate      │                │
 │               │                │                    │                  │     topology      │                │
 │               │                │                    │                  │  2. set check     │                │
 │               │                │                    │                  │     = Ready       │                │
 │               │                │<─────────────────────────────────────────────────────────│                │
 │               │                │                    │                  │                  │                │
 │               │  all checks passed                  │                  │                  │                │
 │               │  → admit (RunWithPodSetsInfo)       │                  │                  │                │
 │               │                │                    │                  │                  │                │
 │               │  RTJ unsuspended                    │                  │                  │                │
 │               │────────────────────────────────────────────────────────────────────────────────────────────>│
 │               │                │                    │                  │                  │  create child  │
 │               │                │                    │                  │                  │  JobSet on     │
 │               │                │                    │                  │                  │  provisioned   │
 │               │                │                    │                  │                  │  topology-     │
 │               │                │                    │                  │                  │  assigned nodes│
```

## Detailed Design

### Topology Spec on RTJ

Phase 4 adds an optional `spec.topology` section to the RTJ:

```go
// TopologySpec declares topology placement requirements for the training job.
// When set, the operator includes TopologyRequest on the Workload PodSets,
// enabling Kueue's TopologyAwareScheduling to assign topology domains.
// +optional
type TopologySpec struct {
    // Required specifies the topology level that MUST be satisfied.
    // Value is a node label key (e.g., "topology.kubernetes.io/zone",
    // "kubernetes.io/hostname", or a custom rack label).
    // When set, all pods in a PodSet are assigned to topology domains
    // at this level.
    // +optional
    Required *string `json:"required,omitempty"`

    // Preferred specifies the topology level that SHOULD be satisfied
    // on a best-effort basis. Kueue tries to place pods in the same
    // domain at this level but may spread across domains if capacity
    // is insufficient.
    // +optional
    Preferred *string `json:"preferred,omitempty"`
}
```

### PodSet Synthesis with TopologyRequest

`PodSetsFromRTJTemplate` is extended to populate `TopologyRequest` from
`spec.topology`:

```go
func PodSetsFromRTJTemplate(job *v1alpha1.ResumableTrainingJob) ([]kueuev1beta2.PodSet, error) {
    // ... existing count, minCount logic from Phase 2/3 ...

    for i, replicatedJob := range spec.ReplicatedJobs {
        podSets[i] = kueuev1beta2.PodSet{
            Name:     replicatedJob.Name,
            Template: *replicatedJob.Template.Spec.Template.DeepCopy(),
            Count:    podsCount(&replicatedJob),
        }

        // Phase 3: MinCount for partial admission
        // ... existing logic ...

        // Phase 4: TopologyRequest from spec.topology
        if job.Spec.Topology != nil {
            topReq := &kueuev1beta2.PodSetTopologyRequest{}
            if job.Spec.Topology.Required != nil {
                topReq.Required = job.Spec.Topology.Required
            }
            if job.Spec.Topology.Preferred != nil {
                topReq.Preferred = job.Spec.Topology.Preferred
            }
            podSets[i].TopologyRequest = topReq
        }
    }
    return podSets, nil
}
```

### ResumeReadiness AdmissionCheck Controller

The ResumeReadiness controller is a new controller in the operator that
manages a custom Kueue `AdmissionCheck`.

**Registration:**

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: AdmissionCheck
metadata:
  name: resume-readiness
spec:
  controllerName: checkpoint-native.example.io/resume-readiness
```

The `ClusterQueue` references this check:

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: topology-cq
spec:
  admissionChecks:
    - resume-readiness
  # ... flavors, cohorts, etc.
```

**Controller behavior:**

1. Watch Workloads that have an admission check with `controllerName`
   matching `checkpoint-native.example.io/resume-readiness`.
2. When a Workload enters `QuotaReserved` state with topology assigned:
   a. Read the `TopologyAssignment` from `PodSetAssignments`.
   b. Validate the topology is renderable (all domain values are non-empty,
      pod counts sum to admitted count).
   c. On initial launch: mark check as `Ready`.
   d. On resume: additionally validate that the latest compatible checkpoint
      is compatible with the new admitted shape. This uses the same
      compatibility checker from Phase 2/3.
3. If validation fails, set the check state to `Retry` with a message.
4. Kueue handles the rest: once all checks are `Ready`, it admits the
   Workload (calls `RunWithPodSetsInfo`, sets `suspend=false`).

**State machine:**

```
Workload created
    │
    v
QuotaReserved (topology assigned by TAS)
    │
    v
ResumeReadiness check: Pending
    │
    ├── validate topology → OK
    │   ├── first launch → Ready
    │   └── resume → validate checkpoint compat → Ready
    │
    └── validate topology → FAIL
        └── Retry (with message)
```

### Topology Materialization into Child JobSet

After Kueue admits the Workload and unsuspends the RTJ, the RTJ controller
reads the topology assignments and injects scheduling constraints into the
child JobSet.

The materialization strategy depends on OQ-4 resolution (see
[open-questions.md](open-questions.md)). The design supports two approaches:

**Approach A: Kueue-managed scheduling gates (preferred if available).**
If Kueue's TAS uses scheduling gates and pod labels to enforce topology,
the operator follows the same pattern. Pods are created with scheduling
gates that Kueue resolves post-admission. The operator injects the topology
domain label but does not set nodeSelector for topology (Kueue handles it).

**Approach B: Operator-injected nodeSelector/affinity (fallback).**
If Kueue expects the job controller to materialize topology:
- For single-domain placement: add the topology domain value to nodeSelector.
- For multi-domain placement: use pod topology spread constraints or
  per-domain replicatedJobs (splitting the worker pod set).

In both approaches, the child JobSet remains a **plain runtime resource**
with no Kueue metadata.

### Topology Status

Phase 4 adds topology information to RTJ status:

```go
// TopologyStatus records the topology assignment from Kueue admission.
type TopologyStatus struct {
    // Levels records the topology level keys from the assignment.
    // +optional
    Levels []string `json:"levels,omitempty"`

    // Domains records the assigned topology domains with pod counts.
    // +optional
    Domains []TopologyDomainStatus `json:"domains,omitempty"`
}

type TopologyDomainStatus struct {
    // Values are the topology domain values for each level.
    Values []string `json:"values,omitempty"`

    // Count is the number of pods assigned to this domain.
    Count int32 `json:"count,omitempty"`
}
```

### Data Flow: TopologyRequest → TopologyAssignment → Child JobSet

```
RTJ spec.topology.required = "topology.kubernetes.io/zone"
    │
    │  (PodSetsFromRTJTemplate)
    v
Workload.spec.podSets[i].topologyRequest.required = "topology.kubernetes.io/zone"
    │
    │  (Kueue TAS scheduling)
    v
Workload.status.admission.podSetAssignments[i].topologyAssignment:
  levels: ["topology.kubernetes.io/zone"]
  domains:
    - values: ["zone-a"], count: 4
    - values: ["zone-b"], count: 4
    │
    │  (ResumeReadiness validates → Ready)
    │  (Kueue admits → RunWithPodSetsInfo)
    │  (RTJ Controller reads assignment)
    v
Child JobSet replicatedJobs[i]:
  replicas: 8
  template.spec:
    nodeSelector:
      pool: a100                              (from flavor)
      topology.kubernetes.io/zone: zone-a     (from topology, if single domain)
    affinity:
      podAffinity / topologySpreadConstraints (if multi-domain)
    env:
      YIELD_SDK_TOPOLOGY_DOMAIN = "zone-a"
```

### Phase 3 Backward Compatibility

When `spec.topology` is nil and no ResumeReadiness admission check is
configured on the ClusterQueue:

1. **PodSets:** No `TopologyRequest` set. Kueue admits without TAS.
2. **Admission checks:** No ResumeReadiness gate. Kueue admits directly.
3. **Child JobSet:** Rendered exactly as in Phase 3 (flavor nodeSelector,
   tolerations, admitted counts, no topology constraints).
4. **Status:** `status.topology` is nil. All Phase 3 status fields unchanged.

Phase 3 behavior is fully preserved.

## Invariants

All Phase 0 through Phase 3 invariants remain in force. Phase 4 adds:

| Invariant | Description |
| --- | --- |
| **RTJ is the only Kueue-managed admission object** | Unchanged. The child JobSet remains a plain runtime resource with no Kueue metadata. |
| **Topology Faithfulness** | The child JobSet MUST reflect the topology assignment from Kueue admission. The controller MUST NOT create a child JobSet with scheduling constraints that diverge from the assigned topology domains. |
| **Admission-Gated Launch** | The RTJ controller MUST NOT create a child JobSet before the full admission pipeline completes (quota reserved, admission checks passed, RTJ unsuspended). |
| **ResumeReadiness Is Stateless** | The ResumeReadiness controller re-validates topology and checkpoint compatibility on every admission cycle. It does not cache previous validation results across preemption boundaries. |
| **ProvisioningRequest Is Optional** | Phase 4 is complete without ProvisioningRequest. The operator does not implement ProvisioningRequest logic; it only ensures PodSet synthesis is compatible with the built-in ProvisioningRequest admission check. |
| **Phase 3 Preserved When Features Disabled** | When `spec.topology` is nil and no ResumeReadiness admission check is on the ClusterQueue, behavior is identical to Phase 3. |
