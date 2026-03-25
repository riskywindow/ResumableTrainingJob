# Migration from Phase 3

## What Stays the Same

### Kueue Authority Model

RTJ remains the **only** Kueue-managed admission object. Child JobSets remain
**plain runtime resources** with no Kueue management metadata. The external
`jobframework` integration path, the `RTJGenericJob` adapter, and the Kueue
generic reconciler are unchanged in their overall structure.

### Lifecycle State Machine

All lifecycle phases are unchanged:

```
Pending → Queued → Admitted → Starting → Running
    → YieldRequested → Draining → Queued (Kueue re-queue)
    → Restoring → Running
    → Succeeded | Failed
```

Phase 4 does not add new phases. The `QuotaReserved` → `Admitted` transition
now includes an admission check gate, but the RTJ phase names and allowed
transitions are identical to Phase 3.

### Suspend Semantics

- `spec.suspend` remains the Kueue-facing admission gate.
- `spec.control.desiredState` remains the user-facing manual hold surface.
- These two fields are not aliases. Their semantics and interaction rules
  are unchanged from Phase 2.

### Checkpoint Contract

The checkpoint storage layout, manifest schema, manifest-last publication
semantics, yield-marker contract, and checkpoint completeness/validity rules
are **unchanged** from Phase 3.

### Graceful Yield and Drain

The graceful yield protocol is unchanged. Control ConfigMap, step-boundary
yield, DCP checkpoint, yield marker, manifest publication, bounded drain
timer, fail-closed on timeout — all identical to Phase 2/3.

### Resume Selection

The `LatestCompatibleComplete` source policy remains the only supported
policy. The selection algorithm is unchanged. World-size-flexible resume
from Phase 3 (`allowWorldSizeChange`) continues to work.

### Flavor-Aware Rendering

Phase 3's flavor-aware child JobSet rendering (nodeSelector, tolerations
from ResourceFlavors, admitted replica counts, Kueue label stripping)
continues to work unchanged. Phase 4 topology constraints are **additive**
to flavor constraints, not replacements.

### Phase 3 API Fields

All Phase 3 spec and status fields are preserved:

- `spec.parallelism` (preferredCount, minCount, podSetName, enablePartialAdmission)
- `spec.resume.allowWorldSizeChange`
- `status.admission` (admittedWorkerCount, preferredWorkerCount, admittedFlavors)
- `status.restore` (restoreMode, checkpointWorldSize, restoreWorldSize)

### Existing Environment Variables

All Phase 1/2/3 environment variables remain supported and unchanged:

| Variable | Status |
| --- | --- |
| `YIELD_SDK_STORAGE_URI` | Unchanged |
| `YIELD_SDK_CONTROL_FILE` | Unchanged |
| `YIELD_SDK_RUN_ATTEMPT` | Unchanged |
| `YIELD_SDK_RESTORE_MANIFEST_URI` | Unchanged |
| `YIELD_SDK_RTJ_IDENTITY` | Unchanged |
| `YIELD_SDK_CLUSTER_IDENTITY` | Unchanged |
| `YIELD_SDK_RUNTIME_MODE` | Unchanged |
| `YIELD_SDK_GPU_SHAPE` | Unchanged |
| `YIELD_SDK_IMAGE_IDENTITY` | Unchanged |
| `YIELD_SDK_CODE_VERSION` | Unchanged |
| `YIELD_SDK_OPTIMIZER_MODE` | Unchanged |
| `YIELD_SDK_SHARDING_MODE` | Unchanged |
| `YIELD_SDK_STAGING_ROOT` | Unchanged |
| `YIELD_SDK_RESTORE_ROOT` | Unchanged |
| `YIELD_SDK_YIELD_MARKER_PATH` | Unchanged |
| `YIELD_SDK_YIELD_MARKER_URI` | Unchanged |
| `YIELD_SDK_WORLD_SIZE` | Unchanged |
| `YIELD_SDK_ORIGINAL_WORLD_SIZE` | Unchanged |
| `YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE` | Unchanged |
| `YIELD_SDK_ADMITTED_FLAVOR` | Unchanged |

### Pinned Versions

Kueue v0.15.1, JobSet v0.10.1, controller-runtime v0.22.4. No version bumps.

## What Changes in Launch Gating

### Before (Phase 3)

In Phase 3, the launch gate is:

1. `spec.suspend=false` (Kueue has admitted the RTJ).
2. `spec.control.desiredState=Running`.
3. No active child JobSet exists.

When these conditions are met, the controller creates the child JobSet
immediately.

### After (Phase 4, with ResumeReadiness)

In Phase 4, when the ResumeReadiness admission check is configured on the
ClusterQueue, the launch pipeline gains an additional gate:

1. Kueue reserves quota and assigns topology (TAS).
2. The ResumeReadiness controller validates topology and checkpoint
   compatibility, then marks the admission check as `Ready`.
3. Kueue admits the Workload (calls `RunWithPodSetsInfo`, sets `suspend=false`).
4. `spec.suspend=false`, `spec.control.desiredState=Running`, no active child.
5. The controller creates the topology-aware child JobSet.

The **net effect on the RTJ controller** is minimal: by the time
`spec.suspend=false`, the full admission pipeline (including topology and
readiness) has already completed. The controller's launch gate logic
(`spec.suspend=false && desiredState=Running && no active child`) is
unchanged. The difference is that the controller now has topology data
available and uses it during rendering.

### Without ResumeReadiness (Phase 3 Compatibility)

When no ResumeReadiness admission check is configured on the ClusterQueue,
the admission pipeline is identical to Phase 3:

1. Kueue reserves quota, admits directly (no admission check gate).
2. `spec.suspend=false`.
3. Controller creates child JobSet.

No behavioral change from Phase 3.

## What Changes in Topology Handling

### Before (Phase 3)

Phase 3 uses ResourceFlavor `nodeSelector` and `tolerations` for placement.
Kueue assigns a flavor, and the controller applies the flavor's scheduling
constraints to the child JobSet. There is no topology-level awareness — pods
from the same job may land on nodes in different racks or zones within the
same flavor's node pool.

### After (Phase 4)

Phase 4 adds **topology-aware placement** on top of flavor-aware placement:

1. **RTJ declares topology.** `spec.topology.required` or `spec.topology.preferred`
   specifies the topology level (e.g., zone, rack, hostname).

2. **PodSets carry TopologyRequest.** Kueue's TAS uses this to assign pods to
   specific topology domains during scheduling.

3. **Child JobSet gets topology constraints.** The controller reads the
   `TopologyAssignment` from the Workload admission and injects scheduling
   constraints (nodeSelector, affinity, topology spread) into the child
   JobSet pod templates.

4. **Pods land in assigned domains.** Training pods are co-located within
   their assigned topology domains, enabling NCCL-efficient communication.

Topology handling is **additive** to flavor handling. A child JobSet may have
both flavor-derived constraints (e.g., `pool: a100`) and topology-derived
constraints (e.g., `topology.kubernetes.io/zone: zone-a`).

### Without Topology (Phase 3 Compatibility)

When `spec.topology` is nil:

1. No `TopologyRequest` on PodSets. Kueue does not invoke TAS.
2. No topology constraints on child JobSet. Placement is flavor-only.
3. Behavior is identical to Phase 3.

## What Changes in the Admission Pipeline

### Before (Phase 3)

```
submit → queue → schedule (flavor assignment) → admit (RunWithPodSetsInfo)
→ controller creates child JobSet
```

Admission is immediate once Kueue schedules and reserves quota.

### After (Phase 4, full pipeline)

```
submit → queue → schedule (flavor + topology assignment) → quota reserved
→ admission checks (ProvisioningRequest* → ResumeReadiness) → admit
→ controller creates topology-aware child JobSet
```

The admission check gate adds a validation step between quota reservation
and admission. This is transparent to the RTJ controller — by the time
`spec.suspend=false`, all gates have cleared.

`*` ProvisioningRequest is optional.

## Why Elastic Workloads Are Still Deferred

Phase 4 adds topology-aware admission and launch. It does NOT add:

- **True in-place elastic scaling.** Phase 3 handles resume-time shape
  changes only. Live scaling of running workloads (adding/removing pods
  without restart) is a fundamentally different problem that requires:
  - A new lifecycle phase (e.g., `Scaling`).
  - Live coordination with the training runtime (PyTorch elastic agent).
  - Partial checkpoint of only the affected ranks.
  - Kueue does not support in-place scaling of admitted workloads.

- **Elastic Workloads as a Kueue feature.** Kueue's Elastic Workloads
  concept (dynamic resource adjustment) is still evolving upstream. The
  RTJ operator should not depend on unstable upstream APIs.

- **Automatic world-size optimization.** Choosing the optimal world size
  based on available capacity requires a scheduling policy that is Kueue's
  responsibility, not the operator's.

Phase 3's partial admission (experimental) already provides the building
block for resume-time shape flexibility. Phase 4 focuses on the orthogonal
concern of topology-aware placement. Elastic Workloads remain deferred to a
future phase.

## Upgrade Path

### From Phase 3 to Phase 4 (No Feature Changes)

1. Deploy the Phase 4 operator.
2. Existing RTJs with no `spec.topology` and no ResumeReadiness admission
   check on the ClusterQueue continue to work identically to Phase 3.
3. No behavioral changes unless topology or admission check features are
   explicitly enabled.

### Enabling Topology-Aware Placement

1. Create a `Topology` CR in Kueue defining the topology levels and node
   labels.
2. Set `spec.topology.required` (or `preferred`) on the RTJ.
3. Ensure the ClusterQueue's ResourceFlavors correspond to nodes that have
   the topology labels.
4. The Workload PodSets will carry `TopologyRequest`, and Kueue's TAS will
   assign topology domains.

### Enabling ResumeReadiness Gate

1. Create the `AdmissionCheck` CR:
   ```yaml
   apiVersion: kueue.x-k8s.io/v1beta1
   kind: AdmissionCheck
   metadata:
     name: resume-readiness
   spec:
     controllerName: checkpoint-native.example.io/resume-readiness
   ```
2. Add `resume-readiness` to the ClusterQueue's `admissionChecks` list.
3. The operator's ResumeReadiness controller will automatically gate
   Workloads with this check.

### Enabling ProvisioningRequest (Optional)

1. Configure a ProvisioningRequest admission check on the ClusterQueue
   alongside `resume-readiness`.
2. Deploy or configure the cloud provider's provisioning controller.
3. Workloads will go through: provision → topology → readiness → admit.
4. This path is optional and not required for local success.
