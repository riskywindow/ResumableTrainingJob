package multikueue

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// -------------------------------------------------------------------------
// FormatExternalFrameworkName
// -------------------------------------------------------------------------

func TestFormatExternalFrameworkName(t *testing.T) {
	got := FormatExternalFrameworkName(RTJGroupVersionKind)
	if got != ExternalFrameworkName {
		t.Errorf("FormatExternalFrameworkName(RTJGroupVersionKind) = %q, want %q", got, ExternalFrameworkName)
	}
}

func TestFormatExternalFrameworkName_MatchesRegisterPackage(t *testing.T) {
	// The external framework name in this package must match the one in
	// internal/kueue/register.go. Both should produce the same string.
	expected := "ResumableTrainingJob.v1alpha1.training.checkpoint.example.io"
	got := FormatExternalFrameworkName(RTJGroupVersionKind)
	if got != expected {
		t.Errorf("framework name mismatch: got %q, want %q", got, expected)
	}
}

// -------------------------------------------------------------------------
// RTJGroupVersionKind / RTJGroupVersionResource
// -------------------------------------------------------------------------

func TestRTJGroupVersionKind(t *testing.T) {
	if RTJGroupVersionKind.Kind != "ResumableTrainingJob" {
		t.Errorf("Kind = %q, want 'ResumableTrainingJob'", RTJGroupVersionKind.Kind)
	}
	if RTJGroupVersionKind.Version != "v1alpha1" {
		t.Errorf("Version = %q, want 'v1alpha1'", RTJGroupVersionKind.Version)
	}
	if RTJGroupVersionKind.Group != "training.checkpoint.example.io" {
		t.Errorf("Group = %q, want 'training.checkpoint.example.io'", RTJGroupVersionKind.Group)
	}
}

func TestRTJGroupVersionResource(t *testing.T) {
	if RTJGroupVersionResource.Resource != "resumabletrainingjobs" {
		t.Errorf("Resource = %q, want 'resumabletrainingjobs'", RTJGroupVersionResource.Resource)
	}
	if RTJGroupVersionResource.Group != "training.checkpoint.example.io" {
		t.Errorf("Group = %q, want 'training.checkpoint.example.io'", RTJGroupVersionResource.Group)
	}
}

// -------------------------------------------------------------------------
// IsRTJEligibleForMultiKueue
// -------------------------------------------------------------------------

func makeRTJ(managedBy string) *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			ManagedBy:                 managedBy,
			QueueName:                 "default-queue",
			WorkloadPriorityClassName: "default-priority",
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "test:latest",
				CodeVersion: "v1",
				WorldSize:   4,
				GPUShape:    "nvidia-a100",
			},
			Runtime: trainingv1alpha1.ResumableTrainingJobRuntime{
				Mode:          trainingv1alpha1.RuntimeModeFSDP,
				OptimizerMode: "adamw",
				ShardingMode:  "full",
				Template: trainingv1alpha1.JobSetTemplate{
					APIVersion: "jobset.x-k8s.io/v1alpha2",
					Kind:       "JobSet",
					Spec:       runtime.RawExtension{Raw: []byte(`{"replicatedJobs":[]}`)},
				},
			},
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI:      "s3://bucket/checkpoints",
				Interval:        metav1.Duration{Duration: 300000000000},
				FreshnessBudget: metav1.Duration{Duration: 600000000000},
				MaxDrainTime:    metav1.Duration{Duration: 60000000000},
				SafePointMode:   trainingv1alpha1.SafePointModeStepBoundary,
			},
			Resume: trainingv1alpha1.ResumePolicy{
				SourcePolicy:     trainingv1alpha1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3,
			},
		},
	}
}

func TestIsRTJEligibleForMultiKueue_Nil(t *testing.T) {
	eligible, reason := IsRTJEligibleForMultiKueue(nil)
	if eligible {
		t.Error("expected nil RTJ to be ineligible")
	}
	if reason == "" {
		t.Error("expected reason for ineligibility")
	}
}

func TestIsRTJEligibleForMultiKueue_NoManagedBy(t *testing.T) {
	job := makeRTJ("")
	eligible, reason := IsRTJEligibleForMultiKueue(job)
	if eligible {
		t.Error("expected RTJ without managedBy to be ineligible")
	}
	if reason == "" {
		t.Error("expected reason for ineligibility")
	}
}

func TestIsRTJEligibleForMultiKueue_WrongManagedBy(t *testing.T) {
	job := makeRTJ("some-other-controller/v1")
	eligible, reason := IsRTJEligibleForMultiKueue(job)
	if eligible {
		t.Error("expected RTJ with wrong managedBy to be ineligible")
	}
	if reason == "" {
		t.Error("expected reason for ineligibility")
	}
}

func TestIsRTJEligibleForMultiKueue_Valid(t *testing.T) {
	job := makeRTJ(trainingv1alpha1.MultiKueueControllerName)
	eligible, reason := IsRTJEligibleForMultiKueue(job)
	if !eligible {
		t.Errorf("expected RTJ with correct managedBy to be eligible, got reason: %s", reason)
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestIsRTJEligibleForMultiKueue_ConsistentWithIsManagedByMultiKueue(t *testing.T) {
	// Verify that IsRTJEligibleForMultiKueue agrees with the type's
	// IsManagedByMultiKueue method.
	tests := []struct {
		name      string
		managedBy string
	}{
		{"empty", ""},
		{"multikueue", trainingv1alpha1.MultiKueueControllerName},
		{"other", "other/controller"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := makeRTJ(tt.managedBy)
			eligible, _ := IsRTJEligibleForMultiKueue(job)
			isMK := job.IsManagedByMultiKueue()
			if eligible != isMK {
				t.Errorf("IsRTJEligibleForMultiKueue=%v but IsManagedByMultiKueue=%v for managedBy=%q",
					eligible, isMK, tt.managedBy)
			}
		})
	}
}

// -------------------------------------------------------------------------
// RTJManagerRBACRules
// -------------------------------------------------------------------------

func TestRTJManagerRBACRules(t *testing.T) {
	rules := RTJManagerRBACRules()

	if rules.Group != "training.checkpoint.example.io" {
		t.Errorf("Group = %q, want 'training.checkpoint.example.io'", rules.Group)
	}
	if rules.Resource != "resumabletrainingjobs" {
		t.Errorf("Resource = %q, want 'resumabletrainingjobs'", rules.Resource)
	}
	if !rules.StatusSubresource {
		t.Error("expected StatusSubresource to be true")
	}

	// Manager verbs must include read and write for status mirroring.
	managerVerbSet := toSet(rules.ManagerVerbs)
	for _, verb := range []string{"get", "list", "watch", "update", "patch"} {
		if !managerVerbSet[verb] {
			t.Errorf("ManagerVerbs missing %q", verb)
		}
	}

	// Worker verbs (via remote client) must include create, get, delete.
	workerVerbSet := toSet(rules.WorkerVerbs)
	for _, verb := range []string{"get", "list", "watch", "create", "delete"} {
		if !workerVerbSet[verb] {
			t.Errorf("WorkerVerbs missing %q", verb)
		}
	}
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// -------------------------------------------------------------------------
// Constants consistency
// -------------------------------------------------------------------------

func TestMultiKueueLabels(t *testing.T) {
	if PrebuiltWorkloadLabel != "kueue.x-k8s.io/prebuilt-workload-name" {
		t.Errorf("PrebuiltWorkloadLabel = %q, want 'kueue.x-k8s.io/prebuilt-workload-name'", PrebuiltWorkloadLabel)
	}
	if MultiKueueOriginLabel != "kueue.x-k8s.io/multikueue-origin" {
		t.Errorf("MultiKueueOriginLabel = %q, want 'kueue.x-k8s.io/multikueue-origin'", MultiKueueOriginLabel)
	}
}

func TestMultiKueueAdmissionCheckController(t *testing.T) {
	// The admission check controller name must match the value used in the
	// RTJ type for spec.managedBy.
	if MultiKueueAdmissionCheckController != trainingv1alpha1.MultiKueueControllerName {
		t.Errorf("MultiKueueAdmissionCheckController = %q, want %q",
			MultiKueueAdmissionCheckController, trainingv1alpha1.MultiKueueControllerName)
	}
}
