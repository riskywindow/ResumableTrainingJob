# Phase 1 Operations

Phase 1 observability is intentionally lightweight.
There is no UI.
The main sources of truth are:

- operator logs
- RTJ status
- trainer logs
- object-store manifests
- child `JobSet` and Kueue objects
- Prometheus-style operator metrics

## Operator Logs

The default Phase 1 flow runs the operator locally:

```bash
go run ./cmd/operator --leader-elect=false
```

In that path, operator logs are wherever that command is running.
If you want a durable file, redirect them:

```bash
go run ./cmd/operator --leader-elect=false 2>&1 | tee .tmp/operator.log
```

The same local process also serves metrics on `:8080` by default:

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'checkpoint_native_operator'
```

## Trainer Logs

Trainer logs live in the pods created by the child `JobSet`.
Start with:

```bash
kubectl get jobset -n checkpoint-dev
kubectl get pods -n checkpoint-dev
```

Then inspect the trainer pod directly:

```bash
kubectl logs -n checkpoint-dev <trainer-pod>
```

The most useful Phase 1 trainer events are:

- `trainer_start`
- `periodic_checkpoint`
- `yield_complete`
- `training_complete`

## Checkpoint Manifests

The RTJ status records the current manifest URIs in:

- `status.lastCompletedCheckpoint.manifestURI`
- `status.selectedCheckpoint.manifestURI`

You can fetch them directly:

```bash
kubectl get resumabletrainingjobs.training.checkpoint.example.io -n checkpoint-dev phase1-demo -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}'
echo
```

If you are using the local MinIO deployment, port-forward it first:

```bash
kubectl -n checkpoint-dev port-forward service/minio 9000:9000
```

Then inspect the manifest with `mc`:

```bash
mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
mc cat local/rtj-checkpoints/phase1-demo/manifests/<checkpoint-id>.manifest.json
```

## Child JobSets And Queues

To inspect the child runtime carrier:

```bash
kubectl get jobset -n checkpoint-dev
kubectl get jobset -n checkpoint-dev -l training.checkpoint.example.io/rtj-name=phase1-demo -o yaml
```

To inspect Kueue admission state:

```bash
kubectl get localqueues.kueue.x-k8s.io -n checkpoint-dev
kubectl get clusterqueues.kueue.x-k8s.io
kubectl get workloads.kueue.x-k8s.io -n checkpoint-dev
```

## Built-In Metrics

The operator now exports these metrics:

- `checkpoint_native_operator_rtjs_by_phase`
- `checkpoint_native_operator_pauses_requested_total`
- `checkpoint_native_operator_pause_timeouts_total`
- `checkpoint_native_operator_resumes_attempted_total`
- `checkpoint_native_operator_resumes_succeeded_total`
- `checkpoint_native_operator_resumes_failed_total`
- `checkpoint_native_operator_checkpoints_discovered_total`

These are intended for quick local inspection, not a full monitoring stack.
