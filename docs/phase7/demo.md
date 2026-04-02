# Phase 7 Demo: Capacity-Guaranteed Launch

This document provides the exact command sequence for demonstrating
capacity-guaranteed launch, provisioning failure, and waitForPodsReady
timeout recovery.

## Prerequisites

1. Kind cluster with Phase 7 profile installed:

   ```bash
   make phase7-up
   ```

2. Operator image built and loaded:

   ```bash
   make phase7-load-images IMAGES=controller:latest
   ```

3. Operator running (in a separate terminal):

   ```bash
   go run ./cmd/operator/ \
     --metrics-bind-address=:8080 \
     --provisioning-ac-names=dev-provisioning,dev-provisioning-failure,dev-provisioning-expiry
   ```

4. Verify infrastructure:

   ```bash
   make phase7-smoke
   make phase7-status
   ```

### Dev profile timing

The Phase 7 fake provisioner uses these defaults:

| Parameter | Value | Purpose |
|-----------|-------|---------|
| Fake provisioner delay | 10s | Time before `Provisioned=True` |
| Fake failure class | Immediate | Sets `Failed=True` on first reconcile |
| waitForPodsReady timeout | 120s | Kueue eviction threshold |
| waitForPodsReady backoff | 10-300s | Requeue after eviction |
| waitForPodsReady retries | 3 | Maximum requeue attempts |

---

## Scenario 1: Pending Provisioning, Then Successful Launch

This demonstrates the primary Phase 7 flow: RTJ is admitted by Kueue,
but the launch gate holds child JobSet creation until the
ProvisioningRequest succeeds.

### Step 1: Submit the delayed-success RTJ

```bash
make phase7-submit-success PHASE7_RTJ_NAME=phase7-demo
```

### Step 2: Observe provisioning pending (no runtime)

Immediately after submission, check the launch gate. The RTJ should be
admitted but the launch gate should be Blocked:

```bash
make phase7-inspect-launchgate PHASE7_RTJ_NAME=phase7-demo
```

Expected output:

```
launchGateState: Blocked
message: ProvisioningRequest AdmissionCheck not yet Ready

provisioningState: Pending
provisioningRequestRef: <workload-name>-dev-provisioning-1

capacityGuaranteed: false
```

Confirm no child JobSet exists:

```bash
make phase7-inspect-workload PHASE7_RTJ_NAME=phase7-demo
```

Expected: `<no active child JobSet -- launch gate may be blocking>`

### Step 3: Observe the ProvisioningRequest

```bash
make phase7-inspect-provisioningrequest PHASE7_RTJ_NAME=phase7-demo
```

Expected: The ProvisioningRequest exists with `provisioningClassName: check-capacity.fake.dev`
and no conditions yet (or `Provisioned=False` while waiting).

### Step 4: Wait for provisioning to succeed (~10s)

After the fake backend delay elapses, the ProvisioningRequest transitions
to `Provisioned=True`. The AdmissionCheck on the Workload transitions to
`Ready`, and the launch gate opens.

```bash
# Wait ~15s, then check again
sleep 15
make phase7-inspect-launchgate PHASE7_RTJ_NAME=phase7-demo
```

Expected output:

```
launchGateState: Open

provisioningState: Provisioned

capacityGuaranteed: true
```

### Step 5: Confirm child JobSet launched

```bash
make phase7-inspect-workload PHASE7_RTJ_NAME=phase7-demo
```

Expected: A child JobSet exists with pods scheduled.

### Step 6: Verify metrics

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_capacity_guaranteed
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_launches_blocked_by_provisioning
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_rtjs_by_launch_gate_state
```

### Cleanup

```bash
kubectl -n checkpoint-dev delete rtj phase7-demo
```

---

## Scenario 2: Provisioning Failure, No Launch

This demonstrates that provisioning failure prevents child JobSet creation.

### Step 1: Submit the failure RTJ

```bash
make phase7-submit-fail
```

### Step 2: Observe provisioning failure

```bash
make phase7-inspect-launchgate PHASE7_RTJ_NAME=phase7-fail
```

Expected output:

```
launchGateState: Blocked

provisioningState: Failed
```

### Step 3: Confirm no child JobSet

```bash
make phase7-inspect-workload PHASE7_RTJ_NAME=phase7-fail
```

Expected: `<no active child JobSet -- launch gate may be blocking>`

### Step 4: Inspect the ProvisioningRequest

```bash
make phase7-inspect-provisioningrequest PHASE7_RTJ_NAME=phase7-fail
```

Expected: ProvisioningRequest with `Failed=True` condition.

### Step 5: Observe Kueue behavior

Kueue may re-suspend the Workload after seeing the failed AdmissionCheck.
Check the Workload conditions:

```bash
make phase7-inspect-workload PHASE7_RTJ_NAME=phase7-fail
```

Look for `Evicted=True` or `QuotaReserved=False` conditions.

### Cleanup

```bash
kubectl -n checkpoint-dev delete rtj phase7-fail
```

---

## Scenario 3: waitForPodsReady Timeout / Requeue Path

This demonstrates the startup timeout flow where pods fail to become ready
within Kueue's waitForPodsReady timeout, triggering eviction and requeue.

### Step 1: Submit the startup-timeout RTJ

This RTJ uses a nonexistent image to guarantee pods never become ready:

```bash
PHASE7_RTJ_NAME=phase7-timeout PHASE7_TRAINER_IMAGE=nonexistent-image:v999.999.999 \
  make phase7-submit-success
```

Or apply the dedicated sample:

```bash
sed \
  -e 's|__RTJ_NAME__|phase7-timeout|g' \
  -e 's|__TRAINER_IMAGE__|nonexistent-image:v999.999.999|g' \
  -e 's|__DEV_NAMESPACE__|checkpoint-dev|g' \
  deploy/dev/phase7/samples/rtj-startup-timeout.yaml | kubectl apply -f -
```

### Step 2: Wait for provisioning to succeed

The ProvisioningRequest will succeed after ~10s:

```bash
sleep 15
make phase7-inspect-launchgate PHASE7_RTJ_NAME=phase7-timeout
```

Expected: `launchGateState: Open`, child JobSet created, pods in
`ImagePullBackOff`.

### Step 3: Wait for waitForPodsReady timeout

The Kueue config sets a 120s timeout. After ~2 minutes, Kueue evicts
the Workload:

```bash
# Wait ~2.5 minutes
sleep 150
make phase7-inspect-launchgate PHASE7_RTJ_NAME=phase7-timeout
```

Expected:

```
startupState: StartupTimedOut
evictionReason: PodsReadyTimeout
```

### Step 4: Observe requeue behavior

After eviction, the RTJ should transition through the yield path and
re-enter the queue:

```bash
make phase7-inspect-workload PHASE7_RTJ_NAME=phase7-timeout
```

Check the RTJ phase:

```bash
kubectl -n checkpoint-dev get rtj phase7-timeout \
  -o jsonpath='{.status.phase}'
```

Expected: The RTJ enters `Queued` or `YieldRequested` after the timeout.

### Step 5: Verify startup timeout metrics

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_startup_timeout
```

### Cleanup

```bash
kubectl -n checkpoint-dev delete rtj phase7-timeout
```

---

## Quick reference: all commands

```bash
# Setup
make phase7-up
make phase7-load-images IMAGES=controller:latest
make phase7-smoke

# Submit
make phase7-submit-success PHASE7_RTJ_NAME=phase7-demo
make phase7-submit-fail

# Inspect
make phase7-inspect-launchgate           PHASE7_RTJ_NAME=phase7-demo
make phase7-inspect-workload             PHASE7_RTJ_NAME=phase7-demo
make phase7-inspect-provisioningrequest  PHASE7_RTJ_NAME=phase7-demo
make phase7-inspect-checkpoints          PHASE7_RTJ_NAME=phase7-demo

# Cluster state
make phase7-status

# E2E tests
make e2e-phase7 PHASE7_TRAINER_IMAGE=$PHASE7_TRAINER_IMAGE

# Cleanup
make phase7-down
```

## Metrics endpoint

While the operator is running, Phase 7 metrics are available at:

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator | grep -E 'launch_gate|provisioning|startup_timeout|recovery_timeout|capacity_guaranteed|fake_provisioner'
```

Key metrics to watch during the demo:

| Metric | What it shows |
|--------|---------------|
| `rtjs_by_launch_gate_state{state="Open"}` | RTJs with open launch gates |
| `rtjs_by_launch_gate_state{state="Blocked"}` | RTJs with blocked launch gates |
| `provisioning_states_observed_total{state="..."}` | Provisioning state transitions |
| `launches_blocked_by_provisioning_total` | How many times provisioning blocked launch |
| `launches_blocked_by_delayed_topology_total` | How many times delayed topology blocked launch |
| `startup_timeout_events_total` | waitForPodsReady startup timeouts |
| `recovery_timeout_events_total` | waitForPodsReady recovery timeouts |
| `capacity_guaranteed_launches_total` | Successful capacity-guaranteed launches |
| `fake_provisioner_observations_total` | Fake backend reconcile count |
| `fake_provisioner_failures_total` | Fake backend failure count |
