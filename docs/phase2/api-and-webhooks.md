# Phase 2 API And Webhooks

## Purpose

Phase 2 uses the RTJ API plus webhook/defaulting semantics required for native Kueue external integration.
The runtime-side `GenericJob` adapter is implemented in the repo and is part of the signed-off Phase 2 path.

The key API split is now explicit:

- `spec.control.desiredState` is the user-facing manual intent layer.
- `spec.suspend` is the Kueue-facing suspend gate used for external `jobframework` integration.

## Spec Shape

Phase 2 keeps the existing Phase 1 spec fields and adds one new Kueue-facing field:

- `spec.suspend`
- `spec.queueName`
- `spec.workloadPriorityClassName`
- `spec.identity.*`
- `spec.runtime.*`
- `spec.checkpoint.*`
- `spec.resume.*`
- `spec.control.desiredState`

### Ownership Semantics

`spec.suspend`:

- is the field Kueue will mutate through the supported external integration path
- controls whether the RTJ is eligible to run from Kueue's point of view
- defaults to suspended on create for queued RTJs through the mutating webhook

`spec.control.desiredState`:

- remains the backward-compatible manual intent field from Phase 1
- continues to accept `Running` and `Paused`
- does not bypass Kueue admission

The important Phase 2 distinction is:

- `desiredState=Running` means "run when Kueue admits me"
- `desiredState=Paused` means "stay manually held even if Kueue would otherwise admit me"
- `spec.suspend=true` means "Kueue currently considers this RTJ suspended"

## Metadata Projection For Kueue

The user-facing queue and workload-priority inputs stay in `spec`:

- `spec.queueName`
- `spec.workloadPriorityClassName`

The webhook projects those values onto RTJ metadata labels so Kueue's existing helper methods can validate the object using their current label-based invariants:

- `kueue.x-k8s.io/queue-name`
- `kueue.x-k8s.io/priority-class`

In Phase 2, those Kueue labels belong on RTJ.
They must no longer be projected onto the child `JobSet`.

## Status Additions

Phase 2 defines RTJ status fields for native Kueue visibility:

- `status.workloadReference`
- `status.admittedClusterQueue`
- `status.currentSuspension`
- `status.currentRunAttempt`
- `status.selectedCheckpoint`
- `status.lastCompletedCheckpoint`
- `status.phase`
- `status.conditions`

`status.currentSuspension` is populated by the controller today and explains the dominant active suspension source and reason at the RTJ level.

`status.workloadReference` and `status.admittedClusterQueue` are defined in the API but are not yet projected by the controller.
Operators still need to inspect the Kueue `Workload` object directly for those two values.

## Webhook Behavior

The mutating webhook now does three things:

1. applies the existing RTJ defaults
2. projects `spec.queueName` and `spec.workloadPriorityClassName` into RTJ metadata labels
3. calls Kueue's `ApplyDefaultForSuspend`

The validating webhook now:

1. runs RTJ spec validation
2. projects the spec-backed Kueue labels onto in-memory copies
3. calls Kueue's `ValidateJobOnCreate`
4. calls Kueue's `ValidateJobOnUpdate`

This gives Phase 2 the Kueue invariants used by the current repo state.

Under the pinned Kueue `v0.15.1` helper behavior verified in this prompt:

- queue-name updates are rejected while the RTJ is unsuspended
- queue-name updates are allowed while the RTJ remains suspended
- workload-priority-class updates remain allowed by the helper layer

## Phase 2 Boundary Notes

- The webhook projects Kueue labels onto RTJ, not onto the child `JobSet`.
- The local live demo and e2e path still sets `spec.suspend` and the Kueue labels explicitly in manifests because it runs the operator locally rather than through an in-cluster webhook deployment.
- The status projection of workload reference and admitted cluster queue remains a follow-up item; this is a visibility gap, not an admission-ownership gap.
