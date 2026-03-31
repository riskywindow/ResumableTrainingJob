package provisioning

import (
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// LaunchReadinessView is a compact internal snapshot of the launch readiness
// state derived from a Kueue Workload. The RTJ controller consumes this view
// to decide whether launching child runtime is safe.
//
// The view is built exclusively from Workload status fields:
//   - status.admission                (quota reservation, PodSetAssignments)
//   - status.admissionChecks          (per-AC state, PodSetUpdates)
//
// See docs/phase7/provisioning-observation.md for field-level documentation.
type LaunchReadinessView struct {
	// QuotaReserved is true when the Workload has a non-nil admission,
	// indicating quota has been reserved in the ClusterQueue.
	QuotaReserved bool

	// AdmissionChecks captures the state of every admission check on
	// the Workload, parsed into an internal form.
	AdmissionChecks []AdmissionCheckView

	// ProvisioningRequestPresent is true when at least one admission check
	// was identified as a ProvisioningRequest AC.
	ProvisioningRequestPresent bool

	// Provisioning classifies the current provisioning lifecycle state.
	// NotConfigured when no provisioning AC is present (Phase 6 fallback).
	Provisioning ProvisioningClassification

	// ProvisioningRequestRef is the derived ProvisioningRequest reference.
	// Nil when provisioning is not configured or the reference cannot be
	// derived.
	ProvisioningRequestRef *ProvisioningRequestRef

	// PodSetUpdates contains the parsed podSetUpdates from all ACs.
	// Each entry groups updates from a single AC.
	PodSetUpdates []PodSetUpdateSet

	// TopologyState captures topology assignment and delayed topology state
	// derived from PodSetAssignments.
	TopologyState TopologyView

	// AllChecksReady is true when every AC on the Workload is in Ready state.
	// Defaults to true when no ACs are configured (Phase 6 fail-open).
	AllChecksReady bool
}

// AdmissionCheckView captures the state of a single admission check.
type AdmissionCheckView struct {
	// Name is the admission check name.
	Name string

	// State is the normalized check state classification.
	State CheckStateClassification

	// Message is the check's human-readable status message.
	Message string

	// PodSetUpdates contains the parsed per-pod-set updates from this AC.
	PodSetUpdates []PodSetUpdateEntry

	// IsProvisioningCheck is true when this AC is a ProvisioningRequest AC.
	IsProvisioningCheck bool
}

// CheckStateClassification normalizes Kueue CheckState values into an
// internal classification.
type CheckStateClassification string

const (
	CheckPending  CheckStateClassification = "Pending"
	CheckReady    CheckStateClassification = "Ready"
	CheckRetry    CheckStateClassification = "Retry"
	CheckRejected CheckStateClassification = "Rejected"
)

// ViewOptions configures how the LaunchReadinessView is built.
type ViewOptions struct {
	// ProvisioningACNames identifies which admission check names are
	// ProvisioningRequest checks. When empty, provisioning falls back
	// to NotConfigured.
	ProvisioningACNames map[string]bool

	// TopologyEnabled indicates whether the RTJ has topology configured
	// (spec.topology.mode != Disabled).
	TopologyEnabled bool

	// WorkloadName is the Workload resource name, used for ProvisioningRequest
	// reference derivation.
	WorkloadName string

	// WorkloadNamespace is the Workload namespace.
	WorkloadNamespace string
}

// BuildView constructs a LaunchReadinessView from a Kueue Workload.
// Returns a view with safe defaults when the workload is nil.
func BuildView(workload *kueuev1beta2.Workload, opts ViewOptions) *LaunchReadinessView {
	if workload == nil {
		return &LaunchReadinessView{
			Provisioning:   ProvisioningNotConfigured,
			AllChecksReady: true,
			TopologyState:  TopologyView{Configured: opts.TopologyEnabled},
		}
	}

	view := &LaunchReadinessView{
		QuotaReserved: workload.Status.Admission != nil,
	}

	// Parse admission checks.
	allReady := true
	var provisioningACName string
	for _, ac := range workload.Status.AdmissionChecks {
		acView := parseAdmissionCheck(ac, opts.ProvisioningACNames)
		view.AdmissionChecks = append(view.AdmissionChecks, acView)

		if acView.State != CheckReady {
			allReady = false
		}

		if acView.IsProvisioningCheck && provisioningACName == "" {
			view.ProvisioningRequestPresent = true
			provisioningACName = acView.Name
		}

		if len(acView.PodSetUpdates) > 0 {
			view.PodSetUpdates = append(view.PodSetUpdates, PodSetUpdateSet{
				AdmissionCheckName: acView.Name,
				Updates:            acView.PodSetUpdates,
			})
		}
	}

	// No ACs means all checks trivially satisfied (Phase 6 fail-open).
	if len(workload.Status.AdmissionChecks) == 0 {
		allReady = true
	}
	view.AllChecksReady = allReady

	// Classify provisioning.
	if view.ProvisioningRequestPresent {
		view.Provisioning = ClassifyProvisioningFromChecks(
			workload.Status.AdmissionChecks,
			opts.ProvisioningACNames,
		)
		view.ProvisioningRequestRef = ResolveProvisioningRequestRef(
			opts.WorkloadName,
			opts.WorkloadNamespace,
			provisioningACName,
			1,
		)
	} else {
		view.Provisioning = ProvisioningNotConfigured
	}

	// Parse topology.
	if workload.Status.Admission != nil {
		view.TopologyState = ParseTopologyFromAssignments(
			workload.Status.Admission.PodSetAssignments,
			opts.TopologyEnabled,
		)
	} else {
		view.TopologyState = TopologyView{
			Configured:    opts.TopologyEnabled,
			SecondPassPending: opts.TopologyEnabled,
		}
	}

	return view
}

// IsLaunchReady returns true when all conditions for safe child runtime
// launch are satisfied:
//  1. Quota is reserved
//  2. All admission checks are Ready
//  3. Topology is ready (if configured)
func (v *LaunchReadinessView) IsLaunchReady() bool {
	if v == nil {
		return false
	}
	return v.QuotaReserved && v.AllChecksReady && IsTopologyReady(v.TopologyState)
}

// MergedPodSetUpdates returns all podSetUpdates merged by pod set name.
// Later AC updates take precedence for conflicting map keys.
func (v *LaunchReadinessView) MergedPodSetUpdates() map[string]PodSetUpdateEntry {
	if v == nil {
		return nil
	}
	return MergePodSetUpdates(v.PodSetUpdates)
}

func parseAdmissionCheck(
	ac kueuev1beta2.AdmissionCheckState,
	provisioningACNames map[string]bool,
) AdmissionCheckView {
	return AdmissionCheckView{
		Name:                string(ac.Name),
		State:               normalizeCheckState(ac.State),
		Message:             ac.Message,
		PodSetUpdates:       ParsePodSetUpdates(ac.PodSetUpdates),
		IsProvisioningCheck: provisioningACNames[string(ac.Name)],
	}
}

func normalizeCheckState(state kueuev1beta2.CheckState) CheckStateClassification {
	switch state {
	case kueuev1beta2.CheckStateReady:
		return CheckReady
	case kueuev1beta2.CheckStatePending:
		return CheckPending
	case kueuev1beta2.CheckStateRetry:
		return CheckRetry
	case kueuev1beta2.CheckStateRejected:
		return CheckRejected
	default:
		return CheckPending
	}
}
