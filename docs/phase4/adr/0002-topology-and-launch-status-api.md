# ADR 0002: Topology and Launch-Readiness Status API

- **Status:** Accepted
- **Date:** 2026-03-24
- **Context:** Phase 4 API extension for topology intent and launch-readiness status.

## Decision

Extend the RTJ API with:

1. **`spec.topology`** — optional topology placement intent for the worker pod set.
2. **`status.launchReadiness`** — pre-launch readiness gate summary.
3. **`status.topology`** — admitted topology assignment from Kueue TAS.
4. **`status.effectiveLaunchShape`** — computed launch shape from admission.

These fields are purely additive. No existing fields are modified or removed.

## Context

ADR 0001 locked the Phase 4 admission pipeline: topology-aware PodSet
synthesis (G1), topology-aware materialization (G2), ResumeReadiness
AdmissionCheck (G3), and admission-gated launch (G4). That ADR defined the
pipeline but did not specify the exact API shape for expressing topology
intent or surfacing admission/launch-readiness status on the RTJ.

This ADR fills that gap: it specifies the concrete types, fields, defaulting,
and validation for the Phase 4 API surface.

## Decisions

### D1: TopologyMode Enum with Four Values

**Decision:** The topology mode is an enum with four values:
`Disabled`, `Required`, `Preferred`, `Unconstrained`.

**Rationale:** This maps directly to Kueue's TAS model:
- `Required` maps to `TopologyRequest.Required`.
- `Preferred` maps to `TopologyRequest.Preferred`.
- `Unconstrained` enables TAS awareness without constraining placement.
- `Disabled` is the zero-topology state (Phase 3 behavior).

**Alternative considered:** A single boolean `enabled` + a string `level`.
Rejected because it cannot express the distinction between required and
preferred placement.

**Alternative considered:** Separate `spec.topology.required` and
`spec.topology.preferred` string fields (as sketched in architecture.md).
Rejected because a mode enum is more explicit about the intent and avoids
ambiguity when both would be set.

### D2: TopologyLevel as a String Label Key

**Decision:** `spec.topology.topologyLevel` is a string that names a node
label key (e.g., `topology.kubernetes.io/zone`, `kubernetes.io/hostname`).

**Rationale:** This is the standard Kueue TAS convention. The topology level
is a label key, not a label value. The label key identifies the domain
boundary; Kueue populates domain values from node labels at admission time.

### D3: LeaderWorkerColocation Flag

**Decision:** `spec.topology.leaderWorkerColocation` is an optional boolean
that requests the leader pod (if present as a separate replicatedJob) to be
co-located in the same topology domain as workers.

**Rationale:** Multi-role templates (leader + workers) are common in
distributed training. Without this flag, only the worker pod set would get
a topology request, potentially placing the leader in a different domain.
The flag is optional and defaults to false.

**Constraint:** Forbidden when mode is `Disabled` (no topology to co-locate
within).

### D4: TopologyLevel Required for Required/Preferred Modes

**Decision:** `topologyLevel` is required when mode is `Required` or
`Preferred`. It is optional (ignored) when mode is `Disabled` or
`Unconstrained`.

**Rationale:** Required and Preferred modes need a specific topology level
to constrain placement. Without a level, the scheduler has no domain to
bind to. Unconstrained mode does not constrain placement to a specific
level; it enables TAS awareness without a level requirement.

### D5: Launch-Readiness Status as a Separate Struct

**Decision:** `status.launchReadiness` is a standalone status struct with
`ready`, `gateState`, `reason`, `message`, and `lastTransitionTime`.

**Rationale:** This mirrors the Kueue AdmissionCheck state model. The
controller populates this struct when the ResumeReadiness check is active.
When the check is not configured, the field stays nil (no operational cost).

**Alternative considered:** Use a Condition instead. Rejected because
conditions are list-based and less convenient for a single readiness gate
that needs a summary view.

### D6: Topology Status Mirrors Kueue TopologyAssignment

**Decision:** `status.topology` mirrors the shape of Kueue's
`TopologyAssignment` (levels + domains with values and counts).

**Rationale:** This provides observability without requiring the user to
read the internal Workload object. The status is a denormalized copy of
the admission assignment.

### D7: EffectiveLaunchShape for Operational Visibility

**Decision:** `status.effectiveLaunchShape` captures the derived launch
parameters: worker count, world size, resume mode, and selected checkpoint.

**Rationale:** These values are scattered across multiple status fields
(admission, restore, selectedCheckpoint). The launch shape provides a
single-read summary of what the next or current launch looks like. This
is especially useful for operators and dashboards.

### D8: Backward Compatibility via Nil Semantics

**Decision:** When `spec.topology` is nil, all Phase 4 behavior is skipped.
No defaulting occurs, no validation runs, and all new status fields stay nil.

**Rationale:** Phase 3 manifests must work unchanged. Nil means "this
feature is not requested." This is consistent with the existing `spec.parallelism`
nil semantics from Phase 3.

## Consequences

### Positive

- Clean API surface for topology intent.
- Explicit mode enum prevents ambiguity.
- Full backward compatibility with Phase 3.
- Status fields provide operational visibility without reading internal
  Kueue objects.

### Negative

- The mode enum requires webhook validation (implemented).
- `effectiveLaunchShape` is partially redundant with existing fields
  (admission, restore, selectedCheckpoint). Justified by the single-read
  summary benefit.

### Neutral

- No changes to existing fields or their semantics.
- No changes to workload synthesis or operator launch logic (deferred to
  subsequent implementation sessions).

## Verification

| Decision | Verification |
|----------|-------------|
| D1 | Unit test: all four modes accepted by validation. |
| D2 | Unit test: topologyLevel required for Required/Preferred. |
| D3 | Unit test: colocation forbidden with Disabled mode, accepted with Required. |
| D4 | Unit test: missing topologyLevel rejected for Required/Preferred. |
| D5 | Type compilation + DeepCopy test. |
| D6 | Type compilation + DeepCopy test. |
| D7 | Type compilation + DeepCopy test. |
| D8 | Unit test: nil topology passes validation, status fields stay nil. |

All verification tests are implemented in
`api/v1alpha1/resumabletrainingjob_webhook_test.go`.
