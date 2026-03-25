# Phase 2 Index

This is the entry point for the Phase 2 design pack.
It now documents the implemented Phase 2 native Kueue path, the operator runbooks, the live e2e coverage, and the signoff audit.

## Read This First

1. [README.md](README.md)
2. [goals.md](goals.md)
3. [architecture.md](architecture.md)
4. [kueue-external-integration.md](kueue-external-integration.md)
5. [api-and-webhooks.md](api-and-webhooks.md)
6. [adr/0001-native-kueue-integration.md](adr/0001-native-kueue-integration.md)
7. [adr/0002-suspend-semantics.md](adr/0002-suspend-semantics.md)
8. [migration-from-phase1.md](migration-from-phase1.md)
9. [open-questions.md](open-questions.md)
10. [workload-shape.md](workload-shape.md)
11. [preemption-flow.md](preemption-flow.md)
12. [dev-environment.md](dev-environment.md)
13. [demo.md](demo.md)
14. [operations.md](operations.md)
15. [troubleshooting.md](troubleshooting.md)
16. [e2e.md](e2e.md)
17. [review/consistency-audit.md](review/consistency-audit.md)
18. [review/gaps.md](review/gaps.md)
19. [PHASE2_SIGNOFF.md](PHASE2_SIGNOFF.md)
20. [session-handoff.md](session-handoff.md)

## Phase 2 Core Rules

- `RTJ` becomes the only Kueue-managed admission object.
- The child `JobSet` becomes a plain runtime resource and must not become a second Kueue-managed workload.
- Kueue admission, suspension, and preemption intent flow through RTJ via external `jobframework` integration.
- The RTJ controller remains responsible for checkpoint selection, graceful yield coordination, and runtime launch or teardown.
- Resume continues to use only the latest compatible complete checkpoint unless a later ADR expands the policy.

## What Changes From Phase 1

- Phase 1 queued and admitted the child `JobSet`.
- Phase 2 queues and admits the RTJ itself.
- Phase 1 treated Kueue-driven preemption as deferred.
- Phase 2 brings Kueue-driven suspend and preemption into scope.

## What Does Not Change

- JobSet remains the runtime primitive.
- The trainer still yields only at step boundaries.
- Checkpoints still use DCP plus S3-compatible storage and manifest-last publication.
- Resume compatibility remains strict and fail-closed.
- Out-of-scope items from Phase 0 remain out of scope in Phase 2.

## Signoff Artifacts

- [review/consistency-audit.md](review/consistency-audit.md) audits the implementation and docs against the accepted Phase 0 and Phase 1 contracts.
- [review/gaps.md](review/gaps.md) records the remaining known narrowing or visibility gaps after the hardening pass.
- [PHASE2_SIGNOFF.md](PHASE2_SIGNOFF.md) is the concise Phase 2 signoff statement.
