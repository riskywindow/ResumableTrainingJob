# Phase 8 -- End-to-End Test Coverage

## Overview

Phase 8 e2e tests exercise the DRA (Dynamic Resource Allocation) integration
end-to-end in a local kind cluster with the example DRA driver, Kueue
`deviceClassMappings`, and the RTJ operator.

**Prerequisites**: `make phase8-up` must complete successfully before running.

```bash
RUN_KIND_E2E=1 PHASE8_TRAINER_IMAGE=<image> go test -v -count=1 -timeout 20m ./test/e2e/ -run TestDRA
```

## Tests

### TestDRAQuotaAndAllocation

**File**: `test/e2e/dra_quota_and_allocation_test.go`

**What it proves**:

1. **ResourceClaimTemplate lifecycle**: When a DRA-backed RTJ is submitted,
   the operator creates companion ResourceClaimTemplate objects with correct
   ownership, device request spec, and deterministic naming
   (`<rtj-name>-<claim-name>`).

2. **Device status population**: `status.devices` is populated with the
   SHA256 device profile fingerprint, requested device classes, and
   ResourceClaimTemplate references.

3. **Kueue quota accounting via deviceClassMappings**: The Workload created
   by the RTJ is admitted to the Phase 8 ClusterQueue. The admission
   accounts for the DRA logical resource (`example.dev/gpu`) through
   Kueue's `deviceClassMappings` configuration.

4. **DRA allocation enables launch**: The RTJ transitions to Running only
   after the Workload is admitted and DRA allocation succeeds. The child
   JobSet is created as a plain runtime resource (Phase 2 invariant).

5. **Quota exhaustion blocks admission**: A second RTJ requesting more
   GPUs than the remaining quota stays Queued until the first RTJ is
   deleted, freeing the quota. This proves Kueue enforces DRA device
   quota through logical resource accounting.

6. **Phase 2 invariant preserved**: No Workload is owned by any child
   JobSet.

**Test fixtures**: `test/e2e/testdata/phase8/rtj-dra-launch.yaml`,
`test/e2e/testdata/phase8/rtj-dra-quota-hog.yaml`

---

### TestDRAResumeCompatibleProfile

**File**: `test/e2e/dra_resume_compatible_profile_test.go`

**What it proves**:

1. **Compatible pause/resume cycle**: A DRA-backed RTJ can pause, save a
   checkpoint, and resume using the same device profile. The device
   profile fingerprint is preserved across the pause/resume boundary.

2. **Checkpoint manifest includes device fingerprint**: The checkpoint
   manifest saved to S3 includes the `deviceProfileFingerprint` field
   matching the RTJ's current profile. This is verified by reading the
   manifest directly from MinIO.

3. **Resume selects compatible checkpoint**: When the device profile
   fingerprint matches between the current RTJ spec and the saved
   checkpoint manifest, the resume proceeds and training continues from
   the checkpoint (run attempt increments).

4. **Child JobSet is recreated**: After resume, a new child JobSet is
   created (for run attempt 2) as a plain runtime resource.

**Test fixtures**: `test/e2e/testdata/phase8/rtj-dra-pause-resume.yaml`

---

### TestDRAIncompatibleResumeRejection

**File**: `test/e2e/dra_incompatible_resume_rejection_test.go`

**What it proves**:

1. **Device profile fingerprint divergence**: Two RTJs using different
   DeviceClasses (`example-gpu` vs `example-gpu-alt`) produce different
   SHA256 device profile fingerprints. This is verified by comparing
   `status.devices.currentDeviceProfileFingerprint` on both RTJs.

2. **Fail-closed rejection**: A checkpoint saved with device profile
   fingerprint A is NOT selected for resume by an RTJ with device
   profile fingerprint B. The operator refuses to use the incompatible
   checkpoint.

3. **Clear status surfacing**: When the operator rejects an incompatible
   checkpoint, it surfaces the rejection reason through the `Degraded`
   condition with reason `NoCompatibleCheckpoint`, and/or the RTJ
   transitions to `Failed` phase.

4. **Checkpoint manifest fidelity**: The checkpoint manifest stored in
   S3 preserves the device profile fingerprint from the checkpoint
   creation time, enabling accurate compatibility checks at resume time.

**Test fixtures**: `test/e2e/testdata/phase8/rtj-dra-pause-resume.yaml`
(source checkpoint), `test/e2e/testdata/phase8/rtj-dra-incompatible-profile.yaml`
(incompatible target)

**Unit test complement**: The comprehensive device profile mismatch
rejection logic is covered by unit tests:
- `internal/checkpoints/compatibility_test.go`: `TestDifferentDeviceProfileIncompatible`
- `internal/checkpoints/compatibility_test.go`: `TestCheckpointWithoutFingerprintIncompatibleWithDRARequest`
- `internal/checkpoints/selector_test.go`: `TestSelectLatestCompatibleSkipsIncompatibleDeviceProfile`

---

## Test Infrastructure

### Helper files

- **`test/e2e/phase8_helpers_test.go`**: Phase 8-specific test helpers
  including `phase8RTJView` (with `status.devices` DRA fields),
  `setupPhase8Env()`, ResourceClaimTemplate/ResourceClaim query helpers,
  Phase 8 Workload helpers with DRA admission fields, and cleanup
  functions.

### Test data

- **`test/e2e/testdata/phase8/rtj-dra-launch.yaml`**: DRA-backed RTJ
  requesting 2 GPUs via `example-gpu` DeviceClass (4 total across 2
  workers). Used for launch and quota tests.
- **`test/e2e/testdata/phase8/rtj-dra-quota-hog.yaml`**: RTJ requesting
  4 GPUs per worker (8 total) to exhaust the Phase 8 ClusterQueue quota.
- **`test/e2e/testdata/phase8/rtj-dra-pause-resume.yaml`**: DRA-backed
  RTJ for compatible pause/resume testing with `example-gpu` DeviceClass.
- **`test/e2e/testdata/phase8/rtj-dra-incompatible-profile.yaml`**: RTJ
  with `example-gpu-alt` DeviceClass for incompatible profile rejection.
- **`test/e2e/testdata/phase8/localqueue-hold-phase8.yaml`**: LocalQueue
  with hold policy for admission-gated testing.

### Environment requirements

| Component | Requirement |
|-----------|------------|
| Kind cluster | v1.33+ (DRA v1beta1 support) |
| Kueue | v0.15.1+ with `DynamicResourceAllocation` feature gate |
| DRA driver | Example DRA driver DaemonSet publishing `dra.example.dev` ResourceSlices |
| DeviceClass | `example-gpu` matching `dra.example.dev` driver |
| ClusterQueue | `phase8-cq` with `example.dev/gpu` quota (8 devices) |
| LocalQueue | `phase8-training` in `checkpoint-dev` namespace |
| MinIO | Checkpoint storage backend |

---

## What Remains Deferred

### Multi-cluster DRA e2e (Phase 8 Session 8+)

- DRA-backed RTJ dispatch via MultiKueue to a remote worker cluster
- DRA status mirroring from worker to manager cluster
- Deferred until the MultiKueue adapter has DRA-aware Workload creation

### DRA + ProvisioningRequest interaction (OQ9)

- Testing the interaction between DRA device allocation and Phase 7
  ProvisioningRequest AdmissionCheck
- Both features active simultaneously on the same RTJ
- Deferred pending Kueue support for combined DRA + provisioning checks

### Device failure recovery e2e

- DRA claim allocation failure → `DRAClaimAllocationFailed` condition →
  operator-driven recovery
- Requires the example DRA driver to simulate device-level failures
  (setting Ready=False per-device conditions)
- The observation and condition-setting logic is covered by unit tests in
  `internal/dra/claims_test.go` and `internal/controller/dra_status_test.go`

### Shared/manual ResourceClaim e2e

- Pre-allocated devices reused across pods or runs
- Deferred to a future phase (Phase 8 only supports per-pod templates)

### Real hardware DRA e2e

- Testing with actual GPU DRA drivers (NVIDIA, AMD)
- Requires hardware that is not available in the local dev environment
- The Phase 8 e2e tests use the example fake DRA driver exclusively
