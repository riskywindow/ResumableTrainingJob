# ADR 0002: Launch Gate Status API

## Status

Accepted

## Date

2026-03-30

## Context

### Problem

Phase 7 introduces capacity-guaranteed launch, which requires the RTJ
operator to evaluate multiple prerequisites before rendering the child
JobSet. Users and operators need visibility into:

1. **Why the launch gate is blocked** -- which admission check or
   prerequisite is not yet satisfied.
2. **Provisioning state** -- whether physical capacity has been confirmed,
   how many attempts have been made, and what the ProvisioningRequest
   reference is.
3. **Startup/recovery health** -- whether pods reached Ready, why they
   were evicted, and what the most recent failure or requeue reason was.
4. **Capacity guarantee** -- a simple boolean indicator of whether the
   workload has a physical capacity guarantee (not just quota).

### Design question

Should these be status-only fields, or do we need new spec fields?

### Options considered

**Option A: Status-only additions**

Add four new controller-owned status sections:
- `status.launchGate` -- aggregate gate state
- `status.provisioning` -- provisioning-specific state
- `status.startupRecovery` -- startup/recovery lifecycle
- `status.capacity` -- capacity guarantee indicator

No new spec fields. Provisioning is configured at the ClusterQueue level
via Kueue's AdmissionCheck mechanism (decided in ADR 0001). The RTJ
operator reads Workload state and derives all status values.

**Option B: Add a spec field for provisioning timeout**

Add `spec.provisioning.timeout` to let users control how long to wait
for provisioning before giving up.

Rejected because:
- Kueue's waitForPodsReady already provides timeout semantics.
- ProvisioningRequest backends have their own timeout mechanisms.
- Adding a spec field creates a user-authored knob that overlaps with
  infrastructure configuration.
- The Phase 0 contract principle is to keep user-authored spec narrow.

**Option C: Add a spec field for launch gate policy**

Add `spec.launchGate.policy` with values like `FailOpen`, `FailClosed`.

Rejected because:
- The fail-safe behavior is an operator-level decision, not per-workload.
- ADR 0001 already defines the fail-safe contract (fail-open on missing
  AC state, fail-closed on unknown AC state).
- Adding per-workload policy increases API surface without clear benefit.

## Decision

### Status-only additions (Option A)

Phase 7 adds **four new controller-owned status sections** and **zero
new spec fields**. This preserves the Phase 0 principle of keeping the
user-authored spec narrow and pushes observability to status.

### Rationale for no spec fields

1. **Provisioning is infrastructure configuration**, not workload
   configuration. It is configured at the ClusterQueue level. The RTJ
   author does not choose whether provisioning is enabled.

2. **Launch gate behavior is operator policy**, not user intent. The
   fail-safe contract (ADR 0001) applies uniformly. Per-workload
   overrides would create confusing precedence rules.

3. **All inputs to the launch gate are already expressed** through
   existing spec fields (`spec.topology`, `spec.suspend`) or external
   configuration (ClusterQueue AdmissionChecks). No new spec field is
   needed to express user intent.

4. **Status-only fields are backward compatible by construction**. They
   are nil in Phase 6 manifests and populated only by the controller.
   No webhook validation changes are needed.

### Status field design principles

1. **Flat JSON field names match the requirements list.** Fields like
   `launchGateState`, `provisioningState`, `provisioningAttempt` appear
   directly in the JSON, not nested behind generic containers.

2. **Each status section groups related fields.** The four sections
   (`launchGate`, `provisioning`, `startupRecovery`, `capacity`) each
   have a clear owner (the Phase 7 controller logic) and a clear
   consumer (users, dashboards, alerting).

3. **Derived fields are explicitly marked.** `status.capacity.guaranteeActive`
   is a derived boolean computed from admission + provisioning state.
   `status.startupRecovery.podsReadyState` is derived from child runtime
   pod observations.

4. **Controller-owned semantics follow existing patterns.** Phase 4
   (`launchReadiness`), Phase 5 (`priorityShaping`), and Phase 6
   (`multiCluster`) all use the same pattern: optional status section,
   controller-owned, nil when feature is not active.

### Manager-side and worker-side compatibility

- **Worker mode**: The controller populates all four Phase 7 status
  sections based on local Workload and child runtime state.
- **Manager mode**: The controller does not populate `startupRecovery`
  or `capacity` (no local child runtime). It may populate `launchGate`
  and `provisioning` if the manager cluster has a Kueue admission
  pipeline. Worker-side Phase 7 status is visible via the existing
  `status.multiCluster.remotePhase` mirroring mechanism.

## Consequences

### Positive

1. **Zero breaking changes.** Phase 6 manifests accepted unchanged.
2. **No webhook changes.** Status-only fields bypass webhook validation.
3. **Clear ownership model.** All Phase 7 fields are controller-owned.
4. **Consistent patterns.** Follows Phase 4/5/6 status design patterns.
5. **Rich observability.** Each status section answers a specific
   operational question (why is launch blocked? is provisioning done?
   did startup succeed? is capacity guaranteed?).

### Negative

1. **Status sections are numerous.** RTJ status now has 11 optional
   sections. This is a natural consequence of the phased design but may
   feel large to new users.
2. **No user override for fail-safe behavior.** The fail-open/fail-closed
   contract is fixed in ADR 0001. If a user wants different behavior,
   they must change the ClusterQueue configuration.

### Risks

1. **Status field bloat.** Mitigated by keeping each section focused and
   nil when not active.
2. **Derived fields may diverge from source.** `capacity.guaranteeActive`
   could briefly be stale during reconciliation. Mitigated by updating
   all derived fields atomically in each reconciliation pass.

## Verification

| Criterion | Test |
|---|---|
| Phase 6 manifest accepted unchanged | Unit test: backward-compatible decoding |
| Webhook does not inject Phase 7 status | Unit test: defaulting behavior |
| DeepCopy preserves Phase 7 status | Unit test: deep copy independence |
| Controller-owned fields survive spec update | Unit test: status preservation |
| All new types compile and serialize | Build + test suite |
