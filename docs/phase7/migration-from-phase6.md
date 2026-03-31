# Phase 7 -- Migration from Phase 6

## What stays the same

These Phase 6 (and earlier) behaviors are **unchanged** in Phase 7:

| Concern | Phase 6 behavior | Phase 7 status |
|---|---|---|
| RTJ as only Kueue-managed object | RTJ creates Workload; child JobSet is plain runtime | Unchanged |
| Kueue as admission/preemption authority | Kueue queues, admits, preempts RTJ workloads | Unchanged |
| RTJ lifecycle state machine | Pending → Queued → Admitted → Starting → Running → YieldRequested → Draining → Paused → Restoring | Unchanged (new conditions added, phases unchanged) |
| Graceful yield path | On preemption: signal checkpoint → drain → delete child → Paused | Unchanged (also used for timeout evictions) |
| Resume path | Paused → checkpoint selected → Workload re-queued → re-admitted → resume | Unchanged |
| Checkpoint contract | PyTorch DCP to S3-compatible storage | Unchanged |
| Suspend/unsuspend semantics | spec.suspend controls Workload suspension | Unchanged |
| Priority shaping (Phase 5) | CheckpointPriorityPolicy adjusts effective priority | Unchanged |
| Manager/worker split (Phase 6) | managedBy field, MultiKueue dispatch, status mirroring | Unchanged |
| Topology spec (Phase 4) | Required/Preferred/Unconstrained modes | Unchanged |
| Partial admission (Phase 3) | MinCount-based partial admission | Unchanged |
| Resume readiness AC (Phase 4) | ResumeReadiness AdmissionCheck for restore validation | Unchanged |
| All RTJ spec fields | No Phase 6 spec fields are removed or renamed | Unchanged |

## What changes in launch gating

### Phase 6 launch gate

In Phase 6, the RTJ operator transitions from `Admitted` to `Starting`
when:

```
Workload is Admitted  AND  RTJ.spec.suspend == false
```

(Plus the Phase 4 ResumeReadiness AC check, which is already an AC.)

The operator renders the child JobSet immediately upon admission.

### Phase 7 launch gate

In Phase 7, the RTJ operator transitions from `Admitted` to `Starting`
when **all** of the following are true:

```
1. Workload is Admitted (quota reserved)
2. ALL AdmissionChecks on the Workload report state = Ready
3. Topology assignment is present (if topology mode != Disabled)
4. RTJ.spec.suspend == false
```

**What this means in practice:**

- If no ProvisioningRequest AC is configured on the ClusterQueue, condition
  (2) is trivially satisfied (there are no additional ACs to wait for, or
  only the existing ResumeReadiness AC which was already checked). Behavior
  is identical to Phase 6.

- If a ProvisioningRequest AC **is** configured, the operator waits for
  Kueue to report that the provisioning check is Ready before launching.
  This delay can be seconds (fake backend) to minutes (real autoscaler
  scale-up).

- If topology is configured, the operator waits for topology assignment to
  appear in the Workload status. This may arrive in the same reconciliation
  as admission or in a later pass.

### Migration impact

**Zero-config migration**: Existing Phase 6 deployments that do not
configure a ProvisioningRequest AdmissionCheck on their ClusterQueues
see **no behavior change**. The launch gate falls through to the same
logic as Phase 6.

**Opting in**: To enable capacity-guaranteed launch, the cluster
administrator adds:
1. An AdmissionCheck resource of type `provisioning-request`.
2. A ProvisioningRequestConfig resource.
3. References the AdmissionCheck in the ClusterQueue.

No changes to RTJ specs are required.

## How topology assignment may now arrive after provisioning

### Phase 6 behavior

In Phase 6, topology assignment is part of the Kueue admission decision.
When Kueue admits a workload, the topology assignment (if any) is included
in the admission response. The RTJ operator reads it immediately.

### Phase 7 behavior

When a ProvisioningRequest AdmissionCheck is configured, Kueue may perform
admission in **two passes**:

1. **Pass 1 -- Quota + ProvisioningRequest**: Kueue reserves quota and
   creates the ProvisioningRequest. The Workload enters an intermediate
   state where quota is reserved but the AC is not yet Ready.

2. **Pass 2 -- AC satisfied + Topology**: After the ProvisioningRequest
   backend reports success, Kueue marks the AC as Ready. Kueue may then
   perform a topology assignment pass, updating the Workload's
   `podSetAssignments[*].topologyAssignment` fields.

The RTJ operator must handle the case where:
- Admission is set (quota reserved).
- ProvisioningRequest AC is Ready.
- But topology assignment has not yet arrived.

The launch gate explicitly checks for topology assignment and does not
open until it is present. This is a **new** check in Phase 7; in Phase 6,
topology was always present at admission time.

### Why this matters

Without this check, the RTJ operator would render the child JobSet
without topology annotations, leading to:
- Pods scheduled without topology constraints.
- Potential scheduling to the wrong topology domain.
- Wasted capacity if pods must be rescheduled.

The two-pass delay is typically one Kueue reconciliation cycle (seconds).

## How waitForPodsReady changes failure semantics

### Phase 6 behavior

In Phase 6, if pods fail to start (e.g., image pull error, scheduling
failure), the behavior depends on Kubernetes retry semantics and any
externally configured timeouts. RTJ does not have a built-in startup
timeout. A misconfigured training image can leave an RTJ in `Starting`
indefinitely.

### Phase 7 behavior

With `waitForPodsReady` enabled in Kueue:

1. **Startup timeout**: After the Workload is admitted and the child
   JobSet is rendered, Kueue starts a timer. If pods don't reach Ready
   within `waitForPodsReady.timeout`, Kueue evicts the Workload.

2. **Recovery timeout**: If pods were Ready but become NotReady (e.g.,
   node failure, OOM), Kueue starts a recovery timer. If pods don't
   return to Ready within the timeout, Kueue evicts.

3. **Eviction triggers yield**: The RTJ operator detects Workload
   eviction and enters the standard graceful yield path. This is the
   same code path used for Kueue preemption -- no new yield logic.

4. **Status visibility**: RTJ status conditions distinguish:
   - `StartupTimeoutEvicted` -- pods never reached Ready.
   - `RecoveryTimeoutEvicted` -- pods lost Ready and didn't recover.
   - These are distinct from `Preempted` (Kueue preemption) and
     `ManualYield` (user-initiated).

5. **Automatic requeue**: Kueue requeues the evicted workload with
   configurable backoff. The RTJ does not need to re-create its Workload;
   it waits in `Paused` for re-admission.

### Migration impact

`waitForPodsReady` is configured **globally** in Kueue's Configuration
resource, not per-RTJ. Enabling it affects all Kueue workloads.

- If `waitForPodsReady` is not enabled, Phase 7 behavior is identical to
  Phase 6 -- no startup/recovery timeouts, no eviction.
- If `waitForPodsReady` is enabled, all workloads (including RTJs) get
  timeout-based eviction. This is a cluster-wide policy decision.

## Why the local path uses a fake ProvisioningRequest backend

The real ProvisioningRequest backends (GKE NAP, Karpenter) require:
- A real cloud provider with auto-scaling capabilities.
- Cluster-autoscaler or Karpenter installed and configured.
- Actual node scaling operations (minutes of wall-clock time).

For local development and CI/e2e testing, we need:
- **Deterministic behavior**: Tests must not depend on cloud provider state.
- **Fast execution**: Tests should complete in seconds, not minutes.
- **Failure simulation**: Tests must be able to simulate provisioning
  failures without breaking real infrastructure.
- **No cloud credentials**: CI environments must not require cloud access.

The fake ProvisioningRequest backend solves all of these by:
1. Watching ProvisioningRequest resources.
2. Setting their status to Provisioned after a configurable delay.
3. Optionally setting status to Failed for negative tests.
4. Running as a simple in-cluster controller with no external dependencies.

This is the same pattern used in Kueue's own e2e tests for
ProvisioningRequest functionality.

## Upgrade path

### Single-cluster Phase 6 → Phase 7

1. **No RTJ spec changes required.**
2. Upgrade RTJ operator to Phase 7 version.
3. (Optional) Install fake ProvisioningRequest backend for testing.
4. (Optional) Create AdmissionCheck + ProvisioningRequestConfig resources.
5. (Optional) Add AdmissionCheck reference to ClusterQueue.
6. (Optional) Enable `waitForPodsReady` in Kueue Configuration.

Steps 3-6 are opt-in. Without them, Phase 7 operator behaves identically
to Phase 6.

### Multi-cluster Phase 6 → Phase 7

1. Upgrade RTJ operator on **worker clusters** to Phase 7 version.
2. (Optional) Configure ProvisioningRequest AC on worker ClusterQueues.
3. (Optional) Enable `waitForPodsReady` on worker Kueue instances.
4. Manager cluster operator can stay at Phase 6 or upgrade; manager does
   not participate in provisioning or waitForPodsReady.

Phase 7 features are worker-local; the manager cluster is unaffected.
