# ADR-0002: Core CRDs Graduated to v1beta1

**Status:** Accepted
**Date:** 2026-04-06
**Deciders:** Project maintainers
**Phase:** 10
**Supersedes:** Partial scope of ADR-0001 (which planned RTJ-only graduation)

---

## Context

ADR-0001 established the Phase 10 production hardening plan. It decided to
graduate only `ResumableTrainingJob` to v1beta1, keeping
`CheckpointPriorityPolicy` (CPP) and `ResumeReadinessPolicy` (RRP) at v1alpha1.

During implementation, we reconsidered this decision. The production path for
the checkpoint-native preemption controller relies on all three CRDs working
together:

1. **RTJ** is the core workload resource
2. **CPP** configures checkpoint-aware priority shaping that makes preemption
   safe and fair — without it, jobs are preempted without regard to checkpoint
   freshness
3. **RRP** gates resume attempts on checkpoint readiness — without it, jobs
   may resume from stale or incomplete checkpoints

Graduating only RTJ while leaving CPP and RRP at alpha would send a confusing
signal: "the workload resource is production-ready, but the policies that make
it safe to use are not."

## Decision

Graduate all three core CRDs to `v1beta1` as the smallest coherent production
set:

| CRD | v1alpha1 | v1beta1 |
|-----|----------|---------|
| `ResumableTrainingJob` | served + stored | served (not stored yet) |
| `CheckpointPriorityPolicy` | served + stored | served (not stored yet) |
| `ResumeReadinessPolicy` | served + stored | served (not stored yet) |

### Key properties of the graduation

- **Schema identity:** v1beta1 schema is structurally identical to v1alpha1
  for all three CRDs. No fields added, removed, or renamed.
- **Storage version unchanged:** v1alpha1 remains the storage version.
  Switching to v1beta1 storage requires a conversion webhook and
  StorageVersionMigration (deferred).
- **Conversion strategy:** `None` (default). The API server handles
  apiVersion-field-only conversion automatically since schemas match.
- **Experimental fields preserved:** `spec.devices`, `spec.elasticity`, and
  `spec.parallelism.enablePartialAdmission` on RTJ remain marked experimental
  and may change without deprecation.

### What stays in alpha

Experimental scheduling research surfaces that are not part of the core
production path:
- No additional CRDs beyond RTJ, CPP, and RRP are promoted
- Experimental sub-fields within RTJ remain documented as experimental

## Consequences

### Positive

- Users get a coherent production API surface: all three CRDs they need for
  safe checkpoint-native preemption are at v1beta1
- Clear stability signal: v1beta1 = production-suitable with deprecation policy
- CPP and RRP schemas are small and stable (Phase 4/5 vintage, unchanged since)
- Parity tests prove structural identity between v1alpha1 and v1beta1

### Negative

- CPP priority model parameters (boost values, window durations) are locked
  at beta stability earlier than originally planned — future changes to the
  priority algorithm will need deprecation handling
- RRP coupling to Kueue AdmissionCheck pipeline means any upstream
  AdmissionCheck evolution must be handled through deprecation

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| CPP priority model needs redesign | Low | Medium | Schema is config-style; new parameters can be added without breaking |
| Kueue AdmissionCheck API changes break RRP | Low | Medium | RRP schema is minimal (4 fields); adaptable via deprecation |
| Users expect full stability for experimental sub-fields | Medium | Low | Clear EXPERIMENTAL markers in docs, types, and graduation doc |

## Alternatives Considered

### Alternative A: RTJ-Only Graduation (ADR-0001 Original)

**Reconsidered because:** Graduating RTJ alone signals an incomplete production
story. Users would ask "is it production-ready?" and the answer would be "yes,
but the policies that make it safe are still alpha." This is confusing.

### Alternative B: Skip Beta, Go to v1

**Rejected:** Same reasoning as ADR-0001. Experimental sub-fields are not
mature enough for a full v1 commitment.

## References

- [ADR-0001: Production Hardening and API Beta](0001-production-hardening-and-api-beta.md)
- [API Graduation Details](../api-graduation.md)
- [Phase 10 Index](../index.md)
