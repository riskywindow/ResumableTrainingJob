# ADR 0003: Experimental Partial Admission for RTJ

## Status

Accepted

## Context

Phase 3 Goal G4 requires an experimental partial-admission path for
ResumableTrainingJob. Kueue supports partial admission through `PodSet.MinCount`:
when a Workload declares MinCount, Kueue may admit fewer pods than the preferred
Count (but not fewer than MinCount). This lets training workloads start with
reduced parallelism when full resources are unavailable.

Key constraints:
- The feature must be experimental and off by default.
- It must not affect Phase 2 behavior when disabled.
- It must work with the pinned Kueue v0.15.1 external framework integration.
- Only the worker pod set should be partially admittable; leaders are fixed-size.

## Decision

### 1. Double-gated activation

Partial admission requires opt-in at two levels:

- **Operator level:** `--enable-experimental-partial-admission` CLI flag.
  This controls a package-level variable in `internal/kueue`. When false,
  `PodSetsFromRTJTemplate` never sets `PodSet.MinCount`.

- **Per-job level:** `spec.parallelism.enablePartialAdmission: true` on
  the RTJ. `EffectiveMinCount()` returns nil when this is false, so even
  if the operator flag is on, the per-job flag must also be true.

**Rationale:** The operator-level gate prevents accidental activation across
all RTJs in a cluster. The per-job gate gives workload authors control over
which training jobs can tolerate reduced parallelism.

### 2. Worker-only MinCount

Only the pod set identified by `spec.parallelism.podSetName` (defaulting to
the first replicatedJob) gets `PodSet.MinCount`. All other pod sets retain
their template-derived counts with no MinCount.

**Rationale:** Leaders/coordinators must always have exactly the specified
count. Kueue enforces that only one PodSet per Workload may use MinCount,
which aligns with our single-worker-pod-set model.

### 3. PreferredCount overrides template count

When `spec.parallelism` is set, the worker PodSet.Count is set from
`EffectivePreferredCount()` (which prefers `parallelism.preferredCount` or
falls back to `identity.worldSize`) rather than from the template's
replicas * podsPerReplica. This ensures consistency between the preferred
count, MinCount, and Kueue's admission decisions.

**Rationale:** Without this override, the PodSet.Count could diverge from
the validation constraints on MinCount, leading to invalid Workloads.

### 4. Kueue v0.15.1 compatibility verified

- `kueuev1beta2.PodSet.MinCount` field exists as `*int32`.
- `PartialAdmission` feature gate is **Beta, default-on** in v0.15.1.
- The generic reconciler's `clearMinCountsIfFeatureDisabled()` automatically
  clears MinCount if the Kueue deployment has disabled the feature gate.
- No Kueue code changes or version bumps are needed.

### 5. No end-to-end blockers identified

The pinned Kueue v0.15.1 provides all necessary APIs and behavior for
external framework partial admission:

| Requirement | Kueue v0.15.1 Status |
| --- | --- |
| `PodSet.MinCount *int32` | Available in kueuev1beta2 |
| `PartialAdmission` feature gate | Beta, default-on |
| External framework support | Works via GenericReconciler |
| Single-PodSet MinCount restriction | Enforced by Kueue |
| `clearMinCountsIfFeatureDisabled` | Automatic in reconciler |

The end-to-end path from RTJ spec through Kueue admission to child JobSet
rendering is fully functional with the scaffolding implemented in this session.

## Alternatives Considered

### A. Global feature gate via environment variable

Instead of a CLI flag, use an environment variable like
`ENABLE_EXPERIMENTAL_PARTIAL_ADMISSION=true`. Rejected because CLI flags
are the established pattern in the operator and are more visible in
deployment manifests.

### B. Kueue feature gate check in our code

Import Kueue's feature gate package and check `features.Enabled(features.PartialAdmission)`
in our code. Rejected because:
- It creates a tight coupling to Kueue internals.
- The generic reconciler already handles this via `clearMinCountsIfFeatureDisabled`.
- Our operator-level gate provides a separate control plane.

### C. Per-namespace feature gate

Gate partial admission at the namespace level. Rejected as over-engineered
for an experimental feature. The double-gate (operator + per-job) provides
sufficient control.

### D. No operator-level gate (per-job only)

Rely solely on per-job `enablePartialAdmission`. Rejected because the
operator owner should be able to globally disable experimental features
without modifying individual RTJ specs.

## Consequences

- Phase 2 behavior is fully preserved when the operator flag is off.
- Cluster administrators must explicitly enable the flag to allow any
  RTJ to use partial admission.
- The double-gate pattern can be reused for future experimental features.
- If Kueue's `PartialAdmission` feature gate is disabled on the Kueue side,
  MinCount is silently cleared and all RTJs get full admission only. This
  is a safe degradation.
