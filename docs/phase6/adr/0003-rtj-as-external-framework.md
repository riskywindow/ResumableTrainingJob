# ADR-0003: RTJ as a MultiKueue External Framework

- **Status:** Accepted
- **Date:** 2026-03-30
- **Scope:** Phase 6, Goal G1 (MultiKueue external-framework integration)
- **Depends on:** ADR-0001 (multi-cluster spillover), ADR-0002 (managedBy and remote status)

## Context

Phase 6 requires RTJ to be dispatched from a manager cluster to worker
clusters via Kueue's MultiKueue. Kueue v0.15.1 provides two mechanisms
for multi-cluster job dispatch:

1. **Built-in framework adapters:** Kueue ships native MultiKueue adapters
   for built-in frameworks (batch/Job, JobSet, etc.). These implement the
   `MultiKueueAdapter` interface directly in Go.

2. **External-framework generic adapter:** For CRDs not built into Kueue
   (like RTJ), Kueue v0.15.1 ships a generic unstructured adapter
   (`externalframeworks.Adapter`) that handles any CRD listed in the Kueue
   Configuration's `integrations.externalFrameworks`. This adapter operates
   on unstructured objects and requires no CRD-specific Go code in Kueue.

The decision is how RTJ should integrate with MultiKueue dispatch.

## Decision

### D1: RTJ uses Kueue's generic external-framework adapter

RTJ is registered as an external framework in the Kueue Configuration
using the name `ResumableTrainingJob.v1alpha1.training.checkpoint.example.io`.
The RTJ operator does NOT implement the `MultiKueueAdapter` interface.
Instead, Kueue's built-in `externalframeworks.Adapter` handles all
MultiKueue operations for RTJ.

**Rationale:** The generic adapter already handles:
- Remote RTJ creation (deep copy, strip `managedBy`, add labels)
- Remote RTJ status mirroring (full `.status` copy)
- Remote RTJ deletion (on completion, manager-side deletion, GC)
- `managedBy`-based eligibility check
- Watch event propagation from worker clusters

Implementing a custom adapter would duplicate this functionality without
adding RTJ-specific value, violating the hard boundary "no custom
MultiKueue dispatcher in the core milestone."

### D2: Two feature gates must be enabled

The following Kueue feature gates are required:
- `MultiKueue` (Beta, default-on since v0.9)
- `MultiKueueAdaptersForCustomJobs` (Beta, default-on since v0.15)

Both are default-on in Kueue v0.15.1, so no explicit configuration is
needed unless they were previously disabled.

### D3: spec.managedBy is the eligibility signal

The generic adapter checks `spec.managedBy == "kueue.x-k8s.io/multikueue"`
on the manager-side RTJ to determine if the job should be dispatched via
MultiKueue. This is the same field defined in ADR-0002.

On the remote copy, the adapter strips `spec.managedBy` so the worker-side
Kueue and RTJ operator treat it as a locally-owned job.

### D4: Status mirroring is full unstructured copy

The generic adapter copies the entire `.status` object from the remote RTJ
to the manager-side RTJ as an unstructured map. This means all RTJ status
fields (phase, conditions, checkpoint references, admission status, etc.)
are automatically mirrored without field-by-field mapping.

This is consistent with ADR-0002's `status.multiCluster` design: the
manager-side RTJ controller populates `multiCluster` fields based on
the mirrored status, and the mirrored fields from the worker (phase,
conditions, etc.) flow through naturally.

### D5: Spec mutations are not propagated

The generic adapter does NOT propagate spec mutations from the manager-side
RTJ to the remote copy. If the manager-side RTJ spec changes (e.g.,
`desiredState` changes for pause), the remote Workload is deleted and
recreated. This means pause propagation requires a mechanism outside of
simple spec sync.

**Impact on G3 (pause propagation):** Pause cannot be propagated by
patching `spec.control.desiredState` on the manager and expecting it to
flow to the worker. The pause mechanism must use Kueue's built-in
Workload suspension (setting `spec.suspend = true` on the manager RTJ,
which triggers Kueue to suspend the remote Workload). This is documented
as OQ-2 resolution.

### D6: Remote cleanup is automatic

The generic adapter handles remote object cleanup in three scenarios:
1. Manager-side RTJ/Workload deletion triggers remote deletion.
2. Remote Workloads that drift from the manager spec are deleted.
3. Periodic GC removes orphaned remote objects.

No custom cleanup logic is needed in the RTJ operator.

### D7: KeepAdmissionCheckPending returns true

The generic adapter's `KeepAdmissionCheckPending()` returns `true` for
external-framework jobs. This keeps the manager-side RTJ suspended (via
the AdmissionCheck remaining in Pending state) while the remote copy runs
on a worker. This is critical: it prevents the manager-side RTJ operator
from attempting to create local child JobSets.

Combined with the `--mode=manager` suppression from Session 3, there are
two layers of protection against accidental local runtime creation.

### D8: RBAC is required on both clusters

The manager-side Kueue controller needs RBAC to:
- Read RTJ objects (get, list, watch) for the `IsJobManagedByKueue` check
- Write RTJ status (update, patch) for mirroring remote status

The worker-side remote client (via MultiKueueCluster kubeconfig) needs:
- Create, get, delete RTJ objects for remote copy management
- Read RTJ status for status sync back to manager

## Alternatives Considered

### A1: Custom MultiKueueAdapter implementation

Implement the `MultiKueueAdapter` and `MultiKueueWatcher` interfaces in
the RTJ operator and register them with Kueue's integration manager.

**Rejected:** This would require the RTJ adapter to be compiled into Kueue
or use a plugin mechanism that doesn't exist. The generic external-framework
adapter already handles RTJ correctly. A custom adapter adds complexity
without benefit and violates the "no custom MultiKueue dispatcher" boundary.

### A2: CRD-specific spec mutation propagation

Build a sidecar or webhook that watches for manager-side RTJ spec changes
and propagates them to the remote copy.

**Rejected:** This introduces a custom cross-cluster sync mechanism outside
of MultiKueue, which violates the architecture boundary. Pause is handled
via Kueue Workload suspension instead.

### A3: Status mirroring with field-level mapping

Implement field-by-field status mapping instead of relying on the generic
adapter's full `.status` copy.

**Rejected:** The full status copy is simpler, requires no maintenance when
new status fields are added, and provides all information needed for the
manager-side `multiCluster` status population.

## Consequences

### Positive

- Zero RTJ-specific MultiKueue code in Kueue. The generic adapter handles
  everything.
- Status mirroring automatically includes all RTJ status fields, including
  future additions.
- Remote object lifecycle (creation, cleanup, GC) is handled by Kueue's
  battle-tested implementation.
- Feature gates are Beta and default-on, minimizing configuration burden.

### Negative

- Spec mutations are not propagated. Pause requires Kueue Workload
  suspension rather than direct spec patching. This is a constraint but
  aligns with the Kueue-centric architecture.
- The full `.status` copy may include worker-local fields (like
  `activeJobSetName`) that are meaningless on the manager. The manager
  controller must be aware of this and use `multiCluster` fields for
  authoritative remote status.
- Dependence on Kueue's generic adapter means RTJ behavior during
  MultiKueue dispatch is governed by Kueue's implementation. Changes in
  Kueue's adapter behavior may affect RTJ.

### Risks

- If Kueue changes the generic adapter's behavior in a future version
  (e.g., changing how `managedBy` is handled or how status is mirrored),
  RTJ MultiKueue integration may break. Mitigated by pinning Kueue v0.15.1.
- The generic adapter uses unstructured objects, which means no compile-time
  type safety for RTJ fields during dispatch. Mitigated by comprehensive
  integration tests.

## Verification

- Unit tests in `internal/multikueue/` validate configuration and constants.
- Deploy artifacts in `deploy/multikueue/` provide ready-to-apply Kubernetes
  manifests for the manager cluster.
- Integration testing (deferred to G5) will verify end-to-end dispatch in a
  three-cluster kind environment.
