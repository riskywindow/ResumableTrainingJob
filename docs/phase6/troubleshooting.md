# Phase 6 Troubleshooting

Common issues and resolution steps for MultiKueue RTJ execution.

## Missing MultiKueue External-Framework Config

**Symptom:** RTJ submitted on manager stays in `Pending` dispatch phase
indefinitely. Workload is created but never admitted.

**Check:**

```bash
MANAGER_CTX="kind-phase6-manager"

# Verify Kueue configuration includes RTJ external framework.
kubectl -n kueue-system get configmap kueue-manager-config \
  -o jsonpath='{.data.controller_manager_config\.yaml}' --context $MANAGER_CTX

# Look for externalFrameworks section with RTJ GVK.
# Expected:
#   externalFrameworks:
#   - "training.checkpoint.example.io/v1alpha1, Kind=ResumableTrainingJob"
```

**Resolution:**

1. Apply the Kueue configuration that includes the RTJ external framework:
   ```bash
   kubectl -n kueue-system apply -f deploy/dev/kueue/controller_manager_config.phase2-rtj-external-framework.yaml \
     --context $MANAGER_CTX
   ```
2. Restart the Kueue controller:
   ```bash
   kubectl -n kueue-system rollout restart deployment kueue-controller-manager \
     --context $MANAGER_CTX
   ```
3. Verify the MultiKueue AdmissionCheck exists and is Active:
   ```bash
   kubectl get admissionchecks.kueue.x-k8s.io multikueue --context $MANAGER_CTX
   ```

Also ensure both Kueue feature gates are enabled:
- `MultiKueue` (Beta, default-on in v0.15.1)
- `MultiKueueBatchJobWithManagedBy` (Beta, default-on in v0.15.1)

---

## Manager Operator Accidentally Launching Local Runtime

**Symptom:** Child JobSets and/or trainer pods appear on the manager
cluster for a MultiKueue-managed RTJ.

**Check:**

```bash
MANAGER_CTX="kind-phase6-manager"
NS="checkpoint-dev"
RTJ="phase6-dispatch-demo"

# Should return zero results for a MultiKueue-managed RTJ.
kubectl -n $NS get jobsets.jobset.x-k8s.io \
  -l training.checkpoint.example.io/rtj-name=$RTJ \
  --context $MANAGER_CTX --no-headers

kubectl -n $NS get pods \
  -l training.checkpoint.example.io/rtj-name=$RTJ \
  --context $MANAGER_CTX --no-headers

# Verify the operator is running in manager mode.
kubectl -n checkpoint-system get deployment rtj-operator \
  -o jsonpath='{.spec.template.spec.containers[0].args}' \
  --context $MANAGER_CTX
```

**Resolution:**

1. Confirm the operator deployment uses `--mode=manager`:
   ```bash
   # The manager operator must include: --mode=manager
   # If missing, patch the deployment:
   kubectl -n checkpoint-system set env deployment/rtj-operator --containers=manager \
     MODE=manager --context $MANAGER_CTX
   ```

2. Confirm the RTJ has `spec.managedBy` set:
   ```bash
   kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
     -o jsonpath='{.spec.managedBy}' --context $MANAGER_CTX
   # Expected: kueue.x-k8s.io/multikueue
   ```

3. If the RTJ does NOT have `spec.managedBy`, it is treated as a normal
   single-cluster RTJ even on the manager. This is by design — the
   manager preserves the Phase 5 path for non-MultiKueue RTJs.

4. If local JobSets already exist, delete them and the operator will
   not recreate them as long as mode=manager and managedBy is set.

---

## No Worker Selected

**Symptom:** RTJ stays in `Dispatched` phase but never becomes `Active`.
Or stays in `Pending` if no worker is available at all.

**Check:**

```bash
MANAGER_CTX="kind-phase6-manager"

# Check MultiKueueCluster health.
kubectl get multikueueclusters.kueue.x-k8s.io --context $MANAGER_CTX
kubectl get multikueueclusters.kueue.x-k8s.io worker-1 \
  -o jsonpath='{.status.conditions}' --context $MANAGER_CTX | python3 -m json.tool

# Check worker ClusterQueue quota and stopPolicy.
for ctx in kind-phase6-worker-1 kind-phase6-worker-2; do
  echo "--- $ctx ---"
  kubectl get clusterqueues.kueue.x-k8s.io phase6-worker-cq \
    -o jsonpath='{.spec.stopPolicy}' --context $ctx 2>/dev/null
  echo
  kubectl get clusterqueues.kueue.x-k8s.io phase6-worker-cq \
    -o jsonpath='{.status}' --context $ctx 2>/dev/null | python3 -m json.tool
done

# Check Workload admission check status.
kubectl -n $NS get workloads.kueue.x-k8s.io -o wide --context $MANAGER_CTX
```

**Resolution:**

1. If MultiKueueCluster shows `Active=False`, check kubeconfig Secrets:
   ```bash
   kubectl get secrets -l kueue.x-k8s.io/multikueue=true --context $MANAGER_CTX
   ```

2. If worker ClusterQueues have `stopPolicy: HoldAndDrain`, the worker
   is intentionally stopped. Remove the stopPolicy to allow admission:
   ```bash
   kubectl patch clusterqueues.kueue.x-k8s.io phase6-worker-cq \
     --type json -p '[{"op":"remove","path":"/spec/stopPolicy"}]' \
     --context kind-phase6-worker-1
   ```

3. If worker ClusterQueues have no available quota, check resource
   limits and running workloads:
   ```bash
   kubectl get clusterqueues.kueue.x-k8s.io phase6-worker-cq \
     -o jsonpath='{.status.flavorsReservation}' --context kind-phase6-worker-1
   ```

---

## Manager/Worker Namespace or LocalQueue Mismatch

**Symptom:** RTJ is dispatched but the worker rejects it because the
namespace or LocalQueue does not exist.

**Check:**

```bash
NS="checkpoint-dev"

# Verify namespace exists on all clusters.
for ctx in kind-phase6-manager kind-phase6-worker-1 kind-phase6-worker-2; do
  echo -n "$ctx: "
  kubectl get namespace $NS --context $ctx --no-headers 2>/dev/null || echo "MISSING"
done

# Verify LocalQueue names match across clusters.
for ctx in kind-phase6-manager kind-phase6-worker-1 kind-phase6-worker-2; do
  echo "--- $ctx ---"
  kubectl -n $NS get localqueues.kueue.x-k8s.io --context $ctx --no-headers 2>/dev/null || echo "  (none)"
done

# Check the RTJ's queueName.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o jsonpath='{.spec.queueName}' --context kind-phase6-manager
```

**Resolution:**

1. The namespace must exist on all clusters. The Phase 6 install scripts
   create `checkpoint-dev` on all three clusters:
   ```bash
   kubectl create namespace $NS --context kind-phase6-worker-1
   kubectl create namespace $NS --context kind-phase6-worker-2
   ```

2. LocalQueue names must match between manager and workers. The manager
   uses `phase6-training` pointing to `phase6-multikueue-cq`. Workers
   must have `phase6-training` pointing to `phase6-worker-cq`:
   ```bash
   kubectl -n $NS apply -f deploy/dev/phase6/workers/20-local-queue.yaml \
     --context kind-phase6-worker-1
   kubectl -n $NS apply -f deploy/dev/phase6/workers/20-local-queue.yaml \
     --context kind-phase6-worker-2
   ```

3. The RTJ CRD must be installed on all clusters:
   ```bash
   for ctx in kind-phase6-manager kind-phase6-worker-1 kind-phase6-worker-2; do
     kubectl get crd resumabletrainingjobs.training.checkpoint.example.io \
       --context $ctx --no-headers
   done
   ```

---

## Shared Checkpoint Store Not Reachable

**Symptom:** Worker cannot write or read checkpoints. The trainer logs
show S3 connection errors. Cross-worker resume fails because the new
worker cannot find the checkpoint written by the previous worker.

**Check:**

```bash
MANAGER_CTX="kind-phase6-manager"
NS="checkpoint-dev"

# Check shared store ConfigMap.
kubectl -n $NS get configmap shared-checkpoint-store \
  -o jsonpath='{.data}' --context $MANAGER_CTX

# Check store reachability from manager.
STORE_ENDPOINT="$(kubectl -n $NS get configmap shared-checkpoint-store \
  -o jsonpath='{.data.endpoint}' --context $MANAGER_CTX)"
docker exec phase6-manager-control-plane \
  wget -q -O /dev/null --timeout=5 "${STORE_ENDPOINT}/minio/health/ready"

# Check credentials on all clusters.
for ctx in kind-phase6-manager kind-phase6-worker-1 kind-phase6-worker-2; do
  echo -n "$ctx: "
  kubectl -n $NS get secret checkpoint-storage-credentials \
    --context $ctx --no-headers 2>/dev/null && echo "ok" || echo "MISSING"
done

# Check MinIO pod health (runs on worker-1).
kubectl -n $NS get pods -l app=minio --context kind-phase6-worker-1

# Check MinIO logs.
kubectl -n $NS logs -l app=minio --context kind-phase6-worker-1 --tail=20
```

**Resolution:**

1. MinIO runs on worker-1 with a NodePort (30900). The endpoint must use
   the Docker-internal IP of the worker-1 node:
   ```bash
   WORKER1_IP="$(docker inspect phase6-worker-1-control-plane \
     --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')"
   echo "MinIO endpoint: http://${WORKER1_IP}:30900"
   ```

2. If the endpoint in the ConfigMap is wrong, re-run the shared store
   install:
   ```bash
   PHASE6_MANAGER=phase6-manager PHASE6_WORKER_1=phase6-worker-1 \
     PHASE6_WORKER_2=phase6-worker-2 DEV_NAMESPACE=checkpoint-dev \
     ./hack/dev/install-phase6-shared-store.sh
   ```

3. If credentials are missing on a cluster, apply the credentials
   template:
   ```bash
   kubectl -n $NS apply -f deploy/dev/phase6/shared-store/checkpoint-credentials-template.yaml \
     --context kind-phase6-worker-2
   ```

4. Verify the `rtj-checkpoints` bucket exists:
   ```bash
   kubectl -n $NS exec deploy/minio --context kind-phase6-worker-1 -- \
     mc ls local/rtj-checkpoints
   ```

---

## Remote Pause/Resume Status Not Reflecting

**Symptom:** After patching `spec.control.desiredState` to `Paused`,
the manager-side RTJ remains in a running phase. Or after patching
back to `Running`, the RTJ stays in `Paused`.

**Check:**

```bash
MANAGER_CTX="kind-phase6-manager"
NS="checkpoint-dev"
RTJ="phase6-dispatch-demo"

# Check desired state.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o jsonpath='{.spec.control.desiredState}' --context $MANAGER_CTX

# Check current phase.
kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
  -o jsonpath='{.status.phase}' --context $MANAGER_CTX

# Check if the adapter has processed the spec change.
# Look at the Workload's admission check status.
kubectl -n $NS get workloads.kueue.x-k8s.io -o wide --context $MANAGER_CTX

# Check manager operator logs.
kubectl -n checkpoint-system logs deploy/rtj-operator --context $MANAGER_CTX --tail=30 \
  | grep -i "pause\|resume\|remote\|teardown"

# Check if the remote RTJ still exists on the worker.
for ctx in kind-phase6-worker-1 kind-phase6-worker-2; do
  echo "--- $ctx ---"
  kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
    --context $ctx --no-headers 2>/dev/null || echo "  not present"
done
```

**Resolution:**

1. **Pause not reflecting:** The manager waits for the adapter to tear
   down the remote. The controller checks `hasRemoteStatusSignal()` —
   if `activeJobSetName` or `currentRunAttempt > 0` on the manager-side
   status, the controller assumes the remote is still active and
   requeues. The adapter must delete the remote and mirror the fresh
   (empty) status. If this takes too long:

   - Check the Kueue controller logs on the manager for MultiKueue
     adapter errors:
     ```bash
     kubectl -n kueue-system logs deploy/kueue-controller-manager \
       --context $MANAGER_CTX --tail=50 | grep -i multikueue
     ```
   - Verify the Kueue controller can reach the worker cluster API:
     ```bash
     kubectl get multikueueclusters.kueue.x-k8s.io worker-1 \
       -o jsonpath='{.status.conditions[?(@.type=="Active")].status}' \
       --context $MANAGER_CTX
     ```

2. **Resume not reflecting:** After patching desiredState to Running,
   the adapter must delete the Paused remote and create a new Running
   remote. The manager detects the new remote via `hasRemoteStatusSignal`.

   - If the Workload was deleted during pause, it may need to be
     recreated. Check if a Workload exists:
     ```bash
     kubectl -n $NS get workloads.kueue.x-k8s.io -o wide --context $MANAGER_CTX
     ```
   - The RTJ `spec.suspend` must be `true` for Kueue to manage it.
     Verify:
     ```bash
     kubectl -n $NS get resumabletrainingjobs.training.checkpoint.example.io $RTJ \
       -o jsonpath='{.spec.suspend}' --context $MANAGER_CTX
     ```

3. **Requeue interval:** The manager controller requeues at 5-second
   intervals when waiting for remote state changes. Allow up to 30
   seconds for the full pause or resume cycle to complete.

4. **Checkpoint preservation during pause:** If the remote checkpoint
   summary is lost after pause, check the operator logs for
   `preserveRemoteCheckpoint` / `restoreRemoteCheckpoint` messages.
   The controller preserves the summary before `syncRemoteStatus` and
   restores it if the sync clears it.
