# Phase 2 Demo

This is the exact command sequence for the core Phase 2 demo.
It exercises native RTJ admission, Kueue-driven preemption, graceful yield, and resume from checkpoint.

## Terminal A: Bring Up The Dev Environment

```bash
cd /Users/rishivinodkumar/Daedelus
make dev-up
```

## Terminal A: Run The Operator

```bash
cd /Users/rishivinodkumar/Daedelus
go run ./cmd/operator --leader-elect=false
```

Keep that process running.

## Terminal B: Build And Load The Trainer Image

```bash
cd /Users/rishivinodkumar/Daedelus
docker build -t phase2-ddp-counter:dev -f fixtures/pytorch_ddp_counter/Dockerfile .
make load-images IMAGES=phase2-ddp-counter:dev
make phase2-smoke
```

## Terminal B: Submit The Low-Priority RTJ

```bash
cd /Users/rishivinodkumar/Daedelus
make submit-low PHASE2_TRAINER_IMAGE=phase2-ddp-counter:dev PHASE2_LOW_RTJ_NAME=phase2-low
make inspect-rtj RTJ_NAME=phase2-low
make inspect-kueue
```

Wait until `phase2-low` reaches `status.phase=Running`.

## Terminal B: Submit The High-Priority RTJ

```bash
cd /Users/rishivinodkumar/Daedelus
make submit-high PHASE2_TRAINER_IMAGE=phase2-ddp-counter:dev PHASE2_HIGH_RTJ_NAME=phase2-high
make inspect-rtj RTJ_NAME=phase2-high
make inspect-kueue
```

Wait until:

- `phase2-low` moves back to `status.phase=Queued`
- `phase2-low.status.pauseRequestID` starts with `kueue-suspend-`
- `phase2-low.status.lastCompletedCheckpoint.manifestURI` is populated
- `phase2-high` reaches `status.phase=Running`

## Terminal B: Verify Resume After Releasing High Priority Quota

```bash
cd /Users/rishivinodkumar/Daedelus
kubectl -n checkpoint-dev delete resumabletrainingjobs.training.checkpoint.example.io phase2-high --wait=true
make inspect-rtj RTJ_NAME=phase2-low
make inspect-kueue
```

Wait until `phase2-low` returns to `status.phase=Running` with a higher `status.currentRunAttempt`.

## Terminal B: Inspect Metrics

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'checkpoint_native_operator_(rtjs_by_phase|workloads_created|admissions_observed|kueue_suspensions_observed|preemption_yields_completed|resumes_|duplicate_child_jobset_preventions)'
```

## Optional: Run The Live Phase 2 e2e Tests

```bash
cd /Users/rishivinodkumar/Daedelus
make e2e-phase2 PHASE2_TRAINER_IMAGE=phase2-ddp-counter:dev
```
