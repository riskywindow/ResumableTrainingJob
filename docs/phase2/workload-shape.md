# Phase 2 Workload Shape

This note records the concrete Phase 2 workload-shape synthesis used for native Kueue admission of `ResumableTrainingJob` (`RTJ`).

## Scope

This pass stays narrow to the embedded JobSet template shape already supported by the repo:

- `spec.runtime.template.kind=JobSet`
- `spec.runtime.template.spec.replicatedJobs[]`
- each replicated job carries one `batch/v1 JobTemplateSpec`
- each JobTemplateSpec carries one pod template under `template.spec.template`

Phase 2 does not add another runtime template format.

## PodSet Synthesis

Kueue `Workload.spec.podSets` are synthesized directly from the embedded JobSet template on RTJ.

For each `replicatedJobs[i]` entry:

- podSet name = `replicatedJobs[i].name`
- podSet template = deep copy of `replicatedJobs[i].template.spec.template`
- podSet count = `replicas * podsPerReplica`

`podsPerReplica` follows the same shape already used by Kueue's built-in JobSet integration:

- default `parallelism=1`
- default `completions=parallelism`
- `podsPerReplica = min(parallelism, completions)`

That keeps Phase 2 admission accounting aligned with the current plain JobSet runtime model:

- Kueue admits the same pod template shape the runtime will launch
- Kueue sees the same per-pod resource requests declared on the template containers
- RTJ becomes the only admitted object, but admission size still matches the child runtime

## Supported Resource Model

The synthesized podSet template is copied from the runtime pod template, so Kueue accounts for the resource requests already declared there:

- CPU
- memory
- GPU or other extended resources
- node selectors, tolerations, and affinity once Kueue mutates the admitted podSet info

This pass does not introduce custom resource summarization outside the current pod-template request model.

## Child JobSet Rules

The child JobSet is runtime-only in Phase 2.

That means the rendered child JobSet must not carry top-level Kueue admission identity:

- no `kueue.x-k8s.io/queue-name`
- no `kueue.x-k8s.io/priority-class`
- no other top-level `kueue.x-k8s.io/*` labels or annotations
- no `provreq.kueue.x-k8s.io/*` annotations

The child JobSet may still carry operator bookkeeping metadata such as:

- RTJ name
- run attempt
- operator managed-by label

## Ancestor-Management Guardrail

Removing queue and priority labels alone is not sufficient once RTJ itself is registered as a Kueue-managed external job.

Kueue's jobframework can discover a managed ancestor through a controller owner reference chain.
To avoid the child JobSet being treated as a second Kueue-managed job:

- the child JobSet keeps an owner reference to RTJ for garbage collection
- the child JobSet does not use a controller owner reference to RTJ

This keeps the runtime object linked to RTJ without making it discoverable as a controller-owned Kueue descendant.

## Admission Gating

The RTJ controller must not create a child JobSet before RTJ is effectively admitted.

For this pass, the runtime launch gate is:

- `spec.control.desiredState=Running`
- `spec.suspend=false`
- no active child JobSet already exists

If `desiredState=Running` but `spec.suspend=true`, the controller leaves the RTJ in `Queued` and does not create:

- a new child JobSet
- a new control ConfigMap

This keeps RTJ as the only Kueue-managed admission object while preserving the existing Phase 1 runtime model based on JobSet.
