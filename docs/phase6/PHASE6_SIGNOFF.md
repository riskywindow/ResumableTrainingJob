# Phase 6 Signoff

**Date:** 2026-03-30
**Phase:** Multi-Cluster Checkpoint-Native Spillover
**Status:** Signed off

---

## What Phase 6 Can Do

Phase 6 enables checkpoint-native training jobs to spill execution across multiple Kubernetes clusters using Kueue's MultiKueue framework.

### Core Capabilities (Shipped)

1. **Multi-cluster dispatch via MultiKueue.** An RTJ with `spec.managedBy: kueue.x-k8s.io/multikueue` is dispatched by Kueue's generic external-framework adapter to a remote worker cluster. No custom MultiKueueAdapter is required.

2. **Manager/worker operator split.** The `--mode=manager` flag runs the operator in control-plane-only mode: it suppresses local child JobSet creation for MultiKueue-managed RTJs and delegates runtime to a remote worker. `--mode=worker` (the default) runs the full Phase 5 runtime path, unchanged.

3. **Shared-checkpoint remote pause/resume.** A user patches `spec.control.desiredState=Paused` on the manager-side RTJ. The adapter detects the spec drift, tears down the active remote copy, and creates a new one with the paused spec. The manager preserves the last-known checkpoint summary across the teardown. On resume, the adapter creates a new Running remote, and the worker resumes from the shared S3-compatible checkpoint store.

4. **Manager-visible remote status.** The manager-side RTJ's `status.multiCluster` section reports:
   - `dispatchPhase`: Pending, Dispatched, or Active
   - `executionCluster`: resolved from the Kueue Workload's admission check
   - `remotePhase`: mirrors the worker's `status.phase`
   - `remoteCheckpoint`: mirrors the worker's latest completed checkpoint (ID, time, storageURI)
   - `localExecutionSuppressed`: always true for manager-mode MultiKueue-managed RTJs

5. **Single-cluster path preserved.** RTJs without `spec.managedBy` follow the unchanged Phase 5 path on any operator mode. Manager-mode operators also run the Phase 5 path for non-MultiKueue RTJs (data-loss protection). Worker-mode operators ignore `spec.managedBy` entirely.

6. **Three-cluster dev environment.** Kind-based local environment with a manager cluster (control-plane only), two worker clusters (control-plane + nodes), and a shared MinIO instance on worker-1. Fully scripted setup, teardown, and inspection.

7. **Observability.** Nine Phase 6-specific Prometheus metrics registered: execution role tracking, remote cluster tracking, manager suppressions, remote status sync success/failure, remote pause/resume events, remote checkpoint observations, shared store access failures.

8. **Operational tooling.** Six hack/dev scripts for submit, pause, resume, and inspect operations. Three documentation files (demo, operations, troubleshooting) with copy-paste command sequences.

---

## What Remains Experimental

| Item | Reason | Gate |
|------|--------|------|
| Partial admission (`spec.parallelism.enablePartialAdmission`) | Phase 3 experimental flag, not Phase 6-specific | `--enable-experimental-partial-admission` operator flag |
| Multi-cluster partial admission | Not tested in multi-cluster context | Would require shared-store resharding validation |

No Phase 6-specific features are behind experimental gates. All Phase 6 capabilities are on by default when `spec.managedBy` is set.

---

## What Remains Deferred

| Item | Rationale | Suggested Phase |
|------|-----------|-----------------|
| Active emission of Phase 6 Prometheus metrics from controller hot paths | Metrics infrastructure is complete; wiring recorder calls is mechanical (~20 lines) | Phase 7 |
| Cross-cluster preemption (preempt on cluster A, resume on cluster B) | Requires cross-cluster priority comparison; out of Phase 6 scope | Phase 8+ |
| Live migration between worker clusters | Explicitly excluded in Phase 6 design lock | Not planned |
| Automatic shared-store provisioning | Shared S3 store must be pre-configured by the operator | Phase 8+ |
| Worker-to-manager status push (bypassing adapter polling) | Generic adapter's polling is sufficient for current scale | Phase 8+ |
| Multi-cluster partial admission validation | Requires e2e testing with resharding across clusters | Phase 7 |
| Manager-side aggregated checkpoint catalog | Manager currently mirrors only the latest checkpoint, not the full catalog | Phase 7 |

---

## Known Risks

### R1: Adapter Delete-Recreate Pause Latency
**Risk:** Remote pause uses adapter spec-drift detection, which triggers a delete+recreate cycle. This is slower than a direct graceful-yield signal.
**Mitigation:** The adapter polls on its configured interval (default: 10-15s). Total pause latency is adapter-poll-interval + worker-drain-time. Acceptable for training workloads where checkpoints occur every 30-300 seconds.
**Severity:** Low.

### R2: Shared Checkpoint Store as Single Point of Failure
**Risk:** All worker clusters must reach the same S3-compatible checkpoint store. If the store is unavailable, no worker can write checkpoints and no cross-cluster resume is possible.
**Mitigation:** S3-compatible stores (AWS S3, MinIO, GCS with S3 API) are designed for high availability. Operators should configure store redundancy independently of the RTJ controller.
**Severity:** Medium (operational, not architectural).

### R3: hasRemoteStatusSignal Heuristic
**Risk:** The manager uses `activeJobSetName != "" || currentRunAttempt > 0` to detect adapter-mirrored status. If a future phase introduces manager-side run attempts, the heuristic breaks.
**Mitigation:** The heuristic is documented in code. Phase 7 should not introduce manager-side run attempts without updating this function.
**Severity:** Low (design debt, not a bug).

### R4: Kueue Version Coupling
**Risk:** Phase 6 depends on Kueue v0.15.1+ with `MultiKueue` and `MultiKueueAdaptersForCustomJobs` feature gates (both Beta, default-on). A Kueue upgrade that changes these gates could break the integration.
**Mitigation:** Feature gate requirements are validated at startup via `internal/multikueue/config.go`. The operator logs a clear error and refuses to start if gates are missing. Pin Kueue dependency version in go.mod.
**Severity:** Low.

### R5: Namespace Symmetry Requirement
**Risk:** The generic adapter creates the remote RTJ with the same namespace as the manager-side RTJ. If the worker cluster does not have that namespace (or has a different LocalQueue configuration), the remote RTJ fails to schedule.
**Mitigation:** `troubleshooting.md` scenario 4 documents this failure mode with explicit diagnostic steps. Operators must ensure namespace and LocalQueue symmetry across clusters.
**Severity:** Low (operational).

---

## What Phase 7 Should Build Next

### Priority 1: Active Metrics Emission
Wire the nine Phase 6 Prometheus recorder methods into `reconcileManagerIntent` hot paths. This is a ~20-line mechanical change that completes the observability story.

### Priority 2: Cross-Cluster Preemption Awareness
Phase 6 enables multi-cluster execution but does not coordinate preemption across clusters. Phase 7 should explore:
- Manager-side preemption signals (e.g., manager requests yield on worker-A to free capacity for a higher-priority RTJ on worker-B).
- Integration with Kueue's MultiKueue preemption strategy (if/when Kueue supports it).

### Priority 3: Manager-Side Checkpoint Catalog
The manager currently mirrors only the latest completed checkpoint from the worker. For advanced workflows (rollback, checkpoint comparison, audit), the manager should maintain a lightweight catalog of all checkpoints observed across workers.

### Priority 4: Multi-Cluster Integration Testing at Scale
Phase 6 e2e tests use a three-cluster kind environment. Phase 7 should add:
- Stress tests with many concurrent RTJs across clusters.
- Failure injection (worker crash, store unavailability, adapter restart).
- Performance benchmarks for status mirroring latency.

### Priority 5: Operational Hardening
- Alerting rules for Phase 6 metrics (e.g., alert on `shared_store_access_failures_total` > 0).
- Grafana dashboard templates for multi-cluster RTJ monitoring.
- Runbook integration with troubleshooting.md scenarios.

---

## Test Evidence Summary

| Category | Count | Key Tests |
|----------|-------|-----------|
| Webhook unit tests (Phase 6) | 10 | `TestWebhookValidateCreateAcceptsManagedByMultiKueue`, `TestWebhookValidateUpdateRejectsManagedByChange`, `TestWebhookValidateCreateAcceptsManagedByWithAllPhaseFeatures` |
| Mode split unit tests | 8 | `TestShouldSuppressRuntimeManagerModeMultiKueueManaged`, `TestShouldSuppressRuntimeWorkerModeMultiKueueManaged` |
| Mode split integration tests | 6 | `TestManagerModeSuppressesRuntimeForMultiKueueManagedRTJ`, `TestManagerModeAllowsNormalPathForNonMultiKueueRTJ`, `TestSingleClusterBehaviorUnchangedWhenMultiKueueNotUsed` |
| Remote status unit tests | 9 | `TestSyncRemoteStatusInitializesMultiCluster`, `TestSyncRemoteStatusDetectsRemotePhase`, `TestSyncRemoteStatusMirrorsCheckpointSummary` |
| Remote status integration tests | 5 | `TestManagerModeReflectsRemoteExecutionCluster`, `TestManagerModeReflectsRemotePhaseAfterAdapterSync` |
| MultiKueue framework unit tests | 10+ | `internal/multikueue/framework_test.go`, `internal/multikueue/config_test.go` |
| Cluster resolver unit tests | 6+ | `internal/remote/cluster_resolver_test.go` |
| E2E remote execution | 1 | `TestMultiClusterRemoteExecution` |
| E2E remote pause/resume | 1 | `TestMultiClusterRemotePauseResume` |
| E2E manager suppression | 1 | `TestMultiClusterManagerSuppression` |

---

## Signoff

Phase 6 is complete and coherent. All five locked goals are implemented, tested, and documented. No design drift was found. The single-cluster Phase 5 path is preserved in all configurations. Known gaps are low-severity and documented with clear recommendations for Phase 7.
