# Phase 5 Dev Environment

## Overview

The Phase 5 dev environment provides a deterministic local profile for
checkpoint-aware preemption behaviour. It targets **within-ClusterQueue
LowerPriority preemption** driven by the operator's effective priority
shaping. Cohort borrowing/reclaim and Fair Sharing are intentionally
excluded from the core profile.

## Quick Start

```bash
# Stand up the cluster and install the Phase 5 profile.
make phase5-up

# Load your operator + trainer images.
make phase5-load-images IMAGES="controller:latest trainer:latest"

# Validate the profile is installed correctly.
make phase5-smoke

# Check cluster state at any time.
make phase5-status

# Tear down.
make phase5-down
```

## What Gets Installed

| Resource | Name | Purpose |
|---|---|---|
| WorkloadPriorityClass | `phase5-low` (value 100) | Low base priority for preemptible RTJs |
| WorkloadPriorityClass | `phase5-high` (value 10000) | High base priority for preempting RTJs |
| CheckpointPriorityPolicy | `dev-checkpoint-priority` | Short-window policy for e2e observability |
| ClusterQueue | `phase5-cq` | 500m CPU / 512Mi — one RTJ blocks another |
| LocalQueue | `phase5-training` | Namespace-scoped entry point to phase5-cq |
| ResourceFlavor | `default-flavor` | Shared with Phase 1/2 (no node affinity) |

## ClusterQueue Preemption Policy

```yaml
preemption:
  withinClusterQueue: LowerPriority
  reclaimWithinCohort: Never
  borrowWithinCohort:
    maxPriorityThreshold: 0
```

- **withinClusterQueue: LowerPriority** — when a pending workload has
  strictly higher `Workload.Spec.Priority` than an admitted workload in
  the same ClusterQueue, Kueue preempts the lower-priority workload.

- **reclaimWithinCohort: Never** — no cross-ClusterQueue reclaim. There
  is only one ClusterQueue in the Phase 5 profile.

- **borrowWithinCohort disabled** — the queue is not part of a cohort,
  so borrowing is irrelevant. The `maxPriorityThreshold: 0` further
  ensures no borrowing occurs even if a cohort is added later.

## Quota Design

The ClusterQueue quota is **500m CPU / 512Mi memory**. Each sample RTJ
requests **2 replicas x 250m CPU / 256Mi** = 500m CPU / 512Mi total.
This means:

- A single RTJ fills the entire queue.
- A second RTJ cannot be admitted until the first yields or is preempted.
- Priority preemption is deterministic: submit low-priority first, then
  high-priority → Kueue preempts the low-priority workload.

## CheckpointPriorityPolicy Timing

The `dev-checkpoint-priority` policy uses intentionally short windows:

| Field | Value | Rationale |
|---|---|---|
| `startupProtectionWindow` | 30s | Short enough to observe expiry in demos |
| `checkpointFreshnessTarget` | 60s | Staleness triggers within ~1 minute |
| `minRuntimeBetweenYields` | 20s | Prevents rapid yield thrashing |
| `protectedBoost` | +50 | Small positive boost during protection |
| `cooldownBoost` | +25 | Smaller boost during post-resume cooldown |
| `preemptibleOffset` | -500 | Large penalty drops low-priority below zero |
| `maxYieldsPerWindow` | 3 | Budget of 3 yields per 5-minute window |
| `yieldWindow` | 5m | Sliding window for yield counting |

### Effective Priority Examples

For a `phase5-low` RTJ (base 100):

| State | Formula | Effective Priority |
|---|---|---|
| StartupProtected | 100 + 50 | **150** |
| Active | 100 + 0 | **100** |
| Cooldown | 100 + 25 | **125** |
| Preemptible (stale checkpoint) | 100 + (-500) | **-400** |

For a `phase5-high` RTJ (base 10000):

| State | Formula | Effective Priority |
|---|---|---|
| StartupProtected | 10000 + 50 | **10050** |
| Active | 10000 + 0 | **10000** |
| Preemptible (stale checkpoint) | 10000 + (-500) | **9500** |

Even in the worst case (high-priority RTJ is Preemptible at 9500), it
still outranks a protected low-priority RTJ (150). Cross-tier preemption
ordering is preserved.

## Trainer Environment Variables

The sample RTJ manifests thread deterministic timing env vars into the
trainer container:

| Env Var | Value | Effect |
|---|---|---|
| `SLEEP_PER_STEP` | `5` | 5 seconds per training step |
| `CHECKPOINT_EVERY` | `3` | Checkpoint every 3 steps (~15s) |
| `TOTAL_STEPS` | `100` | Run for ~500s (long enough for lifecycle) |

With these settings, the first checkpoint arrives at ~15s (within the 30s
protection window). Subsequent checkpoints arrive every ~15s. If checkpoint
publication is delayed >60s, the RTJ transitions to Preemptible.

## Why Cohort Borrowing/Reclaim and Fair Sharing Are Excluded

Phase 5 targets **within-ClusterQueue** preemption only. The exclusions
are deliberate:

1. **Cohort borrowing/reclaim** is a cross-ClusterQueue concern. It
   requires multiple ClusterQueues in a Cohort, with lending limits and
   reclaim policies. This adds configuration complexity without testing
   the core checkpoint-aware priority shaping logic.

2. **Fair Sharing** (`preemption.fairSharing.weight`) interacts with
   Kueue's DRF (Dominant Resource Fairness) algorithm across tenants.
   It is orthogonal to checkpoint-based priority adjustment and would
   require multi-tenant queue configurations to test meaningfully.

3. **Single ClusterQueue is sufficient** for validating the Phase 5
   goals (G1-G4). The operator's effective priority shaping targets
   `Workload.Spec.Priority`, which Kueue reads for within-ClusterQueue
   LowerPriority preemption decisions. No cross-queue interaction is
   needed.

4. **Incremental scope.** Cohort and Fair Sharing interactions can be
   added in a future phase once the core priority shaping is validated.
   The architecture supports this extension without changes to the
   operator's priority evaluation logic.

## Local Assumptions

- **Kind cluster** with the default configuration (1 control-plane,
  1 worker). Phase 5 does not require multi-worker topology.
- **Kueue v0.15.1** installed via the base dev stack.
- **JobSet v0.10.1** installed via the base dev stack.
- **MinIO** for S3-compatible checkpoint storage (installed by dev-up).
- **CPU-only** workloads (no GPU requirement).
- **gloo** PyTorch distributed backend (CPU-compatible).

## Preserved Profiles

The Phase 5 profile is additive. Earlier phase profiles are not modified:

- Phase 1/2: `checkpoint-dev-cq` + `training` LocalQueue remain.
- Phase 3: `phase3-cq` + `phase3-training` remain (if installed).
- Phase 4: `phase4-cq` + `phase4-training` remain (if installed).

Phase 5 adds `phase5-cq` + `phase5-training` alongside existing queues.

## File Layout

```
deploy/dev/phase5/
  priorities/
    00-phase5-low.yaml          WorkloadPriorityClass (value 100)
    01-phase5-high.yaml         WorkloadPriorityClass (value 10000)
  queues/
    10-cluster-queue.yaml       ClusterQueue (phase5-cq)
    20-local-queue.yaml         LocalQueue (phase5-training)
  policies/
    00-dev-checkpoint-priority-policy.yaml   CheckpointPriorityPolicy
  samples/
    rtj-low-priority.yaml       Low-priority RTJ with priority shaping
    rtj-high-priority.yaml      High-priority RTJ with priority shaping

hack/dev/
  install-phase5-profile.sh     Full profile installer (CRDs + resources + Kueue config)
  phase5-profile.sh             Lightweight profile re-applicator
  phase5-smoke.sh               Infrastructure smoke test
```

## Makefile Targets

| Target | Description |
|---|---|
| `make phase5-up` | Create cluster + install Phase 5 profile |
| `make phase5-down` | Delete the kind cluster |
| `make phase5-status` | Show Phase 5 resources |
| `make phase5-load-images` | Load Docker images into kind |
| `make phase5-smoke` | Validate Phase 5 infrastructure |
| `make phase5-profile` | Re-apply Phase 5 profile on existing cluster |
