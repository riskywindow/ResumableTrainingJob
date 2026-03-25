# Phase 1 API Implementation Notes

## Scope

This file describes the concrete Phase 1 `v1alpha1` API scaffold that now exists in code.
It is a subset of the accepted Phase 0 conceptual API, not a scope expansion.

## Manual Control Field

Phase 1 reuses the accepted Phase 0 field:

- `spec.control.desiredState`

No new manual pause or resume field was added.
The scaffold defaults it to `Running` when omitted.

## Included Spec Fields

The scaffold includes these user-facing spec areas:

- `spec.queueName`
- `spec.workloadPriorityClassName`
- `spec.identity.image`
- `spec.identity.codeVersion`
- `spec.identity.worldSize`
- `spec.identity.gpuShape`
- `spec.runtime.mode`
- `spec.runtime.optimizerMode`
- `spec.runtime.shardingMode`
- `spec.runtime.template`
- `spec.checkpoint.storageURI`
- `spec.checkpoint.interval`
- `spec.checkpoint.freshnessBudget`
- `spec.checkpoint.maxDrainTime`
- `spec.checkpoint.safePointMode`
- `spec.resume.sourcePolicy`
- `spec.resume.maxResumeRetries`
- `spec.control.desiredState`

## Embedded JobSet Template Choice

Phase 0 allowed either a template reference or an embedded template.
Phase 1 implements only the embedded path for now because it is the smallest practical shape for the vertical slice.

The embedded template is intentionally narrow:

- `apiVersion`
- `kind`
- optional embedded labels and annotations
- schemaless `spec`

This keeps the CRD small while still letting Phase 1 carry a child `JobSet` spec payload.
The controller does not materialize that `JobSet` yet.

## Defaults

The scaffold defaults:

- `spec.control.desiredState=Running`
- `spec.checkpoint.safePointMode=StepBoundary`
- `spec.resume.sourcePolicy=LatestCompatibleComplete`
- `spec.resume.maxResumeRetries=3`
- `spec.runtime.template.apiVersion=jobset.x-k8s.io/v1alpha2`
- `spec.runtime.template.kind=JobSet`

These defaults are plain Go helper defaults today.
They do not require an admission webhook to be useful in unit tests or reconcile-time normalization.

## Validation

The scaffold validates:

- required queue, priority, identity, and runtime fields
- positive world size
- embedded template presence
- `JobSet` as the only supported embedded template kind
- valid JSON for the embedded template spec payload
- `s3://` checkpoint storage URI
- positive checkpoint interval, freshness budget, and max drain time
- `freshnessBudget >= interval`
- `safePointMode=StepBoundary`
- `sourcePolicy=LatestCompatibleComplete`
- `maxResumeRetries >= 1`
- `desiredState` limited to `Running` or `Paused`

## Status Shape

The scaffold includes the Phase 0-aligned status fields requested for Phase 1:

- `status.phase`
- `status.conditions`
- `status.currentRunAttempt`
- `status.lastCompletedCheckpoint`
- `status.selectedCheckpoint`
- `status.reason`
- `status.message`
- `status.observedGeneration`
- `status.transitionTimestamps`

The controller currently initializes only a minimal `Pending` state with generation tracking and finalizers.
It does not create or manage child `JobSet` resources yet.
