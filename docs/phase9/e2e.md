# Phase 9 — E2E Test Coverage

## Overview

Phase 9 e2e tests validate the three core resize paths on a single-cluster
kind environment using the Phase 9 dev profile. All tests use deterministic
fixture knobs and explicit `kubectl patch` triggers — no long sleeps or
polling-based flakiness.

## Prerequisites

```bash
make phase9-up           # kind cluster + base stack + Phase 9 profile
make phase9-smoke        # verify infrastructure
make phase9-load-images IMAGES=<trainer-image>
```

## Running

```bash
make e2e-phase9 PHASE9_TRAINER_IMAGE=<trainer-image>
```

Or individually:

```bash
RUN_KIND_E2E=1 PHASE9_TRAINER_IMAGE=<image> \
  go test ./test/e2e -run TestElasticShrinkDynamicReclaim -v -timeout 20m

RUN_KIND_E2E=1 PHASE9_TRAINER_IMAGE=<image> \
  go test ./test/e2e -run TestElasticGrowViaRelaunch -v -timeout 20m

RUN_KIND_E2E=1 PHASE9_TRAINER_IMAGE=<image> \
  go test ./test/e2e -run TestElasticFallbackShrinkViaRelaunch -v -timeout 20m
```

## Test Descriptions

### TestElasticShrinkDynamicReclaim

**File:** `test/e2e/elastic_shrink_reclaim_test.go`

**What it proves:**

1. **In-place shrink via reclaimablePods**: When the runtime reports
   `inPlaceShrinkSupported=true` and the policy is `IfSupported`, the
   controller publishes `reclaimablePods` to the Workload status via SSA
   rather than tearing down the run.

2. **Dynamic quota reclaim**: Kueue reads the `reclaimablePods` field and
   releases the corresponding quota fraction, allowing a previously-queued
   RTJ B to be admitted without RTJ A being fully evicted.

3. **RTJ stays Running during in-place shrink**: The RTJ does not transition
   through Paused or any drain phase — it remains Running while the quota is
   released.

4. **Phase 2 invariant preserved**: Child JobSets are plain runtime objects
   with no Kueue management labels.

**Mechanism:**

| Step | Action | Assertion |
|------|--------|-----------|
| 1 | Submit RTJ A (4 workers, 1000m CPU) | Reaches Running |
| 2 | Submit RTJ B (2 workers, 500m CPU) | Stays Queued (1250m total quota) |
| 3 | Patch RTJ A status: `inPlaceShrinkSupported=true` | Fixture knob set |
| 4 | Patch RTJ A spec: `targetWorkerCount=2` | Triggers shrink evaluation |
| 5 | Wait for Workload reclaimablePods | `{name: workers, count: 2}` |
| 6 | Check RTJ A status | `reclaimablePodsPublished=true`, `resizePath=InPlace` |
| 7 | Check RTJ A phase | Still `Running` (not evicted) |
| 8 | Wait for RTJ B | Progresses past `Queued` (quota freed) |

**Fixture knobs used:**

- `status.elasticity.inPlaceShrinkSupported=true` — patched via status
  subresource to simulate runtime reporting in-place support.
- `YIELD_SDK_SUPPORTS_IN_PLACE_SHRINK=true` — trainer env var matching the
  status fixture.

---

### TestElasticGrowViaRelaunch

**File:** `test/e2e/elastic_grow_relaunch_test.go`

**What it proves:**

1. **Grow always uses checkpoint-and-relaunch**: Regardless of in-place shrink
   support, scale-up requires a new Workload admission at the larger size.
   This exercises Invariant I-10.

2. **Checkpoint progression is monotonic**: The `currentRunAttempt` increments,
   a new child JobSet is created with the target replica count, and the
   `lastCompletedCheckpoint` is populated from the resize drain.

3. **Full lifecycle**: Running(2) → patch → ResizeCheckpointing → drain →
   Paused → re-queue → re-admit(4) → Running(4) → resizeState=Completed.

4. **New child JobSet at correct size**: The relaunched child JobSet has
   exactly the target worker count as replicas.

**Mechanism:**

| Step | Action | Assertion |
|------|--------|-----------|
| 1 | Submit RTJ (2 workers) | Reaches Running, runAttempt=1 |
| 2 | Patch spec: `targetWorkerCount=4` | Triggers grow |
| 3 | Wait for resize state | `InProgress` or `Pending` |
| 4 | Check conditions | `ResizeCheckpointing` or `RelaunchingForResize` |
| 5 | Wait for relaunch | Running with `runAttempt > 1` |
| 6 | Check child JobSet | 4 worker replicas |
| 7 | Check resize state | `Completed` or `Idle` |

---

### TestElasticFallbackShrinkViaRelaunch

**File:** `test/e2e/elastic_fallback_test.go`

**What it proves:**

1. **Coherent fallback**: When `inPlaceShrinkSupported=false` (default DDP),
   patching to a smaller target triggers checkpoint-and-relaunch, NOT an
   in-place shrink pretending to succeed.

2. **No reclaimablePods published**: The Workload status has NO
   `reclaimablePods` entries during the fallback shrink — quota is freed by
   the full suspend/re-admit cycle instead.

3. **Correct conditions**: `ResizeCheckpointing` or `RelaunchingForResize` is
   set, while `ShrinkingInPlace` and `ShrinkReclaimPublished` are NOT set.

4. **resizePath=CheckpointAndRelaunch**: The controller explicitly marks the
   fallback path, providing clear observability.

**Mechanism:**

| Step | Action | Assertion |
|------|--------|-----------|
| 1 | Submit RTJ (4 workers, `SUPPORTS_IN_PLACE_SHRINK=false`) | Reaches Running |
| 2 | Verify `inPlaceShrinkSupported=false` | Default DDP behavior |
| 3 | Patch spec: `targetWorkerCount=2` | Triggers shrink |
| 4 | Check resize path | `CheckpointAndRelaunch` (not `InPlace`) |
| 5 | Check conditions | `ResizeCheckpointing`, NOT `ShrinkingInPlace` |
| 6 | Check Workload | No `reclaimablePods` |
| 7 | Wait for relaunch | Running with `runAttempt > 1`, 2 replicas |

---

## What Remains Deferred

| Item | Reason |
|------|--------|
| **Reclaim completion detection** | Controller does not yet detect surplus pod termination to clear `reclaimablePods`. The in-place shrink test verifies the publish step only. |
| **Multi-cluster resize** | Phase 6 MultiKueue `reclaimablePods` mirroring is deferred to stretch. |
| **DRA + elasticity coexistence** | Phase 8 DRA device claims during resize are not tested in Phase 9. |
| **Resize target changes during in-progress** | Behavior when user changes `targetWorkerCount` mid-resize is not yet tested. |
| **Elapsed time tracking** | Resize duration metrics and `lastResizeCompletedTime` validation are not in scope. |
| **Resize bounds rejection (webhook)** | Webhook validation (`target < minCount`, `target > preferredCount`) is covered by unit tests in `api/v1alpha1/`, not e2e. |
| **Non-elastic backward compatibility** | Implicit from all Phase 1-8 tests continuing to pass. Not re-tested in Phase 9 e2e. |

## File Inventory

| File | Purpose |
|------|---------|
| `test/e2e/phase9_helpers_test.go` | `phase9RTJView`, `phase9WorkloadView`, `phase9Env`, `setupPhase9Env()`, wait/get/patch helpers |
| `test/e2e/elastic_shrink_reclaim_test.go` | `TestElasticShrinkDynamicReclaim` |
| `test/e2e/elastic_grow_relaunch_test.go` | `TestElasticGrowViaRelaunch` |
| `test/e2e/elastic_fallback_test.go` | `TestElasticFallbackShrinkViaRelaunch` |
| `test/e2e/testdata/phase9/rtj-elastic-shrink-4w.yaml` | 4-worker RTJ for shrink/reclaim test |
| `test/e2e/testdata/phase9/rtj-elastic-queued-2w.yaml` | 2-worker RTJ (queued, then admitted after reclaim) |
| `test/e2e/testdata/phase9/rtj-elastic-grow-2w.yaml` | 2-worker RTJ for grow test |
| `test/e2e/testdata/phase9/rtj-elastic-fallback-4w.yaml` | 4-worker RTJ for fallback test (DDP, no in-place) |
