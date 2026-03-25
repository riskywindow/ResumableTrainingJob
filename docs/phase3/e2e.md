# Phase 3 E2E Tests

## Overview

Phase 3 adds three deterministic e2e tests that exercise the core admission-aware
launch and flavor-aware resume flows on a live kind cluster with the Phase 3
multi-flavor dev environment.

## Prerequisites

1. Phase 3 kind cluster running with flavors applied:

   ```bash
   make phase3-up
   ```

2. Trainer image built and loaded:

   ```bash
   docker build -t phase1-ddp-counter:dev -f fixtures/pytorch_ddp_counter/Dockerfile .
   make phase3-load-images IMAGES=phase1-ddp-counter:dev
   ```

3. CRD installed:

   ```bash
   kubectl apply -f deploy/crd/
   ```

## Running

```bash
make e2e-phase3 PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev
```

Or directly:

```bash
RUN_KIND_E2E=1 PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev \
  go test ./test/e2e -run 'TestAdmissionMaterialization|TestFlavorAwareLaunch|TestFlexibleResume' -v -timeout 20m
```

## Tests

### TestAdmissionMaterialization

**File:** `test/e2e/admission_materialization_test.go`

**Goals exercised:** G1 (admission-aware launch)

**Flow:**

1. Creates a LocalQueue with `stopPolicy: Hold` pointing to the Phase 3
   ClusterQueue (`phase3-cq`).
2. Creates an RTJ on the held queue.
3. Verifies the RTJ reaches Queued and no child JobSet exists.
4. Releases the hold (patches `stopPolicy: null`).
5. Verifies the RTJ transitions to Running.
6. Inspects the child JobSet:
   - Has correct replica count.
   - Has no Kueue management labels or annotations (plain runtime resource).
7. Verifies the `admitted-pod-sets` annotation is present on the RTJ.
8. Verifies `status.admission.admittedWorkerCount > 0`.
9. Verifies no Workload is owned by the child JobSet.

**Key assertions:**

- Child JobSet is a plain runtime resource (Phase 2 invariant preserved).
- Admitted worker count is recorded in RTJ status.
- Bridge annotation carries admitted counts from Kueue to the controller.

### TestFlavorAwareLaunch

**File:** `test/e2e/flavor_aware_launch_test.go`

**Goals exercised:** G2 (flavor-aware child JobSet rendering)

**Flow:**

1. Creates an RTJ on the Phase 3 queue (`phase3-training`).
2. Waits for Running.
3. Inspects the child JobSet pod template:
   - Has `nodeSelector` with `checkpoint-native.dev/pool` key.
   - If assigned to `spot` flavor, has the spot toleration.
4. Waits for pods to be scheduled and verifies:
   - Pods landed on nodes whose `checkpoint-native.dev/pool` label matches the
     assigned flavor.

**Key assertions:**

- ResourceFlavor nodeSelector flows through `RunWithPodSetsInfo` → `podset.Merge`
  → RTJ template → child JobSet pod template → pod spec.
- Pods actually schedule to the correct node pool.
- No Kueue management metadata leaks to the child JobSet.

### TestFlexibleResume

**File:** `test/e2e/flexible_resume_test.go`

**Goals exercised:** G1, G2, G3 (world-size-flexible resume)

**Flow:**

1. Creates an RTJ with `allowWorldSizeChange: true` on the Phase 3 queue.
2. Waits for Running.
3. Pauses (patches `desiredState: Paused`).
4. Waits for Paused with a completed checkpoint.
5. Records the checkpoint manifest URI and global step.
6. Resumes (patches `desiredState: Running`).
7. Waits for Running with `selectedCheckpoint` matching the paused checkpoint.
8. Verifies `status.restore` is populated (restore mode, world sizes).
9. Lets training continue, then pauses again.
10. Loads the second checkpoint and verifies global step advanced.
11. Verifies `status.admission.admittedWorkerCount > 0`.

**Key assertions:**

- Resume with `allowWorldSizeChange=true` succeeds.
- Global step is monotonically increasing across pause/resume.
- `status.restore` records the restore mode and world sizes.
- `status.admission` records the admitted worker count.

## World-Size Parity and Partial Admission

### Same-Size Path (Default)

In the default `flavors` profile, Kueue admits all-or-nothing (no `MinCount`).
The admitted world size equals the requested world size. `TestFlexibleResume`
exercises the Phase 3 admission-aware code path in same-size mode. The code
path is identical to the different-size path — only the restore mode differs
(`SameSize` instead of `Reshard`).

### Different-Size Path (Experimental)

Actual different-size resume requires Kueue partial admission
(`PodSet.MinCount`), which is gated behind:

1. Operator flag: `--enable-experimental-partial-admission`
2. Per-job field: `spec.parallelism.enablePartialAdmission: true`
3. Profile: `make phase3-up PHASE3_PROFILE=experimental`

The different-size path is validated by unit tests:

| Package | Tests | Coverage |
| --- | --- | --- |
| `internal/checkpoints` | 8 compatibility + 5 selector | Flexible matching, cross-size selection |
| `internal/kueue` | 8 pod set tests | MinCount synthesis, preferredCount override |
| `sdk/python/tests` | 7 resume + 12 checkpoint | DCP resharding, RNG skip, manifest validation |

To manually test different-size resume end-to-end:

```bash
make phase3-up PHASE3_PROFILE=experimental
go run ./cmd/operator --leader-elect=false --enable-experimental-partial-admission
# Submit the partial admission sample:
# deploy/dev/samples/phase3/rtj-partial-admission.yaml
```

## Test Data

| File | Purpose |
| --- | --- |
| `test/e2e/testdata/phase3/rtj-phase3.yaml` | Base Phase 3 RTJ template |
| `test/e2e/testdata/phase3/rtj-phase3-flexible.yaml` | RTJ with `allowWorldSizeChange: true` |
| `test/e2e/testdata/phase3/localqueue-hold-phase3.yaml` | Hold queue pointing to `phase3-cq` |

## Helpers

Phase 3 e2e helpers are in `test/e2e/phase3_helpers_test.go`:

- `setupPhase3Env()` — sets up the Phase 3 test environment (port forward,
  operator, MinIO). Verifies `phase3-cq` exists. Accepts an
  `experimentalPartialAdmission` flag for operator startup.
- `phase3RTJView` — JSON view struct with Phase 3 status fields (admission,
  restore).
- `jobSetDetailView` — detailed child JobSet view including replicatedJobs,
  pod template nodeSelector, tolerations, and env vars.
- `getPhase3RTJ()`, `waitForPhase3RTJState()`, `waitForPhase3Phase()` —
  polling helpers for Phase 3 RTJ state.
- `getJobSetDetail()`, `waitForJobSetDetailPresent()` — child JobSet
  inspection helpers.
- `getPods()`, `getNodeLabels()` — pod and node inspection helpers.
- `assertChildJobSetPlainRuntime()` — verifies no Kueue management metadata
  on child JobSet.

## Makefile Target

```bash
make e2e-phase3 PHASE3_TRAINER_IMAGE=phase1-ddp-counter:dev
```

Runs the three Phase 3 tests with a 20-minute timeout.
