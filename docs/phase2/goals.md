# Phase 2 Goals

## Objective

Phase 2 replaces the Phase 1 "Kueue-managed child `JobSet`" model with a native Kueue integration for `ResumableTrainingJob`.
The target design is:

- Kueue externally integrates with `RTJ` through `jobframework`
- `RTJ` is the only Kueue-managed admission object
- the child `JobSet` stays a plain runtime carrier
- Kueue-driven suspend and preemption trigger graceful yield and checkpoint
- re-admission resumes from the latest compatible complete checkpoint

## In Scope

- Add a suspend-like field to RTJ for Kueue external integration.
- Implement the Kueue `jobframework` adapter for RTJ and register RTJ as an external framework.
- Derive Kueue pod-set and resource accounting directly from the RTJ's embedded JobSet template.
- Surface `Queued` and `Admitted` as real RTJ lifecycle states.
- Make the RTJ controller launch a child `JobSet` only after RTJ admission.
- Make the RTJ controller treat Kueue-driven suspend or preemption as a graceful-yield request, not as an abrupt delete.
- Tear down the child `JobSet` only after the current drain finishes successfully or the bounded drain contract fails closed.
- Resume through a new child `JobSet` attempt after Kueue re-admits the RTJ.
- Preserve the existing Phase 1 manual pause and resume path if practical by mapping it onto the same Phase 2 drain and resume lifecycle.
- Reuse the pinned Phase 1 development versions unless a documented blocker appears:
  - Kueue `v0.15.1`
  - JobSet `v0.10.1`

## Explicit Phase 2 Statements

- RTJ becomes the only Kueue-managed admission object.
- Child JobSets must no longer be Kueue-managed in Phase 2.
- Kueue-driven suspend and preemption are now in scope.

## Still Out Of Scope

- custom scheduling policy
- fair-sharing innovation beyond stock Kueue behavior
- MultiKueue
- topology-aware scheduling or topology-aware resume
- elastic workloads
- world-size changes on resume
- transparent CUDA, process, or container snapshots
- a custom scheduler
- a custom preemption algorithm

## Success Bar

Phase 2 is successful when the design and later implementation can do all of the following without breaking Phase 0 boundaries:

- queue and admit RTJ through Kueue external integration
- keep only one Kueue-managed admission object per training lineage
- launch plain child JobSets only when RTJ is admitted and eligible to run
- yield gracefully on Kueue-driven suspension or manual pause
- checkpoint before runtime teardown on the controlled preemption path
- resume from the latest compatible complete checkpoint after re-admission
- preserve clear ownership boundaries between Kueue, RTJ control logic, and JobSet runtime reconciliation
