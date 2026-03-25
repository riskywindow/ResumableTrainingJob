# Phase 3 Operations Guide

## Inspecting Kueue Workload Admission

The Kueue Workload is the primary admission object. It is owned by the RTJ and
contains the full admission payload including flavor assignments and pod counts.

### Find the Workload for an RTJ

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -o custom-columns='NAME:.metadata.name,OWNER:.metadata.ownerReferences[0].name,QUEUE:.spec.queueName,CLUSTER_QUEUE:.status.admission.clusterQueue'
```

### Inspect the full admission payload

```bash
# Replace <workload-name> with the actual name.
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> -o yaml
```

Key fields in `.status.admission`:

| Field | Description |
| --- | --- |
| `.clusterQueue` | The ClusterQueue that admitted the Workload |
| `.podSetAssignments[].name` | Pod set name (e.g., `worker`) |
| `.podSetAssignments[].count` | Admitted pod count for this pod set |
| `.podSetAssignments[].flavors` | Map of resource → flavor name (e.g., `cpu: on-demand`) |
| `.podSetAssignments[].resourceUsage` | Map of resource → quantity |

### One-liner: admitted flavor and count

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{range .status.admission.podSetAssignments[*]}name={.name} count={.count} flavors={.flavors}{"\n"}{end}'
```

## Inspecting Admitted Flavors

Flavor assignments flow through two paths:

1. **Workload admission** (source of truth): flavor names in
   `.status.admission.podSetAssignments[].flavors`.
2. **RTJ status** (controller summary): `status.admission.admittedFlavors` map.

### From the Workload

```bash
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io <workload-name> \
  -o jsonpath='{.status.admission.podSetAssignments[0].flavors}'
```

### From the RTJ status

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.admission.admittedFlavors}'
```

### ResourceFlavor details

```bash
# List all ResourceFlavors:
kubectl get resourceflavors.kueue.x-k8s.io

# Inspect a specific flavor:
kubectl get resourceflavors.kueue.x-k8s.io on-demand -o yaml
```

Key ResourceFlavor fields:

| Field | Description |
| --- | --- |
| `.spec.nodeLabels` | Labels applied to node selector |
| `.spec.tolerations` | Tolerations added to pod spec |

### Verify the flavor materialized in the child JobSet

```bash
# Get the child JobSet name from RTJ status:
JOBSET=$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')

# Check nodeSelector:
kubectl -n checkpoint-dev get jobset "$JOBSET" \
  -o jsonpath='{.spec.replicatedJobs[0].template.spec.template.spec.template.spec.nodeSelector}'

# Check tolerations:
kubectl -n checkpoint-dev get jobset "$JOBSET" \
  -o jsonpath='{.spec.replicatedJobs[0].template.spec.template.spec.template.spec.tolerations}'
```

## Inspecting Effective Worker Counts

The effective worker count is determined by Kueue admission, not by the RTJ
spec. Three counts are relevant:

| Count | Source | Description |
| --- | --- | --- |
| Preferred | `spec.parallelism.preferredCount` or `spec.identity.worldSize` | What the user requested |
| Admitted | Bridge annotation → `status.admission.admittedWorkerCount` | What Kueue admitted |
| Active | Pod count | What is actually running |

### RTJ status

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath=$'admitted={.status.admission.admittedWorkerCount}\npreferred={.status.admission.preferredWorkerCount}\n'
```

### Bridge annotation (raw admitted counts)

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/admitted-pod-sets}'
```

This is a JSON map of `{"podSetName": admittedCount}` set by `RunWithPodSetsInfo`.

### Child JobSet replicas

```bash
JOBSET=$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')

kubectl -n checkpoint-dev get jobset "$JOBSET" \
  -o jsonpath='{range .spec.replicatedJobs[*]}{.name}: replicas={.replicas}{"\n"}{end}'
```

### Running pods

```bash
JOBSET=$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')

kubectl -n checkpoint-dev get pods -l "jobset.sigs.k8s.io/jobset-name=${JOBSET}" \
  -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase'
```

### Using the inspect script

```bash
make phase3-inspect-admission RTJ_NAME=<rtj-name>
```

This shows all the above in one command.

## Inspecting Checkpoint Manifest World-Size Metadata

Phase 3 checkpoint manifests include world-size metadata for cross-size resume.

### RTJ status (quick check)

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath=$'lastCheckpoint.worldSize={.status.lastCompletedCheckpoint.worldSize}\nrestore.mode={.status.restore.restoreMode}\nrestore.checkpointWS={.status.restore.lastCheckpointWorldSize}\nrestore.restoreWS={.status.restore.lastRestoreWorldSize}\n'
```

### Using the inspect script

```bash
make phase3-inspect-checkpoints RTJ_NAME=<rtj-name>
```

### Reading the manifest from MinIO

If MinIO is port-forwarded (`kubectl -n checkpoint-dev port-forward svc/minio 9000:9000`):

```bash
# Get the manifest URI:
URI=$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}')

# Convert s3:// to http://localhost:9000/ and fetch:
curl -s "${URI/s3:\/\//http://localhost:9000/}" | python3 -m json.tool
```

Key Phase 3 manifest fields:

| Field | Description |
| --- | --- |
| `worldSize` | World size when checkpoint was taken |
| `leaderCount` | Number of leader pods (Phase 3) |
| `workerCount` | Number of worker pods (Phase 3) |
| `checkpointFormatVersion` | `dcp/v1` for DCP checkpoints |
| `crossSizeRestoreSupported` | `true` if DCP resharding is supported |
| `globalStep` | Training step at checkpoint time |

### Verify restore mode

After a resume, the RTJ status records the restore decision:

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath=$'restoreMode={.status.restore.restoreMode}\ncheckpointWS={.status.restore.lastCheckpointWorldSize}\nrestoreWS={.status.restore.lastRestoreWorldSize}\n'
```

- `SameSize`: checkpoint and restore world sizes matched.
- `Reshard`: DCP resharding was triggered because world sizes differed.

## Metrics

The operator exposes Prometheus metrics on `:8080/metrics`. Phase 3 adds:

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `admission_comparisons_total` | counter | `comparison` | `equal` or `partial` |
| `reshard_restores_attempted_total` | counter | | Reshard restores started |
| `reshard_restores_succeeded_total` | counter | | Reshard restores completed |
| `reshard_restores_failed_total` | counter | | Reshard restores failed |
| `flavor_assignments_total` | counter | `flavor` | By assigned flavor name |
| `partial_admission_launches_total` | counter | | Admitted < preferred |
| `same_size_resumes_total` | counter | | Same world-size resumes |
| `different_size_resumes_total` | counter | | Different world-size resumes |

Query examples:

```bash
# All Phase 3 metrics:
curl -s http://localhost:8080/metrics | grep -E 'admission_comparison|reshard|flavor_assignment|partial_admission|same_size|different_size'

# Partial admission ratio:
curl -s http://localhost:8080/metrics | grep admission_comparisons_total
```
