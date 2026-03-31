# Phase 7 -- Architecture

## Component diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Kubernetes Cluster                            │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                     Control Plane                                │  │
│  │                                                                  │  │
│  │  ┌─────────────┐    ┌──────────────────────────────────────┐    │  │
│  │  │             │    │              Kueue                    │    │  │
│  │  │  RTJ        │    │                                      │    │  │
│  │  │  Operator   │    │  ┌────────────┐  ┌───────────────┐  │    │  │
│  │  │             │    │  │ Admission  │  │ AdmissionCheck │  │    │  │
│  │  │ - launch    │    │  │ Controller │  │ Controller     │  │    │  │
│  │  │   gate      │◄──►│  │            │  │                │  │    │  │
│  │  │ - yield     │    │  │ - quota    │  │ - Provisioning │  │    │  │
│  │  │ - resume    │    │  │ - preempt  │  │   Request AC   │  │    │  │
│  │  │ - render    │    │  │ - admit    │  │ - ResumeReady  │  │    │  │
│  │  │   JobSet    │    │  │            │  │   AC (Phase 4) │  │    │  │
│  │  │             │    │  └────────────┘  └───────┬───────┘  │    │  │
│  │  └──────┬──────┘    │                          │          │    │  │
│  │         │           │  ┌───────────────────────┘          │    │  │
│  │         │           │  │  waitForPodsReady                │    │  │
│  │         │           │  │  (startup + recovery timeout)    │    │  │
│  │         │           └──┴──────────────────────────────────┘    │  │
│  │         │                          │                           │  │
│  │         │                          ▼                           │  │
│  │         │              ┌───────────────────────┐               │  │
│  │         │              │  ProvisioningRequest   │               │  │
│  │         │              │  (Kueue-created)       │               │  │
│  │         │              └───────────┬───────────┘               │  │
│  │         │                          │                           │  │
│  │         │                          ▼                           │  │
│  │         │          ┌───────────────────────────────┐           │  │
│  │         │          │  ProvisioningRequest Backend  │           │  │
│  │         │          │                               │           │  │
│  │         │          │  ┌─────────┐  ┌───────────┐  │           │  │
│  │         │          │  │  Fake   │  │   Real    │  │           │  │
│  │         │          │  │ (local) │  │ (GKE NAP/ │  │           │  │
│  │         │          │  │         │  │ Karpenter) │  │           │  │
│  │         │          │  └─────────┘  └───────────┘  │           │  │
│  │         │          └───────────────────────────────┘           │  │
│  └─────────┼────────────────────────────────────────────────────┘  │
│            │                                                        │
│            ▼                                                        │
│  ┌──────────────────┐     ┌──────────────────┐                     │
│  │  Child JobSet    │────►│  Worker Pods      │                     │
│  │  (plain runtime) │     │  (training)       │                     │
│  └──────────────────┘     └──────────────────┘                     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Key relationships:**

- **Kueue** creates ProvisioningRequest resources when the ProvisioningRequest
  AdmissionCheck is configured on the ClusterQueue.
- **Kueue's AdmissionCheck controller** watches ProvisioningRequest status and
  updates the Workload's AdmissionCheck state.
- **ProvisioningRequest Backend** (fake or real) watches ProvisioningRequest
  resources and sets their status to Provisioned or Failed.
- **RTJ Operator** reads the Workload's admission state (all checks passed,
  topology assigned) to decide when to open the launch gate.
- **RTJ Operator** renders the child JobSet only after the launch gate opens.
- **waitForPodsReady** is a Kueue feature that monitors pod readiness after
  launch and evicts the workload on timeout.

---

## Launch gate inputs

The launch gate is the decision point where the RTJ operator transitions
from `Admitted` to `Starting` and renders the child JobSet.

```
Launch gate = ALL of:
  ├── Workload.status.admission is set (quota reserved)
  ├── ALL AdmissionChecks report Ready:
  │     ├── ProvisioningRequest AC (if configured): Provisioned
  │     └── ResumeReadiness AC (Phase 4, if configured): Ready
  ├── Topology assignment present (if spec.topology.mode != Disabled):
  │     └── Workload.status.admission.podSetAssignments[*].topologyAssignment
  └── RTJ.spec.suspend == false
```

When ProvisioningRequest AC is **not** configured, the launch gate behaves
exactly as Phase 6: admission alone opens the gate.

---

## Sequence diagram 1: Quota reservation → ProvisioningRequest → topology assignment → launch

This is the primary happy-path flow for a new RTJ submission with
ProvisioningRequest and topology configured.

```
User          RTJ Operator       Kueue               ProvReq AC        Fake/Real Backend
 │                │                │                      │                    │
 │  create RTJ    │                │                      │                    │
 │───────────────►│                │                      │                    │
 │                │                │                      │                    │
 │                │  create Workload                      │                    │
 │                │  (suspended=true)                     │                    │
 │                │───────────────►│                      │                    │
 │                │                │                      │                    │
 │                │                │  queue + quota check  │                    │
 │                │                │  quota reserved       │                    │
 │                │                │                      │                    │
 │                │                │  create ProvReq      │                    │
 │                │                │─────────────────────►│                    │
 │                │                │                      │                    │
 │                │                │                      │  create ProvReq CR │
 │                │                │                      │───────────────────►│
 │                │                │                      │                    │
 │                │                │                      │                    │ provision
 │                │                │                      │                    │ capacity
 │                │                │                      │                    │ (or fake
 │                │                │                      │                    │  delay)
 │                │                │                      │                    │
 │                │                │                      │  ProvReq.status =  │
 │                │                │                      │◄─── Provisioned ───│
 │                │                │                      │                    │
 │                │                │  AC state = Ready    │                    │
 │                │                │◄─────────────────────│                    │
 │                │                │                      │                    │
 │                │                │  admit workload      │                    │
 │                │                │  (topology pass)     │                    │
 │                │                │  set podSetAssign-   │                    │
 │                │                │  ments with topology │                    │
 │                │                │                      │                    │
 │                │  Workload admitted                    │                    │
 │                │  + all ACs Ready                      │                    │
 │                │  + topology assigned                  │                    │
 │                │◄───────────────│                      │                    │
 │                │                │                      │                    │
 │                │  LAUNCH GATE OPENS                    │                    │
 │                │                │                      │                    │
 │                │  set RTJ phase = Starting             │                    │
 │                │  render child JobSet                  │                    │
 │                │  (with topology annotations)          │                    │
 │                │                │                      │                    │
 │                │  pods scheduled + running             │                    │
 │                │  set RTJ phase = Running              │                    │
 │                │                │                      │                    │
```

**Notes:**
- Kueue may admit the workload before topology is fully assigned. In that
  case the RTJ operator sees admission but waits for topology before
  rendering. This is the "topology second pass" scenario.
- The ProvisioningRequest AdmissionCheck is configured on the ClusterQueue,
  not on the RTJ. Kueue creates the ProvisioningRequest automatically.

---

## Sequence diagram 2: Startup timeout → eviction / requeue

This shows what happens when pods fail to reach Ready within the
waitForPodsReady startup timeout.

```
RTJ Operator       Kueue (waitForPodsReady)        Child JobSet / Pods
    │                        │                            │
    │  launch gate opened    │                            │
    │  render child JobSet   │                            │
    │────────────────────────┼───────────────────────────►│
    │                        │                            │
    │  RTJ phase = Starting  │                            │
    │                        │  start timeout timer       │
    │                        │  (podsReadyTimeout)        │
    │                        │                            │
    │                        │                            │ pods Pending
    │                        │                            │ (image pull,
    │                        │                            │  scheduling,
    │                        │                            │  init containers)
    │                        │                            │
    │                        │  timeout expires!          │
    │                        │  pods still not Ready      │
    │                        │                            │
    │                        │  evict workload:           │
    │                        │  set Workload.spec.        │
    │                        │  active = false            │
    │                        │  set Eviction condition    │
    │                        │                            │
    │  observe Workload      │                            │
    │  evicted / deactivated │                            │
    │                        │                            │
    │  RTJ yield path:       │                            │
    │  phase → YieldRequested│                            │
    │  → Draining            │                            │
    │  delete child JobSet   │──────────────────────────► X
    │  phase → Paused        │                            │
    │                        │                            │
    │  set RTJ status:       │                            │
    │  condition =           │                            │
    │  StartupTimeoutEvicted │                            │
    │  message = timeout     │                            │
    │  value + context       │                            │
    │                        │                            │
    │  Kueue requeues        │                            │
    │  workload for          │                            │
    │  re-admission          │                            │
    │◄───────────────────────│                            │
    │                        │                            │
```

**Design notes:**
- The RTJ operator does not implement its own startup timer. Kueue's
  waitForPodsReady is the single source of truth.
- When Kueue evicts, the RTJ operator treats it the same as a preemption:
  it enters the graceful yield path, cleans up child resources, and
  transitions to Paused.
- The RTJ status records a `StartupTimeoutEvicted` condition so operators
  can distinguish timeout evictions from preemption evictions.
- Kueue automatically requeues the workload after eviction, so the RTJ
  will be re-admitted when capacity is available again.

---

## Sequence diagram 3: Recovery timeout on a running workload

This shows what happens when a running workload's pods become NotReady
and don't recover within the configured window.

```
RTJ Operator       Kueue (waitForPodsReady)       Child Pods
    │                        │                        │
    │  RTJ phase = Running   │                        │
    │                        │  all pods Ready         │
    │                        │                        │
    │                        │                        │ node failure
    │                        │                        │ or OOM kill
    │                        │                        │ → pod NotReady
    │                        │                        │
    │                        │  detect pods NotReady   │
    │                        │  start recovery timer   │
    │                        │  (requeuingTimestamp)    │
    │                        │                        │
    │                        │                        │ pod stays
    │                        │                        │ NotReady
    │                        │                        │
    │                        │  recovery timeout!      │
    │                        │  evict workload         │
    │                        │                        │
    │  observe Workload      │                        │
    │  evicted               │                        │
    │                        │                        │
    │  RTJ yield path:       │                        │
    │  trigger checkpoint    │                        │
    │  (if safepoint mode    │                        │
    │   allows; else skip)   │                        │
    │  phase → Draining      │                        │
    │  delete child JobSet   │───────────────────────► X
    │  phase → Paused        │                        │
    │                        │                        │
    │  set RTJ status:       │                        │
    │  condition =           │                        │
    │  RecoveryTimeoutEvicted│                        │
    │                        │                        │
```

**Design notes:**
- Recovery timeout handling reuses the same yield path as startup timeout
  and preemption. The only difference is the status condition.
- Whether a checkpoint is taken during recovery-timeout yield depends on
  the safepoint mode and whether training code can respond to the signal
  in time. The RTJ operator attempts the graceful yield but proceeds
  with forced cleanup if the drain window expires.
- Kueue's requeuingTimestamp mechanism controls when the workload
  re-enters the queue after eviction.

---

## Sequence diagram 4: Worker-cluster runtime under Phase 6 manager/worker split

This shows how Phase 7 interacts with the existing Phase 6 multi-cluster
architecture. The manager cluster handles Kueue admission; the worker
cluster handles local provisioning and runtime.

```
Manager Cluster                            Worker Cluster
─────────────────                          ──────────────────

User            RTJ Op        Kueue        RTJ Op        Kueue (local)
 │             (manager)        │          (worker)           │
 │                │             │             │               │
 │  create RTJ   │             │             │               │
 │  (managedBy=  │             │             │               │
 │   multikueue) │             │             │               │
 │───────────────►             │             │               │
 │                │            │             │               │
 │                │  create    │             │               │
 │                │  Workload  │             │               │
 │                │──────────►│             │               │
 │                │            │             │               │
 │                │            │  MultiKueue │               │
 │                │            │  dispatches │               │
 │                │            │  to worker  │               │
 │                │            │─────────────────────────────►
 │                │            │             │               │
 │                │            │             │  create       │
 │                │            │             │  remote RTJ   │
 │                │            │             │◄──────────────│
 │                │            │             │               │
 │                │            │             │  remote RTJ   │
 │                │            │             │  enters local │
 │                │            │             │  Kueue queue  │
 │                │            │             │               │
 │                │            │             │  LOCAL Kueue: │
 │                │            │             │  quota +      │
 │                │            │             │  ProvReq AC   │
 │                │            │             │  (if config'd)│
 │                │            │             │               │
 │                │            │             │  ProvReq      │
 │                │            │             │  satisfied    │
 │                │            │             │  (local fake  │
 │                │            │             │   or real)    │
 │                │            │             │               │
 │                │            │             │  launch gate  │
 │                │            │             │  opens        │
 │                │            │             │               │
 │                │            │             │  render child │
 │                │            │             │  JobSet       │
 │                │            │             │  (local)      │
 │                │            │             │               │
 │                │  status    │             │  RTJ Running  │
 │                │  mirror    │◄────────────────────────────│
 │                │◄───────────│             │               │
 │                │            │             │               │
 │  manager RTJ  │            │             │               │
 │  status shows │            │             │               │
 │  remote phase │            │             │               │
 │◄──────────────│            │             │               │
 │                │            │             │               │
```

**Design notes:**
- The **manager cluster** RTJ operator runs in manager mode. It does not
  render child JobSets locally. It creates the Workload for Kueue
  MultiKueue dispatch.
- The **worker cluster** runs the full Phase 7 path locally: its own Kueue
  instance handles admission, ProvisioningRequest (if configured), topology
  assignment, and waitForPodsReady.
- ProvisioningRequest configuration is **per-cluster**. The manager cluster
  may not have it configured at all; the worker cluster configures it based
  on its own infrastructure (fake for dev, real for cloud).
- Status is mirrored from worker → manager via the existing Phase 6 status
  mirroring mechanism. Phase 7 extends the mirrored status to include
  provisioning gate state.
- This means Phase 7 features are **worker-local** and do not require
  changes to the MultiKueue dispatch protocol.

---

## Detailed design

### 1. ProvisioningRequest AdmissionCheck configuration

Phase 7 does **not** add new fields to the RTJ spec for provisioning.
Instead, provisioning is configured at the **Kueue ClusterQueue level**:

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: AdmissionCheck
metadata:
  name: provisioning-check
spec:
  controllerName: kueue.x-k8s.io/provisioning-request
  parameters:
    apiGroup: autoscaling.x-k8s.io
    kind: ProvisioningRequestConfig
    name: default-provisioning
---
apiVersion: autoscaling.x-k8s.io/v1
kind: ProvisioningRequestConfig
metadata:
  name: default-provisioning
spec:
  provisioningClassName: check-capacity.autoscaling.x-k8s.io
  # Or for real cloud: queued-provisioning.gke.io
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: gpu-cluster-queue
spec:
  admissionChecks:
    - provisioning-check
  # ... existing quota config ...
```

When this configuration exists, Kueue automatically:
1. Creates a ProvisioningRequest for each pending Workload.
2. Waits for the ProvisioningRequest to be satisfied before marking the
   AdmissionCheck as Ready.
3. Includes the provisioning result in the Workload admission status.

### 2. RTJ operator launch gate changes

The RTJ operator's `shouldLaunch()` predicate currently checks:

```
admitted && !suspended && resumeReady (Phase 4)
```

Phase 7 extends this to:

```
admitted && !suspended && allAdmissionChecksReady && topologyAssigned (if configured)
```

Where `allAdmissionChecksReady` is derived from:
```
Workload.status.admissionChecks[*].state == Ready
```

This is a **generalized** check -- it covers the ProvisioningRequest AC,
the ResumeReadiness AC (Phase 4), and any future AdmissionChecks.

### 3. Topology second-pass handling

Kueue may perform admission in two passes when ProvisioningRequest is
involved:

1. **First pass**: Kueue reserves quota and creates the ProvisioningRequest.
   The Workload is "admitted" but the ProvisioningRequest AC is not yet
   Ready.
2. **Second pass**: After the ProvisioningRequest is satisfied, Kueue
   performs topology assignment and updates the Workload's
   podSetAssignments with topologyAssignment.

The RTJ operator handles this by checking topology assignment availability
**separately** from admission. The launch gate does not open until topology
is present (when configured).

### 4. waitForPodsReady integration

Kueue's waitForPodsReady is configured globally in the Kueue configuration:

```yaml
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
waitForPodsReady:
  enable: true
  timeout: 300s  # startup timeout
  requeuingStrategy:
    timestamp: Creation
    backoffBaseSeconds: 60
    backoffMaxSeconds: 3600
    backoffLimitCount: 5
```

The RTJ operator integrates by:

1. **Detecting eviction**: When the Workload's `Evicted` condition becomes
   True with reason `PodsReadyTimeout`, the RTJ operator enters the yield
   path.
2. **Status reporting**: The RTJ operator sets a status condition
   indicating the eviction type:
   - `StartupTimeoutEvicted` -- pods never reached Ready after launch.
   - `RecoveryTimeoutEvicted` -- pods lost Ready state during running.
3. **Requeue handling**: Kueue handles requeueing automatically. The RTJ
   operator does not need to re-create the Workload; it simply transitions
   to Paused and waits for re-admission.

### 5. Fake ProvisioningRequest backend

The fake backend is a minimal controller:

```
Watch: ProvisioningRequest resources
On create/update:
  if annotation "rtj.dev/fake-action" == "reject":
    set ProvisioningRequest.status.conditions = Failed
  else:
    sleep(configured delay, default 5s)
    set ProvisioningRequest.status.conditions = Provisioned
```

It runs as:
- A separate Deployment in dev/e2e environments.
- Gated by the `FakeProvisioningBackend` feature flag.
- Configurable via command-line flags: `--fake-provisioning-delay`,
  `--fake-provisioning-default-action`.

### 6. RTJ status extensions

Phase 7 adds the following to RTJ status:

```go
// ProvisioningStatus reports the state of the ProvisioningRequest
// AdmissionCheck gate.
type ProvisioningStatus struct {
    // Gate is the current provisioning gate state.
    // One of: NotConfigured, Pending, Provisioned, Failed.
    Gate string `json:"gate"`

    // Message is a human-readable message about the provisioning state.
    // +optional
    Message string `json:"message,omitempty"`

    // LastTransitionTime is the last time the gate state changed.
    // +optional
    LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}
```

And extends existing status conditions with:

```
Type: StartupTimeoutEvicted
Status: True
Reason: PodsReadyTimeout
Message: "Workload evicted: pods did not reach Ready within 300s"

Type: RecoveryTimeoutEvicted
Status: True
Reason: PodsReadyTimeout
Message: "Workload evicted: pods lost Ready state and did not recover within 300s"
```

---

## Phase 7 invariants

All Phase 0-6 invariants are preserved. Phase 7 adds:

| ID | Invariant |
|---|---|
| P7-1 | RTJ does not render child JobSet until ALL admission checks are Ready |
| P7-2 | RTJ does not render child JobSet until topology is assigned (when configured) |
| P7-3 | ProvisioningRequest resources are created and managed by Kueue, not RTJ |
| P7-4 | waitForPodsReady eviction triggers the same yield path as preemption |
| P7-5 | The fake ProvisioningRequest backend is the only Phase 7 custom controller |
| P7-6 | Phase 6 behavior is preserved when ProvisioningRequest AC is not configured |
| P7-7 | Multi-cluster Phase 7 is worker-local; manager cluster is unaffected |
