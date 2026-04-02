# Phase 8 -- Architecture

## Component diagram

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                            Kubernetes Cluster                                 │
│                                                                               │
│  ┌────────────────────────────────────────────────────────────────────────┐   │
│  │                        Control Plane                                   │   │
│  │                                                                        │   │
│  │  ┌─────────────────┐     ┌──────────────────────────────────────────┐  │   │
│  │  │                 │     │                 Kueue                     │  │   │
│  │  │  RTJ Operator   │     │                                          │  │   │
│  │  │                 │     │  ┌──────────────┐  ┌─────────────────┐   │  │   │
│  │  │ - device spec   │     │  │  Admission   │  │ AdmissionCheck  │   │  │   │
│  │  │   → RCT create  │     │  │  Controller  │  │ Controller      │   │  │   │
│  │  │ - launch gate   │◄───►│  │              │  │                 │   │  │   │
│  │  │ - DRA-aware     │     │  │ - quota      │  │ - ProvReq AC    │   │  │   │
│  │  │   JobSet render │     │  │ - preempt    │  │ - ResumeReady   │   │  │   │
│  │  │ - compat check  │     │  │ - admit      │  │   AC (Phase 4)  │   │  │   │
│  │  │ - RCT cleanup   │     │  │ - deviceClass│  │                 │   │  │   │
│  │  │                 │     │  │   Mappings   │  └─────────────────┘   │  │   │
│  │  └──────┬──────────┘     │  └──────────────┘                       │  │   │
│  │         │                │                                          │  │   │
│  │         │                └──────────────────────────────────────────┘  │   │
│  │         │                                                              │   │
│  │         ▼                                                              │   │
│  │  ┌──────────────────────┐                                              │   │
│  │  │ ResourceClaimTemplate│  (RTJ-owned, per-RTJ, namespace-scoped)      │   │
│  │  │                      │                                              │   │
│  │  │ spec.devices.requests│                                              │   │
│  │  │   deviceClassName    │                                              │   │
│  │  │   selectors          │                                              │   │
│  │  │   count              │                                              │   │
│  │  └──────────┬───────────┘                                              │   │
│  └─────────────┼──────────────────────────────────────────────────────────┘   │
│                │                                                               │
│                ▼                                                               │
│  ┌──────────────────────┐     ┌─────────────────────────────────┐             │
│  │  Child JobSet        │     │  Worker Pods                    │             │
│  │  (plain runtime)     │────►│                                 │             │
│  │                      │     │  spec.resourceClaims:           │             │
│  │  replicatedJobs:     │     │    - name: rtj-devices          │             │
│  │    workers:          │     │      resourceClaimTemplateName: │             │
│  │      template:       │     │        <rtj-name>-devices       │             │
│  │        spec:         │     │                                 │             │
│  │          resource-   │     │  containers[0].resources.claims:│             │
│  │          Claims: ... │     │    - name: rtj-devices          │             │
│  └──────────────────────┘     └─────────────┬───────────────────┘             │
│                                              │                                │
│                                              ▼                                │
│                               ┌──────────────────────────────┐                │
│                               │  ResourceClaim (per-pod)     │                │
│                               │  (created by kubelet from    │                │
│                               │   ResourceClaimTemplate)     │                │
│                               └──────────────┬───────────────┘                │
│                                              │                                │
│                                              ▼                                │
│                               ┌──────────────────────────────┐                │
│                               │  DRA Driver                  │                │
│                               │                              │                │
│                               │  ┌──────────┐  ┌──────────┐ │                │
│                               │  │ Example  │  │  Real    │ │                │
│                               │  │ (fake    │  │ (NVIDIA/ │ │                │
│                               │  │  devices)│  │  AMD/    │ │                │
│                               │  │          │  │  Intel)  │ │                │
│                               │  └──────────┘  └──────────┘ │                │
│                               └──────────────────────────────┘                │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
```

**Key relationships:**

- **RTJ Operator** creates a ResourceClaimTemplate when `spec.devices` is
  present. The template is owned by the RTJ (ownerReference).
- **RTJ Operator** renders the child JobSet with DRA claim references in
  pod templates, pointing at the ResourceClaimTemplate.
- **Kubelet** creates per-pod ResourceClaim instances from the template when
  pods are scheduled.
- **DRA Driver** (example or real) allocates devices to ResourceClaims and
  makes them available to containers.
- **Kueue** accounts for DRA device classes through `deviceClassMappings`
  on ClusterQueue ResourceGroups. It does not manage ResourceClaimTemplates
  or ResourceClaims directly.
- **ResourceClaimTemplates and ResourceClaims** are helper runtime objects.
  They are not Kueue-managed workloads. Kueue sees the device request on
  the Workload PodSet and uses `deviceClassMappings` for quota accounting.

---

## Object ownership model

```
RTJ (Kueue-managed)
 │
 ├── owns ──► Workload (Kueue manages admission/preemption)
 │
 ├── owns ──► ResourceClaimTemplate (RTJ operator creates/manages)
 │              │
 │              └── (kubelet creates per-pod ResourceClaims from template)
 │
 └── owns ──► Child JobSet (plain runtime, RTJ operator renders)
               │
               └── Worker Pods reference ResourceClaimTemplate
                    │
                    └── Per-pod ResourceClaim (kubelet-created, allocated by DRA driver)
```

**Ownership rules:**

| Object | Owner | Managed by | Kueue-managed? |
|---|---|---|---|
| RTJ | User | RTJ Operator | **Yes** (only Kueue-managed object) |
| Workload | RTJ | Kueue | No (Kueue controls admission) |
| ResourceClaimTemplate | RTJ | RTJ Operator | **No** (helper runtime object) |
| ResourceClaim (per-pod) | Pod | Kubelet + DRA driver | **No** |
| Child JobSet | RTJ | RTJ Operator | **No** (plain runtime) |
| Worker Pods | JobSet | JobSet controller | **No** |

---

## Sequence diagram 1: RTJ submit -> Kueue admit -> DRA template reconciliation -> JobSet launch -> claim allocation

This is the primary happy-path flow for a new RTJ with `spec.devices`.

```
User          RTJ Operator       Kueue               DRA Driver        Kubelet
 │                │                │                      │                │
 │  create RTJ    │                │                      │                │
 │  (spec.devices │                │                      │                │
 │   present)     │                │                      │                │
 │───────────────►│                │                      │                │
 │                │                │                      │                │
 │                │  create Workload                      │                │
 │                │  (PodSets include                     │                │
 │                │   DRA device request                  │                │
 │                │   for deviceClassMappings)            │                │
 │                │───────────────►│                      │                │
 │                │                │                      │                │
 │                │  create ResourceClaimTemplate         │                │
 │                │  (owned by RTJ, in same namespace)    │                │
 │                │                │                      │                │
 │                │                │  queue + quota check  │                │
 │                │                │  (deviceClassMappings │                │
 │                │                │   maps DeviceClass    │                │
 │                │                │   to quota flavor)    │                │
 │                │                │                      │                │
 │                │                │  quota reserved       │                │
 │                │                │  ACs checked          │                │
 │                │                │                      │                │
 │                │  Workload admitted                    │                │
 │                │  + all ACs Ready                      │                │
 │                │◄───────────────│                      │                │
 │                │                │                      │                │
 │                │  LAUNCH GATE OPENS                    │                │
 │                │                │                      │                │
 │                │  render child JobSet:                 │                │
 │                │  - pod.spec.resourceClaims →          │                │
 │                │    resourceClaimTemplateName           │                │
 │                │  - container.resources.claims →        │                │
 │                │    claim name reference                │                │
 │                │  - topology annotations (if configured)│                │
 │                │  - podSetUpdates (if any)              │                │
 │                │                │                      │                │
 │                │  RTJ phase = Starting                 │                │
 │                │                │                      │                │
 │                │                │                      │                │
 │                │  pods scheduled │                      │                │
 │                │                │                      │   pod created  │
 │                │                │                      │◄───────────────│
 │                │                │                      │                │
 │                │                │                      │  kubelet creates│
 │                │                │                      │  ResourceClaim  │
 │                │                │                      │  from template  │
 │                │                │                      │◄───────────────│
 │                │                │                      │                │
 │                │                │                      │  allocate      │
 │                │                │                      │  devices to    │
 │                │                │                      │  claim         │
 │                │                │                      │───────────────►│
 │                │                │                      │                │
 │                │                │                      │  claim allocated│
 │                │                │                      │  devices bound  │
 │                │                │                      │  to container   │
 │                │                │                      │                │
 │                │  pods Running + Ready                 │                │
 │                │  RTJ phase = Running                  │                │
 │                │                │                      │                │
```

**Notes:**

- The RTJ operator creates the ResourceClaimTemplate **before** creating the
  child JobSet. This ensures the template exists when pods reference it.
- Kueue accounts for DRA devices through `deviceClassMappings`, not by
  watching ResourceClaimTemplates. The Workload PodSet's resource list
  includes the DRA device request for quota purposes.
- Kubelet creates per-pod ResourceClaim instances from the template. The DRA
  driver allocates devices to each claim independently.
- The launch gate (Phase 7) still applies: the child JobSet is not created
  until all AdmissionChecks pass and topology is assigned (if configured).

---

## Sequence diagram 2: Pause/resume with same DRA device profile

This shows the checkpoint-resume flow when the device profile (DeviceClass
+ selectors) is unchanged between pause and resume.

```
RTJ Operator           Checkpoint Store        DRA Driver
    │                        │                      │
    │  RTJ Running with      │                      │
    │  DeviceClass=gpu.ex    │                      │
    │  selectors=[mem>=80Gi] │                      │
    │                        │                      │
    │  PAUSE requested       │                      │
    │  (manual or Kueue      │                      │
    │   preemption)          │                      │
    │                        │                      │
    │  signal trainer:       │                      │
    │  write checkpoint      │                      │
    │───────────────────────►│                      │
    │                        │                      │
    │                        │  checkpoint written   │
    │                        │  manifest includes:   │
    │                        │    deviceClass: gpu.ex│
    │                        │    deviceFingerprint: │
    │                        │      hash(selectors)  │
    │                        │    worldSize: N       │
    │                        │    (all Phase 0 fields│
    │                        │     plus device info) │
    │                        │                      │
    │  delete child JobSet   │                      │
    │  RTJ phase = Paused    │                      │
    │                        │                      │
    │  ResourceClaimTemplate │                      │
    │  preserved (RTJ still  │                      │
    │  owns it)              │                      │
    │                        │                      │
    │  RESUME requested      │                      │
    │                        │                      │
    │  select checkpoint     │                      │
    │  (latest compatible    │                      │
    │   complete)            │                      │
    │◄───────────────────────│                      │
    │                        │                      │
    │  compatibility check:  │                      │
    │  RTJ deviceClass ==    │                      │
    │    ckpt deviceClass    │ ✓                    │
    │  RTJ deviceFingerprint │                      │
    │    == ckpt fingerprint │ ✓                    │
    │  worldSize match       │ ✓                    │
    │  (all Phase 0 checks)  │ ✓                    │
    │                        │                      │
    │  COMPATIBLE            │                      │
    │                        │                      │
    │  Workload re-queued    │                      │
    │  → re-admitted         │                      │
    │                        │                      │
    │  launch gate opens     │                      │
    │  render child JobSet   │                      │
    │  (same RCT, same       │                      │
    │   device profile)      │                      │
    │                        │                      │
    │  pods scheduled        │                      │
    │  claims allocated      │                      │
    │  (may be same or       │                      │
    │   different physical   │                      │
    │   devices, but same    │                      │
    │   class + selectors)   │                      │
    │                        │◄─────────────────────│
    │                        │                      │
    │  trainer restores      │                      │
    │  from checkpoint       │                      │
    │  RTJ phase = Running   │                      │
    │                        │                      │
```

**Notes:**

- The ResourceClaimTemplate survives the pause because it is owned by the
  RTJ, not by the child JobSet. When the child JobSet is deleted, the
  template remains.
- On resume, the operator verifies device profile compatibility before
  re-creating the child JobSet. The same ResourceClaimTemplate is reused.
- Physical device assignment may differ between runs (different GPUs on
  different nodes), but the device class and selector constraints ensure
  equivalent capability.
- Checkpoint metadata records the device fingerprint for compatibility
  verification on resume.

---

## Sequence diagram 3: Resume rejection due to incompatible device profile

This shows what happens when the user changes the RTJ's device spec between
pause and resume, making the existing checkpoint incompatible.

```
RTJ Operator           Checkpoint Store
    │                        │
    │  RTJ Paused with       │
    │  lastCheckpoint from   │
    │  DeviceClass=gpu-a100  │
    │  selectors=[mem>=80Gi] │
    │                        │
    │  User modifies RTJ:    │
    │  DeviceClass=gpu-h100  │
    │  selectors=[mem>=80Gi] │
    │                        │
    │  RESUME requested      │
    │                        │
    │  select checkpoint     │
    │  (latest compatible    │
    │   complete)            │
    │◄───────────────────────│
    │                        │
    │  compatibility check:  │
    │  RTJ deviceClass =     │
    │    gpu-h100            │
    │  ckpt deviceClass =    │
    │    gpu-a100            │
    │                        │
    │  gpu-h100 != gpu-a100  │
    │                        │
    │  INCOMPATIBLE          │
    │                        │
    │  Decision depends on   │
    │  checkpoint catalog:   │
    │                        │
    │  Case A: No compatible │
    │  checkpoint exists     │
    │  ┌─────────────────────┤
    │  │ fresh start (no     │
    │  │ restore, attempt=1) │
    │  │ RTJ creates new     │
    │  │ ResourceClaimTemplate│
    │  │ with gpu-h100 spec  │
    │  │ Workload queued     │
    │  │ with new device req │
    │  └─────────────────────┤
    │                        │
    │  Case B: Older compat  │
    │  checkpoint exists     │
    │  (from gpu-h100 era)   │
    │  ┌─────────────────────┤
    │  │ restore from older  │
    │  │ compatible checkpoint│
    │  │ (loses progress since│
    │  │ gpu-a100 checkpoint) │
    │  └─────────────────────┤
    │                        │
    │  Case C: Max retries   │
    │  exceeded, no compat   │
    │  checkpoint            │
    │  ┌─────────────────────┤
    │  │ RTJ phase = Failed  │
    │  │ reason: NoCompatible│
    │  │ Checkpoint           │
    │  └─────────────────────┤
    │                        │
```

**Notes:**

- Device class mismatch is treated identically to GPU shape mismatch in
  Phase 0 ADR 0003. It is a hard incompatibility.
- The user can change the device spec on the RTJ while paused. The operator
  will attempt to find a compatible checkpoint for the new spec. If none
  exists, it falls through to the standard "no compatible checkpoint"
  handling: fresh start if this is the first attempt, or fail if retries
  are exhausted.
- The ResourceClaimTemplate is updated (or recreated) to reflect the new
  device spec before the child JobSet is rendered.
- This behavior matches the existing Phase 0 fail-closed resume contract.
  No new failure modes are introduced -- only a new compatibility dimension.

---

## Detailed design

### 1. RTJ spec.devices

```go
// DeviceSpec declares DRA device requirements for worker pods.
// When present, the RTJ operator creates a companion ResourceClaimTemplate
// and renders DRA claim references in the child JobSet pod templates.
// When absent, the RTJ follows the Phase 7 path unchanged.
type DeviceSpec struct {
    // DeviceClassName is the name of the DRA DeviceClass.
    // Must match a DeviceClass installed in the cluster and configured
    // in Kueue's deviceClassMappings for quota accounting.
    // +kubebuilder:validation:MinLength=1
    DeviceClassName string `json:"deviceClassName"`

    // Selectors are optional CEL-expression selectors for device attributes.
    // Each selector is a CEL expression that evaluates against device
    // attributes published in the ResourceSlice. All selectors must match.
    // +optional
    Selectors []string `json:"selectors,omitempty"`

    // Count is the number of devices requested per worker pod.
    // Default: 1.
    // +optional
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:default=1
    Count int32 `json:"count,omitempty"`

    // ClaimName is an optional override for the generated
    // ResourceClaimTemplate name. Default: "<rtj-name>-devices".
    // +optional
    ClaimName string `json:"claimName,omitempty"`
}
```

### 2. ResourceClaimTemplate generation

The RTJ operator generates a ResourceClaimTemplate from `spec.devices`:

```yaml
apiVersion: resource.k8s.io/v1beta2
kind: ResourceClaimTemplate
metadata:
  name: <rtj-name>-devices    # or spec.devices.claimName
  namespace: <rtj-namespace>
  ownerReferences:
    - apiVersion: training.checkpoint.example.io/v1alpha1
      kind: ResumableTrainingJob
      name: <rtj-name>
      uid: <rtj-uid>
      controller: true
      blockOwnerDeletion: true
spec:
  spec:
    devices:
      requests:
        - name: accelerator
          deviceClassName: <spec.devices.deviceClassName>
          count: <spec.devices.count>
          selectors:
            - cel:
                expression: "<selector[0]>"
            - cel:
                expression: "<selector[1]>"
```

### 3. Child JobSet pod template injection

The rendered child JobSet worker pod template is extended with:

```yaml
spec:
  resourceClaims:
    - name: rtj-devices
      resourceClaimTemplateName: <rtj-name>-devices
  containers:
    - name: worker
      resources:
        claims:
          - name: rtj-devices
```

This is applied additively, after topology injection and podSetUpdate
application (Phase 7).

### 4. Kueue deviceClassMappings configuration

The cluster admin configures Kueue to account for DRA devices:

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: gpu-training-queue
spec:
  resourceGroups:
    - coveredResources:
        - cpu
        - memory
      flavors:
        - name: default
          resources:
            - name: cpu
              nominalQuota: "100"
            - name: memory
              nominalQuota: "400Gi"
    - deviceClassMappings:
        - deviceClassName: gpu.example.com
          resourceFlavorReference: gpu-flavor
          count: 8    # total device quota for this class
```

The Workload PodSet synthesized by the RTJ operator includes the DRA
device request, so Kueue can account for it through the mapping.

### 5. Checkpoint device fingerprint

The checkpoint manifest is extended with:

```json
{
  "deviceClass": "gpu.example.com",
  "deviceFingerprint": "sha256:<hash-of-sorted-selectors>",
  "worldSize": 4,
  "gpuShape": "simulated-gpu",
  ...existing Phase 0 fields...
}
```

The fingerprint is computed as `SHA256(sorted(selectors))`. When
`spec.devices` is nil, both fields are empty strings in the manifest.

Compatibility rules:
- Empty-to-empty: compatible (Phase 7 behavior).
- Non-empty-to-empty: incompatible.
- Empty-to-non-empty: incompatible.
- Non-empty-to-non-empty: compatible only if deviceClass and fingerprint
  match exactly.

---

## Phase 8 invariants

All Phase 0-7 invariants are preserved. Phase 8 adds:

| ID | Invariant |
|---|---|
| P8-1 | RTJ creates a ResourceClaimTemplate when spec.devices is present |
| P8-2 | ResourceClaimTemplates are owned by the RTJ and garbage-collected on deletion |
| P8-3 | ResourceClaimTemplates and ResourceClaims are helper runtime objects, NOT Kueue-managed workloads |
| P8-4 | Kueue accounts for DRA devices via deviceClassMappings, not direct claim management |
| P8-5 | Child JobSet pods reference the ResourceClaimTemplate, not direct ResourceClaims |
| P8-6 | Checkpoint compatibility includes device class and device selector fingerprint |
| P8-7 | Phase 7 behavior is preserved when spec.devices is absent |
| P8-8 | Manager-mode RTJ does not create ResourceClaimTemplates (worker-local) |
| P8-9 | Native DRA claims are the core path; extended-resource bridge is not required |
