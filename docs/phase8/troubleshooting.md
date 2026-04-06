# Phase 8 Troubleshooting

Common issues when running DRA-backed ResumableTrainingJobs.

## DRA APIs Missing

**Symptom**: `phase8-smoke` fails with "ResourceSlice API not available"
or "DeviceClass API not available".

**Cause**: The cluster is running Kubernetes < v1.33. The DRA APIs
(`resource.k8s.io/v1beta1`) were promoted to beta in v1.31 but the
stable device-class-based allocation requires v1.33+.

**Resolution**:

```bash
# Check cluster version.
kubectl version --short

# Phase 8 requires v1.33+. Recreate the cluster:
make phase8-down
PHASE8_KIND_NODE_IMAGE=kindest/node:v1.33.0 make phase8-up
```

If using a managed cluster (EKS, GKE, AKS), verify the control plane
version supports DRA and that the `DynamicResourceAllocation` feature
gate is enabled.

**Verification**:

```bash
kubectl api-resources --api-group=resource.k8s.io
```

Expected: `deviceclasses`, `resourceclaims`, `resourceclaimtemplates`,
`resourceslices` should all be listed.

## Example Driver Not Advertising Resources

**Symptom**: DeviceClass exists but `kubectl get resourceslices` returns
no results, or the Phase 8 smoke test fails on "no ResourceSlice objects
found".

**Cause**: The example DRA driver DaemonSet is not running or has not
yet published ResourceSlice objects.

**Resolution**:

```bash
# Check DaemonSet status.
kubectl -n dra-example-driver get daemonset dra-example-driver

# Check pod logs for errors.
kubectl -n dra-example-driver logs -l app=dra-example-driver --tail=50

# Verify RBAC (driver needs create/update on resourceslices).
kubectl -n dra-example-driver get clusterrolebinding dra-example-driver-binding

# Reinstall the driver.
KIND_CLUSTER_NAME=checkpoint-phase1 ./hack/dev/install-phase8-dra-driver.sh
```

Common sub-issues:

- **Pod stuck in Pending**: Node may not have the expected labels.
  Check `kubectl get nodes --show-labels`.
- **RBAC error in logs**: The driver ClusterRole may be missing
  `resourceslices` permissions. Reinstall the driver.
- **Driver publishes slices but devices are empty**: The driver
  script may have failed. Check the container logs for shell errors.

**Verification**:

```bash
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver
# Should show one ResourceSlice per worker node.

kubectl get resourceslice <name> -o jsonpath='{.spec.devices}' | jq length
# Should return 4 (4 simulated GPUs per node).
```

## ResourceClaimTemplate Created but Claims Never Allocate

**Symptom**: The RTJ has `status.devices.claimAllocationState: Pending`
indefinitely. ResourceClaimTemplates exist but no ResourceClaims appear.

**Cause**: ResourceClaims are created by the Kubernetes scheduler when
pods from the child JobSet are bound to nodes. If the Workload is never
admitted by Kueue, or the child JobSet is never created, claims will
not exist.

**Resolution**:

1. **Check Workload admission**:

```bash
make phase8-inspect-kueue PHASE8_RTJ_NAME=<name>
```

If the Workload is not admitted:
- Verify ClusterQueue has sufficient `example.dev/gpu` quota
- Verify no other RTJ is consuming all the quota
- Verify `deviceClassMappings` is configured in Kueue

2. **Check child JobSet creation**:

```bash
kubectl -n checkpoint-dev get jobsets -l training.checkpoint.example.io/rtj-name=<name>
```

If no child JobSet exists, check the operator logs:

```bash
kubectl -n checkpoint-dev logs deploy/rtj-operator | grep -i 'dra\|template\|launch'
```

3. **Check DRA template readiness**:

The operator gates child JobSet creation on DRA template readiness.
If `reconcileDRATemplates()` has not completed, the launch is blocked.

```bash
kubectl -n checkpoint-dev get resourceclaimtemplates \
  -l training.checkpoint.example.io/rtj-name=<name>
```

4. **Check scheduler logs** (if claims exist but are unallocated):

```bash
kubectl -n kube-system logs -l component=kube-scheduler --tail=100 | grep -i 'dra\|claim\|allocat'
```

The scheduler may reject allocation if:
- The DeviceClass selector doesn't match any ResourceSlice devices
- Insufficient devices are available on the target nodes
- The DRA driver is not running on the target node

## Kueue Quota/Accounting Mismatch from deviceClassMappings

**Symptom**: RTJs are stuck in `Queued` state even though devices appear
available, or Kueue admits more RTJs than there are physical devices.

**Cause**: Mismatch between the `deviceClassMappings` configuration
and the ClusterQueue resource groups.

**Resolution**:

1. **Verify deviceClassMappings**:

```bash
kubectl -n kueue-system get configmap kueue-manager-config \
  -o jsonpath='{.data.controller_manager_config\.yaml}' | grep -A5 deviceClassMappings
```

Expected for Phase 8 dev:

```yaml
deviceClassMappings:
  - deviceClassName: example-gpu
    resourceName: example.dev/gpu
```

2. **Verify ClusterQueue covers the mapped resource**:

```bash
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq \
  -o jsonpath='{.spec.resourceGroups[0].coveredResources}'
```

Must include `example.dev/gpu`.

3. **Verify quota matches physical capacity**:

```bash
# Total simulated GPUs (4 per node × N nodes).
kubectl get resourceslices -l app.kubernetes.io/managed-by=dra-example-driver \
  -o jsonpath='{range .items[*]}{.spec.devices}{"\n"}{end}' | grep -c '"name"'

# ClusterQueue quota.
kubectl get clusterqueues.kueue.x-k8s.io phase8-cq \
  -o jsonpath='{.spec.resourceGroups[0].flavors[0].resources[0].nominalQuota}'
```

These should match (8 in the default dev profile: 4 GPUs × 2 nodes).

4. **Verify DynamicResourceAllocation feature gate**:

```bash
kubectl -n kueue-system get configmap kueue-manager-config \
  -o jsonpath='{.data.controller_manager_config\.yaml}' | grep DynamicResourceAllocation
```

If missing, Kueue may not resolve DRA claims through deviceClassMappings.
Reinstall the Phase 8 profile:

```bash
make phase8-profile
```

5. **Check Kueue controller logs**:

```bash
kubectl -n kueue-system logs deploy/kueue-controller-manager --tail=100 | grep -iE 'dra|device|claim|example.dev'
```

## Resume Rejected Due to Incompatible Device Profile

**Symptom**: After pause, the RTJ fails to resume or enters a degraded
state. The operator logs show "incompatible device profile" or the
checkpoint is not selected.

**Cause**: The checkpoint was saved with a different device profile
fingerprint than the current RTJ's `spec.devices` configuration.
Phase 8 uses fail-closed compatibility: if the fingerprints don't
match, the checkpoint is rejected.

**Resolution**:

1. **Inspect fingerprints**:

```bash
make phase8-inspect-checkpoints PHASE8_RTJ_NAME=<name>
```

Compare:

- `currentDeviceProfileFingerprint` (from current `spec.devices`)
- `lastCheckpointDeviceProfileFingerprint` (from the checkpoint)

2. **Common causes of fingerprint mismatch**:

| Change | Fingerprint impact |
|--------|--------------------|
| Different `deviceClassName` | Different fingerprint |
| Different `count` | Different fingerprint |
| Different `selectors` | Different fingerprint |
| Different selector **order** | Same fingerprint (order-independent) |
| Different `containers` targets | Same fingerprint (not in hash) |
| Claim **name** change | Same fingerprint (not in hash) |

3. **Recovery options**:

**Option A**: Restore the original device spec.

Change `spec.devices` back to match the checkpoint's device profile.
The fingerprint is deterministic, so identical specs produce identical
fingerprints.

**Option B**: Start fresh.

Delete the RTJ and create a new one. The new RTJ will start from
scratch without attempting to resume from the incompatible checkpoint.

**Option C**: Downgrade to non-DRA.

Remove `spec.devices` entirely (or set `mode: Disabled`). Checkpoints
saved with a device profile **are** compatible with non-DRA RTJs.
The downgrade direction is always allowed.

4. **Verify the expected fingerprint**:

The fingerprint is SHA256 of the canonical form:

```
class=<className>;selectors=<sorted-selectors>;count=<count>
```

For multiple claims, entries are sorted alphabetically and joined
with newlines.

5. **Check operator logs**:

```bash
kubectl -n checkpoint-dev logs deploy/rtj-operator | grep -i 'device.*profile\|fingerprint\|compat'
```
