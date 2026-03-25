# Phase 3 Troubleshooting

## Missing Flavor Injection

**Symptom:** RTJ is `Running` but child JobSet pods have no `nodeSelector` for
the assigned flavor. Pods may schedule on wrong nodes.

**Diagnosis:**

```bash
# 1. Check if the Workload was admitted with flavors:
make phase3-inspect-admission RTJ_NAME=<rtj-name>
# Look for "podSetAssignments" — flavors should be non-empty.

# 2. Check if the bridge annotation was set:
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/admitted-pod-sets}'
# Should be a JSON map like {"worker":2}.

# 3. Check the child JobSet nodeSelector:
JOBSET=$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')
kubectl -n checkpoint-dev get jobset "$JOBSET" \
  -o jsonpath='{.spec.replicatedJobs[0].template.spec.template.spec.template.spec.nodeSelector}'
```

**Common causes:**

1. **Kueue not configured for RTJ external framework.** Verify the Kueue
   manager config includes the RTJ external framework:

   ```bash
   kubectl -n kueue-system get configmap kueue-manager-config -o yaml | grep -A 5 externalFrameworks
   ```

   Must include `checkpoint-native.dev/kueue-managed`.

2. **ClusterQueue has no ResourceFlavors.** Verify the Phase 3 ClusterQueue
   exists and has flavors:

   ```bash
   kubectl get clusterqueues.kueue.x-k8s.io phase3-cq -o yaml
   ```

   The `resourceGroups` should list `on-demand` and `spot` flavors.

3. **ResourceFlavor nodeLabels don't match node labels.** Verify nodes have
   the expected labels:

   ```bash
   kubectl get nodes -L checkpoint-native.dev/pool
   ```

   Must show `on-demand` on 2 nodes and `spot` on 2 nodes.

4. **RTJ submitted to Phase 2 queue.** Phase 2 queue (`training`) uses the
   `default-flavor` without nodeLabels. Submit to `phase3-training` instead:

   ```yaml
   labels:
     kueue.x-k8s.io/queue-name: phase3-training
   ```

## Admitted Count Not Reflected in Child JobSet

**Symptom:** RTJ `status.admission.admittedWorkerCount` shows a value, but the
child JobSet has a different replica count.

**Diagnosis:**

```bash
# 1. Check admitted count in RTJ status:
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.admission.admittedWorkerCount}'

# 2. Check bridge annotation:
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/admitted-pod-sets}'

# 3. Check child JobSet replicas:
JOBSET=$(kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.status.activeJobSetName}')
kubectl -n checkpoint-dev get jobset "$JOBSET" \
  -o jsonpath='{range .spec.replicatedJobs[*]}{.name}: replicas={.replicas}{"\n"}{end}'
```

**Common causes:**

1. **Bridge annotation missing or malformed.** `RunWithPodSetsInfo` sets the
   annotation when Kueue admits the Workload. If the annotation is absent,
   check:

   - Is the RTJ managed by Kueue? (Has `kueue.x-k8s.io/queue-name` label.)
   - Is the Workload admitted? (Check `.status.admission` on the Workload.)
   - Operator logs: look for `RunWithPodSetsInfo` errors.

2. **Replica count math discrepancy.** The renderer computes replicas as
   `admittedPodCount / podsPerReplica`. If the template has `parallelism > 1`,
   the pods-per-replica is `parallelism * completions`, and the replica count
   is scaled down accordingly. Check template values:

   ```bash
   kubectl -n checkpoint-dev get jobset "$JOBSET" \
     -o jsonpath='{.spec.replicatedJobs[0].template.spec.parallelism}'
   ```

3. **Phase 2 backward compatibility.** If no bridge annotation is present
   (Phase 2 RTJ), the renderer uses the template's replica count unchanged.
   This is expected behavior.

## Incompatible Reshard Restore

**Symptom:** RTJ transitions to `Failed` with reason
`NoCompatibleCheckpoint` after resume, or `status.restore.restoreMode` is
unexpectedly `SameSize` when expecting `Reshard`.

**Diagnosis:**

```bash
# 1. Check restore status:
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath=$'restoreMode={.status.restore.restoreMode}\ncheckpointWS={.status.restore.lastCheckpointWorldSize}\nrestoreWS={.status.restore.lastRestoreWorldSize}\n'

# 2. Check the checkpoint manifest:
make phase3-inspect-checkpoints RTJ_NAME=<rtj-name>
# Look at crossSizeRestoreSupported in the manifest.

# 3. Check RTJ resume spec:
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath='{.spec.resume.allowWorldSizeChange}'
```

**Common causes:**

1. **`allowWorldSizeChange` not set.** The RTJ spec must have
   `spec.resume.allowWorldSizeChange: true` for cross-size resume. Without it,
   the compatibility checker requires exact world-size match.

2. **Checkpoint doesn't support cross-size restore.** Phase 2 checkpoints
   (before Phase 3 SDK changes) have `crossSizeRestoreSupported=null`, which
   is treated as `false`. Only Phase 3 checkpoints with DCP format
   (`checkpointFormatVersion: dcp/v1`) set `crossSizeRestoreSupported: true`.

   The fix is to run at least one pause/resume cycle with the Phase 3 trainer
   image to produce a cross-size-compatible checkpoint.

3. **GPU shape mismatch.** Phase 3 does NOT relax GPU shape compatibility
   (OQ-4). A checkpoint taken on one GPU type cannot be restored on a
   different GPU type, regardless of `allowWorldSizeChange`. Verify:

   ```bash
   # spec.identity.gpuShape must match:
   kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
     -o jsonpath='{.spec.identity.gpuShape}'
   ```

4. **World sizes actually match.** If the admitted count equals the checkpoint
   world size, restore mode is `SameSize` even with `allowWorldSizeChange=true`.
   This is correct behavior — no reshard is needed. To test actual resharding,
   use the experimental partial admission path where Kueue can admit a
   different count.

## Experimental Partial-Admission Profile Misconfiguration

**Symptom:** RTJ with `enablePartialAdmission: true` always gets full
admission (admitted count == preferred count), never partial.

**Diagnosis:**

```bash
# 1. Check operator flag:
# Operator must be started with --enable-experimental-partial-admission
# Look at operator startup log:
# "starting manager" ... "experimentalPartialAdmission":true

# 2. Check RTJ spec:
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> \
  -o jsonpath=$'enablePartialAdmission={.spec.parallelism.enablePartialAdmission}\npreferredCount={.spec.parallelism.preferredCount}\nminCount={.spec.parallelism.minCount}\n'

# 3. Check the Workload PodSet.MinCount:
WORKLOAD=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}")
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io "$WORKLOAD" \
  -o jsonpath='{range .spec.podSets[*]}name={.name} count={.count} minCount={.minCount}{"\n"}{end}'

# 4. Check ClusterQueue capacity:
kubectl get clusterqueues.kueue.x-k8s.io phase3-cq -o yaml
```

**Common causes:**

1. **Operator flag not set.** The operator must be started with
   `--enable-experimental-partial-admission`. Without it, `MinCount` is never
   set on the PodSet, and Kueue does all-or-nothing admission.

   ```bash
   go run ./cmd/operator --leader-elect=false --enable-experimental-partial-admission
   ```

2. **Per-job flag not set.** Both the operator flag AND the per-job field are
   required (double gating):

   ```yaml
   spec:
     parallelism:
       enablePartialAdmission: true
       preferredCount: 4
       minCount: 2
   ```

3. **`allowWorldSizeChange` not set.** Partial admission without
   `allowWorldSizeChange: true` is rejected by validation. Both must be set:

   ```yaml
   spec:
     resume:
       allowWorldSizeChange: true
     parallelism:
       enablePartialAdmission: true
   ```

4. **Cluster has enough capacity.** If the ClusterQueue has enough quota for
   `preferredCount`, Kueue admits the full amount. Partial admission only
   activates when capacity is constrained. To test, reduce ClusterQueue
   quota or submit competing workloads.

5. **Wrong profile.** Ensure the experimental profile was applied:

   ```bash
   make phase3-profile PHASE3_PROFILE=experimental
   ```

6. **Kueue PartialAdmission feature gate.** In Kueue v0.15.1, the
   PartialAdmission feature gate is Beta and default-on. If using a
   non-standard Kueue build, verify the gate is enabled:

   ```bash
   kubectl -n kueue-system get configmap kueue-manager-config -o yaml | grep -i partial
   ```

## General Debugging

### Operator logs

```bash
# If running locally:
go run ./cmd/operator --leader-elect=false 2>&1 | tee operator.log

# Key Phase 3 log patterns:
grep -E 'admitted|flavor|reshard|restore|partial|worldSize' operator.log
```

### Event timeline

```bash
kubectl -n checkpoint-dev get events --sort-by=.lastTimestamp --field-selector involvedObject.name=<rtj-name>
```

### Full RTJ YAML

```bash
kubectl -n checkpoint-dev get resumabletrainingjobs.training.checkpoint.example.io <rtj-name> -o yaml
```

### Kueue controller logs

```bash
kubectl -n kueue-system logs -l app.kubernetes.io/name=kueue --tail=100
```
