# Phase 2 Operations

Phase 2 observability stays intentionally lightweight.
The main sources of truth are:

- RTJ status
- Kueue `Workload` status
- runtime child `JobSet` objects
- checkpoint manifests and yield markers in object storage
- operator metrics on `:8080`

## Inspect RTJ Status

The quickest local path is:

```bash
make inspect-rtj RTJ_NAME=phase2-low
```

The authoritative RTJ fields for Phase 2 are:

- `status.phase`
- `spec.suspend`
- `status.currentSuspension`
- `status.pauseRequestID`
- `status.currentRunAttempt`
- `status.activeJobSetName`
- `status.lastCompletedCheckpoint.manifestURI`
- `status.selectedCheckpoint.manifestURI`

For a narrow view:

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low \
  -o jsonpath='{.status.phase}{"\n"}{.spec.suspend}{"\n"}{.status.pauseRequestID}{"\n"}{.status.lastCompletedCheckpoint.manifestURI}{"\n"}'
```

## Inspect Kueue Workloads

Use the helper first:

```bash
make inspect-kueue
```

The raw objects are:

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io -o yaml
```

In Phase 2 the RTJ-owned `Workload` is the admission object.
The workload owner reference should point to `kind=ResumableTrainingJob`, not `JobSet`.

## Verify The Child JobSet Is Runtime Only

The child `JobSet` should exist only after the RTJ is admitted and unsuspended.
Once it exists, verify that it is plain runtime only:

```bash
kubectl -n checkpoint-dev get jobset -l training.checkpoint.example.io/rtj-name=phase2-low -o yaml
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io -o yaml
```

Check these invariants:

- the child `JobSet` has no `kueue.x-k8s.io/queue-name` label
- the child `JobSet` has no `kueue.x-k8s.io/priority-class` label
- there is no `Workload` owned by `kind=JobSet`
- the only Phase 2 admission `Workload` owner is the RTJ

## Inspect Checkpoint Manifests And Yield Markers

Start by reading the RTJ status:

```bash
manifest_uri="$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}')"
storage_uri="$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low -o jsonpath='{.spec.checkpoint.storageURI}')"
run_attempt="$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low -o jsonpath='{.status.currentRunAttempt}')"
printf 'manifest=%s\nstorage=%s\nrunAttempt=%s\n' "$manifest_uri" "$storage_uri" "$run_attempt"
```

If you are using the local MinIO stack, port-forward it:

```bash
kubectl -n checkpoint-dev port-forward service/minio 9000:9000
```

Then inspect the manifest and yield marker with `mc`:

```bash
mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
mc cat "local/${manifest_uri#s3://}"
mc cat "local/${storage_uri#s3://}/yield-markers/run-${run_attempt}.json"
```

The yield marker should carry the same `requestID` and `manifestURI` that the operator accepted for graceful teardown.

## Inspect Metrics

The local operator serves Prometheus-style metrics on `:8080`:

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'checkpoint_native_operator'
```

The most useful Phase 2 metrics are:

- `checkpoint_native_operator_rtjs_by_phase`
- `checkpoint_native_operator_workloads_created_total`
- `checkpoint_native_operator_admissions_observed_total`
- `checkpoint_native_operator_kueue_suspensions_observed_total`
- `checkpoint_native_operator_preemption_yields_completed_total`
- `checkpoint_native_operator_resumes_attempted_total`
- `checkpoint_native_operator_resumes_succeeded_total`
- `checkpoint_native_operator_resumes_failed_total`
- `checkpoint_native_operator_duplicate_child_jobset_preventions_total`
