# Open Questions

These are unresolved Phase 0 questions that materially affect the `v1` design.
They are intentionally limited to real issues that still need decisions.

## Kueue Intent Surface

How exactly should `v1` observe Kueue-driven preemption intent?
Phase 0 binds Kueue as the authority, but it does not yet specify the authoritative signal or handoff that the product should consume.

## Yield Signal Delivery

How exactly should the operator communicate a yield request to the in-pod SDK or agent?
Phase 0 now fixes the authority model, but it does not yet choose the concrete signaling mechanism between the control plane and the runtime.

## Manual Yield API Shape

Should manual yield remain a declarative field such as `spec.control.desiredState`, or SHOULD a later transport choose a different surface such as a subresource or out-of-band action API?
Phase 0 now defines the lifecycle and protocol semantics, but it does not yet finalize the user-facing manual control transport.
