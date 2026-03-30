# Phase 5 Demo: Checkpoint-Aware Priority Shaping

This document provides the exact command sequence for demonstrating
checkpoint-aware priority shaping with within-ClusterQueue preemption.

## Prerequisites

1. Kind cluster with Phase 5 profile installed:

   ```bash
   make phase5-up
   ```

2. Operator image built and loaded:

   ```bash
   make phase5-load-images IMAGES=controller:latest
   ```

3. Operator running (in a separate terminal):

   ```bash
   go run ./cmd/operator/ --metrics-bind-address=:8080
   ```

4. Trainer image built and loaded into kind:

   ```bash
   export PHASE5_TRAINER_IMAGE=<your-trainer-image>
   ```

5. Verify infrastructure:

   ```bash
   make phase5-smoke
   make phase5-status
   ```

## Demo Scenario

The demo exercises the full Phase 5 lifecycle:

1. A protected low-priority workload blocks a higher-base-priority aggressor.
2. Checkpoint freshness degrades and the protection/cooldown window expires.
3. Effective priority drops below the aggressor's priority.
4. Kueue preempts the victim (low-priority RTJ).
5. The victim later resumes from its checkpoint.

### Dev profile timing

The `dev-checkpoint-priority` policy uses short windows for fast iteration:

| Parameter | Value | Purpose |
|-----------|-------|---------|
| `startupProtectionWindow` | 30s | Shield new/resumed jobs for 30s |
| `checkpointFreshnessTarget` | 60s | Checkpoint older than 60s → stale |
| `minRuntimeBetweenYields` | 20s | Minimum runtime between yields |
| `protectedBoost` | +50 | Boost during protection window |
| `cooldownBoost` | +25 | Boost during post-resume cooldown |
| `preemptibleOffset` | -500 | Penalty when checkpoint is stale |

Trainer settings (SLEEP_PER_STEP=5, CHECKPOINT_EVERY=3) produce checkpoints
every ~15s. The full lifecycle is observable in ~3-5 minutes.

---

## Step 1: Submit the low-priority victim RTJ

```bash
make phase5-submit-low PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE
```

Verify it starts and enters the Protected state:

```bash
make phase5-inspect-priority PHASE5_LOW_RTJ_NAME=phase5-low-demo
```

Expected output:

```
basePriority: 100
effectivePriority: 150       # 100 + 50 (protectedBoost)
preemptionState: Protected
preemptionStateReason: WithinProtectionWindow
```

## Step 2: Observe protection window

The RTJ is protected for 30s after starting. During this time, its effective
priority is 150 (base 100 + protectedBoost 50).

Watch the priority evolve:

```bash
watch -n 5 "kubectl -n checkpoint-dev get rtj phase5-low-demo \
  -o jsonpath='{.status.priorityShaping}' | python3 -m json.tool"
```

After ~30s, the protection window expires and the RTJ transitions to Active:

```
basePriority: 100
effectivePriority: 100       # base priority, no adjustment
preemptionState: Active
preemptionStateReason: CheckpointFresh
```

## Step 3: Wait for checkpoint staleness

With CHECKPOINT_EVERY=3 and SLEEP_PER_STEP=5, checkpoints arrive every ~15s.
The checkpoint freshness target is 60s. If a checkpoint is delayed or the
operator misses one, the age exceeds 60s and the RTJ transitions to
Preemptible.

To simulate staleness faster, you can stop the trainer from checkpointing
(e.g., kill the MinIO port-forward) or simply wait ~75s after the last
checkpoint.

When the checkpoint becomes stale:

```bash
make phase5-inspect-priority PHASE5_LOW_RTJ_NAME=phase5-low-demo
```

Expected output:

```
basePriority: 100
effectivePriority: -400      # 100 + (-500) preemptibleOffset
preemptionState: Preemptible
preemptionStateReason: CheckpointStale
checkpointAge: 1m15s         # exceeds 60s target
```

## Step 4: Submit the high-priority aggressor

Now submit the high-priority RTJ. Kueue will see:
- Pending workload with priority 10000 (phase5-high base)
- Admitted workload with effective priority -400 (phase5-low, stale)

```bash
make phase5-submit-high PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE
```

## Step 5: Observe Kueue preemption

Kueue's `withinClusterQueue: LowerPriority` policy will preempt the
low-priority RTJ because -400 < 10000.

Watch the preemption:

```bash
make phase5-inspect-workload PHASE5_RTJ_NAME=phase5-low-demo
make phase5-inspect-workload PHASE5_RTJ_NAME=phase5-high-demo
```

The low-priority RTJ will transition through:
1. `YieldRequested` — Kueue signals preemption via `spec.suspend=true`
2. `Draining` — operator initiates graceful checkpoint drain
3. `Paused` — trainer checkpoints and stops
4. `Queued` — RTJ re-enters the queue for later admission

The high-priority RTJ will be admitted and start running.

## Step 6: Observe the victim resumes

Once the high-priority RTJ finishes or is deleted, capacity becomes available
and Kueue re-admits the low-priority RTJ.

```bash
# Delete the high-priority RTJ to free capacity
kubectl -n checkpoint-dev delete rtj phase5-high-demo

# Watch the low-priority RTJ resume
watch -n 5 "make phase5-inspect-priority PHASE5_LOW_RTJ_NAME=phase5-low-demo"
```

After resume, the RTJ enters Cooldown state:

```
basePriority: 100
effectivePriority: 125       # 100 + 25 (cooldownBoost)
preemptionState: Cooldown
preemptionStateReason: CooldownAfterResume
```

The cooldown period (20s, `minRuntimeBetweenYields`) protects the RTJ from
immediate re-preemption after resuming.

After the cooldown expires and a fresh checkpoint arrives, the RTJ returns
to Active:

```
basePriority: 100
effectivePriority: 100
preemptionState: Active
preemptionStateReason: CheckpointFresh
```

---

## Quick reference: all commands

```bash
# Setup
make phase5-up
make phase5-load-images IMAGES=controller:latest
make phase5-smoke

# Submit
make phase5-submit-low  PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE
make phase5-submit-high PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE

# Inspect
make phase5-inspect-priority     PHASE5_LOW_RTJ_NAME=phase5-low-demo
make phase5-inspect-policy       PHASE5_RTJ_NAME=phase5-low-demo
make phase5-inspect-workload     PHASE5_RTJ_NAME=phase5-low-demo
make phase5-inspect-checkpoints  PHASE5_RTJ_NAME=phase5-low-demo

# Cluster state
make phase5-status

# E2E tests
make e2e-phase5 PHASE5_TRAINER_IMAGE=$PHASE5_TRAINER_IMAGE

# Cleanup
make phase5-down
```

## Metrics endpoint

While the operator is running, priority shaping metrics are available at:

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_priority
```

Key metrics to watch during the demo:

| Metric | What it shows |
|--------|---------------|
| `rtjs_by_preemption_state{state="Protected"}` | RTJs in protection window |
| `rtjs_by_preemption_state{state="Preemptible"}` | RTJs with stale checkpoints |
| `priority_effective_value{rtj="..."}` | Per-RTJ effective priority |
| `priority_base_value{rtj="..."}` | Per-RTJ base priority |
| `priority_decisions_total{state="...",reason="..."}` | Decision histogram |
| `priority_materialization_updates_total` | Workload priority patches |
| `priority_telemetry_failures_total` | Telemetry collection failures |
