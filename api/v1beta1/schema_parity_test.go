package v1beta1_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	v1beta1 "github.com/example/checkpoint-native-preemption-controller/api/v1beta1"
)

// TestJSONFieldParityRTJ verifies that a fully populated RTJ produces
// identical JSON field keys in v1alpha1 and v1beta1. This is the
// definitive structural parity check between the two versions.
func TestJSONFieldParityRTJ(t *testing.T) {
	tc := int32(4)
	suspend := false
	minCount := int32(2)

	// Build a v1alpha1 RTJ with all optional sections populated.
	alpha := &v1alpha1.ResumableTrainingJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "training.checkpoint.example.io/v1alpha1",
			Kind:       "ResumableTrainingJob",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "parity", Namespace: "test"},
		Spec: v1alpha1.ResumableTrainingJobSpec{
			Suspend:                   &suspend,
			QueueName:                 "q",
			WorkloadPriorityClassName: "wpc",
			Identity: v1alpha1.ResumableTrainingJobIdentity{
				Image: "img", CodeVersion: "v1", WorldSize: 4, GPUShape: "a100",
			},
			Runtime: v1alpha1.ResumableTrainingJobRuntime{
				Mode: v1alpha1.RuntimeModeFSDP, OptimizerMode: "AdamW", ShardingMode: "FULL",
				Template: v1alpha1.JobSetTemplate{
					APIVersion: "jobset.x-k8s.io/v1alpha2", Kind: "JobSet",
					Metadata: &v1alpha1.EmbeddedObjectMetadata{Labels: map[string]string{"a": "b"}},
					Spec:     runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
			Checkpoint: v1alpha1.CheckpointPolicy{
				StorageURI: "s3://b/c", Interval: metav1.Duration{Duration: 30 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 1 * time.Hour},
				MaxDrainTime:    metav1.Duration{Duration: 5 * time.Minute},
				SafePointMode:   v1alpha1.SafePointModeStepBoundary,
			},
			Resume: v1alpha1.ResumePolicy{
				SourcePolicy: v1alpha1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3, AllowWorldSizeChange: true,
			},
			Parallelism: &v1alpha1.ParallelismSpec{
				PreferredCount: 4, MinCount: &minCount, PodSetName: "w",
				EnablePartialAdmission: true,
			},
			Topology: &v1alpha1.TopologySpec{
				Mode: v1alpha1.TopologyModeRequired, TopologyLevel: "zone",
			},
			PriorityPolicyRef: &v1alpha1.PriorityPolicyReference{Name: "pol"},
			ManagedBy:         "kueue.x-k8s.io/multikueue",
			Devices: &v1alpha1.DeviceSpec{
				Mode: v1alpha1.DeviceModeDRA,
				Claims: []v1alpha1.DeviceClaimSpec{{
					Name: "gpu", Containers: []string{"c"},
					Request: v1alpha1.DeviceRequestSpec{DeviceClassName: "dc", Count: 1},
				}},
			},
			Elasticity: &v1alpha1.ElasticitySpec{
				Mode: v1alpha1.ElasticityModeManual, TargetWorkerCount: &tc,
				InPlaceShrinkPolicy: v1alpha1.InPlaceShrinkPolicyIfSupported,
				ReclaimMode:         v1alpha1.ReclaimModeReclaimablePods,
			},
			Control: &v1alpha1.ControlSpec{DesiredState: v1alpha1.DesiredStateRunning},
		},
	}

	// Build the same RTJ in v1beta1 with equivalent field values.
	beta := &v1beta1.ResumableTrainingJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "training.checkpoint.example.io/v1beta1",
			Kind:       "ResumableTrainingJob",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "parity", Namespace: "test"},
		Spec: v1beta1.ResumableTrainingJobSpec{
			Suspend:                   &suspend,
			QueueName:                 "q",
			WorkloadPriorityClassName: "wpc",
			Identity: v1beta1.ResumableTrainingJobIdentity{
				Image: "img", CodeVersion: "v1", WorldSize: 4, GPUShape: "a100",
			},
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Mode: v1beta1.RuntimeModeFSDP, OptimizerMode: "AdamW", ShardingMode: "FULL",
				Template: v1beta1.JobSetTemplate{
					APIVersion: "jobset.x-k8s.io/v1alpha2", Kind: "JobSet",
					Metadata: &v1beta1.EmbeddedObjectMetadata{Labels: map[string]string{"a": "b"}},
					Spec:     runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
			Checkpoint: v1beta1.CheckpointPolicy{
				StorageURI: "s3://b/c", Interval: metav1.Duration{Duration: 30 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 1 * time.Hour},
				MaxDrainTime:    metav1.Duration{Duration: 5 * time.Minute},
				SafePointMode:   v1beta1.SafePointModeStepBoundary,
			},
			Resume: v1beta1.ResumePolicy{
				SourcePolicy: v1beta1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3, AllowWorldSizeChange: true,
			},
			Parallelism: &v1beta1.ParallelismSpec{
				PreferredCount: 4, MinCount: &minCount, PodSetName: "w",
				EnablePartialAdmission: true,
			},
			Topology: &v1beta1.TopologySpec{
				Mode: v1beta1.TopologyModeRequired, TopologyLevel: "zone",
			},
			PriorityPolicyRef: &v1beta1.PriorityPolicyReference{Name: "pol"},
			ManagedBy:         "kueue.x-k8s.io/multikueue",
			Devices: &v1beta1.DeviceSpec{
				Mode: v1beta1.DeviceModeDRA,
				Claims: []v1beta1.DeviceClaimSpec{{
					Name: "gpu", Containers: []string{"c"},
					Request: v1beta1.DeviceRequestSpec{DeviceClassName: "dc", Count: 1},
				}},
			},
			Elasticity: &v1beta1.ElasticitySpec{
				Mode: v1beta1.ElasticityModeManual, TargetWorkerCount: &tc,
				InPlaceShrinkPolicy: v1beta1.InPlaceShrinkPolicyIfSupported,
				ReclaimMode:         v1beta1.ReclaimModeReclaimablePods,
			},
			Control: &v1beta1.ControlSpec{DesiredState: v1beta1.DesiredStateRunning},
		},
	}

	alphaJSON, err := json.Marshal(alpha)
	if err != nil {
		t.Fatalf("marshal alpha: %v", err)
	}
	betaJSON, err := json.Marshal(beta)
	if err != nil {
		t.Fatalf("marshal beta: %v", err)
	}

	// Unmarshal into generic maps and compare key sets (ignoring apiVersion).
	var alphaMap, betaMap map[string]interface{}
	if err := json.Unmarshal(alphaJSON, &alphaMap); err != nil {
		t.Fatalf("unmarshal alpha map: %v", err)
	}
	if err := json.Unmarshal(betaJSON, &betaMap); err != nil {
		t.Fatalf("unmarshal beta map: %v", err)
	}

	// Compare everything except apiVersion.
	delete(alphaMap, "apiVersion")
	delete(betaMap, "apiVersion")

	if !reflect.DeepEqual(alphaMap, betaMap) {
		// Compute which keys differ at spec level for a useful error.
		alphaSpec, _ := json.MarshalIndent(alphaMap["spec"], "", "  ")
		betaSpec, _ := json.MarshalIndent(betaMap["spec"], "", "  ")
		t.Errorf("JSON structures differ.\nalpha spec keys:\n%s\n\nbeta spec keys:\n%s",
			string(alphaSpec), string(betaSpec))
	}
}

// TestJSONFieldParityCPP verifies field parity for CheckpointPriorityPolicy.
func TestJSONFieldParityCPP(t *testing.T) {
	boost := int32(50)

	alpha := &v1alpha1.CheckpointPriorityPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "training.checkpoint.example.io/v1alpha1", Kind: "CheckpointPriorityPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget: metav1.Duration{Duration: 1 * time.Hour},
			StartupProtectionWindow:   metav1.Duration{Duration: 15 * time.Minute},
			MinRuntimeBetweenYields:   metav1.Duration{Duration: 10 * time.Minute},
			ProtectedBoost:            &boost,
		},
	}
	beta := &v1beta1.CheckpointPriorityPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "training.checkpoint.example.io/v1beta1", Kind: "CheckpointPriorityPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1beta1.CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget: metav1.Duration{Duration: 1 * time.Hour},
			StartupProtectionWindow:   metav1.Duration{Duration: 15 * time.Minute},
			MinRuntimeBetweenYields:   metav1.Duration{Duration: 10 * time.Minute},
			ProtectedBoost:            &boost,
		},
	}

	assertJSONParity(t, "CPP", alpha, beta)
}

// TestJSONFieldParityRRP verifies field parity for ResumeReadinessPolicy.
func TestJSONFieldParityRRP(t *testing.T) {
	req := true

	alpha := &v1alpha1.ResumeReadinessPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "training.checkpoint.example.io/v1alpha1", Kind: "ResumeReadinessPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1alpha1.ResumeReadinessPolicySpec{
			RequireCompleteCheckpoint: &req,
			FailurePolicy:            v1alpha1.FailurePolicyFailClosed,
		},
	}
	beta := &v1beta1.ResumeReadinessPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "training.checkpoint.example.io/v1beta1", Kind: "ResumeReadinessPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: v1beta1.ResumeReadinessPolicySpec{
			RequireCompleteCheckpoint: &req,
			FailurePolicy:            v1beta1.FailurePolicyFailClosed,
		},
	}

	assertJSONParity(t, "RRP", alpha, beta)
}

// TestDefaultParity verifies that applying defaults to both versions produces
// the same JSON output.
func TestDefaultParityRTJ(t *testing.T) {
	alpha := &v1alpha1.ResumableTrainingJob{
		Spec: v1alpha1.ResumableTrainingJobSpec{
			Runtime: v1alpha1.ResumableTrainingJobRuntime{
				Template: v1alpha1.JobSetTemplate{
					Spec: runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
		},
	}
	beta := &v1beta1.ResumableTrainingJob{
		Spec: v1beta1.ResumableTrainingJobSpec{
			Runtime: v1beta1.ResumableTrainingJobRuntime{
				Template: v1beta1.JobSetTemplate{
					Spec: runtime.RawExtension{Raw: []byte(`{}`)},
				},
			},
		},
	}

	alpha.Default()
	beta.Default()

	// Compare spec after defaults (ignoring labels set by projectKueueLabels in v1alpha1).
	alphaJSON, _ := json.Marshal(alpha.Spec)
	betaJSON, _ := json.Marshal(beta.Spec)

	var alphaMap, betaMap map[string]interface{}
	json.Unmarshal(alphaJSON, &alphaMap)
	json.Unmarshal(betaJSON, &betaMap)

	if !reflect.DeepEqual(alphaMap, betaMap) {
		t.Errorf("default specs differ:\nalpha: %s\nbeta:  %s", alphaJSON, betaJSON)
	}
}

func TestDefaultParityCPP(t *testing.T) {
	alpha := &v1alpha1.CheckpointPriorityPolicy{}
	beta := &v1beta1.CheckpointPriorityPolicy{}

	alpha.Default()
	beta.Default()

	alphaJSON, _ := json.Marshal(alpha.Spec)
	betaJSON, _ := json.Marshal(beta.Spec)

	if string(alphaJSON) != string(betaJSON) {
		t.Errorf("CPP default specs differ:\nalpha: %s\nbeta:  %s", alphaJSON, betaJSON)
	}
}

func TestDefaultParityRRP(t *testing.T) {
	alpha := &v1alpha1.ResumeReadinessPolicy{}
	beta := &v1beta1.ResumeReadinessPolicy{}

	alpha.Default()
	beta.Default()

	alphaJSON, _ := json.Marshal(alpha.Spec)
	betaJSON, _ := json.Marshal(beta.Spec)

	if string(alphaJSON) != string(betaJSON) {
		t.Errorf("RRP default specs differ:\nalpha: %s\nbeta:  %s", alphaJSON, betaJSON)
	}
}

func assertJSONParity(t *testing.T, name string, alpha, beta interface{}) {
	t.Helper()

	alphaJSON, err := json.Marshal(alpha)
	if err != nil {
		t.Fatalf("%s: marshal alpha: %v", name, err)
	}
	betaJSON, err := json.Marshal(beta)
	if err != nil {
		t.Fatalf("%s: marshal beta: %v", name, err)
	}

	var alphaMap, betaMap map[string]interface{}
	json.Unmarshal(alphaJSON, &alphaMap)
	json.Unmarshal(betaJSON, &betaMap)

	delete(alphaMap, "apiVersion")
	delete(betaMap, "apiVersion")

	if !reflect.DeepEqual(alphaMap, betaMap) {
		t.Errorf("%s: JSON structures differ (ignoring apiVersion)", name)
	}
}
