package v1beta1_test

import (
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1beta1 "github.com/example/checkpoint-native-preemption-controller/api/v1beta1"
)

// ---------------------------------------------------------------------------
// Scheme registration
// ---------------------------------------------------------------------------

func TestGroupVersion(t *testing.T) {
	want := schema.GroupVersion{
		Group:   "training.checkpoint.example.io",
		Version: "v1beta1",
	}
	if v1beta1.GroupVersion != want {
		t.Errorf("GroupVersion = %v, want %v", v1beta1.GroupVersion, want)
	}
}

func TestAddToScheme(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	gvks := []schema.GroupVersionKind{
		v1beta1.GroupVersion.WithKind("ResumableTrainingJob"),
		v1beta1.GroupVersion.WithKind("ResumableTrainingJobList"),
		v1beta1.GroupVersion.WithKind("CheckpointPriorityPolicy"),
		v1beta1.GroupVersion.WithKind("CheckpointPriorityPolicyList"),
		v1beta1.GroupVersion.WithKind("ResumeReadinessPolicy"),
		v1beta1.GroupVersion.WithKind("ResumeReadinessPolicyList"),
	}

	for _, gvk := range gvks {
		obj, err := s.New(gvk)
		if err != nil {
			t.Errorf("scheme.New(%v): %v", gvk, err)
			continue
		}
		if obj == nil {
			t.Errorf("scheme.New(%v) returned nil", gvk)
		}
	}
}

// ---------------------------------------------------------------------------
// Round-trip object construction: ResumableTrainingJob
// ---------------------------------------------------------------------------

func TestRTJRoundTrip(t *testing.T) {
	now := metav1.Now()
	rtj := &v1beta1.ResumableTrainingJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "training.checkpoint.example.io/v1beta1",
			Kind:       "ResumableTrainingJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-training-job",
			Namespace: "default",
		},
		Spec: v1beta1.ResumableTrainingJobSpec{
			QueueName:                 "team-a",
			WorkloadPriorityClassName: "high-priority",
			Identity: v1beta1.ResumableTrainingJobIdentity{
				Image:       "nvcr.io/nvidia/pytorch:24.01-py3",
				CodeVersion: "v1.2.3",
				WorldSize:   8,
				GPUShape:    "nvidia-a100-80gb",
			},
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Mode:          v1beta1.RuntimeModeFSDP,
				OptimizerMode: "AdamW",
				ShardingMode:  "FULL_SHARD",
				Template: v1beta1.JobSetTemplate{
					APIVersion: "jobset.x-k8s.io/v1alpha2",
					Kind:       "JobSet",
					Spec:       runtime.RawExtension{Raw: []byte(`{"replicatedJobs":[]}`)},
				},
			},
			Checkpoint: v1beta1.CheckpointPolicy{
				StorageURI:      "s3://my-bucket/checkpoints",
				Interval:        metav1.Duration{Duration: 30 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 1 * time.Hour},
				MaxDrainTime:    metav1.Duration{Duration: 5 * time.Minute},
				SafePointMode:   v1beta1.SafePointModeStepBoundary,
			},
			Resume: v1beta1.ResumePolicy{
				SourcePolicy:     v1beta1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3,
			},
			Parallelism: &v1beta1.ParallelismSpec{
				PreferredCount: 8,
				PodSetName:     "workers",
			},
			Topology: &v1beta1.TopologySpec{
				Mode:          v1beta1.TopologyModeRequired,
				TopologyLevel: "topology.kubernetes.io/zone",
			},
			PriorityPolicyRef: &v1beta1.PriorityPolicyReference{
				Name: "default-priority-policy",
			},
			ManagedBy: "kueue.x-k8s.io/multikueue",
			Devices: &v1beta1.DeviceSpec{
				Mode: v1beta1.DeviceModeDRA,
				Claims: []v1beta1.DeviceClaimSpec{
					{
						Name:       "gpu",
						Containers: []string{"trainer"},
						Request: v1beta1.DeviceRequestSpec{
							DeviceClassName: "nvidia.com/a100",
							Count:           8,
						},
					},
				},
			},
			Elasticity: &v1beta1.ElasticitySpec{
				Mode:                v1beta1.ElasticityModeManual,
				InPlaceShrinkPolicy: v1beta1.InPlaceShrinkPolicyIfSupported,
				ReclaimMode:         v1beta1.ReclaimModeReclaimablePods,
			},
			Control: &v1beta1.ControlSpec{
				DesiredState: v1beta1.DesiredStateRunning,
			},
		},
		Status: v1beta1.ResumableTrainingJobStatus{
			Phase:            v1beta1.PhaseRunning,
			CurrentRunAttempt: 2,
			Admission: &v1beta1.AdmissionStatus{
				AdmittedWorkerCount:  8,
				PreferredWorkerCount: 8,
				ActiveWorkerCount:    8,
			},
			PriorityShaping: &v1beta1.PriorityShapingStatus{
				BasePriority:      100,
				EffectivePriority: 100,
				PreemptionState:   v1beta1.PreemptionStateActive,
			},
			LaunchGate: &v1beta1.LaunchGateStatus{
				State: v1beta1.LaunchGateOpen,
			},
			MultiCluster: &v1beta1.MultiClusterStatus{
				DispatchPhase:    v1beta1.DispatchPhaseActive,
				ExecutionCluster: "worker-1",
			},
			Devices: &v1beta1.DeviceStatus{
				DeviceMode:           v1beta1.DeviceModeDRA,
				ClaimAllocationState: v1beta1.ClaimAllocationAllocated,
			},
			Elasticity: &v1beta1.ElasticityStatus{
				ResizeState:          v1beta1.ResizeStateIdle,
				CurrentExecutionMode: v1beta1.ExecutionModeElastic,
			},
			TransitionTimestamps: v1beta1.TransitionTimestamps{
				RunningAt: &now,
			},
		},
	}

	// Marshal and unmarshal to verify round-trip.
	data, err := json.Marshal(rtj)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	got := &v1beta1.ResumableTrainingJob{}
	if err := json.Unmarshal(data, got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Verify key fields survived the round-trip.
	if got.APIVersion != "training.checkpoint.example.io/v1beta1" {
		t.Errorf("APIVersion = %q, want v1beta1", got.APIVersion)
	}
	if got.Spec.QueueName != "team-a" {
		t.Errorf("QueueName = %q, want team-a", got.Spec.QueueName)
	}
	if got.Spec.Identity.WorldSize != 8 {
		t.Errorf("WorldSize = %d, want 8", got.Spec.Identity.WorldSize)
	}
	if got.Spec.Runtime.Mode != v1beta1.RuntimeModeFSDP {
		t.Errorf("RuntimeMode = %v, want FSDP", got.Spec.Runtime.Mode)
	}
	if got.Spec.Checkpoint.StorageURI != "s3://my-bucket/checkpoints" {
		t.Errorf("StorageURI = %q, want s3://my-bucket/checkpoints", got.Spec.Checkpoint.StorageURI)
	}
	if got.Status.Phase != v1beta1.PhaseRunning {
		t.Errorf("Phase = %v, want Running", got.Status.Phase)
	}
	if got.Status.CurrentRunAttempt != 2 {
		t.Errorf("CurrentRunAttempt = %d, want 2", got.Status.CurrentRunAttempt)
	}
	if got.Spec.ManagedBy != "kueue.x-k8s.io/multikueue" {
		t.Errorf("ManagedBy = %q, want kueue.x-k8s.io/multikueue", got.Spec.ManagedBy)
	}
	if got.Spec.Devices == nil || got.Spec.Devices.Mode != v1beta1.DeviceModeDRA {
		t.Errorf("Devices.Mode = %v, want DRA", got.Spec.Devices)
	}
	if got.Spec.Elasticity == nil || got.Spec.Elasticity.Mode != v1beta1.ElasticityModeManual {
		t.Errorf("Elasticity.Mode = %v, want Manual", got.Spec.Elasticity)
	}
	if got.Status.MultiCluster == nil || got.Status.MultiCluster.ExecutionCluster != "worker-1" {
		t.Errorf("MultiCluster.ExecutionCluster missing or wrong")
	}
	if got.Status.Elasticity == nil || got.Status.Elasticity.CurrentExecutionMode != v1beta1.ExecutionModeElastic {
		t.Errorf("Elasticity.CurrentExecutionMode = %v, want Elastic", got.Status.Elasticity)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: CheckpointPriorityPolicy
// ---------------------------------------------------------------------------

func TestCPPRoundTrip(t *testing.T) {
	protectedBoost := int32(50)
	failOpen := true
	cpp := &v1beta1.CheckpointPriorityPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "training.checkpoint.example.io/v1beta1",
			Kind:       "CheckpointPriorityPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-policy",
		},
		Spec: v1beta1.CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget: metav1.Duration{Duration: 1 * time.Hour},
			StartupProtectionWindow:   metav1.Duration{Duration: 15 * time.Minute},
			MinRuntimeBetweenYields:   metav1.Duration{Duration: 10 * time.Minute},
			ProtectedBoost:            &protectedBoost,
			FailOpenOnTelemetryLoss:   &failOpen,
		},
	}

	data, err := json.Marshal(cpp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	got := &v1beta1.CheckpointPriorityPolicy{}
	if err := json.Unmarshal(data, got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Spec.CheckpointFreshnessTarget.Duration != 1*time.Hour {
		t.Errorf("CheckpointFreshnessTarget = %v, want 1h", got.Spec.CheckpointFreshnessTarget)
	}
	if got.Spec.ProtectedBoost == nil || *got.Spec.ProtectedBoost != 50 {
		t.Errorf("ProtectedBoost = %v, want 50", got.Spec.ProtectedBoost)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: ResumeReadinessPolicy
// ---------------------------------------------------------------------------

func TestRRPRoundTrip(t *testing.T) {
	reqComplete := true
	allowInitial := false
	rrp := &v1beta1.ResumeReadinessPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "training.checkpoint.example.io/v1beta1",
			Kind:       "ResumeReadinessPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "strict-policy",
		},
		Spec: v1beta1.ResumeReadinessPolicySpec{
			RequireCompleteCheckpoint:           &reqComplete,
			FailurePolicy:                       v1beta1.FailurePolicyFailClosed,
			AllowInitialLaunchWithoutCheckpoint: &allowInitial,
		},
	}

	data, err := json.Marshal(rrp)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	got := &v1beta1.ResumeReadinessPolicy{}
	if err := json.Unmarshal(data, got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Spec.FailurePolicy != v1beta1.FailurePolicyFailClosed {
		t.Errorf("FailurePolicy = %v, want FailClosed", got.Spec.FailurePolicy)
	}
	if got.Spec.AllowInitialLaunchWithoutCheckpoint == nil || *got.Spec.AllowInitialLaunchWithoutCheckpoint != false {
		t.Errorf("AllowInitialLaunchWithoutCheckpoint = %v, want false", got.Spec.AllowInitialLaunchWithoutCheckpoint)
	}
}

// ---------------------------------------------------------------------------
// Default application
// ---------------------------------------------------------------------------

func TestRTJDefaults(t *testing.T) {
	rtj := &v1beta1.ResumableTrainingJob{
		Spec: v1beta1.ResumableTrainingJobSpec{
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Template: v1beta1.JobSetTemplate{
					Spec: runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
		},
	}

	rtj.Default()

	if rtj.Spec.Control == nil {
		t.Fatal("Control should be defaulted")
	}
	if rtj.Spec.Control.DesiredState != v1beta1.DesiredStateRunning {
		t.Errorf("DesiredState = %v, want Running", rtj.Spec.Control.DesiredState)
	}
	if rtj.Spec.Checkpoint.SafePointMode != v1beta1.SafePointModeStepBoundary {
		t.Errorf("SafePointMode = %v, want StepBoundary", rtj.Spec.Checkpoint.SafePointMode)
	}
	if rtj.Spec.Resume.SourcePolicy != v1beta1.ResumeSourcePolicyLatestCompatibleComplete {
		t.Errorf("SourcePolicy = %v, want LatestCompatibleComplete", rtj.Spec.Resume.SourcePolicy)
	}
	if rtj.Spec.Resume.MaxResumeRetries != 3 {
		t.Errorf("MaxResumeRetries = %d, want 3", rtj.Spec.Resume.MaxResumeRetries)
	}
	if rtj.Spec.Runtime.Template.APIVersion != v1beta1.DefaultJobSetAPIVersion {
		t.Errorf("Template.APIVersion = %q, want %q", rtj.Spec.Runtime.Template.APIVersion, v1beta1.DefaultJobSetAPIVersion)
	}
	if rtj.Spec.Runtime.Template.Kind != v1beta1.DefaultJobSetKind {
		t.Errorf("Template.Kind = %q, want %q", rtj.Spec.Runtime.Template.Kind, v1beta1.DefaultJobSetKind)
	}
}

func TestRTJDefaultsElasticity(t *testing.T) {
	rtj := &v1beta1.ResumableTrainingJob{
		Spec: v1beta1.ResumableTrainingJobSpec{
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Template: v1beta1.JobSetTemplate{
					Spec: runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
			Elasticity: &v1beta1.ElasticitySpec{
				Mode: v1beta1.ElasticityModeManual,
			},
		},
	}

	rtj.Default()

	if rtj.Spec.Elasticity.InPlaceShrinkPolicy != v1beta1.InPlaceShrinkPolicyIfSupported {
		t.Errorf("InPlaceShrinkPolicy = %v, want IfSupported", rtj.Spec.Elasticity.InPlaceShrinkPolicy)
	}
	if rtj.Spec.Elasticity.ReclaimMode != v1beta1.ReclaimModeReclaimablePods {
		t.Errorf("ReclaimMode = %v, want ReclaimablePods", rtj.Spec.Elasticity.ReclaimMode)
	}
}

func TestRTJDefaultsDevices(t *testing.T) {
	rtj := &v1beta1.ResumableTrainingJob{
		Spec: v1beta1.ResumableTrainingJobSpec{
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Template: v1beta1.JobSetTemplate{
					Spec: runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
			Devices: &v1beta1.DeviceSpec{
				Claims: []v1beta1.DeviceClaimSpec{
					{
						Name:       "gpu",
						Containers: []string{"trainer"},
						Request: v1beta1.DeviceRequestSpec{
							DeviceClassName: "nvidia.com/a100",
						},
					},
				},
			},
		},
	}

	rtj.Default()

	if rtj.Spec.Devices.Mode != v1beta1.DeviceModeDisabled {
		t.Errorf("Devices.Mode = %v, want Disabled (default)", rtj.Spec.Devices.Mode)
	}
	if rtj.Spec.Devices.Claims[0].Request.Count != 1 {
		t.Errorf("Claims[0].Request.Count = %d, want 1 (default)", rtj.Spec.Devices.Claims[0].Request.Count)
	}
}

func TestCPPDefaults(t *testing.T) {
	cpp := &v1beta1.CheckpointPriorityPolicy{
		Spec: v1beta1.CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget: metav1.Duration{Duration: 1 * time.Hour},
			StartupProtectionWindow:   metav1.Duration{Duration: 15 * time.Minute},
			MinRuntimeBetweenYields:   metav1.Duration{Duration: 10 * time.Minute},
		},
	}

	cpp.Default()

	if cpp.Spec.FailOpenOnTelemetryLoss == nil || !*cpp.Spec.FailOpenOnTelemetryLoss {
		t.Errorf("FailOpenOnTelemetryLoss = %v, want true", cpp.Spec.FailOpenOnTelemetryLoss)
	}
	if cpp.Spec.FailOpenOnCheckpointStoreErrors == nil || *cpp.Spec.FailOpenOnCheckpointStoreErrors {
		t.Errorf("FailOpenOnCheckpointStoreErrors = %v, want false", cpp.Spec.FailOpenOnCheckpointStoreErrors)
	}
	if cpp.Spec.ProtectedBoost == nil || *cpp.Spec.ProtectedBoost != 0 {
		t.Errorf("ProtectedBoost = %v, want 0", cpp.Spec.ProtectedBoost)
	}
	if cpp.Spec.CooldownBoost == nil || *cpp.Spec.CooldownBoost != 0 {
		t.Errorf("CooldownBoost = %v, want 0", cpp.Spec.CooldownBoost)
	}
	if cpp.Spec.StaleCheckpointBoost == nil || *cpp.Spec.StaleCheckpointBoost != 0 {
		t.Errorf("StaleCheckpointBoost = %v, want 0", cpp.Spec.StaleCheckpointBoost)
	}
	if cpp.Spec.PreemptibleOffset == nil || *cpp.Spec.PreemptibleOffset != 0 {
		t.Errorf("PreemptibleOffset = %v, want 0", cpp.Spec.PreemptibleOffset)
	}
}

func TestRRPDefaults(t *testing.T) {
	rrp := &v1beta1.ResumeReadinessPolicy{}

	rrp.Default()

	if rrp.Spec.RequireCompleteCheckpoint == nil || !*rrp.Spec.RequireCompleteCheckpoint {
		t.Errorf("RequireCompleteCheckpoint = %v, want true", rrp.Spec.RequireCompleteCheckpoint)
	}
	if rrp.Spec.FailurePolicy != v1beta1.FailurePolicyFailClosed {
		t.Errorf("FailurePolicy = %v, want FailClosed", rrp.Spec.FailurePolicy)
	}
	if rrp.Spec.AllowInitialLaunchWithoutCheckpoint == nil || !*rrp.Spec.AllowInitialLaunchWithoutCheckpoint {
		t.Errorf("AllowInitialLaunchWithoutCheckpoint = %v, want true", rrp.Spec.AllowInitialLaunchWithoutCheckpoint)
	}
}

// ---------------------------------------------------------------------------
// Backward-compatible decoding: v1alpha1-shaped JSON into v1beta1 types
//
// Since v1beta1 schema is identical to v1alpha1, a JSON payload that was valid
// for v1alpha1 should decode cleanly into v1beta1 types (ignoring apiVersion).
// ---------------------------------------------------------------------------

func TestBackwardCompatibleDecode_RTJ(t *testing.T) {
	// A minimal v1alpha1-shaped JSON.
	alpha1JSON := `{
		"apiVersion": "training.checkpoint.example.io/v1alpha1",
		"kind": "ResumableTrainingJob",
		"metadata": {"name": "test", "namespace": "default"},
		"spec": {
			"queueName": "team-a",
			"workloadPriorityClassName": "high",
			"identity": {
				"image": "nvcr.io/test",
				"codeVersion": "v1.0",
				"worldSize": 4,
				"gpuShape": "nvidia-a100-80gb"
			},
			"runtime": {
				"mode": "FSDP",
				"optimizerMode": "AdamW",
				"shardingMode": "FULL_SHARD",
				"template": {
					"apiVersion": "jobset.x-k8s.io/v1alpha2",
					"kind": "JobSet",
					"spec": {"replicatedJobs": []}
				}
			},
			"checkpoint": {
				"storageURI": "s3://bucket/ckpt",
				"interval": "30m",
				"freshnessBudget": "1h",
				"maxDrainTime": "5m",
				"safePointMode": "StepBoundary"
			},
			"resume": {
				"sourcePolicy": "LatestCompatibleComplete",
				"maxResumeRetries": 3
			},
			"control": {"desiredState": "Running"}
		},
		"status": {
			"phase": "Running",
			"currentRunAttempt": 1
		}
	}`

	got := &v1beta1.ResumableTrainingJob{}
	if err := json.Unmarshal([]byte(alpha1JSON), got); err != nil {
		t.Fatalf("Unmarshal v1alpha1-shaped JSON into v1beta1: %v", err)
	}

	if got.Spec.QueueName != "team-a" {
		t.Errorf("QueueName = %q, want team-a", got.Spec.QueueName)
	}
	if got.Spec.Identity.WorldSize != 4 {
		t.Errorf("WorldSize = %d, want 4", got.Spec.Identity.WorldSize)
	}
	if got.Status.Phase != v1beta1.PhaseRunning {
		t.Errorf("Phase = %v, want Running", got.Status.Phase)
	}
	if got.Spec.Resume.MaxResumeRetries != 3 {
		t.Errorf("MaxResumeRetries = %d, want 3", got.Spec.Resume.MaxResumeRetries)
	}
}

func TestBackwardCompatibleDecode_CPP(t *testing.T) {
	alpha1JSON := `{
		"apiVersion": "training.checkpoint.example.io/v1alpha1",
		"kind": "CheckpointPriorityPolicy",
		"metadata": {"name": "test-policy"},
		"spec": {
			"checkpointFreshnessTarget": "1h",
			"startupProtectionWindow": "15m",
			"minRuntimeBetweenYields": "10m",
			"failOpenOnTelemetryLoss": true,
			"protectedBoost": 50
		}
	}`

	got := &v1beta1.CheckpointPriorityPolicy{}
	if err := json.Unmarshal([]byte(alpha1JSON), got); err != nil {
		t.Fatalf("Unmarshal v1alpha1-shaped CPP JSON into v1beta1: %v", err)
	}

	if got.Spec.CheckpointFreshnessTarget.Duration != 1*time.Hour {
		t.Errorf("CheckpointFreshnessTarget = %v, want 1h", got.Spec.CheckpointFreshnessTarget)
	}
	if got.Spec.ProtectedBoost == nil || *got.Spec.ProtectedBoost != 50 {
		t.Errorf("ProtectedBoost = %v, want 50", got.Spec.ProtectedBoost)
	}
}

func TestBackwardCompatibleDecode_RRP(t *testing.T) {
	alpha1JSON := `{
		"apiVersion": "training.checkpoint.example.io/v1alpha1",
		"kind": "ResumeReadinessPolicy",
		"metadata": {"name": "strict"},
		"spec": {
			"requireCompleteCheckpoint": true,
			"failurePolicy": "FailClosed",
			"allowInitialLaunchWithoutCheckpoint": false
		}
	}`

	got := &v1beta1.ResumeReadinessPolicy{}
	if err := json.Unmarshal([]byte(alpha1JSON), got); err != nil {
		t.Fatalf("Unmarshal v1alpha1-shaped RRP JSON into v1beta1: %v", err)
	}

	if got.Spec.FailurePolicy != v1beta1.FailurePolicyFailClosed {
		t.Errorf("FailurePolicy = %v, want FailClosed", got.Spec.FailurePolicy)
	}
	if got.Spec.AllowInitialLaunchWithoutCheckpoint == nil || *got.Spec.AllowInitialLaunchWithoutCheckpoint {
		t.Errorf("AllowInitialLaunchWithoutCheckpoint = %v, want false", got.Spec.AllowInitialLaunchWithoutCheckpoint)
	}
}

// ---------------------------------------------------------------------------
// Schema parity: verify v1beta1 enum values match the expected set
// ---------------------------------------------------------------------------

func TestRTJPhaseEnumParity(t *testing.T) {
	// The exact set of phases must match between v1alpha1 and v1beta1.
	phases := []v1beta1.ResumableTrainingJobPhase{
		v1beta1.PhasePending,
		v1beta1.PhaseQueued,
		v1beta1.PhaseAdmitted,
		v1beta1.PhaseStarting,
		v1beta1.PhaseRunning,
		v1beta1.PhaseYieldRequested,
		v1beta1.PhaseDraining,
		v1beta1.PhasePaused,
		v1beta1.PhaseRestoring,
		v1beta1.PhaseSucceeded,
		v1beta1.PhaseFailed,
	}
	expected := map[string]bool{
		"Pending": true, "Queued": true, "Admitted": true,
		"Starting": true, "Running": true, "YieldRequested": true,
		"Draining": true, "Paused": true, "Restoring": true,
		"Succeeded": true, "Failed": true,
	}
	if len(phases) != len(expected) {
		t.Errorf("phase count = %d, want %d", len(phases), len(expected))
	}
	for _, p := range phases {
		if !expected[string(p)] {
			t.Errorf("unexpected phase %q", p)
		}
	}
}

func TestPreemptionStateEnumParity(t *testing.T) {
	states := []v1beta1.PreemptionState{
		v1beta1.PreemptionStateProtected,
		v1beta1.PreemptionStateActive,
		v1beta1.PreemptionStateCooldown,
		v1beta1.PreemptionStatePreemptible,
	}
	if len(states) != 4 {
		t.Errorf("preemption state count = %d, want 4", len(states))
	}
}

func TestElasticityEnumParity(t *testing.T) {
	modes := []v1beta1.ElasticityMode{
		v1beta1.ElasticityModeDisabled,
		v1beta1.ElasticityModeManual,
	}
	if len(modes) != 2 {
		t.Errorf("elasticity mode count = %d, want 2", len(modes))
	}

	resizeStates := []v1beta1.ResizeState{
		v1beta1.ResizeStateIdle,
		v1beta1.ResizeStatePending,
		v1beta1.ResizeStateInProgress,
		v1beta1.ResizeStateBlocked,
		v1beta1.ResizeStateCompleted,
		v1beta1.ResizeStateFailed,
	}
	if len(resizeStates) != 6 {
		t.Errorf("resize state count = %d, want 6", len(resizeStates))
	}
}

func TestConstantParity(t *testing.T) {
	// Verify constants match expected values (same as v1alpha1).
	if v1beta1.DefaultJobSetAPIVersion != "jobset.x-k8s.io/v1alpha2" {
		t.Errorf("DefaultJobSetAPIVersion = %q", v1beta1.DefaultJobSetAPIVersion)
	}
	if v1beta1.DefaultMaxResumeRetries != 3 {
		t.Errorf("DefaultMaxResumeRetries = %d", v1beta1.DefaultMaxResumeRetries)
	}
	if v1beta1.MultiKueueControllerName != "kueue.x-k8s.io/multikueue" {
		t.Errorf("MultiKueueControllerName = %q", v1beta1.MultiKueueControllerName)
	}
	if v1beta1.MaxManagedByLength != 256 {
		t.Errorf("MaxManagedByLength = %d", v1beta1.MaxManagedByLength)
	}
}
