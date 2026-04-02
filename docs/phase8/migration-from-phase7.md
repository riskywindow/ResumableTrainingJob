# Phase 8 -- Migration from Phase 7

## What stays the same

These Phase 7 (and earlier) behaviors are **unchanged** in Phase 8:

| Concern | Phase 7 behavior | Phase 8 status |
|---|---|---|
| RTJ as only Kueue-managed object | RTJ creates Workload; child JobSet is plain runtime | Unchanged |
| Kueue as admission/preemption authority | Kueue queues, admits, preempts RTJ workloads | Unchanged |
| RTJ lifecycle state machine | Pending → Queued → Admitted → Starting → Running → YieldRequested → Draining → Paused → Restoring | Unchanged (new compat dimension added, phases unchanged) |
| Graceful yield path | On preemption: signal checkpoint → drain → delete child → Paused | Unchanged |
| Resume path | Paused → checkpoint selected → Workload re-queued → re-admitted → resume | Unchanged (device compat added to selection) |
| Checkpoint contract | PyTorch DCP to S3-compatible storage | Unchanged (manifest extended with device fields) |
| Suspend/unsuspend semantics | spec.suspend controls Workload suspension | Unchanged |
| Priority shaping (Phase 5) | CheckpointPriorityPolicy adjusts effective priority | Unchanged |
| Manager/worker split (Phase 6) | managedBy field, MultiKueue dispatch, status mirroring | Unchanged |
| Topology spec (Phase 4) | Required/Preferred/Unconstrained modes | Unchanged |
| Partial admission (Phase 3) | MinCount-based partial admission | Unchanged |
| Launch gate (Phase 7) | Quota → all ACs Ready → topology assigned → launch | Unchanged (DRA template created before launch) |
| ProvisioningRequest AC (Phase 7) | Capacity-guaranteed launch via ProvisioningRequest | Unchanged |
| waitForPodsReady (Phase 7) | Startup/recovery timeout → eviction → yield | Unchanged |
| All RTJ spec fields | No Phase 7 spec fields are removed or renamed | Unchanged |

## What changes in runtime resource modeling

### Phase 7: Extended-resource model

In Phase 7, GPU/accelerator requirements are expressed through:

1. **`spec.identity.gpuShape`**: A string identifier for resume
   compatibility (e.g., `"nvidia-a100-80gb"`). This is a semantic tag
   used by the checkpoint compatibility checker.

2. **Extended-resource requests in the JobSet template**: The embedded
   JobSet spec contains container resource requests like
   `nvidia.com/gpu: 8`. Kueue accounts for these through its standard
   resource-flavor quota mechanism.

3. **No structured device model**: The RTJ has no awareness of device
   classes, device attributes, or device lifecycle. GPU shape is an
   opaque string; the actual device binding is handled by the device
   plugin framework.

### Phase 8: DRA device model

Phase 8 adds an optional `spec.devices` section that uses the Kubernetes
DRA vocabulary:

1. **`spec.devices.deviceClassName`**: References a DRA DeviceClass
   installed in the cluster (e.g., `gpu.example.com`). This is a
   Kubernetes-native resource, not an opaque string.

2. **`spec.devices.selectors`**: CEL-expression selectors for device
   attributes (e.g., `device.attributes["memory"].compareTo(quantity("80Gi")) >= 0`).
   These select specific devices from the class based on published
   attributes.

3. **`spec.devices.count`**: Number of devices per worker pod.

4. **ResourceClaimTemplate**: The operator creates a per-RTJ
   ResourceClaimTemplate that encodes the device request. Worker pods
   reference this template, and the kubelet creates per-pod
   ResourceClaims from it.

5. **Kueue `deviceClassMappings`**: Kueue accounts for DRA device
   classes through its `deviceClassMappings` mechanism on ClusterQueue
   ResourceGroups, mapping DeviceClass names to quota flavors.

### What the user sees

**Phase 7 RTJ (no change needed):**

```yaml
spec:
  identity:
    gpuShape: "nvidia-a100-80gb"
  runtime:
    template:
      spec:
        # embedded JobSet with nvidia.com/gpu: 8 in container resources
```

**Phase 8 RTJ (opt-in):**

```yaml
spec:
  identity:
    gpuShape: "nvidia-a100-80gb"   # still required for Phase 0 compat
  devices:
    deviceClassName: gpu.example.com
    selectors:
      - 'device.attributes["memory"].compareTo(quantity("80Gi")) >= 0'
    count: 8
  runtime:
    template:
      spec:
        # embedded JobSet -- DRA claims injected by operator
        # no need to include nvidia.com/gpu in container resources
```

## Why Phase 8 uses native DRA claims instead of the alpha extended-resource bridge

### What is the extended-resource bridge?

Kubernetes DRA includes an experimental mechanism to translate DRA device
requests into traditional extended-resource requests
(`<domain>/<resource-name>: <count>`). This allows legacy schedulers and
quota systems to account for DRA devices without native DRA support.

### Why Phase 8 does NOT use it

1. **Alpha stability**: The extended-resource bridge is an alpha feature
   in DRA. It may change or be removed in future Kubernetes releases.
   Building on alpha APIs creates upgrade risk.

2. **Kueue already supports DRA natively**: Kueue's `deviceClassMappings`
   provides native DRA quota/accounting without the bridge. Using the
   bridge would add an unnecessary translation layer.

3. **Loss of structured information**: The bridge collapses structured
   device requests (class + selectors + attributes) into a flat count.
   This loses the ability to select devices by attributes, which is the
   primary value of DRA over extended resources.

4. **No selector support**: The bridge maps device requests to a single
   extended-resource counter. CEL selectors are lost in translation.
   Phase 8's core value proposition is structured device selection.

5. **Two incompatible paths**: If Phase 8 used the bridge as the core
   path, it would need to maintain both the bridge path and a future
   native-claim path. Starting with native claims avoids this.

### What Phase 8 does instead

- Uses native DRA `ResourceClaimTemplate` + `ResourceClaim` objects.
- Relies on Kueue `deviceClassMappings` for quota/accounting.
- Defers the extended-resource bridge to an optional/experimental path
  for clusters that cannot yet run DRA drivers.

## Why the local path uses an example DRA driver

Real DRA drivers (NVIDIA `k8s-device-plugin`, AMD `k8s-device-plugin`)
require:

- Real hardware (GPUs, FPGAs) installed in the nodes.
- Vendor-specific kernel drivers and container runtime integration.
- Vendor-specific device advertisement (ResourceSlice publishing).

For local development and CI/e2e testing, we need:

- **Deterministic behavior**: Tests must not depend on hardware.
- **Fast execution**: No driver installation or kernel module loading.
- **No vendor lock-in**: Tests work on any developer machine.
- **Full DRA lifecycle**: ResourceClaimTemplate → ResourceClaim →
  allocation → pod binding must all work.

The example DRA driver solves this by:

1. Publishing fake ResourceSlices with simulated device attributes.
2. Allocating simulated devices to ResourceClaims.
3. Running as a DaemonSet + controller with no hardware dependencies.
4. Being maintained as part of the upstream Kubernetes DRA test
   infrastructure.

This is the same pattern used in Kubernetes DRA e2e tests and in
`kubernetes-sigs/dra-example-driver`.

## Upgrade path

### Single-cluster Phase 7 → Phase 8

1. **No existing RTJ spec changes required.** Phase 7 RTJs without
   `spec.devices` work unchanged.
2. Upgrade RTJ operator to Phase 8 version.
3. (Optional) Install example DRA driver for testing.
4. (Optional) Create DeviceClass resource for the DRA driver.
5. (Optional) Add `deviceClassMappings` to ClusterQueue.
6. (Optional) Add `spec.devices` to new or existing RTJs.

Steps 3-6 are opt-in. Without them, Phase 8 operator behaves identically
to Phase 7.

### Multi-cluster Phase 7 → Phase 8

1. Upgrade RTJ operator on **worker clusters** to Phase 8 version.
2. (Optional) Install DRA driver on worker cluster nodes.
3. (Optional) Configure DeviceClass + `deviceClassMappings` on worker
   ClusterQueues.
4. Manager cluster operator can stay at Phase 7 or upgrade; manager does
   not participate in DRA claim creation.
5. RTJ `spec.devices` is propagated from manager to worker via MultiKueue.
   The worker operator creates ResourceClaimTemplates locally.

Phase 8 features are worker-local; the manager cluster is unaffected.

### Coexistence with extended-resource GPUs

Phase 8 does not remove support for extended-resource GPU requests in the
embedded JobSet template. An RTJ can have:

- `spec.devices` for DRA-based device requests (Phase 8).
- Extended-resource requests in the embedded JobSet template (Phase 7).
- Both (though this is not recommended and may cause double-counting).

The recommended migration path:

1. Start with Phase 7 extended-resource requests (existing behavior).
2. Add `spec.devices` to new RTJs that target DRA-enabled clusters.
3. Remove extended-resource requests from the template when the cluster
   fully supports DRA.
4. Eventually, `spec.identity.gpuShape` and `spec.devices.deviceClassName`
   can be reconciled (future phase).
