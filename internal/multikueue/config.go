// Package multikueue provides configuration types, validation, and integration
// helpers for wiring ResumableTrainingJob (RTJ) into Kueue's MultiKueue
// external-framework dispatch protocol.
//
// Kueue v0.15.1 ships a generic unstructured adapter
// (pkg/controller/admissionchecks/multikueue/externalframeworks) that handles
// remote object creation, status mirroring, and cleanup for any CRD listed in
// the Kueue Configuration's integrations.externalFrameworks list. The RTJ
// operator does NOT implement the MultiKueueAdapter interface directly; it
// relies on the upstream generic adapter.
//
// This package provides:
//   - Constants for the external-framework name and feature gates.
//   - ManagerClusterConfig, a validated representation of the Kueue
//     configuration entries required on the manager cluster.
//   - Validation helpers that verify a Kueue Configuration contains the
//     expected RTJ external framework entry and feature-gate settings.
package multikueue

import (
	"fmt"
	"strings"
)

// -------------------------------------------------------------------------
// Constants
// -------------------------------------------------------------------------

const (
	// ExternalFrameworkName is the Kueue external-framework identifier for RTJ.
	// Format: <Kind>.<version>.<group>  as required by Kueue.
	// Must match the value registered in internal/kueue/register.go.
	ExternalFrameworkName = "ResumableTrainingJob.v1alpha1.training.checkpoint.example.io"

	// MultiKueueAdmissionCheckController is the controller name that Kueue
	// uses for the MultiKueue AdmissionCheck.
	MultiKueueAdmissionCheckController = "kueue.x-k8s.io/multikueue"

	// FeatureGateMultiKueue is the Kueue feature gate for the core MultiKueue
	// functionality. Beta and default-on since Kueue v0.9.
	FeatureGateMultiKueue = "MultiKueue"

	// FeatureGateMultiKueueAdaptersForCustomJobs is the Kueue feature gate
	// that enables the generic unstructured adapter for external-framework
	// CRDs (including RTJ). Beta and default-on since Kueue v0.15.
	FeatureGateMultiKueueAdaptersForCustomJobs = "MultiKueueAdaptersForCustomJobs"

	// FeatureGateMultiKueueBatchJobWithManagedBy is the Kueue feature gate
	// that enables the managedBy-based dispatch for batch/Job. Beta and
	// default-on since Kueue v0.15. Not strictly required for RTJ but
	// typically enabled alongside MultiKueueAdaptersForCustomJobs.
	FeatureGateMultiKueueBatchJobWithManagedBy = "MultiKueueBatchJobWithManagedBy"

	// PrebuiltWorkloadLabel is the label Kueue's external-framework adapter
	// sets on remote objects to associate them with a pre-built Workload.
	PrebuiltWorkloadLabel = "kueue.x-k8s.io/prebuilt-workload-name"

	// MultiKueueOriginLabel is the label Kueue's external-framework adapter
	// sets on remote objects to identify the originating manager cluster.
	MultiKueueOriginLabel = "kueue.x-k8s.io/multikueue-origin"

	// DefaultAdmissionCheckName is the conventional name for the MultiKueue
	// AdmissionCheck resource on the manager cluster.
	DefaultAdmissionCheckName = "multikueue"

	// DefaultMultiKueueConfigName is the conventional name for the
	// MultiKueueConfig resource on the manager cluster.
	DefaultMultiKueueConfigName = "multikueue-config"
)

// -------------------------------------------------------------------------
// ManagerClusterConfig
// -------------------------------------------------------------------------

// ManagerClusterConfig captures the Kueue configuration entries required on the
// manager cluster for RTJ MultiKueue dispatch. It is used for validation and
// for generating deploy artifacts.
type ManagerClusterConfig struct {
	// ExternalFrameworks is the list of external-framework identifiers in
	// the Kueue Configuration's integrations.externalFrameworks.
	ExternalFrameworks []string

	// FeatureGates is a map of Kueue feature gate name to enabled state.
	FeatureGates map[string]bool

	// AdmissionCheckName is the name of the MultiKueue AdmissionCheck
	// resource. Empty means the default name will be used.
	AdmissionCheckName string

	// MultiKueueConfigName is the name of the MultiKueueConfig resource.
	// Empty means the default name will be used.
	MultiKueueConfigName string

	// WorkerClusters lists the MultiKueueCluster names that the manager
	// can dispatch to.
	WorkerClusters []string
}

// EffectiveAdmissionCheckName returns the admission check name, falling back
// to the default.
func (c *ManagerClusterConfig) EffectiveAdmissionCheckName() string {
	if c.AdmissionCheckName != "" {
		return c.AdmissionCheckName
	}
	return DefaultAdmissionCheckName
}

// EffectiveMultiKueueConfigName returns the MultiKueueConfig name, falling
// back to the default.
func (c *ManagerClusterConfig) EffectiveMultiKueueConfigName() string {
	if c.MultiKueueConfigName != "" {
		return c.MultiKueueConfigName
	}
	return DefaultMultiKueueConfigName
}

// -------------------------------------------------------------------------
// Validation
// -------------------------------------------------------------------------

// ValidationError captures a configuration validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateManagerConfig checks that a ManagerClusterConfig contains the
// entries required for RTJ MultiKueue dispatch. Returns a slice of
// validation errors (empty when valid).
func ValidateManagerConfig(cfg *ManagerClusterConfig) []ValidationError {
	var errs []ValidationError

	// External framework must include RTJ.
	if !containsExternalFramework(cfg.ExternalFrameworks, ExternalFrameworkName) {
		errs = append(errs, ValidationError{
			Field:   "integrations.externalFrameworks",
			Message: fmt.Sprintf("must include %q for RTJ MultiKueue dispatch", ExternalFrameworkName),
		})
	}

	// Required feature gates.
	for _, gate := range []string{
		FeatureGateMultiKueue,
		FeatureGateMultiKueueAdaptersForCustomJobs,
	} {
		enabled, present := cfg.FeatureGates[gate]
		if !present {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("featureGates.%s", gate),
				Message: fmt.Sprintf("feature gate %q must be present and enabled (default-on in Kueue v0.15.1 Beta, but verify it is not explicitly disabled)", gate),
			})
		} else if !enabled {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("featureGates.%s", gate),
				Message: fmt.Sprintf("feature gate %q must be enabled for RTJ MultiKueue dispatch", gate),
			})
		}
	}

	// At least one worker cluster.
	if len(cfg.WorkerClusters) == 0 {
		errs = append(errs, ValidationError{
			Field:   "workerClusters",
			Message: "at least one MultiKueueCluster must be configured for dispatch",
		})
	}

	return errs
}

// ValidateExternalFrameworkList checks that a list of external framework names
// includes the RTJ framework. Returns an error describing the issue, or nil.
func ValidateExternalFrameworkList(frameworks []string) error {
	if !containsExternalFramework(frameworks, ExternalFrameworkName) {
		return fmt.Errorf(
			"Kueue integrations.externalFrameworks does not include %q; "+
				"RTJ objects will not be recognized for MultiKueue dispatch",
			ExternalFrameworkName,
		)
	}
	return nil
}

// RequiredFeatureGates returns the Kueue feature gates that must be enabled
// for RTJ MultiKueue dispatch. All are Beta and default-on in v0.15.1.
func RequiredFeatureGates() map[string]bool {
	return map[string]bool{
		FeatureGateMultiKueue:                      true,
		FeatureGateMultiKueueAdaptersForCustomJobs: true,
	}
}

// containsExternalFramework checks whether the given framework name is present
// in the list, using case-sensitive comparison.
func containsExternalFramework(frameworks []string, name string) bool {
	for _, f := range frameworks {
		if strings.TrimSpace(f) == name {
			return true
		}
	}
	return false
}
