# Phase 5 Operations Guide

This document explains how to inspect and operate Phase 5 checkpoint-aware
priority shaping resources.

## Inspect RTJ base/effective priority

The RTJ's `status.priorityShaping` sub-object contains all priority shaping
state. Use the inspect-priority script or direct kubectl:

```bash
# Via Makefile
make phase5-inspect-priority PHASE5_LOW_RTJ_NAME=<rtj-name>

# Direct kubectl
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.status.priorityShaping}' | python3 -m json.tool
```

Key fields:

| Field | Description |
|-------|-------------|
| `basePriority` | Static priority from WorkloadPriorityClass (e.g., 100 for phase5-low) |
| `effectivePriority` | Dynamically computed priority written to Workload.Spec.Priority |
| `preemptionState` | Current state: Protected, Active, Cooldown, or Preemptible |
| `preemptionStateReason` | Machine-readable reason (e.g., WithinProtectionWindow, CheckpointStale) |
| `protectedUntil` | Timestamp when the protection window expires (nil when not Protected) |

The effective priority is also available as an annotation for quick access:

```bash
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/effective-priority}'
```

The preemption state annotation:

```bash
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.metadata.annotations.training\.checkpoint\.example\.io/preemption-state}'
```

## Inspect Workload priority and Kueue conditions

The effective priority is materialized into the Kueue Workload's
`spec.priority` field. This is the value Kueue uses for preemption ordering.

```bash
# Via Makefile
make phase5-inspect-workload PHASE5_RTJ_NAME=<rtj-name>

# Direct kubectl — find the Workload owned by the RTJ
WORKLOAD=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io \
  -o jsonpath="{range .items[?(@.metadata.ownerReferences[0].name==\"<rtj-name>\")]}{.metadata.name}{end}")

# Check the priority on the Workload
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='priority={.spec.priority}  priorityClass={.spec.priorityClassName}'

# Check Workload conditions (Admitted, Evicted, etc.)
kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='{range .status.conditions[*]}  {.type}={.status} ({.reason}){"\n"}{end}'
```

To verify the Workload priority matches the RTJ's effective priority:

```bash
RTJ_EP=$(kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.status.priorityShaping.effectivePriority}')
WL_P=$(kubectl -n checkpoint-dev get workloads.kueue.x-k8s.io $WORKLOAD \
  -o jsonpath='{.spec.priority}')
echo "RTJ effectivePriority=$RTJ_EP  Workload spec.priority=$WL_P"
```

These values should match. If they diverge, see the troubleshooting guide.

## Inspect checkpoint freshness evidence

Checkpoint freshness is the primary signal driving priority shaping transitions.

```bash
# Via Makefile
make phase5-inspect-checkpoints PHASE5_RTJ_NAME=<rtj-name>

# Direct kubectl
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath=$'lastCompletedCheckpointTime={.status.priorityShaping.lastCompletedCheckpointTime}\ncheckpointAge={.status.priorityShaping.checkpointAge}\n'
```

Key freshness fields:

| Field | Source | Description |
|-------|--------|-------------|
| `lastCompletedCheckpointTime` | RTJ status or catalog | Timestamp of most recent checkpoint |
| `checkpointAge` | Computed at reconcile time | Duration since last checkpoint (e.g., "45s") |
| `checkpointFreshnessTarget` | Policy spec | Maximum acceptable checkpoint age |

The RTJ transitions to Preemptible when `checkpointAge > checkpointFreshnessTarget`.

To check the freshness target from the attached policy:

```bash
POLICY=$(kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.spec.priorityPolicyRef.name}')
kubectl get checkpointprioritypolicies.training.checkpoint.example.io $POLICY \
  -o jsonpath='checkpointFreshnessTarget={.spec.checkpointFreshnessTarget}'
```

To check the underlying checkpoint evidence in MinIO (requires port-forward):

```bash
kubectl -n checkpoint-dev port-forward svc/minio 9000:9000 &

MANIFEST_URI=$(kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.status.lastCompletedCheckpoint.manifestURI}')
# Convert s3://bucket/path to http://localhost:9000/bucket/path
curl -s "${MANIFEST_URI/s3:\/\//http://localhost:9000/}" | python3 -m json.tool
```

## Inspect the policy attached to an RTJ

```bash
# Via Makefile
make phase5-inspect-policy PHASE5_RTJ_NAME=<rtj-name>

# Direct kubectl — resolve the policy name
POLICY=$(kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{.spec.priorityPolicyRef.name}')

# View the full policy spec
kubectl get checkpointprioritypolicies.training.checkpoint.example.io $POLICY -o yaml
```

Policy sections to check:

### Timing windows

```bash
kubectl get cpp $POLICY \
  -o jsonpath=$'startupProtectionWindow={.spec.startupProtectionWindow}\ncheckpointFreshnessTarget={.spec.checkpointFreshnessTarget}\nminRuntimeBetweenYields={.spec.minRuntimeBetweenYields}\n'
```

### Priority adjustments

```bash
kubectl get cpp $POLICY \
  -o jsonpath=$'protectedBoost={.spec.protectedBoost}\ncooldownBoost={.spec.cooldownBoost}\npreemptibleOffset={.spec.preemptibleOffset}\n'
```

### Fail-open controls

```bash
kubectl get cpp $POLICY \
  -o jsonpath=$'failOpenOnTelemetryLoss={.spec.failOpenOnTelemetryLoss}\nfailOpenOnCheckpointStoreErrors={.spec.failOpenOnCheckpointStoreErrors}\n'
```

### Yield budget

```bash
kubectl get cpp $POLICY \
  -o jsonpath=$'maxYieldsPerWindow={.spec.maxYieldsPerWindow}\nyieldWindow={.spec.yieldWindow}\n'
```

## Inspect RTJ conditions

The `PriorityShaping` condition on the RTJ summarizes the current state:

```bash
kubectl -n checkpoint-dev get rtj <rtj-name> \
  -o jsonpath='{range .status.conditions[?(@.type=="PriorityShaping")]}{.status} {.reason}: {.message}{"\n"}{end}'
```

Condition states:

| Status | Reason | Meaning |
|--------|--------|---------|
| True | WithinProtectionWindow | Job is protected from priority demotion |
| True | CheckpointFresh | Normal operation, checkpoint is fresh |
| True | CheckpointStale | Checkpoint stale, effective priority lowered |
| True | CooldownAfterResume | Post-resume cooldown active |
| True | YieldBudgetExhausted | Yield budget exceeded, anti-thrash protection |
| True | TelemetryUnavailableFailOpen | Telemetry unavailable, keeping base priority |
| False | PolicyResolutionFailed | Could not find the referenced CheckpointPriorityPolicy |
| False | BasePriorityResolutionFailed | Could not find the WorkloadPriorityClass |
| False | WorkloadPatchFailed | Could not patch the Workload priority |

## Prometheus metrics

Query Phase 5 metrics from the operator's metrics endpoint:

```bash
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_priority
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_rtjs_by_preemption
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_protected_workloads
curl -s http://localhost:8080/metrics | grep checkpoint_native_operator_preemptible_workloads
```

## List all RTJs with priority shaping

```bash
kubectl -n checkpoint-dev get rtj \
  -o custom-columns='NAME:.metadata.name,PHASE:.status.phase,STATE:.status.priorityShaping.preemptionState,BASE:.status.priorityShaping.basePriority,EFFECTIVE:.status.priorityShaping.effectivePriority,POLICY:.status.priorityShaping.appliedPolicyRef'
```
