package integration

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	v1beta1 "github.com/example/checkpoint-native-preemption-controller/api/v1beta1"
)

// TestRTJRoundTrip_Alpha1_Beta1_Alpha1 proves that a fully-populated v1alpha1
// ResumableTrainingJob survives conversion to v1beta1 and back without data loss.
func TestRTJRoundTrip_Alpha1_Beta1_Alpha1(t *testing.T) {
	src := fullyPopulatedAlphaRTJ()

	hub := &v1beta1.ResumableTrainingJob{}
	if err := src.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	dst := &v1alpha1.ResumableTrainingJob{}
	if err := dst.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	assertRTJEqual(t, src, dst)
}

// TestRTJRoundTrip_Beta1_Alpha1_Beta1 proves the reverse direction round-trip.
func TestRTJRoundTrip_Beta1_Alpha1_Beta1(t *testing.T) {
	src := fullyPopulatedBetaRTJ()

	spoke := &v1alpha1.ResumableTrainingJob{}
	if err := spoke.ConvertFrom(src); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	dst := &v1beta1.ResumableTrainingJob{}
	if err := spoke.ConvertTo(dst); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	assertBetaRTJEqual(t, src, dst)
}

// TestCPPRoundTrip proves that CheckpointPriorityPolicy converts losslessly.
func TestCPPRoundTrip(t *testing.T) {
	src := &v1alpha1.CheckpointPriorityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-cpp",
			Labels: map[string]string{"env": "test"},
		},
		Spec: v1alpha1.CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget:       metav1.Duration{Duration: 5 * time.Minute},
			StartupProtectionWindow:         metav1.Duration{Duration: 10 * time.Minute},
			MinRuntimeBetweenYields:         metav1.Duration{Duration: 2 * time.Minute},
			MaxYieldsPerWindow:              3,
			YieldWindow:                     &metav1.Duration{Duration: 1 * time.Hour},
			FailOpenOnTelemetryLoss:         ptr.To(true),
			FailOpenOnCheckpointStoreErrors: ptr.To(false),
			ProtectedBoost:                  ptr.To[int32](100),
			CooldownBoost:                   ptr.To[int32](50),
			StaleCheckpointBoost:            ptr.To[int32](-30),
			PreemptibleOffset:               ptr.To[int32](-200),
			MinEffectivePriority:            ptr.To[int32](-500),
			MaxEffectivePriority:            ptr.To[int32](1000),
		},
		Status: v1alpha1.CheckpointPriorityPolicyStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Configured",
					Message:            "policy is active",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	hub := &v1beta1.CheckpointPriorityPolicy{}
	if err := src.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	dst := &v1alpha1.CheckpointPriorityPolicy{}
	if err := dst.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if dst.Name != src.Name {
		t.Errorf("Name mismatch: got %q, want %q", dst.Name, src.Name)
	}
	if dst.Spec.CheckpointFreshnessTarget != src.Spec.CheckpointFreshnessTarget {
		t.Errorf("CheckpointFreshnessTarget mismatch")
	}
	if dst.Spec.MaxYieldsPerWindow != src.Spec.MaxYieldsPerWindow {
		t.Errorf("MaxYieldsPerWindow mismatch: got %d, want %d",
			dst.Spec.MaxYieldsPerWindow, src.Spec.MaxYieldsPerWindow)
	}
	if *dst.Spec.ProtectedBoost != *src.Spec.ProtectedBoost {
		t.Errorf("ProtectedBoost mismatch")
	}
	if *dst.Spec.PreemptibleOffset != *src.Spec.PreemptibleOffset {
		t.Errorf("PreemptibleOffset mismatch")
	}
	if len(dst.Status.Conditions) != len(src.Status.Conditions) {
		t.Errorf("Conditions length mismatch: got %d, want %d",
			len(dst.Status.Conditions), len(src.Status.Conditions))
	}
}

// TestRRPRoundTrip proves that ResumeReadinessPolicy converts losslessly.
func TestRRPRoundTrip(t *testing.T) {
	src := &v1alpha1.ResumeReadinessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-rrp"},
		Spec: v1alpha1.ResumeReadinessPolicySpec{
			RequireCompleteCheckpoint:           ptr.To(true),
			MaxCheckpointAge:                    &metav1.Duration{Duration: 30 * time.Minute},
			FailurePolicy:                       v1alpha1.FailurePolicyFailOpen,
			AllowInitialLaunchWithoutCheckpoint: ptr.To(false),
		},
		Status: v1alpha1.ResumeReadinessPolicyStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Configured",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	hub := &v1beta1.ResumeReadinessPolicy{}
	if err := src.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	dst := &v1alpha1.ResumeReadinessPolicy{}
	if err := dst.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if dst.Name != src.Name {
		t.Errorf("Name mismatch")
	}
	if *dst.Spec.RequireCompleteCheckpoint != *src.Spec.RequireCompleteCheckpoint {
		t.Errorf("RequireCompleteCheckpoint mismatch")
	}
	if dst.Spec.MaxCheckpointAge.Duration != src.Spec.MaxCheckpointAge.Duration {
		t.Errorf("MaxCheckpointAge mismatch")
	}
	if dst.Spec.FailurePolicy != src.Spec.FailurePolicy {
		t.Errorf("FailurePolicy mismatch: got %q, want %q",
			dst.Spec.FailurePolicy, src.Spec.FailurePolicy)
	}
	if *dst.Spec.AllowInitialLaunchWithoutCheckpoint != *src.Spec.AllowInitialLaunchWithoutCheckpoint {
		t.Errorf("AllowInitialLaunchWithoutCheckpoint mismatch")
	}
}

// TestConvertToWrongType verifies type-safety of conversion methods.
func TestConvertToWrongType(t *testing.T) {
	rtj := &v1alpha1.ResumableTrainingJob{}
	cppHub := &v1beta1.CheckpointPriorityPolicy{}
	if err := rtj.ConvertTo(cppHub); err == nil {
		t.Fatal("expected error when converting RTJ to CPP hub, got nil")
	}
}

// TestHubInterfaceSatisfied verifies that v1beta1 types implement conversion.Hub.
func TestHubInterfaceSatisfied(t *testing.T) {
	(&v1beta1.ResumableTrainingJob{}).Hub()
	(&v1beta1.CheckpointPriorityPolicy{}).Hub()
	(&v1beta1.ResumeReadinessPolicy{}).Hub()
}

// TestConvertibleInterfaceSatisfied verifies that v1alpha1 types implement conversion.Convertible.
func TestConvertibleInterfaceSatisfied(t *testing.T) {
	hub := &v1beta1.ResumableTrainingJob{}
	spoke := &v1alpha1.ResumableTrainingJob{}
	if err := spoke.ConvertTo(hub); err != nil {
		t.Fatalf("empty ConvertTo failed: %v", err)
	}
	if err := spoke.ConvertFrom(hub); err != nil {
		t.Fatalf("empty ConvertFrom failed: %v", err)
	}
}

// TestMinimalObjectRoundTrip verifies that a minimal object (zero-value spec/status)
// survives conversion. This simulates decoding old stored objects that may lack
// fields added in later phases.
func TestMinimalObjectRoundTrip(t *testing.T) {
	src := &v1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minimal",
			Namespace: "default",
		},
		Spec: v1alpha1.ResumableTrainingJobSpec{
			QueueName:                 "q",
			WorkloadPriorityClassName: "wpc",
			Identity: v1alpha1.ResumableTrainingJobIdentity{
				Image:     "img:latest",
				WorldSize: 2,
				GPUShape:  "nvidia-a100",
			},
			Runtime: v1alpha1.ResumableTrainingJobRuntime{
				Mode:     v1alpha1.RuntimeModeDDP,
				Template: v1alpha1.JobSetTemplate{},
			},
			Checkpoint: v1alpha1.CheckpointPolicy{
				StorageURI: "s3://bucket/ckpt",
			},
			Resume: v1alpha1.ResumePolicy{},
		},
	}

	hub := &v1beta1.ResumableTrainingJob{}
	if err := src.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	dst := &v1alpha1.ResumableTrainingJob{}
	if err := dst.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if dst.Name != src.Name {
		t.Errorf("Name mismatch: got %q, want %q", dst.Name, src.Name)
	}
	if dst.Spec.QueueName != src.Spec.QueueName {
		t.Errorf("QueueName mismatch")
	}
	if dst.Spec.Identity.Image != src.Spec.Identity.Image {
		t.Errorf("Identity.Image mismatch")
	}
}

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func fullyPopulatedAlphaRTJ() *v1alpha1.ResumableTrainingJob {
	now := metav1.Now()
	return &v1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "roundtrip-test",
			Namespace: "default",
			Labels:    map[string]string{"app": "training"},
			Annotations: map[string]string{"phase": "10"},
		},
		Spec: v1alpha1.ResumableTrainingJobSpec{
			Suspend:                   ptr.To(false),
			QueueName:                 "gpu-queue",
			WorkloadPriorityClassName: "high-priority",
			Identity: v1alpha1.ResumableTrainingJobIdentity{
				Image:       "training:v2",
				CodeVersion: "abc123",
				WorldSize:   8,
				GPUShape:    "nvidia-a100-80gb",
			},
			Runtime: v1alpha1.ResumableTrainingJobRuntime{
				Mode: v1alpha1.RuntimeModeFSDP,
				Template: v1alpha1.JobSetTemplate{
					APIVersion: "jobset.x-k8s.io/v1alpha2",
					Kind:       "JobSet",
					Spec:       runtime.RawExtension{Raw: []byte(`{"replicas":1}`)},
				},
			},
			Checkpoint: v1alpha1.CheckpointPolicy{
				StorageURI:      "s3://bucket/checkpoints",
				Interval:        metav1.Duration{Duration: 5 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 10 * time.Minute},
				MaxDrainTime:    metav1.Duration{Duration: 2 * time.Minute},
				SafePointMode:   v1alpha1.SafePointModeStepBoundary,
			},
			Resume: v1alpha1.ResumePolicy{
				SourcePolicy:         v1alpha1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries:     5,
				AllowWorldSizeChange: true,
			},
			Parallelism: &v1alpha1.ParallelismSpec{
				PreferredCount:         4,
				MinCount:               ptr.To[int32](2),
				PodSetName:             "workers",
				EnablePartialAdmission: true,
			},
			Topology: &v1alpha1.TopologySpec{
				Mode:          v1alpha1.TopologyModeRequired,
				TopologyLevel: "rack",
			},
			PriorityPolicyRef: &v1alpha1.PriorityPolicyReference{
				Name: "default-cpp",
			},
			Devices: &v1alpha1.DeviceSpec{
				Mode: v1alpha1.DeviceModeDRA,
				Claims: []v1alpha1.DeviceClaimSpec{
					{
						Name:       "gpu-claim",
						Containers: []string{"trainer"},
						Request: v1alpha1.DeviceRequestSpec{
							DeviceClassName: "gpu.nvidia.com",
							Count:           2,
						},
					},
				},
			},
			Elasticity: &v1alpha1.ElasticitySpec{
				Mode:                v1alpha1.ElasticityModeManual,
				TargetWorkerCount:   ptr.To[int32](4),
				InPlaceShrinkPolicy: v1alpha1.InPlaceShrinkPolicyIfSupported,
				ReclaimMode:         v1alpha1.ReclaimModeReclaimablePods,
			},
			Control: &v1alpha1.ControlSpec{
				DesiredState: v1alpha1.DesiredStateRunning,
			},
		},
		Status: v1alpha1.ResumableTrainingJobStatus{
			Phase:             v1alpha1.PhaseRunning,
			CurrentRunAttempt: 2,
			Admission: &v1alpha1.AdmissionStatus{
				AdmittedWorkerCount:  4,
				PreferredWorkerCount: 4,
				ActiveWorkerCount:    4,
				AdmittedFlavors:      map[string]string{"gpu": "nvidia-a100"},
			},
			Restore: &v1alpha1.RestoreStatus{
				LastCheckpointWorldSize: 8,
				LastRestoreWorldSize:    8,
				RestoreMode:             v1alpha1.RestoreModeSameSize,
			},
			LaunchReadiness: &v1alpha1.LaunchReadinessStatus{
				Ready: true,
			},
			PriorityShaping: &v1alpha1.PriorityShapingStatus{
				BasePriority:      1000,
				EffectivePriority: 900,
				PreemptionState:   v1alpha1.PreemptionStateActive,
			},
			Elasticity: &v1alpha1.ElasticityStatus{
				DesiredWorkerCount:   4,
				TargetWorkerCount:    4,
				ActiveWorkerCount:    4,
				AdmittedWorkerCount:  4,
				ResizeState:          v1alpha1.ResizeStateIdle,
				CurrentExecutionMode: v1alpha1.ExecutionModeElastic,
			},
			TransitionTimestamps: v1alpha1.TransitionTimestamps{
				QueuedAt:  &now,
				RunningAt: &now,
			},
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Running",
					LastTransitionTime: now,
				},
			},
		},
	}
}

func fullyPopulatedBetaRTJ() *v1beta1.ResumableTrainingJob {
	now := metav1.Now()
	return &v1beta1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "roundtrip-beta",
			Namespace: "default",
			Labels:    map[string]string{"app": "training"},
		},
		Spec: v1beta1.ResumableTrainingJobSpec{
			Suspend:                   ptr.To(false),
			QueueName:                 "gpu-queue",
			WorkloadPriorityClassName: "high-priority",
			Identity: v1beta1.ResumableTrainingJobIdentity{
				Image:       "training:v3",
				CodeVersion: "def456",
				WorldSize:   16,
				GPUShape:    "nvidia-h100",
			},
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Mode: v1beta1.RuntimeModeFSDP,
				Template: v1beta1.JobSetTemplate{
					APIVersion: "jobset.x-k8s.io/v1alpha2",
					Kind:       "JobSet",
					Spec:       runtime.RawExtension{Raw: []byte(`{"replicas":2}`)},
				},
			},
			Checkpoint: v1beta1.CheckpointPolicy{
				StorageURI:      "s3://bucket/v3",
				Interval:        metav1.Duration{Duration: 3 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 8 * time.Minute},
				SafePointMode:   v1beta1.SafePointModeStepBoundary,
			},
			Resume: v1beta1.ResumePolicy{
				SourcePolicy:     v1beta1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3,
			},
			Elasticity: &v1beta1.ElasticitySpec{
				Mode:              v1beta1.ElasticityModeManual,
				TargetWorkerCount: ptr.To[int32](8),
			},
			Control: &v1beta1.ControlSpec{
				DesiredState: v1beta1.DesiredStateRunning,
			},
		},
		Status: v1beta1.ResumableTrainingJobStatus{
			Phase:             v1beta1.PhaseRunning,
			CurrentRunAttempt: 1,
			Admission: &v1beta1.AdmissionStatus{
				AdmittedWorkerCount: 8,
				ActiveWorkerCount:   8,
			},
			Elasticity: &v1beta1.ElasticityStatus{
				DesiredWorkerCount:   8,
				ActiveWorkerCount:    8,
				ResizeState:          v1beta1.ResizeStateIdle,
				CurrentExecutionMode: v1beta1.ExecutionModeElastic,
			},
			TransitionTimestamps: v1beta1.TransitionTimestamps{
				RunningAt: &now,
			},
		},
	}
}

func assertRTJEqual(t *testing.T, src, dst *v1alpha1.ResumableTrainingJob) {
	t.Helper()

	// ObjectMeta
	if dst.Name != src.Name {
		t.Errorf("Name mismatch: got %q, want %q", dst.Name, src.Name)
	}
	if dst.Namespace != src.Namespace {
		t.Errorf("Namespace mismatch: got %q, want %q", dst.Namespace, src.Namespace)
	}

	// Spec core fields
	if *dst.Spec.Suspend != *src.Spec.Suspend {
		t.Errorf("Suspend mismatch")
	}
	if dst.Spec.QueueName != src.Spec.QueueName {
		t.Errorf("QueueName mismatch: got %q, want %q", dst.Spec.QueueName, src.Spec.QueueName)
	}
	if dst.Spec.Identity.WorldSize != src.Spec.Identity.WorldSize {
		t.Errorf("WorldSize mismatch: got %d, want %d",
			dst.Spec.Identity.WorldSize, src.Spec.Identity.WorldSize)
	}
	if dst.Spec.Identity.Image != src.Spec.Identity.Image {
		t.Errorf("Image mismatch")
	}
	if dst.Spec.Runtime.Mode != src.Spec.Runtime.Mode {
		t.Errorf("Runtime.Mode mismatch: got %q, want %q",
			dst.Spec.Runtime.Mode, src.Spec.Runtime.Mode)
	}
	if dst.Spec.Checkpoint.StorageURI != src.Spec.Checkpoint.StorageURI {
		t.Errorf("StorageURI mismatch")
	}
	if dst.Spec.Checkpoint.Interval != src.Spec.Checkpoint.Interval {
		t.Errorf("Interval mismatch")
	}
	if dst.Spec.Resume.SourcePolicy != src.Spec.Resume.SourcePolicy {
		t.Errorf("SourcePolicy mismatch")
	}
	if dst.Spec.Resume.MaxResumeRetries != src.Spec.Resume.MaxResumeRetries {
		t.Errorf("MaxResumeRetries mismatch: got %d, want %d",
			dst.Spec.Resume.MaxResumeRetries, src.Spec.Resume.MaxResumeRetries)
	}

	// Optional fields
	if dst.Spec.Parallelism == nil {
		t.Fatal("Parallelism lost during conversion")
	}
	if dst.Spec.Parallelism.PreferredCount != src.Spec.Parallelism.PreferredCount {
		t.Errorf("PreferredCount mismatch")
	}
	if dst.Spec.Parallelism.EnablePartialAdmission != src.Spec.Parallelism.EnablePartialAdmission {
		t.Errorf("EnablePartialAdmission mismatch")
	}

	if dst.Spec.Topology == nil {
		t.Fatal("Topology lost during conversion")
	}
	if dst.Spec.Topology.Mode != src.Spec.Topology.Mode {
		t.Errorf("Topology.Mode mismatch")
	}

	if dst.Spec.PriorityPolicyRef == nil {
		t.Fatal("PriorityPolicyRef lost during conversion")
	}
	if dst.Spec.PriorityPolicyRef.Name != src.Spec.PriorityPolicyRef.Name {
		t.Errorf("PriorityPolicyRef.Name mismatch")
	}

	// Devices
	if dst.Spec.Devices == nil {
		t.Fatal("Devices lost during conversion")
	}
	if dst.Spec.Devices.Mode != src.Spec.Devices.Mode {
		t.Errorf("Devices.Mode mismatch")
	}
	if len(dst.Spec.Devices.Claims) != len(src.Spec.Devices.Claims) {
		t.Errorf("Devices.Claims length mismatch")
	}

	// Elasticity
	if dst.Spec.Elasticity == nil {
		t.Fatal("Elasticity lost during conversion")
	}
	if dst.Spec.Elasticity.Mode != src.Spec.Elasticity.Mode {
		t.Errorf("Elasticity.Mode mismatch: got %q, want %q",
			dst.Spec.Elasticity.Mode, src.Spec.Elasticity.Mode)
	}

	// Status
	if dst.Status.Phase != src.Status.Phase {
		t.Errorf("Phase mismatch: got %q, want %q", dst.Status.Phase, src.Status.Phase)
	}
	if dst.Status.CurrentRunAttempt != src.Status.CurrentRunAttempt {
		t.Errorf("CurrentRunAttempt mismatch")
	}

	if dst.Status.Admission == nil {
		t.Fatal("Admission status lost during conversion")
	}
	if dst.Status.Admission.AdmittedWorkerCount != src.Status.Admission.AdmittedWorkerCount {
		t.Errorf("AdmittedWorkerCount mismatch")
	}

	if dst.Status.PriorityShaping == nil {
		t.Fatal("PriorityShaping status lost during conversion")
	}
	if dst.Status.PriorityShaping.EffectivePriority != src.Status.PriorityShaping.EffectivePriority {
		t.Errorf("EffectivePriority mismatch")
	}

	if dst.Status.Elasticity == nil {
		t.Fatal("Elasticity status lost during conversion")
	}
	if dst.Status.Elasticity.ResizeState != src.Status.Elasticity.ResizeState {
		t.Errorf("ResizeState mismatch")
	}

	if len(dst.Status.Conditions) != len(src.Status.Conditions) {
		t.Errorf("Conditions length mismatch")
	}
}

func assertBetaRTJEqual(t *testing.T, src, dst *v1beta1.ResumableTrainingJob) {
	t.Helper()

	if dst.Name != src.Name {
		t.Errorf("Name mismatch: got %q, want %q", dst.Name, src.Name)
	}
	if dst.Spec.QueueName != src.Spec.QueueName {
		t.Errorf("QueueName mismatch")
	}
	if dst.Spec.Identity.WorldSize != src.Spec.Identity.WorldSize {
		t.Errorf("WorldSize mismatch")
	}
	if dst.Status.Phase != src.Status.Phase {
		t.Errorf("Phase mismatch")
	}
	if dst.Status.Elasticity == nil {
		t.Fatal("Elasticity status lost during conversion")
	}
	if dst.Status.Elasticity.ResizeState != src.Status.Elasticity.ResizeState {
		t.Errorf("ResizeState mismatch")
	}
}
