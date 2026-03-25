# ADR 0002: Suspend Semantics

- Status: Accepted
- Date: 2026-03-22

## Context

Phase 1 exposed manual intent through `spec.control.desiredState`.
Phase 2 also needs a suspend-like field in `spec` so RTJ can participate in Kueue external integration through `jobframework`.

That creates a semantic tension:

- the product already has a manual hold surface
- Kueue needs a separate admission gate

Phase 2 must resolve that tension without breaking backward compatibility unnecessarily.

## Decision

### 1. RTJ Gets A Dedicated Kueue-Facing Suspend Field

RTJ now includes:

- `spec.suspend`

This field is the Kueue-facing admission gate.
It is the field intended for Kueue webhook defaulting and later Kueue reconciler control.

### 2. Manual Intent Stays In `spec.control.desiredState`

RTJ keeps:

- `spec.control.desiredState`

This remains the user-facing manual compatibility surface from Phase 1.

Its meaning in Phase 2 is narrowed and clarified:

- `Running` means the user wants the RTJ to run when Kueue admits it
- `Paused` means the user wants a sticky manual hold after graceful drain completes

### 3. The Fields Are Related But Not The Same

The fields must not be treated as aliases.

They answer different questions:

- `spec.suspend`: "Is Kueue currently holding this RTJ from an admission perspective?"
- `spec.control.desiredState`: "What does the user want the RTJ to do when admission allows progress?"

That means these combinations are valid:

- `desiredState=Running`, `suspend=true`
  This is the normal queued state for a Kueue-managed RTJ.
- `desiredState=Running`, `suspend=false`
  This is the admitted or runnable state.
- `desiredState=Paused`, `suspend=true`
  This is a manual hold that is also safe from a Kueue admission perspective.

### 4. Backward Compatibility Is Preserved

Phase 1 clients that patch only `spec.control.desiredState` are still valid in Phase 2.

They do not directly control `spec.suspend`.
Instead, the controller will later interpret them this way:

- patching to `Paused` requests graceful yield and a sticky post-drain hold
- patching to `Running` removes the manual hold, but the RTJ still waits for Kueue admission

So the compatibility layer is preserved, but the ownership model is corrected.

### 5. Kueue Label Projection Is Derived From Spec

The queue and workload-priority fields remain spec-backed API:

- `spec.queueName`
- `spec.workloadPriorityClassName`

The webhook projects them onto RTJ metadata labels only because the pinned Kueue helper methods validate those invariants from labels.
Those labels are derived transport for Kueue integration, not the primary user API.

### 6. Child JobSet Remains Outside These Semantics

The child `JobSet` must not carry Kueue queue or workload-priority admission identity in Phase 2.
These suspend and queue semantics belong to RTJ only.

## Consequences

Positive consequences:

- Phase 2 gets a clean Kueue-facing suspend field without deleting the Phase 1 manual API.
- Existing manual clients can continue to work with clearer semantics.
- Kueue queue and update invariants can be enforced through the webhook layer now.

Negative consequences:

- There are now two related fields that users and implementers must not confuse.
- The controller must later reconcile manual hold and Kueue hold into one runtime lifecycle.

## Rejected Alternatives

### Reuse `spec.control.desiredState` As The Kueue Suspend Field

Rejected because `desiredState` carries user intent, not Kueue-owned admission state.
Treating it as the Kueue field would blur ownership and make queued `Running` objects awkward.

### Replace `spec.control.desiredState` Entirely

Rejected for Phase 2 because it would break the existing Phase 1 manual control surface without enough implementation benefit.
