# Phase 8 -- Goals and Acceptance Criteria

## Mission

Give RTJ first-class support for Kubernetes DRA so that accelerator
requests are expressed as structured device claims -- not opaque
extended-resource counters -- while preserving checkpoint-resume safety
and Kueue quota/accounting.

Provide a local dev path using an example DRA driver with simulated
devices so the full flow is testable without real GPUs.

---

## Core goals

### G1 -- Optional DRA device spec on RTJ

Add an optional `spec.devices` section to the RTJ spec. When present,
the operator translates it into per-worker ResourceClaimTemplate objects
and DRA pod-level claim references on the child JobSet. When absent, the
RTJ follows the Phase 7 path unchanged.

The `spec.devices` section declares:

- **DeviceClassName**: the DRA DeviceClass name (e.g., `gpu.example.com`).
- **DeviceSelectors**: optional CEL-expression selectors for device
  attributes (e.g., `device.attributes["memory"].compareTo(quantity("80Gi")) >= 0`).
- **Count**: number of devices per worker pod (default 1).
- **ClaimName**: optional override for the generated ResourceClaimTemplate
  name (default: derived from RTJ name).

### G2 -- Companion ResourceClaimTemplate lifecycle

The RTJ operator creates a ResourceClaimTemplate for each RTJ that has a
`spec.devices` section. The template is:

- **Owned** by the RTJ (ownerReference with controller=true).
- **Created** during the render path, before child JobSet creation.
- **Updated** if the RTJ's device spec changes (on a new run attempt).
- **Garbage-collected** by Kubernetes when the RTJ is deleted.

ResourceClaimTemplates are namespace-scoped. Each worker pod gets its own
ResourceClaim instance created by the kubelet from the template.

### G3 -- Kueue deviceClassMappings-based quota/accounting

Kueue accounts for DRA device classes via `deviceClassMappings` on
ClusterQueue ResourceGroups. This maps DRA DeviceClass names to Kueue
resource flavors for quota purposes.

Phase 8 does **not** introduce a custom quota engine. The cluster admin
configures `deviceClassMappings` on the ClusterQueue, and Kueue handles
the rest. The RTJ Workload's PodSets include the DRA device request so
Kueue can account for it.

### G4 -- DRA-aware child JobSet rendering

The child JobSet's worker pod template includes:

- `spec.resourceClaims[]` entries referencing the RTJ-managed
  ResourceClaimTemplate (via `resourceClaimTemplateName`).
- Container `resources.claims[]` entries that bind the pod-level claims
  to the container.

This rendering is additive to existing template processing (topology
injection, podSetUpdates, flavor selectors). DRA claims are applied
after Phase 7 podSetUpdate application.

### G5 -- Conservative checkpoint compatibility for device profiles

The Phase 0 ADR 0003 resume-compatibility contract is extended:

- **DeviceClass** is added to the checkpoint compatibility fingerprint.
- **DeviceSelector fingerprint** (a hash of the sorted CEL selectors)
  is added to the fingerprint.
- A checkpoint taken with DeviceClass=X and selectors=Y is incompatible
  with a resume attempt using DeviceClass=X' or selectors=Y' unless
  X==X' and Y==Y'.
- When `spec.devices` is nil (Phase 7 behavior), the device fingerprint
  is empty and matches any other empty fingerprint. This preserves
  backward compatibility.

The compatibility check is fail-closed: unknown or missing device
fingerprints on the checkpoint side are treated as incompatible with a
non-empty device spec on the RTJ side.

### G6 -- Example DRA driver local dev profile

The local/dev environment uses an upstream example DRA driver that
advertises fake/simulated devices. This enables:

- Full end-to-end testing of the DRA-aware launch flow locally.
- Testing ResourceClaimTemplate creation, claim allocation, and pod
  scheduling with DRA.
- Testing checkpoint compatibility for device profiles.
- No real GPUs or vendor-specific drivers required.

Candidate drivers:
- `registry.k8s.io/e2e-test-images/sample-device-plugin` (KEP-4381 example)
- `dra-example-driver` from `kubernetes-sigs/dra-example-driver`
- Any conformant DRA driver that can run with simulated resources.

The driver is deployed as a DaemonSet + controller in the dev namespace.

### G7 -- Worker-cluster runtime compatibility

DRA claims are allocated on the worker cluster, not the manager cluster.
Phase 8 follows the same manager/worker split as Phase 6-7:

- **Manager mode**: creates Workload, dispatches via MultiKueue,
  suppresses local runtime. Does not create ResourceClaimTemplates.
- **Worker mode**: creates ResourceClaimTemplates and child JobSet
  locally. DRA claims are allocated by the local kubelet and DRA driver.

---

## Non-goals (explicit out-of-scope)

| ID | Non-goal | Reason |
|---|---|---|
| NG1 | DRA extended-resource bridge as core path | Alpha, not stable; native claims are the path |
| NG2 | Shared/manual ResourceClaims | Deferred; per-pod templates are the core path |
| NG3 | Custom DRA driver for RTJ | Use upstream/example drivers; no vendor lock-in |
| NG4 | Replacing Kueue quota with DRA-native quota | Kueue deviceClassMappings is sufficient |
| NG5 | Multi-device-class per pod | Single device class per RTJ; multi-class is future |
| NG6 | Device topology/affinity constraints | Kueue TAS (Phase 4) handles topology; DRA does not add a second topology mechanism |
| NG7 | CUDA/container snapshots | Out of v1 scope (Phase 0 ADR 0001) |
| NG8 | Live migration between device types | Out of scope |
| NG9 | Real vendor DRA drivers in CI | Example driver is sufficient for local/CI |
| NG10 | Speculative device APIs not required for Phase 8 | Explicitly deferred |

---

## Must-ship success criteria

| ID | Criterion | Verification |
|---|---|---|
| MS1 | RTJ with `spec.devices` creates a ResourceClaimTemplate owned by the RTJ | Unit test + e2e test |
| MS2 | Child JobSet worker pods reference the ResourceClaimTemplate in `spec.resourceClaims` | Unit test + e2e test |
| MS3 | ResourceClaim is allocated by the DRA driver and pods run successfully | e2e test with example DRA driver |
| MS4 | Kueue accounts for DRA device via `deviceClassMappings` on ClusterQueue | e2e test: RTJ admitted only when ClusterQueue has device quota |
| MS5 | RTJ without `spec.devices` follows Phase 7 path unchanged | e2e test: Phase 7 e2e suite regression |
| MS6 | Checkpoint created with DeviceClass=X is incompatible with resume using DeviceClass=Y | Unit test |
| MS7 | Checkpoint created without device spec is compatible with resume without device spec | Unit test |
| MS8 | ResourceClaimTemplate is garbage-collected when RTJ is deleted | e2e test |
| MS9 | Example DRA driver works in local kind cluster with simulated devices | e2e test |
| MS10 | Manager-mode RTJ does not create ResourceClaimTemplates | Unit test |

## Should-ship success criteria

| ID | Criterion | Verification |
|---|---|---|
| SS1 | RTJ status shows device allocation state (claim name, device class, allocation status) | Unit test + e2e inspection |
| SS2 | Prometheus metrics for DRA claim lifecycle (created, allocated, failed) | Unit test |
| SS3 | RTJ pause/resume with same device profile successfully resumes from checkpoint | e2e test |
| SS4 | Documentation for real vendor DRA driver configuration (NVIDIA, AMD) | Doc only |
| SS5 | Device selector fingerprint stored in checkpoint manifest | Unit test |

## Exit criteria

1. All MS* criteria pass in CI.
2. Phase 7 e2e suite passes without regression.
3. Docs pack complete (this directory).
4. session-handoff.md updated with final state.
