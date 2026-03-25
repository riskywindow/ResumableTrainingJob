# Reference Environment

This document defines the canonical reference environment for the Phase 0 docs and for later Phase 1 benchmark work.
It is the baseline environment against which the `v1` product promise SHOULD be evaluated.

## Purpose

The reference environment exists to prevent benchmark drift and architectural ambiguity.
Phase 1 benchmarks SHOULD target this environment unless a benchmark explicitly documents why it deviates.

## Canonical Environment

### Cluster Boundary

- One Kubernetes cluster only
- One administrative control plane operated by platform admins
- No multi-cluster failover or cross-cluster checkpoint portability in scope

### Control-Plane Components

- Kueue installed and authoritative for queueing, admission, and queue-driven preemption intent
- JobSet installed as the only supported runtime or orchestration primitive
- One `ResumableTrainingJob` operator deployment managing RTJs in the cluster
- No custom scheduler implementation beyond Kueue plus the platform's normal Kubernetes scheduling stack

### Workload Shape

- One RTJ under test at a time for the canonical benchmark path unless a benchmark explicitly studies contention
- PyTorch DDP or PyTorch FSDP only
- Image identity pinned and declared explicitly
- Code version declared explicitly
- Fixed declared world size for the lifetime of a checkpoint and its resume
- Fixed declared GPU shape for the lifetime of a checkpoint and its resume
- Fixed declared optimizer mode for the lifetime of a checkpoint and its resume
- Fixed declared sharding mode for the lifetime of a checkpoint and its resume

### Canonical Benchmark Topology

The default benchmark topology SHOULD be:

- One admitted RTJ
- One active JobSet
- Homogeneous GPU workers
- A fixed world size of `8`
- A fixed GPU shape held constant between initial run and resume

If a later benchmark needs a different world size, it SHOULD justify the change and MUST still preserve the `v1` no-world-size-change rule across checkpoint and resume.

### Runtime Path

- Training Pods run the supported SDK or agent
- User training code exposes safe training step boundaries
- DCP is the only checkpoint and restore path used by the benchmark
- Yield may be triggered either manually or by Kueue-driven preemption intent
- Resume uses only the latest operator-selected compatible checkpoint unless a benchmark explicitly studies checkpoint selection policy

### Storage Path

- One S3-compatible object store
- Checkpoints stored under a stable prefix per RTJ lineage
- Read and write credentials available to in-scope training Pods
- The operator relies on persisted checkpoint metadata plus object availability, not object paths alone

## Benchmark Rules Derived From the Reference Environment

- Benchmarks MUST stay within one cluster.
- Benchmarks MUST use JobSet-backed RTJs only.
- Benchmarks MUST use PyTorch DDP or FSDP plus DCP only.
- Benchmarks MUST keep image identity, declared code version, world size, GPU shape, optimizer mode, and sharding mode unchanged across checkpoint and resume.
- Benchmarks MUST treat incomplete checkpoints as failures, not partial successes.
- Benchmarks SHOULD measure both manual-yield and Kueue-driven-yield scenarios.

## What the Reference Environment Does Not Cover

- Multi-cluster recovery
- Elastic resize on resume
- Heterogeneous GPU reshaping across checkpoint and resume
- Non-PyTorch training stacks
- Transparent process or container snapshot techniques

The reference environment is intentionally narrow because it exists to validate the `v1` contract, not to simulate every future expansion.
