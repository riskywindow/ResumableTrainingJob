# Phase 5 E2E Test Coverage

## Overview

Phase 5 e2e tests validate checkpoint-aware priority shaping and its
interaction with Kueue's within-ClusterQueue preemption. The tests run
against a Kind cluster with the Phase 5 dev profile installed (`make
phase5-up`).

All tests require `RUN_KIND_E2E=1` and a trainer image loaded into the
Kind cluster (any of `PHASE5_TRAINER_IMAGE`, `PHASE4_TRAINER_IMAGE`,
`PHASE3_TRAINER_IMAGE`, `PHASE2_TRAINER_IMAGE`, or
`PAUSE_FLOW_TRAINER_IMAGE`).

---

## Tests

### 1. TestProtectedPriorityBlocksPreemption

**File:** `test/e2e/protected_priority_blocks_preemption_test.go`

**What it proves:**

- A running RTJ with a `CheckpointPriorityPolicy` in the **Protected**
  state (within its startup protection window) resists same-tier
  preemption by a competitor workload at the same base priority.

- The priority shaping engine correctly computes effective priority =
  base + `protectedBoost` and writes it to `Workload.Spec.Priority`.

- Kueue's `LowerPriority` preemption respects the boosted effective
  priority: a competitor at the raw base priority cannot preempt the
  protected job.

- RTJ status surfaces `priorityShaping.preemptionState = "Protected"`,
  `priorityShaping.effectivePriority`, and the `PriorityShaping`
  condition.

- Observability annotations (`effective-priority`, `preemption-state`)
  are set on the RTJ.

- An RTJ without a `priorityPolicyRef` has no `priorityShaping` status
  (Phase 4 backward compatibility).

**Scenario:**

| Step | Action |
|------|--------|
| 1 | Submit RTJ A (phase5-low, base 100) with `dev-checkpoint-priority` policy |
| 2 | Wait for Running + Protected (effective 150) |
| 3 | Verify status, annotations, Workload priority |
| 4 | Submit RTJ B (phase5-low, base 100) without policy |
| 5 | Verify B stays pending for 15s while A is protected |
| 6 | Verify B has no priorityShaping status |

**Policy used:** `dev-checkpoint-priority` (deployed by `make phase5-up`)

**Timing:** ~2-3 minutes.

---

### 2. TestPriorityDropEnablesPreemption

**File:** `test/e2e/priority_drop_enables_preemption_test.go`

**What it proves:**

This is the **primary Phase 5 lifecycle test**. It demonstrates the
complete checkpoint-aware preemption loop:

1. **Startup protection:** A low-priority RTJ starts with boosted
   effective priority.

2. **Priority drop:** After the protection window expires and the
   checkpoint goes stale, the effective priority drops below zero
   (base 100 + preemptibleOffset -500 = -400).

3. **Preemption:** A higher-priority RTJ triggers Kueue preemption of
   the stale-checkpoint victim.

4. **Graceful yield:** The RTJ operator performs the yield protocol:
   checkpoint save, child JobSet teardown, Workload re-queue.

5. **Competitor runs:** The high-priority RTJ starts on the freed quota.

6. **Resume from checkpoint:** After the competitor is deleted, the
   low-priority RTJ resumes from its preemption checkpoint and advances
   training beyond the preempted step.

**Scenario:**

| Step | Action |
|------|--------|
| 1 | Apply `e2e-fast-lifecycle` policy (15s protection, 15s freshness) |
| 2 | Submit RTJ A (phase5-low, base 100, CHECKPOINT_EVERY=8) |
| 3 | Wait for Running, verify Protected state |
| 4 | Wait for Preemptible state (~55s: protection expires + checkpoint stales) |
| 5 | Verify effective priority < base, Workload priority dropped |
| 6 | Submit RTJ B (phase5-high, base 10000) |
| 7 | Wait for Kueue to preempt A (yield request + drain + re-queue) |
| 8 | Verify checkpoint saved, child JobSet deleted |
| 9 | Wait for B Running |
| 10 | Delete B |
| 11 | Wait for A to resume from checkpoint (run attempt >= 2) |
| 12 | Verify training advanced beyond preempted global step |

**Policy used:** `e2e-fast-lifecycle` (applied by the test from
`test/e2e/testdata/phase5/e2e-fast-lifecycle-policy.yaml`)

**Timing:** ~4-6 minutes.

---

### 3. TestYieldBudgetExhaustion

**File:** `test/e2e/priority_drop_enables_preemption_test.go`

**What it proves:**

- The yield budget anti-thrash mechanism tracks yield events.
- After a manual yield and resume, the `recentYieldCount` in
  `status.priorityShaping` reflects the yield history.
- With `maxYieldsPerWindow=1`, a single yield puts the job into a
  Cooldown or Protected state that prevents further priority demotion
  from checkpoint staleness.
- The `yield-history` annotation records yield timestamps.

**Scenario:**

| Step | Action |
|------|--------|
| 1 | Apply `e2e-yield-budget-test` policy (maxYieldsPerWindow=1) |
| 2 | Submit RTJ, wait for Running + Active/Protected |
| 3 | Manual pause (yield) + resume |
| 4 | Verify yield count and priority state reflect yield budget |

**Timing:** ~3-4 minutes.

---

## Test Infrastructure

### Fixtures

| File | Purpose |
|------|---------|
| `test/e2e/testdata/phase5/rtj-with-policy.yaml` | RTJ template with `priorityPolicyRef` |
| `test/e2e/testdata/phase5/rtj-no-policy.yaml` | RTJ template without policy (Phase 4 compat) |
| `test/e2e/testdata/phase5/e2e-fast-lifecycle-policy.yaml` | Fast-cycling policy for lifecycle test |

### Helpers

`test/e2e/phase5_helpers_test.go` provides:

- `phase5RTJView` — extended view struct with `priorityShaping` status
- `phase5WorkloadView` — Workload view with `Spec.Priority`
- `setupPhase5Env()` — environment setup with Phase 5 prerequisites
- `waitForPhase5RTJState()` — predicate-based RTJ polling
- `assertPriorityShapingState()` — state assertion
- `assertEffectivePriorityAbove/Below()` — priority threshold assertions
- `hasPriorityShapingCondition()` — condition inspection

### Prerequisites

```bash
make phase5-up           # Create Kind cluster + install Phase 5 profile
make phase5-load-images  # Load trainer image into Kind
make phase5-smoke        # Verify infrastructure

# Run tests
RUN_KIND_E2E=1 PHASE5_TRAINER_IMAGE=<image> go test ./test/e2e/ \
  -run "TestProtectedPriority|TestPriorityDrop|TestYieldBudget" \
  -v -timeout 20m
```

---

## What Remains Deferred

The following scenarios are **not** covered by Phase 5 e2e tests:

| Scenario | Reason |
|----------|--------|
| Cohort-level preemption (BorrowWithinCohort, Reclaim) | Out of scope for Phase 5 (within-CQ only) |
| Fair Sharing interaction | Deferred beyond Phase 5 |
| Multi-CQ preemption cascades | Out of scope (single CQ profile) |
| Cloud autoscaler interaction | Not modeled in local Kind cluster |
| Negative effective priority with external Kueue consumers | Would require multi-tenant setup |
| Multiple simultaneous preemption candidates | Requires 3+ RTJs; deferred for complexity |
| Operator restart mid-yield | Would require process kill + restart |
| Fail-closed telemetry loss | Covered by unit tests (73 decision engine tests) |
| Graduated checkpoint staleness penalty | `staleCheckpointBoost` field reserved for future use |
| Protection window reset across multiple resume cycles | Partially covered by yield budget test |
| Metrics verification (Prometheus counters) | Deferred to observability phase |

### Unit Test Coverage

The following are thoroughly covered by unit and integration tests
(not e2e):

- Decision engine evaluation order and state transitions: 73 tests in
  `internal/policy/checkpointpriority/`
- Telemetry collection and status sync: 28 tests in
  `internal/controller/`
- Priority state reconciliation and Workload patching: 30+ tests in
  `internal/controller/`
- Kueue GenericJob adapter `PriorityClass()` method: 3 tests in
  `internal/kueue/`
- CheckpointPriorityPolicy webhook validation: 19 tests in
  `api/v1alpha1/`
