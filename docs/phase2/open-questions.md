# Phase 2 Open Questions

These are the remaining implementation-shaped questions after locking the core Phase 2 design.
They do not reopen the main ownership decision.

## Suspend Field Shape

Phase 2 requires a suspend-like field in `RTJ.spec` for Kueue external integration.
The design is locked that such a field must exist.
The remaining implementation question is the exact API shape:

- `spec.suspend`
- `spec.runPolicy.suspend`
- another clearly Kueue-facing field in `spec`

## Manual-Pause Compatibility Surface

Phase 2 keeps `spec.control.desiredState` if practical.
The remaining question is whether the compatibility path should stay as:

- the long-term user API
- a deprecated compatibility field
- a bridge to a later dedicated subresource

## Admission Data Projection

The design is locked that the RTJ controller must render the child `JobSet` from RTJ plus admitted pod-set information.
The remaining question is where the implementation should store or cache that admitted projection most cleanly:

- only in the Kueue-managed `Workload`
- partly in RTJ status for visibility
- both

## Preventing Accidental Child JobSet Management

The design is locked that the child `JobSet` must stay outside Kueue admission.
The remaining question is the strongest implementation guardrail for clusters that enable default queueing or unlabeled-job management:

- namespace policy
- explicit labels or annotations to opt the child out
- both
