# Phase 7 -- E2E Test Coverage

## Overview

Phase 7 adds three deterministic single-cluster e2e tests that validate the
capacity-guaranteed launch pipeline end-to-end using the fake ProvisioningRequest
backend. All tests rely on the Phase 7 local dev profile (`make phase7-up`) and
do **not** require a real cloud autoscaler or GPU nodes.

## Prerequisites

```bash
make phase7-up                              # Create kind cluster + Phase 7 profile
make phase7-load-images IMAGES=<trainer>    # Load trainer image
make phase7-smoke                           # Verify infrastructure
```

## Running the tests

```bash
# All Phase 7 e2e tests:
make e2e-phase7

# Individual tests:
RUN_KIND_E2E=1 PHASE7_TRAINER_IMAGE=<image> go test ./test/e2e \
  -run TestCapacityGuaranteedLaunch -v -timeout 20m

RUN_KIND_E2E=1 PHASE7_TRAINER_IMAGE=<image> go test ./test/e2e \
  -run TestProvisioningFailureRequeue -v -timeout 20m

RUN_KIND_E2E=1 PHASE7_TRAINER_IMAGE=<image> go test ./test/e2e \
  -run TestWaitForPodsReadyTimeout -v -timeout 20m
```

## Test inventory

### TestCapacityGuaranteedLaunch

**File:** `test/e2e/capacity_guaranteed_launch_test.go`

**What it proves:**

| Assertion | Why it matters |
|---|---|
| RTJ stays Queued while LocalQueue is held | Quota reservation precedes launch |
| No child JobSet before admission | Child runtime waits for Kueue admission |
| Workload created with ProvisioningRequest AC | Kueue wires the provisioning pipeline |
| status.provisioning transitions Pending → Provisioned | RTJ observes provisioning lifecycle |
| Child JobSet created only after provisioning succeeds | Capacity guarantee enforced |
| Child JobSet is plain runtime (no Kueue labels) | Phase 2 invariant preserved |
| status.launchGate shows Open after provisioning | Launch gate correctly evaluates all ACs |
| status.capacity.guaranteeActive = true | Physical capacity guarantee surfaced |
| No Workload owned by child JobSet | RTJ remains sole Kueue-managed object |

**Fake backend class:** `check-capacity.fake.dev` (delayed success, ~10s)

**Determinism:** Uses a held LocalQueue to control admission timing. The fake
backend delay is creation-timestamp-based (no wall-clock flakiness).

---

### TestProvisioningFailureRequeue

**File:** `test/e2e/provisioning_failure_requeue_test.go`

**What it proves:**

| Assertion | Why it matters |
|---|---|
| Workload created for the RTJ | Kueue processes the admission request |
| Fake backend rejects ProvisioningRequest | Failure path exercises correctly |
| No child JobSet is created | Failed provisioning prevents launch |
| RTJ does not reach Running phase | Fail-safe: no launch without capacity |
| status.provisioning shows Failed or RTJ re-suspended | Failure surfaces cleanly in status |
| status.launchGate shows Blocked (when observable) | Gate reflects provisioning state |
| RTJ stays in Queued/suspended state | Kueue correctly requeues after failure |

**Fake backend class:** `failed.fake.dev` (permanent immediate failure)

**Determinism:** The fake backend fails immediately — no timing dependency.
The test auto-applies the failure queue if not present.

**Note on race:** Kueue may re-suspend the RTJ before the operator observes the
provisioning failure. The test accepts either path (explicit Failed status or
Kueue-driven re-suspension), since the critical invariant is that no child
JobSet is created.

---

### TestWaitForPodsReadyTimeout

**File:** `test/e2e/waitforpodsready_timeout_test.go`

**What it proves:**

| Assertion | Why it matters |
|---|---|
| Provisioning succeeds (delayed-success class) | Launch gate opens correctly |
| Child JobSet is created | RTJ launches after provisioning |
| Pods fail to start (nonexistent image) | Startup failure triggers timeout path |
| Kueue evicts Workload after waitForPodsReady timeout | Kueue timeout mechanism works |
| status.startupRecovery reflects timeout/eviction | RTJ distinguishes startup timeout |
| desiredState remains Running (not Paused) | Not confused with manual pause |
| currentSuspension.source is not "manual" | Not confused with manual pause |
| RTJ transitions back to Queued (requeued) | Kueue requeuing works correctly |
| StartupTimeoutEvicted condition set (when observable) | Condition distinguishes timeout from preemption |

**Fake backend class:** `check-capacity.fake.dev` (delayed success, ~10s)

**Image:** `nonexistent-image:v999.999.999` (deliberate ImagePullBackOff)

**Determinism:** The nonexistent image guarantees pods never become ready.
The waitForPodsReady timeout (120s in Phase 7 profile) fires deterministically.
This test has a longer timeout (~6min observation window) to accommodate the
Kueue timeout.

---

## Test infrastructure

### Fake provisioning classes

| Class | Behavior | Used by |
|---|---|---|
| `check-capacity.fake.dev` | Success after configurable delay (default 10s) | TestCapacityGuaranteedLaunch, TestWaitForPodsReadyTimeout |
| `failed.fake.dev` | Immediate permanent failure | TestProvisioningFailureRequeue |
| `booking-expiry.fake.dev` | Success then capacity revoked | (deferred to future session) |

### Test fixtures

| Fixture | Purpose |
|---|---|
| `testdata/phase7/rtj-capacity-guaranteed.yaml` | RTJ on phase7-training queue |
| `testdata/phase7/rtj-provision-failure.yaml` | RTJ on phase7-failure-training queue |
| `testdata/phase7/rtj-startup-timeout.yaml` | RTJ with nonexistent image |
| `testdata/phase7/localqueue-hold-phase7.yaml` | Held LocalQueue → phase7-cq |
| `testdata/phase7/localqueue-hold-failure.yaml` | Held LocalQueue → phase7-failure-cq |

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `RUN_KIND_E2E` | (required) | Must be `1` to run e2e tests |
| `PHASE7_TRAINER_IMAGE` | Falls back to earlier phase images | Trainer image loaded in kind |
| `DEV_NAMESPACE` | `checkpoint-dev` | Kubernetes namespace |
| `MINIO_ROOT_USER` | `minioadmin` | MinIO credentials |
| `MINIO_ROOT_PASSWORD` | `minioadmin123` | MinIO credentials |
| `MINIO_REGION` | `us-east-1` | MinIO region |

## What remains deferred

| Test | Reason deferred | Tracking |
|---|---|---|
| Booking expiry / capacity revocation | Requires long expiry window or short-expiry tuning; lower priority than core paths | OQ: booking-expiry e2e |
| Multi-cluster Phase 7 compatibility | Requires three-cluster setup; separate prompt scope | Phase 7 multi-cluster e2e |
| Topology + provisioning combined | Requires Kueue TAS + ProvisioningRequest interaction; environment-dependent | OQ3 |
| Recovery timeout (vs startup timeout) | Requires a running job to lose readiness; needs checkpoint + resume infrastructure | Future session |
| podSetUpdate materialization in child JobSet | Observable only when Kueue populates podSetUpdates on AC; backend-dependent | Unit-tested |
| Provisioning retry/backoff behavior | Kueue's ProvisioningRequest retry is internal; RTJ observes Retry→Pending mapping | Unit-tested |
