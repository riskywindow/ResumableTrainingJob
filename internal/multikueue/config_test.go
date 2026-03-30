package multikueue

import (
	"testing"
)

func TestValidateManagerConfig_Valid(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{ExternalFrameworkName},
		FeatureGates: map[string]bool{
			FeatureGateMultiKueue:                      true,
			FeatureGateMultiKueueAdaptersForCustomJobs: true,
		},
		WorkerClusters: []string{"worker-1", "worker-2"},
	}

	errs := ValidateManagerConfig(cfg)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateManagerConfig_MissingExternalFramework(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{"SomeOtherJob.v1.example.com"},
		FeatureGates: map[string]bool{
			FeatureGateMultiKueue:                      true,
			FeatureGateMultiKueueAdaptersForCustomJobs: true,
		},
		WorkerClusters: []string{"worker-1"},
	}

	errs := ValidateManagerConfig(cfg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "integrations.externalFrameworks" {
		t.Errorf("expected field 'integrations.externalFrameworks', got %q", errs[0].Field)
	}
}

func TestValidateManagerConfig_EmptyExternalFrameworks(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{},
		FeatureGates: map[string]bool{
			FeatureGateMultiKueue:                      true,
			FeatureGateMultiKueueAdaptersForCustomJobs: true,
		},
		WorkerClusters: []string{"worker-1"},
	}

	errs := ValidateManagerConfig(cfg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
}

func TestValidateManagerConfig_FeatureGateDisabled(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{ExternalFrameworkName},
		FeatureGates: map[string]bool{
			FeatureGateMultiKueue:                      true,
			FeatureGateMultiKueueAdaptersForCustomJobs: false,
		},
		WorkerClusters: []string{"worker-1"},
	}

	errs := ValidateManagerConfig(cfg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "featureGates.MultiKueueAdaptersForCustomJobs" {
		t.Errorf("expected field about MultiKueueAdaptersForCustomJobs, got %q", errs[0].Field)
	}
}

func TestValidateManagerConfig_FeatureGateMissing(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{ExternalFrameworkName},
		FeatureGates:       map[string]bool{},
		WorkerClusters:     []string{"worker-1"},
	}

	errs := ValidateManagerConfig(cfg)
	if len(errs) != 2 {
		t.Fatalf("expected 2 validation errors (both gates missing), got %d: %v", len(errs), errs)
	}
}

func TestValidateManagerConfig_NoWorkerClusters(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{ExternalFrameworkName},
		FeatureGates: map[string]bool{
			FeatureGateMultiKueue:                      true,
			FeatureGateMultiKueueAdaptersForCustomJobs: true,
		},
		WorkerClusters: []string{},
	}

	errs := ValidateManagerConfig(cfg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "workerClusters" {
		t.Errorf("expected field 'workerClusters', got %q", errs[0].Field)
	}
}

func TestValidateManagerConfig_MultipleErrors(t *testing.T) {
	cfg := &ManagerClusterConfig{
		ExternalFrameworks: []string{},
		FeatureGates: map[string]bool{
			FeatureGateMultiKueue: false,
		},
		WorkerClusters: []string{},
	}

	errs := ValidateManagerConfig(cfg)
	// Missing external framework, MultiKueue disabled, MultiKueueAdaptersForCustomJobs missing, no workers
	if len(errs) != 4 {
		t.Fatalf("expected 4 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateExternalFrameworkList_Valid(t *testing.T) {
	frameworks := []string{
		"batch/job",
		ExternalFrameworkName,
	}
	if err := ValidateExternalFrameworkList(frameworks); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateExternalFrameworkList_Missing(t *testing.T) {
	frameworks := []string{"batch/job"}
	if err := ValidateExternalFrameworkList(frameworks); err == nil {
		t.Error("expected error for missing RTJ framework, got nil")
	}
}

func TestValidateExternalFrameworkList_Empty(t *testing.T) {
	if err := ValidateExternalFrameworkList(nil); err == nil {
		t.Error("expected error for nil framework list, got nil")
	}
}

func TestValidateExternalFrameworkList_WhitespaceHandling(t *testing.T) {
	frameworks := []string{" " + ExternalFrameworkName + " "}
	// The name has surrounding whitespace -- should NOT match.
	// containsExternalFramework trims whitespace, so it should still match.
	if err := ValidateExternalFrameworkList(frameworks); err != nil {
		t.Errorf("expected trimmed match to succeed, got: %v", err)
	}
}

func TestRequiredFeatureGates(t *testing.T) {
	gates := RequiredFeatureGates()
	if len(gates) != 2 {
		t.Fatalf("expected 2 required feature gates, got %d", len(gates))
	}
	if !gates[FeatureGateMultiKueue] {
		t.Error("expected MultiKueue gate to be true")
	}
	if !gates[FeatureGateMultiKueueAdaptersForCustomJobs] {
		t.Error("expected MultiKueueAdaptersForCustomJobs gate to be true")
	}
}

func TestEffectiveAdmissionCheckName_Default(t *testing.T) {
	cfg := &ManagerClusterConfig{}
	if got := cfg.EffectiveAdmissionCheckName(); got != DefaultAdmissionCheckName {
		t.Errorf("expected %q, got %q", DefaultAdmissionCheckName, got)
	}
}

func TestEffectiveAdmissionCheckName_Custom(t *testing.T) {
	cfg := &ManagerClusterConfig{AdmissionCheckName: "my-check"}
	if got := cfg.EffectiveAdmissionCheckName(); got != "my-check" {
		t.Errorf("expected 'my-check', got %q", got)
	}
}

func TestEffectiveMultiKueueConfigName_Default(t *testing.T) {
	cfg := &ManagerClusterConfig{}
	if got := cfg.EffectiveMultiKueueConfigName(); got != DefaultMultiKueueConfigName {
		t.Errorf("expected %q, got %q", DefaultMultiKueueConfigName, got)
	}
}

func TestEffectiveMultiKueueConfigName_Custom(t *testing.T) {
	cfg := &ManagerClusterConfig{MultiKueueConfigName: "my-config"}
	if got := cfg.EffectiveMultiKueueConfigName(); got != "my-config" {
		t.Errorf("expected 'my-config', got %q", got)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{Field: "test.field", Message: "test message"}
	expected := "test.field: test message"
	if got := err.Error(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestExternalFrameworkNameConsistency(t *testing.T) {
	// Verify the constant matches the expected Kind.Version.Group format.
	expected := "ResumableTrainingJob.v1alpha1.training.checkpoint.example.io"
	if ExternalFrameworkName != expected {
		t.Errorf("ExternalFrameworkName = %q, want %q", ExternalFrameworkName, expected)
	}
}
