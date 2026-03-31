// Package provisioning implements the Phase 7 provisioning/topology observation
// layer. It builds a compact "launch readiness view" from a Kueue Workload's
// status without mutating any resources.
//
// Kueue v0.15.1 fields relied on:
//   - workload.status.admissionChecks[].name      (AdmissionCheckReference)
//   - workload.status.admissionChecks[].state      (CheckState: Pending|Ready|Retry|Rejected)
//   - workload.status.admissionChecks[].message    (string)
//   - workload.status.admissionChecks[].retryCount (*int32)
//   - workload.status.admissionChecks[].podSetUpdates[].name         (PodSetReference)
//   - workload.status.admissionChecks[].podSetUpdates[].labels       (map)
//   - workload.status.admissionChecks[].podSetUpdates[].annotations  (map)
//   - workload.status.admissionChecks[].podSetUpdates[].nodeSelector (map)
//   - workload.status.admissionChecks[].podSetUpdates[].tolerations  ([]Toleration)
package provisioning

import (
	"fmt"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// ProvisioningClassification classifies the provisioning lifecycle state
// as observed from the Kueue Workload's admission checks.
type ProvisioningClassification string

const (
	// ProvisioningNotConfigured means no ProvisioningRequest AdmissionCheck
	// was detected on the Workload. Phase 6 behavior preserved.
	ProvisioningNotConfigured ProvisioningClassification = "NotConfigured"

	// ProvisioningPending means the ProvisioningRequest AC exists but is
	// in Pending state -- the backend has not yet satisfied it.
	ProvisioningPending ProvisioningClassification = "Pending"

	// ProvisioningProvisioned means the ProvisioningRequest AC is Ready,
	// indicating the backend confirmed physical capacity is available.
	ProvisioningProvisioned ProvisioningClassification = "Provisioned"

	// ProvisioningFailed means the ProvisioningRequest AC was Rejected
	// by the backend.
	ProvisioningFailed ProvisioningClassification = "Failed"

	// ProvisioningRetry means the ProvisioningRequest AC is in Retry state,
	// requesting back-off before the next attempt.
	ProvisioningRetry ProvisioningClassification = "Retry"
)

// ProvisioningRequestRef identifies the expected ProvisioningRequest resource
// derived from Kueue naming conventions.
type ProvisioningRequestRef struct {
	// Name is the expected ProvisioningRequest resource name.
	Name string

	// Namespace is the namespace where the ProvisioningRequest should exist.
	Namespace string
}

// ClassifyProvisioningFromChecks determines the provisioning lifecycle state
// from the workload's admission checks. Only checks whose names are in
// provisioningACNames are considered.
//
// Returns ProvisioningNotConfigured when provisioningACNames is empty or
// no matching AC is found on the workload.
func ClassifyProvisioningFromChecks(
	checks []kueuev1beta2.AdmissionCheckState,
	provisioningACNames map[string]bool,
) ProvisioningClassification {
	if len(provisioningACNames) == 0 {
		return ProvisioningNotConfigured
	}

	for _, ac := range checks {
		if !provisioningACNames[string(ac.Name)] {
			continue
		}
		return classifyCheckState(ac.State)
	}

	// Provisioning AC names configured but none found on the workload.
	return ProvisioningNotConfigured
}

func classifyCheckState(state kueuev1beta2.CheckState) ProvisioningClassification {
	switch state {
	case kueuev1beta2.CheckStateReady:
		return ProvisioningProvisioned
	case kueuev1beta2.CheckStatePending:
		return ProvisioningPending
	case kueuev1beta2.CheckStateRetry:
		return ProvisioningRetry
	case kueuev1beta2.CheckStateRejected:
		return ProvisioningFailed
	default:
		return ProvisioningPending
	}
}

// FindProvisioningCheckName returns the name of the first provisioning AC
// found in the workload's admission checks. Returns empty string if none found.
func FindProvisioningCheckName(
	checks []kueuev1beta2.AdmissionCheckState,
	provisioningACNames map[string]bool,
) string {
	for _, ac := range checks {
		if provisioningACNames[string(ac.Name)] {
			return string(ac.Name)
		}
	}
	return ""
}

// ResolveProvisioningRequestRef derives the expected ProvisioningRequest
// resource reference from the Workload name and the provisioning AC name.
//
// Kueue v0.15.1 naming convention:
//
//	{workload-name}-{check-name}-{attempt}
//
// The attempt suffix starts at 1 for the initial request. Returns nil when
// workloadName or checkName is empty.
func ResolveProvisioningRequestRef(workloadName, workloadNamespace, checkName string, attempt int32) *ProvisioningRequestRef {
	if workloadName == "" || checkName == "" {
		return nil
	}
	if attempt < 1 {
		attempt = 1
	}
	name := fmt.Sprintf("%s-%s-%d", workloadName, checkName, attempt)
	return &ProvisioningRequestRef{
		Name:      name,
		Namespace: workloadNamespace,
	}
}
