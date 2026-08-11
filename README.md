# Resumable Training Job

A Kubernetes operator for checkpoint-aware interruption, resumption, and scheduling of distributed training jobs.

Resumable Training Job (RTJ) coordinates training lifecycle state across a custom resource, Kueue admission, JobSet execution, and an S3-compatible checkpoint store. The implementation focuses on making preemption and resumption explicit: it records checkpoint readiness, gates relaunch on compatible state and resources, and reconstructs runtime objects through controller-owned state transitions.

## Why

Kubernetes can stop and reschedule pods, but distributed training recovery also depends on durable checkpoints, world-size and device compatibility, queue admission, and failure-safe controller behavior. RTJ makes those dependencies part of one reconciled API instead of leaving them to loosely coupled scripts.

## What is implemented

- `ResumableTrainingJob` APIs in `v1alpha1` and `v1beta1`, including conversion webhooks
- Kueue external-framework integration and JobSet workload materialization
- Pause, checkpoint selection, resume-readiness, and startup-recovery controllers
- Priority-aware preemption policy and checkpoint telemetry
- Topology- and flavor-aware launch planning
- MultiKueue manager/worker execution with remote status mirroring
- Dynamic Resource Allocation templates and checkpoint/device compatibility checks
- Elastic shrink/grow planning with in-place and checkpoint/restart paths
- S3-compatible checkpoint catalog plus a Python training-side SDK
- Helm/Kustomize deployment assets, leader election, metrics, alerts, and operational runbooks

## Control flow

```text
ResumableTrainingJob
        |
        v
 Kueue admission ----> topology / device / priority policy
        |
        v
     JobSet ----> training workers ----> checkpoint store
        ^                                  |
        |                                  v
        +------ pause / resume / recovery state
```

The operator remains the source of lifecycle state, Kueue remains the source of admission and quota state, and checkpoint metadata remains the source of restart compatibility.

## Quick start

Run the unit and integration suites:

```bash
go test ./...
```

The local end-to-end profiles require Docker, `kind`, `kubectl`, Kueue, and JobSet:

```bash
make dev-up
make dev-smoke
make dev-down
```

Start with the [Phase 1 development guide](docs/phase1/dev-environment.md) for the minimal local controller path. Later profiles under [`docs/`](docs/) layer in preemption policy, MultiKueue, provisioning gates, DRA, elasticity, and production deployment concerns.

## Repository structure

| Path | Purpose |
| --- | --- |
| `api/` | RTJ and policy APIs, defaulting, validation, and conversion |
| `internal/controller/` | Lifecycle reconciliation and recovery logic |
| `internal/kueue/`, `internal/jobset/` | Admission and execution integration |
| `internal/checkpoints/` | Checkpoint metadata, selection, and compatibility |
| `internal/dra/`, `internal/elastic/` | Device-aware and elastic execution planning |
| `sdk/python/` | Training-side checkpoint/control helpers |
| `test/` | Integration and opt-in `kind` end-to-end suites |
| `charts/`, `deploy/` | Helm and Kustomize deployment assets |

## Current boundaries

- This repository does not publish a ready-to-deploy operator image or a stable release; local and production guides expect you to build and supply an image.
- Full demonstrations require a Kubernetes test cluster and the external controllers named above. Unit tests do not substitute for those cluster-level paths.
- Shared durable checkpoint storage is required for cross-node and multi-cluster recovery.
- The phased design records include production-hardening and disaster-recovery procedures, but operators should validate those procedures against their own Kubernetes, storage, and failure environment.
