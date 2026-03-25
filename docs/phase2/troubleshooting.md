# Phase 2 Troubleshooting

## Webhook Or Defaulting Issues

Symptom:

- RTJ create fails validation
- `spec.suspend` never starts as `true`
- Kueue queue labels are missing from the RTJ

Checks:

```bash
kubectl get resumabletrainingjobs.training.checkpoint.example.io -n checkpoint-dev phase2-low -o yaml
kubectl get mutatingwebhookconfigurations,validatingwebhookconfigurations | rg 'resumabletrainingjob|checkpoint'
```

Notes:

- The local live e2e and demo manifests set `spec.suspend`, `metadata.labels["kueue.x-k8s.io/queue-name"]`, and `metadata.labels["kueue.x-k8s.io/priority-class"]` explicitly because the default local workflow does not rely on an in-cluster webhook deployment.
- If you are testing the webhook path, verify the webhook configuration, service routing, and certificates before assuming the RTJ controller is at fault.

## externalFrameworks Misconfiguration

Symptom:

- RTJ exists but no Kueue `Workload` appears
- RTJ never leaves `Queued`
- Kueue ignores the RTJ admission object entirely

Checks:

```bash
make inspect-kueue
kubectl -n kueue-system get configmap kueue-manager-config -o jsonpath='{.data.controller_manager_config\.yaml}'
```

The Kueue manager config must include:

- `integrations.externalFrameworks` with `ResumableTrainingJob.v1alpha1.training.checkpoint.example.io`
- `manageJobsWithoutQueueName: false`
- `managedJobsNamespaceSelector.matchLabels.checkpoint-native.dev/kueue-managed: "true"`

If any of those are missing, re-run `make dev-up` or re-apply the Phase 2 Kueue config path from `deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml`.

## Accidental Double-Management Of Child JobSets

Symptom:

- a second `Workload` appears for the child `JobSet`
- child `JobSet` objects show queue or priority labels
- Kueue starts admitting both RTJ and `JobSet`

Checks:

```bash
make inspect-kueue
kubectl -n checkpoint-dev get jobset -l training.checkpoint.example.io/rtj-name=phase2-low -o yaml
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io -o yaml
```

Expected state:

- the RTJ-owned `Workload` exists
- no `Workload` is owned by `kind=JobSet`
- child `JobSet` labels do not include `kueue.x-k8s.io/queue-name`
- child `JobSet` labels do not include `kueue.x-k8s.io/priority-class`

If that invariant breaks, inspect:

- `internal/jobset/render.go` for stripped Kueue metadata
- the Kueue config for `manageJobsWithoutQueueName: false`
- any user-supplied JobSet template labels that may have reintroduced queue metadata

## Suspend Or Preemption Without Checkpoint Evidence

Symptom:

- RTJ shows a `kueue-suspend-*` pause request but never publishes a checkpoint
- RTJ stays in `YieldRequested` or `Draining`
- RTJ ends in `Failed` with `DrainTimedOut`

Checks:

```bash
make inspect-rtj RTJ_NAME=phase2-low
kubectl -n checkpoint-dev get configmap phase2-low-run-1-control -o yaml
kubectl -n checkpoint-dev logs <trainer-pod-name>
```

Then inspect object storage:

```bash
manifest_uri="$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}')"
storage_uri="$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low -o jsonpath='{.spec.checkpoint.storageURI}')"
run_attempt="$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io phase2-low -o jsonpath='{.status.currentRunAttempt}')"
mc cat "local/${storage_uri#s3://}/yield-markers/run-${run_attempt}.json"
mc cat "local/${manifest_uri#s3://}"
```

The most common causes are:

- the trainer image was not built from the fixture with the yield SDK entrypoint
- MinIO credentials or endpoint wiring are wrong
- the trainer wrote a partial checkpoint but never published the manifest last
- the operator cannot read back the yield marker and manifest from object storage

If the RTJ already hit `DrainTimedOut`, the child `JobSet` may have been force-deleted, so the RTJ status plus MinIO contents are the remaining evidence.
