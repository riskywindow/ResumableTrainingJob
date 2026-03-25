# Phase 1 Demo

This is the exact command sequence for the core Phase 1 demo.
It assumes Docker, kind, kubectl, and Go are already installed locally.

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
docker build -t phase1-ddp-counter:dev -f fixtures/pytorch_ddp_counter/Dockerfile .
make load-images IMAGES=phase1-ddp-counter:dev
```

## Terminal B: Submit The Example RTJ

```bash
cd /Users/rishivinodkumar/Daedelus
make submit-example EXAMPLE_TRAINER_IMAGE=phase1-ddp-counter:dev
make inspect-example
```

Wait until `status.phase` reaches `Running`.

## Terminal B: Pause The Example

```bash
cd /Users/rishivinodkumar/Daedelus
make pause-example
make inspect-example
```

Wait until `status.phase` reaches `Paused` and `status.lastCompletedCheckpoint.manifestURI` is populated.

## Terminal B: Resume The Example

```bash
cd /Users/rishivinodkumar/Daedelus
make resume-example
make inspect-example
```

Wait until `status.phase` returns to `Running` and `status.selectedCheckpoint.manifestURI` matches the paused manifest.

## Terminal B: Inspect Metrics

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'checkpoint_native_operator'
```

## Optional: Run The Smoke Tests

```bash
cd /Users/rishivinodkumar/Daedelus
make e2e PAUSE_FLOW_TRAINER_IMAGE=phase1-ddp-counter:dev
```
