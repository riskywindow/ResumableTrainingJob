# API Graduation: v1alpha1 to v1beta1

**Phase:** 10 - Production Hardening & API Beta
**Date:** 2026-04-06
**Status:** Scaffolded; conversion webhooks not yet wired

---

## 1. Overview

Phase 10 graduates the three core CRDs from `v1alpha1` to `v1beta1`:

| CRD | Scope | Short | v1alpha1 | v1beta1 | Rationale |
|-----|-------|-------|----------|---------|-----------|
| `ResumableTrainingJob` | Namespaced | `rtj` | served + stored | served (not stored) | Core production resource; API stable since Phase 3 |
| `CheckpointPriorityPolicy` | Cluster | `cpp` | served + stored | served (not stored) | Coherent production set; checkpoint-aware priority is integral to safe preemption |
| `ResumeReadinessPolicy` | Cluster | `rrp` | served + stored | served (not stored) | Coherent production set; resume readiness gates prevent unsafe launches |

**Storage version:** `v1alpha1` remains the storage version. Switching storage to
`v1beta1` requires a conversion webhook and `StorageVersionMigration`, which are
deferred to a subsequent prompt within Phase 10.

**Schema:** The `v1beta1` schema is **structurally identical** to `v1alpha1`. No
fields were added, removed, or renamed. The API server handles conversion by
changing only the `apiVersion` field.

---

## 2. ResumableTrainingJob

### 2.1 User-Authored Fields (spec)

| Field | Type | Required | Phase | Notes |
|-------|------|----------|-------|-------|
| `suspend` | `*bool` | no | 1 | Kueue admission gate |
| `queueName` | `string` | yes | 1 | Target Kueue queue |
| `workloadPriorityClassName` | `string` | yes | 1 | Kueue priority class |
| `identity.image` | `string` | yes | 0 | Resume-compatibility key |
| `identity.codeVersion` | `string` | yes | 0 | Resume-compatibility key |
| `identity.worldSize` | `int32` | yes | 0 | Resume-compatibility key |
| `identity.gpuShape` | `string` | yes | 0 | Resume-compatibility key |
| `runtime.mode` | `RuntimeMode` | yes | 1 | DDP or FSDP |
| `runtime.optimizerMode` | `string` | yes | 1 | |
| `runtime.shardingMode` | `string` | yes | 1 | |
| `runtime.template` | `JobSetTemplate` | yes | 1 | Embedded JobSet spec |
| `checkpoint.storageURI` | `string` | yes | 0 | S3/GCS prefix |
| `checkpoint.interval` | `duration` | yes | 0 | |
| `checkpoint.freshnessBudget` | `duration` | yes | 0 | |
| `checkpoint.maxDrainTime` | `duration` | yes | 0 | |
| `checkpoint.safePointMode` | `SafePointMode` | no | 1 | Locked to StepBoundary |
| `resume.sourcePolicy` | `ResumeSourcePolicy` | no | 1 | Locked to LatestCompatibleComplete |
| `resume.maxResumeRetries` | `int32` | yes | 1 | |
| `resume.allowWorldSizeChange` | `bool` | no | 3 | Enables DCP resharding |
| `parallelism.preferredCount` | `int32` | no | 3 | |
| `parallelism.minCount` | `*int32` | no | 3 | |
| `parallelism.podSetName` | `string` | no | 3 | |
| `parallelism.enablePartialAdmission` | `bool` | no | 3 | **EXPERIMENTAL** |
| `topology.mode` | `TopologyMode` | cond | 4 | |
| `topology.topologyLevel` | `string` | cond | 4 | |
| `topology.leaderWorkerColocation` | `bool` | no | 4 | |
| `priorityPolicyRef.name` | `string` | cond | 5 | Ref to CPP |
| `managedBy` | `string` | no | 6 | Immutable; MultiKueue |
| `devices.mode` | `DeviceMode` | cond | 8 | **EXPERIMENTAL** |
| `devices.claims[]` | `[]DeviceClaimSpec` | cond | 8 | **EXPERIMENTAL** |
| `elasticity.mode` | `ElasticityMode` | cond | 9 | **EXPERIMENTAL** |
| `elasticity.targetWorkerCount` | `*int32` | no | 9 | **EXPERIMENTAL** |
| `elasticity.inPlaceShrinkPolicy` | `InPlaceShrinkPolicy` | no | 9 | **EXPERIMENTAL** |
| `elasticity.reclaimMode` | `ReclaimMode` | no | 9 | **EXPERIMENTAL** |
| `control.desiredState` | `DesiredState` | no | 0 | Running or Paused |

### 2.2 Controller-Authored Status

| Field | Type | Phase | Notes |
|-------|------|-------|-------|
| `phase` | `ResumableTrainingJobPhase` | 1 | 11 phases |
| `conditions` | `[]metav1.Condition` | 1 | Standard conditions |
| `workloadReference` | `*WorkloadReference` | 2 | Kueue Workload ref |
| `admittedClusterQueue` | `string` | 2 | |
| `currentSuspension` | `*SuspensionStatus` | 1 | |
| `currentRunAttempt` | `int32` | 1 | |
| `pauseRequestID` | `string` | 1 | |
| `activeJobSetName` | `string` | 1 | |
| `activeControlConfigMapName` | `string` | 1 | |
| `selectedCheckpoint` | `*CheckpointReference` | 0 | |
| `lastCompletedCheckpoint` | `*CheckpointReference` | 0 | |
| `transitionTimestamps` | `TransitionTimestamps` | 1 | 13 timestamps |
| `reason` | `string` | 1 | |
| `message` | `string` | 1 | |
| `observedGeneration` | `int64` | 1 | |
| `admission` | `*AdmissionStatus` | 2 | |
| `restore` | `*RestoreStatus` | 0 | |
| `launchReadiness` | `*LaunchReadinessStatus` | 4 | |
| `topology` | `*TopologyStatus` | 4 | |
| `effectiveLaunchShape` | `*EffectiveLaunchShape` | 3 | |
| `priorityShaping` | `*PriorityShapingStatus` | 5 | |
| `launchGate` | `*LaunchGateStatus` | 4 | |
| `provisioning` | `*ProvisioningStatus` | 6 | |
| `startupRecovery` | `*StartupRecoveryStatus` | 2 | |
| `capacity` | `*CapacityStatus` | 6 | |
| `multiCluster` | `*MultiClusterStatus` | 6 | |
| `devices` | `*DeviceStatus` | 8 | **EXPERIMENTAL** |
| `elasticity` | `*ElasticityStatus` | 9 | **EXPERIMENTAL** |

### 2.3 Experimental Fields

These fields are present in v1beta1 but documented as **EXPERIMENTAL** and may
change without a deprecation period:

- `spec.parallelism.enablePartialAdmission` - Requires operator flag `--enable-experimental-partial-admission`
- `spec.devices` - DRA device allocation (entire sub-tree)
- `spec.elasticity` - Manual target-based resize (entire sub-tree)
- `status.devices` - DRA device status (entire sub-tree)
- `status.elasticity` - Elasticity status (entire sub-tree)

### 2.4 Deprecated or Alpha-Only Fields

No fields are deprecated or restricted to alpha-only. All v1alpha1 fields are
present in v1beta1 with identical semantics.

### 2.5 Field Renames or Cleanups

None. The v1beta1 schema is a direct copy of v1alpha1.

---

## 3. CheckpointPriorityPolicy

### 3.1 User-Authored Fields (spec)

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `checkpointFreshnessTarget` | `duration` | yes | Max checkpoint age before preemptible |
| `startupProtectionWindow` | `duration` | yes | Protection after start/resume |
| `minRuntimeBetweenYields` | `duration` | yes | Anti-thrashing floor |
| `maxYieldsPerWindow` | `int32` | no | Yield counting budget |
| `yieldWindow` | `*duration` | cond | Required when maxYieldsPerWindow > 0 |
| `failOpenOnTelemetryLoss` | `*bool` | no | Default: true |
| `failOpenOnCheckpointStoreErrors` | `*bool` | no | Default: false |
| `protectedBoost` | `*int32` | no | Default: 0 |
| `cooldownBoost` | `*int32` | no | Default: 0 |
| `staleCheckpointBoost` | `*int32` | no | Default: 0 |
| `preemptibleOffset` | `*int32` | no | Default: 0 (negative allowed) |
| `minEffectivePriority` | `*int32` | no | Floor for effective priority |
| `maxEffectivePriority` | `*int32` | no | Ceiling for effective priority |

### 3.2 Controller-Authored Status

| Field | Type | Notes |
|-------|------|-------|
| `conditions` | `[]metav1.Condition` | Standard conditions |

### 3.3 Experimental / Deprecated Fields

None. All fields are stable.

---

## 4. ResumeReadinessPolicy

### 4.1 User-Authored Fields (spec)

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `requireCompleteCheckpoint` | `*bool` | no | Default: true |
| `maxCheckpointAge` | `*duration` | no | Zero = no limit |
| `failurePolicy` | `FailurePolicy` | no | Default: FailClosed |
| `allowInitialLaunchWithoutCheckpoint` | `*bool` | no | Default: true |

### 4.2 Controller-Authored Status

| Field | Type | Notes |
|-------|------|-------|
| `conditions` | `[]metav1.Condition` | Standard conditions |

### 4.3 Experimental / Deprecated Fields

None. All fields are stable.

---

## 5. Conversion Strategy

### 5.1 Current State (This Prompt)

- Both `v1alpha1` and `v1beta1` are served
- `v1alpha1` is the storage version
- The CRDs use `conversion.strategy: None` (default)
- The API server handles apiVersion-field-only conversion automatically
- No conversion webhook is deployed

### 5.2 Next Steps (Subsequent Prompts)

1. Wire a conversion webhook handler in the operator (port 9443)
2. Update CRDs to `conversion.strategy: Webhook`
3. Switch storage version to `v1beta1`
4. Run `StorageVersionMigration` to migrate etcd objects
5. Write upgrade/rollback runbooks

---

## 6. Test Coverage

| Test | File | Coverage |
|------|------|----------|
| Scheme registration (all 6 GVKs) | `types_test.go` | Confirms types register in v1beta1 scheme |
| RTJ round-trip construction | `types_test.go` | Full spec+status JSON marshal/unmarshal |
| CPP round-trip construction | `types_test.go` | Spec JSON marshal/unmarshal |
| RRP round-trip construction | `types_test.go` | Spec JSON marshal/unmarshal |
| RTJ defaults | `types_test.go` | All default values applied correctly |
| CPP defaults | `types_test.go` | All default values applied correctly |
| RRP defaults | `types_test.go` | All default values applied correctly |
| Backward-compatible decoding (RTJ) | `types_test.go` | v1alpha1-shaped JSON into v1beta1 types |
| Backward-compatible decoding (CPP) | `types_test.go` | v1alpha1-shaped JSON into v1beta1 types |
| Backward-compatible decoding (RRP) | `types_test.go` | v1alpha1-shaped JSON into v1beta1 types |
| JSON field parity (RTJ) | `schema_parity_test.go` | Identical JSON keys between versions |
| JSON field parity (CPP) | `schema_parity_test.go` | Identical JSON keys between versions |
| JSON field parity (RRP) | `schema_parity_test.go` | Identical JSON keys between versions |
| Default parity (RTJ) | `schema_parity_test.go` | Identical defaults between versions |
| Default parity (CPP) | `schema_parity_test.go` | Identical defaults between versions |
| Default parity (RRP) | `schema_parity_test.go` | Identical defaults between versions |
| Enum parity (phases) | `types_test.go` | 11 RTJ phases match |
| Enum parity (preemption states) | `types_test.go` | 4 states match |
| Enum parity (elasticity) | `types_test.go` | Modes + resize states match |
| Constant parity | `types_test.go` | Key constants match |
