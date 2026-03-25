# Migration From Phase 1

## Control-Plane Ownership Changes

Phase 1 ownership:

- RTJ owned lifecycle and child naming.
- The child `JobSet` was the Kueue-managed admission object.
- Kueue admitted and preempted the child `JobSet`, not RTJ itself.

Phase 2 ownership:

- RTJ becomes the Kueue-managed admission object.
- Kueue creates and manages the RTJ-owned `Workload` through external integration.
- The child `JobSet` becomes a plain runtime carrier created only after RTJ admission.

This is the main migration rule:

- Phase 1 had one control-plane object plus one Kueue-managed runtime object.
- Phase 2 must have one control-plane object that is also the Kueue-managed object.

## Why Queue And Priority Labels Must Leave The Child JobSet

In Phase 1, the controller injected these Kueue labels onto the child `JobSet`:

- `kueue.x-k8s.io/queue-name`
- `kueue.x-k8s.io/priority-class`

That must stop in Phase 2 for three reasons:

1. Queue and priority labels on the child `JobSet` would create a second Kueue-managed workload, which violates the Phase 2 rule that RTJ is the only Kueue-managed admission object.
2. Kueue's external integration invariants are attached to RTJ itself, including the rule that queue identity is stable once admission is in play.
3. Leaving queue identity on both RTJ and JobSet would split ownership of admission, suspend, and preemption across two resources and make control-plane truth ambiguous.

The child `JobSet` may still carry controller bookkeeping labels such as RTJ name and run attempt.
It must not carry Kueue queue identity for admission.

## What Phase 1 Behavior Is Preserved

Phase 2 intentionally keeps several Phase 1 behaviors:

- JobSet remains the runtime resource.
- The trainer keeps using step-boundary yield only.
- Checkpoints still use DCP, S3-compatible storage, and manifest-last publication.
- Resume still selects the latest compatible complete checkpoint under the strict Phase 0 compatibility rules.
- The operator still preserves one active runtime attempt at a time.
- The existing Phase 1 manual pause and resume surface should remain usable if practical.

## What The Manual Path Means In Phase 2

Phase 1 manual pause meant:

- user patched `spec.control.desiredState=Paused`
- controller drained the active child `JobSet`
- controller deleted the child `JobSet`
- RTJ settled in `Paused`

Phase 2 keeps that user-facing behavior if practical, but the control-plane contract changes:

- manual pause must use the same graceful-yield path as Kueue-driven suspend
- manual pause must not bypass Kueue admission rules
- manual resume must only make the RTJ eligible for Kueue admission again

So the preserved behavior is "same user intent, same checkpoint-and-yield semantics," not "same underlying Kueue ownership."

## Migration Summary

- Move Kueue integration from child `JobSet` to parent `RTJ`.
- Stop attaching queue and priority labels to child `JobSet`.
- Teach RTJ to expose a suspend-like admission field for Kueue.
- Keep the child `JobSet` as the runtime-only attempt object.
- Keep manual pause and resume as a compatibility layer on top of the new Kueue-native lifecycle.
