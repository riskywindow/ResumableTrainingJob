# Phase 1 Repo Layout

This document defines the intended repository shape for the Phase 1 vertical slice.
The layout is minimal on purpose.
It separates control-plane code, trainer code, local deployment assets, and smoke tests without adding extra abstraction layers yet.

## Top-Level Directories

| Path | Purpose |
| --- | --- |
| `api/v1alpha1` | Go API types for `ResumableTrainingJob` and generated Kubernetes metadata. |
| `cmd/operator` | Operator entrypoint binary and controller manager wiring. |
| `internal/controller` | RTJ reconciliation logic and status transition helpers. |
| `internal/jobset` | Child `JobSet` rendering and lifecycle helpers. |
| `internal/checkpoints` | Checkpoint selection, validation, and storage metadata helpers. |
| `sdk/python/yield_sdk` | Python runtime support code for pause, checkpoint, upload, and resume. |
| `fixtures/pytorch_ddp_counter` | Toy distributed trainer fixture used for local runs and e2e smoke coverage. |
| `deploy` | Kubernetes manifests, Kustomize overlays, CRDs, RBAC, and local object-storage assets. |
| `hack/dev` | `kind` bootstrap scripts, local setup helpers, and developer workflows. |
| `test/e2e` | End-to-end smoke tests and helper assets. |

## Intended Responsibilities

### Operator Side

- `api/v1alpha1` should stay small and carry only the fields needed for the accepted Phase 0 contract and the Phase 1 slice.
- `internal/controller` should own RTJ reconciliation, phase transitions, and status publication.
- `internal/jobset` should own rendering of the Kueue-managed child `JobSet`.
- `internal/checkpoints` should own manifest parsing, latest-compatible-complete selection, and object-store compatibility checks.

### Runtime Side

- `sdk/python/yield_sdk` should contain the thin DCP integration and control-loop helpers shared by the fixture.
- `fixtures/pytorch_ddp_counter` should remain a tiny, legible training workload designed for repeatable checkpoint and resume behavior on CPU.

### Local Ops Side

- `deploy` should contain only assets needed to stand up the slice locally and in CI.
- `hack/dev` should keep one-command setup and teardown workflows for `kind`, Kueue, JobSet, and object storage.
- `test/e2e` should focus on a small number of high-value smoke paths, starting with the manual pause and resume happy path.

## Directory Rules

- Do not put product policy inside generated manifests or the child `JobSet`.
- Do not duplicate checkpoint compatibility logic between the operator and the Python SDK.
- Keep fixtures disposable and deterministic.
- Keep the default test and developer path CPU-first.
