# Kueue External Integration

Phase 2 uses native Kueue external integration for `ResumableTrainingJob` (`RTJ`).

## What Was Wired

- The operator now adds Kueue `Workload` APIs to the shared manager scheme.
- The operator now runs the RTJ Kueue generic reconciler in the same controller-runtime manager as the existing RTJ controller.
- `RTJ` is registered with Kueue's external-framework registry as:
  - `ResumableTrainingJob.v1alpha1.training.checkpoint.example.io`
- The workload-owner field index is installed for the RTJ GVK so the generic reconciler can find RTJ-owned `Workload` objects.
- RBAC now covers:
  - `kueue.x-k8s.io/workloads`
  - `kueue.x-k8s.io/workloadpriorityclasses`
  - `kueue.x-k8s.io/resourceflavors`
  - `scheduling.k8s.io/priorityclasses`

## GenericJob Shape

The RTJ adapter in [rtj_generic_job.go](/Users/rishivinodkumar/Daedelus/internal/kueue/rtj_generic_job.go) implements the current required `jobframework.GenericJob` surface.

- `PodSets()` derives Kueue pod sets directly from the embedded JobSet template in `spec.runtime.template.spec`.
- `RunWithPodSetsInfo()` applies admission-time pod-set mutations back onto the RTJ template and clears `spec.suspend`.
- `RestorePodSetsInfo()` removes those admission-time mutations from the RTJ template.
- `Finished()`, `IsActive()`, and `PodsReady()` map conservatively from RTJ phase for now.

This adapter is part of the active Phase 2 path.
It works with the RTJ controller so RTJ becomes the admitted object while the child `JobSet` remains runtime-only.

## Manager Topology

Phase 2 keeps one manager process.

- Reason: the repo already has one operator manager, one webhook server, and one cache.
- Adding the generic Kueue reconciler to that manager is materially simpler than introducing a second manager with duplicated cert, health, and cache wiring.
- The current setup therefore keeps the control-plane boundary inside one binary while still following Kueue's external-integration model.

## Kueue Manager Config

Kueue itself must also be told that RTJ is an externally managed framework.

Development snippets were added under [deploy/dev/kueue](/Users/rishivinodkumar/Daedelus/deploy/dev/kueue):

- [controller_manager_config.phase2-rtj-external-framework.yaml](/Users/rishivinodkumar/Daedelus/deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml)
- [helm-values.phase2-rtj-external-framework.yaml](/Users/rishivinodkumar/Daedelus/deploy/dev/kueue/helm-values.phase2-rtj-external-framework.yaml)

The relevant Kueue config fragment is:

```yaml
integrations:
  externalFrameworks:
    - ResumableTrainingJob.v1alpha1.training.checkpoint.example.io
```

## Current Boundary

The signed-off Phase 2 runtime contract is:

- the Kueue generic reconciler creates and manages RTJ-owned `Workload` objects
- the RTJ template receives Kueue admission pod-set mutations
- the RTJ controller defers child `JobSet` creation until RTJ admission clears `spec.suspend`
- the rendered child `JobSet` strips Kueue queue and priority identity and does not create a second Kueue `Workload`

The main remaining gaps are visibility and soak depth, not admission ownership:

- RTJ status does not yet project workload reference or admitted cluster queue
- the live suite proves one strong deterministic preemption-resume path, not a repeated multi-cycle soak

Those gaps are tracked in [review/gaps.md](review/gaps.md).
