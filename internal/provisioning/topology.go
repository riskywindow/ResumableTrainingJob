package provisioning

import (
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// DelayedTopologyState classifies the delayed topology request state for
// a PodSet or aggregated across all PodSets.
//
// Kueue v0.15.1 field:
//
//	admission.podSetAssignments[].delayedTopologyRequest (DelayedTopologyRequestState)
type DelayedTopologyState string

const (
	// DelayedTopologyNone means no delayed topology request is present.
	DelayedTopologyNone DelayedTopologyState = "None"

	// DelayedTopologyPending means a delayed topology request exists but
	// has not been satisfied yet.
	DelayedTopologyPending DelayedTopologyState = "Pending"

	// DelayedTopologyReady means the delayed topology request was
	// satisfied and the TopologyAssignment is available.
	DelayedTopologyReady DelayedTopologyState = "Ready"
)

// TopologyView captures the topology assignment and delayed topology state
// derived from a Workload's PodSetAssignments.
//
// Kueue v0.15.1 fields relied on:
//   - admission.podSetAssignments[].topologyAssignment   (non-nil = assigned)
//   - admission.podSetAssignments[].delayedTopologyRequest (Pending|Ready)
type TopologyView struct {
	// Configured indicates whether the RTJ has topology-aware scheduling
	// enabled (from spec.topology.mode != Disabled).
	Configured bool

	// Assigned is true when at least one PodSetAssignment has a non-nil
	// TopologyAssignment.
	Assigned bool

	// DelayedTopologyState is the aggregate delayed topology state.
	// None when no delayed topology request is present on any PodSet.
	DelayedTopologyState DelayedTopologyState

	// SecondPassPending is true when topology is configured but the
	// assignment is not yet available. This happens when:
	//   - quota is reserved but TopologyAssignment is nil, OR
	//   - a DelayedTopologyRequest is in Pending state.
	SecondPassPending bool

	// PodSetStates captures per-PodSet topology state.
	PodSetStates []PodSetTopologyState
}

// PodSetTopologyState captures the topology state for a single PodSet.
type PodSetTopologyState struct {
	// Name is the PodSet name.
	Name string

	// HasTopologyAssignment is true when the PodSet has a non-nil
	// TopologyAssignment on its PodSetAssignment.
	HasTopologyAssignment bool

	// DelayedTopologyState is the per-PodSet delayed topology state.
	DelayedTopologyState DelayedTopologyState
}

// ParseTopologyFromAssignments extracts topology state from PodSetAssignments.
//
// topologyConfigured is derived from the RTJ spec (spec.topology.mode != Disabled).
// When false, the returned view has Configured=false regardless of what
// PodSetAssignments contain.
func ParseTopologyFromAssignments(
	assignments []kueuev1beta2.PodSetAssignment,
	topologyConfigured bool,
) TopologyView {
	view := TopologyView{
		Configured: topologyConfigured,
	}

	if len(assignments) == 0 {
		if topologyConfigured {
			view.SecondPassPending = true
		}
		return view
	}

	anyAssigned := false
	anyDelayedPending := false

	for _, psa := range assignments {
		state := PodSetTopologyState{
			Name:                  string(psa.Name),
			HasTopologyAssignment: psa.TopologyAssignment != nil,
		}

		if psa.TopologyAssignment != nil {
			anyAssigned = true
		}

		if psa.DelayedTopologyRequest != nil {
			switch *psa.DelayedTopologyRequest {
			case kueuev1beta2.DelayedTopologyRequestStatePending:
				state.DelayedTopologyState = DelayedTopologyPending
				anyDelayedPending = true
			case kueuev1beta2.DelayedTopologyRequestStateReady:
				state.DelayedTopologyState = DelayedTopologyReady
			default:
				state.DelayedTopologyState = DelayedTopologyPending
				anyDelayedPending = true
			}
		} else {
			state.DelayedTopologyState = DelayedTopologyNone
		}

		view.PodSetStates = append(view.PodSetStates, state)
	}

	view.Assigned = anyAssigned

	// Aggregate delayed topology state.
	if anyDelayedPending {
		view.DelayedTopologyState = DelayedTopologyPending
	} else if anyAssigned {
		view.DelayedTopologyState = DelayedTopologyReady
	} else {
		view.DelayedTopologyState = DelayedTopologyNone
	}

	// Second pass is pending when topology is configured but assignment is
	// not yet available.
	if topologyConfigured && !anyAssigned {
		view.SecondPassPending = true
	}
	if topologyConfigured && anyDelayedPending {
		view.SecondPassPending = true
	}

	return view
}

// IsTopologyReady returns true when topology is either not configured
// or fully assigned with no pending delayed requests.
func IsTopologyReady(view TopologyView) bool {
	if !view.Configured {
		return true
	}
	return view.Assigned && !view.SecondPassPending
}
