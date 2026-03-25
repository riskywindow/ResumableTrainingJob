# PRD v1

## Problem Statement

Teams running distributed PyTorch training on a single Kubernetes cluster need a controlled way to yield scarce GPU capacity without discarding all training progress.
Today, preemption often means abrupt termination, ad hoc scripts, or framework-specific operational knowledge that is not consistently tied to queueing and admission intent.
For `v1`, the product problem is narrower: when Kueue decides a JobSet-backed PyTorch DDP or FSDP training job should yield, or when an operator requests a manual yield, the system MUST support a graceful step-boundary checkpoint-and-yield flow that can later resume only under tightly matching conditions.

## Target Users

- Platform engineers operating a single-cluster GPU training environment with Kueue and JobSet
- ML infrastructure engineers maintaining PyTorch DDP or FSDP training jobs
- On-call operators who need an explicit manual yield path for capacity or maintenance events

## Product Promise

The `checkpoint-native preemption controller` promises a narrow and predictable `v1` behavior:
within one Kubernetes cluster, for JobSet-managed PyTorch DDP or FSDP jobs, the product coordinates graceful yield at training step boundaries using PyTorch DCP to S3-compatible storage, under Kueue authority or manual operator request, so that the workload can later resume only when the declared runtime identity still matches.

The product does not promise transparent process capture, arbitrary resume portability, or generalized recovery from any interruption.

## Goals

- The product MUST integrate with Kueue as the authority for queueing, admission, and preemption intent.
- The product MUST support JobSet as the only workload orchestration primitive in `v1`.
- The product MUST support only PyTorch DDP and FSDP workloads in `v1`.
- The product MUST use PyTorch DCP as the only checkpoint format and mechanism assumed by the product contract.
- The product MUST use S3-compatible object storage as the only checkpoint storage target.
- The product MUST support both Kueue-driven yield and manual yield in `v1`.
- The product MUST allow graceful yield only at training step boundaries.
- The product MUST allow resume only from the same cluster, with the same image identity, the same declared code version, the same declared world size and GPU shape, and the same optimizer mode and sharding mode.
- The product SHOULD make failure boundaries explicit when checkpoint prerequisites or resume prerequisites are not met.

## Non-Goals

- Multi-cluster resume
- Topology-aware placement
- Elastic shrink or grow in place
- Non-PyTorch frameworks
- Transparent CUDA or container snapshots
- Custom scheduler implementation
- Generalized node-failure recovery as a product guarantee
- Dynamic world-size change on resume

## Reference Environment

The reference `v1` environment is intentionally strict:

- One Kubernetes cluster
- Kueue managing queueing, admission, and preemption intent
- JobSet managing the distributed training workload
- PyTorch DDP or FSDP application code that already cooperates with step-boundary checkpointing
- PyTorch DCP writing checkpoint artifacts to S3-compatible object storage
- GPU workers with a declared world size and GPU shape that must match on resume
- A declared optimizer mode and sharding mode that must match on resume

The Phase 0 product definition assumes that training code, images, credentials, and storage configuration needed for DCP already exist and are managed outside this product.

## Canonical User Journey

1. A platform team runs a distributed PyTorch training workload as a JobSet in a Kueue-managed single cluster.
2. The workload is admitted and begins training with application logic that can checkpoint at training step boundaries using DCP.
3. A yield request occurs through one of two in-scope paths:
   a. Kueue signals preemption intent.
   b. An operator triggers a manual yield.
4. The product recognizes that the workload is eligible only for graceful yield, not immediate arbitrary eviction.
5. The training job continues until the next safe training step boundary.
6. At that boundary, the workload writes a DCP checkpoint to the configured S3-compatible object store.
7. The system treats the job as yielded only after the checkpoint satisfies the declared readiness criteria.
8. Later, the workload resumes only if it is scheduled back onto the same cluster with the same image identity, the same declared code version, the same declared world size and GPU shape, and the same optimizer mode and sharding mode.
9. If those resume conditions are not met, the product MUST fail closed rather than silently attempting an incompatible resume.

## Assumptions

- Kueue emits or authorizes a signal that can be treated as preemption intent for `v1`.
- JobSet is sufficient to represent every in-scope distributed training workload for `v1`.
- In-scope applications already expose a reliable step boundary at which a graceful yield can occur.
- PyTorch DCP artifacts written to S3-compatible storage are sufficient for same-cluster resume when all declared identity constraints match.
- Operators are willing to accept narrow resume rules in exchange for a predictable first release.
- The product MAY refuse yield or resume when declared prerequisites are missing, ambiguous, or inconsistent.

## Risks

- Kueue integration semantics may be less precise than the product boundary assumes, especially around how preemption intent is surfaced.
- Step-boundary latency may be too large for some operational events, which could reduce the usefulness of graceful yield.
- DCP completeness and artifact validation may be underspecified, leading to unsafe or disputed readiness decisions.
- The requirement to resume with the same cluster, same image identity, same declared code version, same world size or GPU shape, and same optimizer mode or sharding mode may be operationally hard to enforce.
- Manual yield and Kueue-driven yield may diverge in policy expectations unless their semantics are explicitly aligned.
- Restricting `v1` to PyTorch DDP or FSDP and JobSet may limit early adoption but is necessary to keep the first release coherent.

## Draft Success Metrics

- At least 90% of in-scope yield attempts SHOULD result in a completed DCP checkpoint before the configured graceful-yield deadline in the reference environment.
- At least 95% of resume attempts that satisfy all declared compatibility constraints SHOULD restore successfully in the reference environment.
- Zero supported resume attempts SHOULD proceed when cluster, image identity, declared code version, world size, GPU shape, optimizer mode, or sharding mode compatibility checks fail.
- Platform operators SHOULD be able to explain whether a yield was Kueue-driven or manual from product-visible state or audit data.
- Phase 0 SHOULD exit with no unresolved disagreement on the `v1` scope, non-goals, or reference environment.
