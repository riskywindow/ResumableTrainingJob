# Phase 8 Demo: DRA-Backed ResumableTrainingJobs

This document walks through the complete Phase 8 demo sequence,
from DRA-backed launch through pause/resume and incompatible device
profile rejection.

## Prerequisites

```bash
# Create the Phase 8 environment (kind v1.33+, DRA driver, Kueue
# with deviceClassMappings, Phase 8 queues).
make phase8-up

# Verify infrastructure.
make phase8-smoke
```

## 1. DRA-Backed Launch

Submit an RTJ requesting 2 simulated GPUs per worker via the
`example-gpu` DeviceClass.

```bash
make phase8-submit PHASE8_RTJ_NAME=demo-launch
```

### Verify ResourceClaimTemplate creation

The operator creates a companion ResourceClaimTemplate named
`demo-launch-gpu` (pattern: `<rtj-name>-<claim-name>`).

```bash
kubectl -n checkpoint-dev get resourceclaimtemplates \
  -l training.checkpoint.example.io/rtj-name=demo-launch
```

Expected output:

```
NAME              AGE
demo-launch-gpu   5s
```

### Verify ResourceClaim creation

ResourceClaims are created by the scheduler when pods are bound.
After the Workload is admitted and the child JobSet is created:

```bash
kubectl -n checkpoint-dev get resourceclaims \
  -l training.checkpoint.example.io/rtj-name=demo-launch
```

### Verify DRA status on RTJ

```bash
make phase8-inspect-dra PHASE8_RTJ_NAME=demo-launch
```

Key fields to check:

- `deviceMode: DRA`
- `currentDeviceProfileFingerprint: sha256:...`
- `requestedDeviceClasses: ["example-gpu"]`
- `claimAllocationState: Allocated` (after scheduling)

### Verify Kueue DRA quota accounting

```bash
make phase8-inspect-kueue PHASE8_RTJ_NAME=demo-launch
```

Key observations:

- ClusterQueue `phase8-cq` shows `example.dev/gpu` usage
- Workload is admitted with DRA resource accounting via
  `deviceClassMappings`

## 2. Pause/Resume with Same Device Profile

Submit an RTJ with a CEL selector, then pause and resume it.

```bash
make phase8-submit PHASE8_RTJ_NAME=demo-resume PHASE8_SAMPLE=pause-resume
```

Wait for the RTJ to reach Running and a checkpoint to be saved:

```bash
# Poll until Running.
kubectl -n checkpoint-dev get rtj demo-resume -w

# Check checkpoint status.
make phase8-inspect-checkpoints PHASE8_RTJ_NAME=demo-resume
```

### Pause

```bash
make phase8-pause PHASE8_RTJ_NAME=demo-resume
```

Verify:

```bash
make phase8-inspect-dra PHASE8_RTJ_NAME=demo-resume
```

Observations:

- Phase transitions to `Paused`
- ResourceClaimTemplates are **preserved** (owned by RTJ, not JobSet)
- Child JobSet is deleted
- Checkpoint is committed with `deviceProfileFingerprint`

### Resume

```bash
make phase8-resume PHASE8_RTJ_NAME=demo-resume
```

Verify:

```bash
make phase8-inspect-dra PHASE8_RTJ_NAME=demo-resume
make phase8-inspect-checkpoints PHASE8_RTJ_NAME=demo-resume
```

Observations:

- Operator selects checkpoint with matching device profile fingerprint
- `currentRunAttempt` increments
- Device profile fingerprint is preserved across pause/resume
- New child JobSet created with DRA claim references

## 3. Incompatible Device-Profile Resume Rejection

This demonstrates the fail-closed checkpoint compatibility check.

### Step 1: Create a checkpoint under "example-gpu"

```bash
make phase8-submit PHASE8_RTJ_NAME=demo-compat PHASE8_SAMPLE=launch
# Wait for Running + checkpoint
kubectl -n checkpoint-dev get rtj demo-compat -w
make phase8-inspect-checkpoints PHASE8_RTJ_NAME=demo-compat
```

### Step 2: Pause

```bash
make phase8-pause PHASE8_RTJ_NAME=demo-compat
```

### Step 3: Submit a different RTJ with incompatible device class

```bash
make phase8-submit PHASE8_RTJ_NAME=demo-incompat PHASE8_SAMPLE=incompatible
```

### Step 4: Verify rejection

```bash
make phase8-inspect-dra PHASE8_RTJ_NAME=demo-incompat
make phase8-inspect-checkpoints PHASE8_RTJ_NAME=demo-incompat
```

Observations:

- The incompatible RTJ has a **different** device profile fingerprint
  (uses `example-gpu-alt` DeviceClass)
- Checkpoints from `demo-compat` are NOT compatible
- The RTJ enters a degraded state or starts fresh (no compatible
  checkpoint available)

## 4. Inspect Infrastructure

At any point, inspect the full DRA infrastructure:

```bash
# DRA driver, DeviceClass, ResourceSlices.
make phase8-status

# Kueue quota and deviceClassMappings.
make phase8-inspect-kueue

# All DRA status for a specific RTJ.
make phase8-inspect-dra PHASE8_RTJ_NAME=demo-launch
```

## 5. Cleanup

```bash
# Delete individual RTJs.
kubectl -n checkpoint-dev delete rtj demo-launch demo-resume demo-compat demo-incompat

# Or tear down the entire environment.
make phase8-down
```

## Demo Command Summary

| Step | Command |
|------|---------|
| Environment setup | `make phase8-up` |
| Smoke test | `make phase8-smoke` |
| Submit DRA RTJ | `make phase8-submit PHASE8_RTJ_NAME=demo` |
| Pause | `make phase8-pause PHASE8_RTJ_NAME=demo` |
| Resume | `make phase8-resume PHASE8_RTJ_NAME=demo` |
| Inspect DRA | `make phase8-inspect-dra PHASE8_RTJ_NAME=demo` |
| Inspect Kueue | `make phase8-inspect-kueue` |
| Inspect checkpoints | `make phase8-inspect-checkpoints PHASE8_RTJ_NAME=demo` |
| Run e2e tests | `make e2e-phase8` |
| Tear down | `make phase8-down` |
