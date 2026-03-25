# Experimental Partial Admission

## Overview

Phase 3 Goal G4 adds an experimental partial-admission path for
ResumableTrainingJob. When enabled, Kueue may admit fewer worker pods than
the preferred count (but not fewer than a configured minimum). The controller
materializes the admitted count into the child JobSet and uses DCP resharding
to resume from checkpoints saved at a different world size.

This feature is **experimental and off by default**. It requires explicit
opt-in at two levels:

1. **Operator level:** `--enable-experimental-partial-admission` flag.
2. **Per-job level:** `spec.parallelism.enablePartialAdmission: true`.

## Architecture

```
RTJ Spec                    Operator Flag
  |                             |
  v                             v
spec.parallelism        --enable-experimental-
  .enablePartialAdmission   partial-admission
  .minCount                     |
  .preferredCount               |
  .podSetName                   |
  |                             |
  +----------+------------------+
             |
             v
    PodSetsFromRTJTemplate()
             |
             +-- Worker PodSet.Count = preferredCount
             +-- Worker PodSet.MinCount = minCount  (only when both gates on)
             |
             v
    Kueue Workload
             |
             v
    Kueue Admission (may reduce count to >= minCount)
             |
             v
    RunWithPodSetsInfo → bridge annotation
             |
             v
    Controller renders child JobSet with admitted count
```

## Double Gating

### Operator Level

The `--enable-experimental-partial-admission` flag on the operator binary
controls `SetExperimentalPartialAdmission(true)` in the `internal/kueue`
package. When `false` (the default), `PodSetsFromRTJTemplate` never sets
`PodSet.MinCount`, regardless of per-job settings.

### Per-Job Level

Even when the operator flag is on, each RTJ must explicitly opt in:

```yaml
spec:
  resume:
    allowWorldSizeChange: true   # required for partial admission
  parallelism:
    preferredCount: 8
    minCount: 4
    podSetName: "worker"
    enablePartialAdmission: true
```

The API validation enforces:
- `enablePartialAdmission` requires `allowWorldSizeChange: true`.
- `minCount` is required when `enablePartialAdmission` is true.
- `minCount` must be <= effective preferred count.

### Kueue Side

Kueue v0.15.1 ships with `PartialAdmission` feature gate at **Beta, default-on**.
The Kueue generic reconciler automatically processes `PodSet.MinCount` when
the feature gate is enabled. No Kueue code changes are needed.

If the Kueue deployment has explicitly disabled the `PartialAdmission` feature
gate, the Kueue reconciler will clear `MinCount` from all pod sets before
creating the Workload. In that case, partial admission has no effect even if
the RTJ and operator both opt in.

## PodSet Synthesis

### Worker Identification

`resolveWorkerPodSetName` determines the scalable worker pod set:

1. If `spec.parallelism.podSetName` is set, use that name.
2. Otherwise, default to the first replicatedJob in the template.

Only the identified worker pod set gets `preferredCount` and `MinCount`
overrides. All other pod sets (leaders, coordinators) keep their
template-derived counts.

### Count Override

When `spec.parallelism` is set:

```
Worker PodSet.Count = EffectivePreferredCount()
                    = spec.parallelism.preferredCount  (if > 0)
                    OR spec.identity.worldSize          (fallback)
```

This overrides the template-derived count for the worker pod set. The
template's parallelism and completions are preserved (used for replica math
at render time).

### MinCount

When both gates are on and `enablePartialAdmission` is true:

```
Worker PodSet.MinCount = EffectiveMinCount()
                       = spec.parallelism.minCount  (if enablePartialAdmission && minCount != nil)
                       OR nil                        (partial admission not configured)
```

## Kueue Integration Details

### Kueue Version: v0.15.1

- `PodSet.MinCount *int32` is available in `kueuev1beta2`.
- `PartialAdmission` feature gate: Beta, default-on.
- `clearMinCountsIfFeatureDisabled()` in the generic reconciler clears
  MinCount if the feature gate is off.
- Only one PodSet per Workload may use MinCount (Kueue enforces this).

### External Framework Compatibility

The RTJ external framework integration uses Kueue's `GenericReconciler`
which handles:
- Workload creation from `PodSets()` output.
- Feature gate enforcement (`clearMinCountsIfFeatureDisabled`).
- Admission tracking and `RunWithPodSetsInfo` callbacks.

No special external-framework configuration is needed for partial admission.

## Test Coverage

### PodSet Tests (8 tests)

| Test | What It Verifies |
| --- | --- |
| `TestPodSetsFromRTJTemplateSynthesizesSupportedRuntimeShape` | Phase 2 baseline shape |
| `TestPodSetsFromRTJTemplateRejectsBlankReplicatedJobName` | Error handling |
| `TestPodSetsFromRTJTemplateDefaultModeDoesNotEmitMinCount` | Operator flag off → no MinCount |
| `TestPodSetsFromRTJTemplateExperimentalModeEmitsMinCountForWorkerOnly` | MinCount on worker only |
| `TestPodSetsFromRTJTemplatePreferredCountOverridesTemplateCount` | Count override from spec |
| `TestPodSetsFromRTJTemplateFixedSizeRTJUnchanged` | No parallelism = Phase 2 behavior |
| `TestPodSetsFromRTJTemplateDefaultsWorkerToFirstReplicatedJob` | Default pod set name |
| `TestPodSetsFromRTJTemplatePartialAdmissionDisabledPerJobIgnoresMinCount` | Per-job flag off → no MinCount |

## What This Does NOT Implement

- Automatic partial-admission negotiation (Kueue handles this).
- Dynamic scaling of running workloads (out of scope for Phase 3).
- Multi-pod-set MinCount (Kueue allows only one pod set with MinCount).
- Topology-aware partial admission.

## Phase 2 Backward Compatibility

When neither gate is enabled:

- `PodSetsFromRTJTemplate` produces the same output as Phase 2.
- No `MinCount` is set on any pod set.
- Pod set counts come from the template, not from `ParallelismSpec`.
- All existing tests pass unchanged.
