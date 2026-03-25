# ADR 0002: v1 Product Boundaries

- Status: Accepted
- Date: 2026-03-19

## Context

ADR 0001 established that `v1` must stay narrow and reviewable, but it intentionally left several product-shaping choices open.
Phase 0 now has binding decisions for the first release environment and supported workload model.
Those decisions materially constrain scope and MUST be recorded as an accepted ADR.

## Decision

The project adopts the following `v1` product boundaries:

1. `v1` MUST support exactly one Kubernetes cluster.
2. Kueue MUST be the authority for queueing, admission, and preemption intent.
3. JobSet MUST be the only supported runtime or orchestration primitive.
4. `v1` MUST support only PyTorch DDP and PyTorch FSDP workloads.
5. PyTorch DCP MUST be the only supported checkpoint mechanism and checkpoint format.
6. S3-compatible object storage MUST be the only supported checkpoint storage target.
7. Graceful yield MUST occur only at training step boundaries.
8. Resume MUST be supported only when the workload returns to the same cluster, with the same image or code version, and the same declared world size and GPU shape.
9. Both manual yield and Kueue-driven yield MUST be in scope.

The project also adopts these explicit non-goals for `v1`:

- Multi-cluster resume
- Topology-aware placement
- Elastic shrink or grow in place
- Non-PyTorch frameworks
- Transparent CUDA or container snapshots
- Custom scheduler implementation
- Generalized node-failure recovery as a product guarantee
- Dynamic world-size change on resume

## Rationale

These boundaries deliberately favor coherence over breadth.
Restricting `v1` to Kueue, JobSet, PyTorch DDP or FSDP, DCP, and S3-compatible storage avoids turning the first release into a generalized distributed-training platform.
The same-cluster and same-identity resume constraints reduce compatibility ambiguity and make failure cases easier to reason about in Phase 0.

## Consequences

Positive consequences:

- The `v1` promise is specific enough to review and implement.
- Product messaging is aligned with a concrete reference environment.
- Resume behavior can fail closed under clear compatibility rules.

Negative consequences:

- The product excludes many plausible future workloads and operational environments.
- Some organizations will consider the same-cluster restriction too limiting.
- Additional work will be needed later to broaden transport, workload, or resume semantics.

## Change Record

Compared with the earlier generic Phase 0 framing, this ADR narrows `v1` further by binding the supported orchestration stack, framework set, checkpoint mechanism, storage target, and resume constraints.

## Follow-Up

- The PRD MUST reflect these product boundaries verbatim.
- The glossary SHOULD define every bound term used in this ADR.
- Open questions SHOULD focus only on unresolved semantics inside these boundaries, not on reopening the boundaries themselves.
