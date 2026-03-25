# Phase 1 Troubleshooting

## Expected Phase Changes

For the manual pause happy path, the RTJ should usually move through:

- `Starting`
- `Running`
- `YieldRequested`
- `Draining`
- `Paused`

If it stays in `Pending`, the child `JobSet` was never launched or the RTJ was created with `desiredState=Paused`.

If it ends in `Failed`, inspect:

- `status.reason`
- `status.message`
- `status.conditions`

The timeout path sets:

- `status.phase=Failed`
- `status.reason=DrainTimedOut`
- `status.conditions[type=Degraded].status=True`

## Expected Logs

The Phase 1 controller is status-first and log-light.
The most useful operator evidence is currently the RTJ status itself.

The trainer emits JSON log lines that are useful during pause smoke tests:

- `{"event":"trainer_start", ...}`
- `{"event":"yield_complete", "checkpointID":"...", "manifestURI":"...", "requestID":"..."}`

If periodic checkpointing is enabled, it also emits:

- `{"event":"periodic_checkpoint", ...}`

## Common Failure Modes

### RTJ never reaches `Running`

Check:

- the queue label on the child `JobSet`
- Kueue `LocalQueue` and `ClusterQueue` existence
- the child `JobSet` workload admission state
- the trainer image is already loaded into kind

Useful commands:

```bash
kubectl get jobset -n checkpoint-dev
kubectl get workloads.kueue.x-k8s.io -n checkpoint-dev
kubectl get pods -n checkpoint-dev
```

### RTJ reaches `Running` but never leaves `YieldRequested` or `Draining`

Check:

- the control `ConfigMap` content changed to `desiredState=Paused`
- the mounted control file is visible inside the trainer container
- the trainer emitted `yield_complete`
- object storage contains both the yield marker and the manifest

Useful commands:

```bash
kubectl get configmap -n checkpoint-dev <rtj>-run-<n>-control -o yaml
kubectl logs -n checkpoint-dev <trainer-pod>
```

If the trainer never emits `yield_complete`, verify the pod is using the DDP entrypoint from the Phase 1 fixture and that the control file path matches `YIELD_SDK_CONTROL_FILE`.

### RTJ fails with `DrainTimedOut`

This means the controller did not observe both a newer yield marker and a newer manifest before `spec.checkpoint.maxDrainTime` elapsed.

Check:

- the trainer can write the local staging directory
- object-store credentials in the trainer pod are valid
- the trainer uploaded the manifest last
- the operator can read the object store endpoint it is configured with

The timeout path force-deletes the child `JobSet`, so trainer pods may disappear before you inspect them.
The main surviving evidence is:

- RTJ status message
- RTJ degraded condition
- any partial objects already uploaded to storage

### Pause e2e smoke hangs before completion

The current e2e smoke expects:

- `make dev-up` already completed
- MinIO running in `checkpoint-dev`
- a trainer image available through `PAUSE_FLOW_TRAINER_IMAGE`
- `RUN_KIND_E2E=1`

The smoke test starts a local operator process and a local `kubectl port-forward` to MinIO.
If the host port `9000` is already busy, the test will fail before reconciliation starts.
