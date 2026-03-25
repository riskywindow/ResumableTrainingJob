# ADR 0001: v1 Scope

- Status: Accepted
- Date: 2026-03-19
- Supersedes: `docs/phase0/adrs/0001-v1-scope-and-contract.md`, `docs/phase0/adrs/0002-v1-product-boundaries.md`

## Context

The project needs a `v1` definition that is narrow enough to implement without turning Phase 0 into an open-ended distributed training platform design exercise.
The product only becomes reviewable if the first release has a constrained workload model, a constrained checkpoint model, and explicit non-goals.

## Decision

`v1` is locked to the following scope:

1. `v1` MUST support exactly one Kubernetes cluster.
2. Kueue MUST be the authority for queueing, admission, and queue-driven preemption intent.
3. A `ResumableTrainingJob` operator MUST orchestrate the checkpoint-and-yield lifecycle within the cluster.
4. JobSet MUST be the only supported runtime or orchestration primitive for distributed training workloads.
5. PyTorch DDP and PyTorch FSDP MUST be the only supported distributed execution modes.
6. PyTorch Distributed Checkpoint (DCP) MUST be the only supported checkpoint mechanism and format.
7. S3-compatible object storage MUST be the only supported checkpoint storage target.
8. Graceful yield MUST happen only at training step boundaries.
9. Both manual yield and Kueue-driven yield MUST be in scope.
10. Resume MUST be allowed only when the checkpoint is compatible under ADR 0003.

`v1` explicitly does not include:

- Multi-cluster resume
- Topology-aware placement
- Elastic shrink or grow in place
- Non-PyTorch frameworks
- Transparent CUDA or container snapshots
- Custom scheduler implementation
- Generalized node-failure recovery as a product guarantee
- Dynamic world-size change on resume

## Why the Scope Is Intentionally Narrow

The scope is intentionally narrow for five reasons:

1. It keeps the authority model legible. Kueue already owns queueing and admission, so `v1` MUST not blur that responsibility with a new scheduler.
2. It keeps the workload surface small. JobSet plus PyTorch DDP or FSDP is a manageable matrix for a first release; broadening workload primitives or frameworks would multiply undefined edge cases.
3. It keeps checkpoint semantics concrete. DCP to S3-compatible storage is specific enough to define readiness and resume compatibility without inventing an abstraction layer that `v1` cannot truly support.
4. It keeps resume semantics fail-closed. Same-cluster and same-identity resume is narrow, but it avoids unsafe portability claims.
5. It keeps product claims honest. `v1` is about graceful yield and controlled resume for one training model, not a general suspension or failure-recovery platform.

## Consequences

Positive consequences:

- The product promise is concrete enough to guide implementation.
- The component and authority boundaries can be specified precisely.
- Compatibility rules can reject unsafe resumes instead of guessing.

Negative consequences:

- Some expected expansions are deferred by design.
- Teams outside the Kueue plus JobSet plus PyTorch stack will not be served in `v1`.
- The product may appear conservative, but that is preferable to shipping an ambiguous first release.

## Follow-Up

- ADR 0002 MUST define authority boundaries among the control-plane and runtime components.
- ADR 0003 MUST define the `v1` checkpoint compatibility contract.
- `system-context.md` MUST show the happy path and authority matrix that implementers should preserve.
