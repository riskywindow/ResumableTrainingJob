# Phase 4 E2E Test Strategy

## Overview

Phase 4 e2e tests validate the full admission-gated launch and topology-aware
resume pipeline on a local kind cluster. The tests are deterministic and run
without real ProvisioningRequest support.

## Prerequisites

- Phase 4 kind cluster running (`make phase4-up`)
- Trainer image loaded (`make phase4-load-images`)
- MinIO deployed in `checkpoint-dev` namespace
- Phase 4 infrastructure verified (`make phase4-smoke`)

## Running

```bash
# Full Phase 4 e2e suite
RUN_KIND_E2E=1 \
  PHASE4_TRAINER_IMAGE=<image> \
  go test ./test/e2e/ -run 'TestResumeReadiness|TestTopology' -v -timeout 30m

# Individual tests
RUN_KIND_E2E=1 \
  PHASE4_TRAINER_IMAGE=<image> \
  go test ./test/e2e/ -run TestResumeReadinessGate -v -timeout 15m

RUN_KIND_E2E=1 \
  PHASE4_TRAINER_IMAGE=<image> \
  go test ./test/e2e/ -run TestTopologyAwareLaunch -v -timeout 15m

RUN_KIND_E2E=1 \
  PHASE4_TRAINER_IMAGE=<image> \
  go test ./test/e2e/ -run TestTopologyAwareResume -v -timeout 20m
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `RUN_KIND_E2E` | (required) | Set to `1` to enable e2e tests |
| `PHASE4_TRAINER_IMAGE` | Falls back to `PHASE3_TRAINER_IMAGE`, etc. | Trainer image in kind cluster |
| `PHASE4_NAMESPACE` | `checkpoint-dev` | Target namespace |
| `MINIO_ROOT_USER` | `minioadmin` | MinIO access key |
| `MINIO_ROOT_PASSWORD` | `minioadmin123` | MinIO secret key |
| `MINIO_REGION` | `us-east-1` | MinIO region |

## Test Matrix

### TestResumeReadinessGate

**File:** `test/e2e/resume_readiness_gate_test.go`

**Exercises:** G3 (ResumeReadiness AdmissionCheck), G4 (admission-gated launch)

**What it proves:**
1. RTJ stays Queued while the LocalQueue is held — no premature launch.
2. No child JobSet is created before admission.
3. A Workload is created owned by the RTJ.
4. After the hold is released, the readiness gate clears (initial launch,
   `allowInitialLaunchWithoutCheckpoint=true`).
5. RTJ transitions to Running with a plain-runtime child JobSet.
6. `status.launchReadiness` shows `ready=true`, `gateState=Ready`.
7. `status.effectiveLaunchShape` is populated with worker count and world size.
8. No Workload is owned by the child JobSet (Phase 2 invariant).

**Topology:** None (isolates the admission check path).

### TestTopologyAwareLaunch

**File:** `test/e2e/topology_aware_launch_test.go`

**Exercises:** G1 (topology synthesis), G2 (topology materialization),
G3 (readiness check), G4 (gated launch)

**What it proves:**
1. RTJ with `topology.mode=Required` and `topologyLevel=topology.example.io/rack`
   reaches Running phase.
2. The Workload has topology-related admission data (TopologyAssignment).
3. The child JobSet pod template has a topology-derived `nodeSelector`
   (e.g., `topology.example.io/rack=rack-1`).
4. The child JobSet is a plain runtime resource.
5. Pods land on nodes matching the assigned topology domain.
6. `status.topology` is populated with levels and domain assignments.
7. `status.effectiveLaunchShape` is populated.
8. `status.launchReadiness` shows Ready.

**Note:** If Kueue TAS is not active (e.g., Topology CRD not installed),
the test adapts — topology nodeSelector assertions become soft checks, and
the RTJ still launches via the gate evaluation fallback path.

### TestTopologyAwareResume

**File:** `test/e2e/topology_resume_test.go`

**Exercises:** G2 (topology on resume), G4 (gated resume flow)

**What it proves:**
1. Full pause-resume cycle with topology: launch → checkpoint → pause → resume.
2. The resumed child JobSet preserves the topology nodeSelector.
3. `status.topology` persists across the resume boundary.
4. `status.effectiveLaunchShape` includes `selectedCheckpointID` after resume.
5. `status.launchReadiness` is Ready after resume.
6. Global step monotonicity — training advances across the resume boundary.

### TestTopologyNonRepresentableDocumented

**File:** `test/e2e/topology_aware_launch_test.go`

**Exercises:** Negative case documentation

**What it documents:**
- Non-representable topology assignments (multi-domain heterogeneous)
  fail with clear status conditions rather than silently degrading.
- The `TopologyNotRepresentable` reason is set on `status.launchReadiness`.
- The RTJ transitions to `Failed` phase (fail-closed principle).
- No child JobSet is created for non-representable assignments.

This is documented as a test rather than fully exercised in e2e because
producing a non-representable topology assignment requires Kueue TAS to
split pods across heterogeneous rack domains, which is not reliably
reproducible in a local kind cluster. The behavior is exhaustively covered
by unit tests in:
- `internal/topology/assignment_test.go`
- `internal/jobset/topology_injection_test.go`
- `internal/controller/resumabletrainingjob_controller_test.go`

## Test Data

All Phase 4 test templates are in `test/e2e/testdata/phase4/`:

| File | Description |
|------|-------------|
| `rtj-topology-required.yaml` | RTJ with Required rack topology |
| `rtj-readiness-gated.yaml` | RTJ with no topology (admission-check-only) |
| `localqueue-hold-phase4.yaml` | Held LocalQueue pointing to `phase4-cq` |

Templates use `__PLACEHOLDER__` tokens replaced at runtime:
- `__RTJ_NAME__` — unique RTJ name (includes timestamp)
- `__TRAINER_IMAGE__` — trainer container image
- `__DEV_NAMESPACE__` — target namespace
- `__LOCAL_QUEUE_NAME__` — LocalQueue name

## Topology Model

The local kind cluster has a simulated two-level topology:

```
block-a / rack-1: worker, worker2   (on-demand pool)
block-b / rack-2: worker3, worker4  (spot pool)
```

Labels:
- `topology.example.io/block`: `block-a` or `block-b`
- `topology.example.io/rack`: `rack-1` or `rack-2`

With 2 replicas and Required mode at rack level, Kueue TAS packs both pods
into a single rack, producing a single-domain representable assignment.

## Known Limitations

1. **Kueue TAS dependency.** The Topology CRD and TAS feature gate must be
   installed and enabled. The e2e setup skips gracefully if not present.

2. **Non-representable topology.** Fully exercising the non-representable
   path in e2e would require manipulating Kueue TAS decisions, which is not
   reliably reproducible locally. Covered by unit tests instead.

3. **ProvisioningRequest.** G5 is deferred; no e2e test exercises the
   ProvisioningRequest admission check path.

4. **Preferred mode.** The Preferred topology mode is not exercised in a
   separate e2e test because the kind cluster's rack capacity makes it
   behave identically to Required mode. The mode mapping is verified by
   unit tests in `internal/kueue/rtj_topology_test.go`.

## File Index

| File | Purpose |
|------|---------|
| `test/e2e/phase4_helpers_test.go` | Phase 4 env setup, view types, wait helpers |
| `test/e2e/resume_readiness_gate_test.go` | Admission-check-gated launch test |
| `test/e2e/topology_aware_launch_test.go` | Topology-aware launch + negative case |
| `test/e2e/topology_resume_test.go` | Topology-aware resume after pause |
| `test/e2e/testdata/phase4/*.yaml` | Test YAML templates |
| `docs/phase4/e2e.md` | This file |
