package provisioning

import (
	"testing"

	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

func TestParseTopologyNotConfiguredNoAssignments(t *testing.T) {
	view := ParseTopologyFromAssignments(nil, false)
	if view.Configured {
		t.Fatal("expected Configured=false")
	}
	if view.Assigned {
		t.Fatal("expected Assigned=false")
	}
	if view.SecondPassPending {
		t.Fatal("expected SecondPassPending=false when not configured")
	}
}

func TestParseTopologyConfiguredNoAssignments(t *testing.T) {
	view := ParseTopologyFromAssignments(nil, true)
	if !view.Configured {
		t.Fatal("expected Configured=true")
	}
	if view.Assigned {
		t.Fatal("expected Assigned=false")
	}
	if !view.SecondPassPending {
		t.Fatal("expected SecondPassPending=true when configured but no assignments")
	}
}

func TestParseTopologyConfiguredEmptyAssignments(t *testing.T) {
	view := ParseTopologyFromAssignments([]kueuev1beta2.PodSetAssignment{}, true)
	if !view.SecondPassPending {
		t.Fatal("expected SecondPassPending=true for empty assignments")
	}
}

func TestParseTopologyAssignmentPresent(t *testing.T) {
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:  "workers",
			Count: ptr.To[int32](4),
			TopologyAssignment: &kueuev1beta2.TopologyAssignment{
				Levels: []string{"topology.kubernetes.io/zone"},
				Slices: []kueuev1beta2.TopologyAssignmentSlice{
					{
						DomainCount: 1,
						ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
							{Universal: ptr.To("us-east-1a")},
						},
						PodCounts: kueuev1beta2.TopologyAssignmentSlicePodCounts{
							Universal: ptr.To[int32](4),
						},
					},
				},
			},
		},
	}

	view := ParseTopologyFromAssignments(assignments, true)
	if !view.Configured {
		t.Fatal("expected Configured=true")
	}
	if !view.Assigned {
		t.Fatal("expected Assigned=true")
	}
	if view.SecondPassPending {
		t.Fatal("expected SecondPassPending=false when assigned")
	}
	if view.DelayedTopologyState != DelayedTopologyReady {
		t.Fatalf("expected DelayedTopologyReady, got %s", view.DelayedTopologyState)
	}
}

func TestParseTopologyDelayedPending(t *testing.T) {
	pending := kueuev1beta2.DelayedTopologyRequestStatePending
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:                   "workers",
			Count:                  ptr.To[int32](4),
			DelayedTopologyRequest: &pending,
		},
	}

	view := ParseTopologyFromAssignments(assignments, true)
	if view.Assigned {
		t.Fatal("expected Assigned=false when no TopologyAssignment")
	}
	if view.DelayedTopologyState != DelayedTopologyPending {
		t.Fatalf("expected DelayedTopologyPending, got %s", view.DelayedTopologyState)
	}
	if !view.SecondPassPending {
		t.Fatal("expected SecondPassPending=true when delayed pending")
	}
}

func TestParseTopologyDelayedReady(t *testing.T) {
	ready := kueuev1beta2.DelayedTopologyRequestStateReady
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:                   "workers",
			Count:                  ptr.To[int32](4),
			DelayedTopologyRequest: &ready,
			TopologyAssignment: &kueuev1beta2.TopologyAssignment{
				Levels: []string{"topology.kubernetes.io/zone"},
			},
		},
	}

	view := ParseTopologyFromAssignments(assignments, true)
	if !view.Assigned {
		t.Fatal("expected Assigned=true")
	}
	if view.DelayedTopologyState != DelayedTopologyReady {
		t.Fatalf("expected DelayedTopologyReady, got %s", view.DelayedTopologyState)
	}
	if view.SecondPassPending {
		t.Fatal("expected SecondPassPending=false when delayed ready and assigned")
	}
}

func TestParseTopologyNoTopologyAssignmentNoDelayed(t *testing.T) {
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:  "workers",
			Count: ptr.To[int32](4),
		},
	}

	view := ParseTopologyFromAssignments(assignments, true)
	if view.Assigned {
		t.Fatal("expected Assigned=false")
	}
	if view.DelayedTopologyState != DelayedTopologyNone {
		t.Fatalf("expected DelayedTopologyNone, got %s", view.DelayedTopologyState)
	}
	if !view.SecondPassPending {
		t.Fatal("expected SecondPassPending=true when configured but not assigned")
	}
}

func TestParseTopologyNotConfiguredIgnoresAssignment(t *testing.T) {
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:  "workers",
			Count: ptr.To[int32](4),
			TopologyAssignment: &kueuev1beta2.TopologyAssignment{
				Levels: []string{"topology.kubernetes.io/zone"},
			},
		},
	}

	// When topology is not configured on the RTJ, assignment is still noted
	// but SecondPassPending is false.
	view := ParseTopologyFromAssignments(assignments, false)
	if view.Configured {
		t.Fatal("expected Configured=false")
	}
	if !view.Assigned {
		t.Fatal("expected Assigned=true (assignment present on PodSet)")
	}
	if view.SecondPassPending {
		t.Fatal("expected SecondPassPending=false when not configured")
	}
}

func TestParseTopologyMultiplePodSets(t *testing.T) {
	pending := kueuev1beta2.DelayedTopologyRequestStatePending
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:  "driver",
			Count: ptr.To[int32](1),
		},
		{
			Name:                   "workers",
			Count:                  ptr.To[int32](8),
			DelayedTopologyRequest: &pending,
		},
	}

	view := ParseTopologyFromAssignments(assignments, true)
	if len(view.PodSetStates) != 2 {
		t.Fatalf("expected 2 PodSetStates, got %d", len(view.PodSetStates))
	}

	driver := view.PodSetStates[0]
	if driver.Name != "driver" {
		t.Fatalf("expected 'driver', got %q", driver.Name)
	}
	if driver.HasTopologyAssignment {
		t.Fatal("driver should not have topology assignment")
	}
	if driver.DelayedTopologyState != DelayedTopologyNone {
		t.Fatalf("expected None for driver, got %s", driver.DelayedTopologyState)
	}

	workers := view.PodSetStates[1]
	if workers.Name != "workers" {
		t.Fatalf("expected 'workers', got %q", workers.Name)
	}
	if workers.DelayedTopologyState != DelayedTopologyPending {
		t.Fatalf("expected Pending for workers, got %s", workers.DelayedTopologyState)
	}

	// Aggregate state should be Pending because at least one PodSet is pending.
	if view.DelayedTopologyState != DelayedTopologyPending {
		t.Fatalf("expected aggregate Pending, got %s", view.DelayedTopologyState)
	}
	if !view.SecondPassPending {
		t.Fatal("expected SecondPassPending=true when delayed pending")
	}
}

func TestIsTopologyReadyNotConfigured(t *testing.T) {
	view := TopologyView{Configured: false}
	if !IsTopologyReady(view) {
		t.Fatal("expected ready when not configured")
	}
}

func TestIsTopologyReadyAssigned(t *testing.T) {
	view := TopologyView{
		Configured:        true,
		Assigned:          true,
		SecondPassPending: false,
	}
	if !IsTopologyReady(view) {
		t.Fatal("expected ready when assigned")
	}
}

func TestIsTopologyReadyNotAssigned(t *testing.T) {
	view := TopologyView{
		Configured:        true,
		Assigned:          false,
		SecondPassPending: true,
	}
	if IsTopologyReady(view) {
		t.Fatal("expected not ready when not assigned")
	}
}

func TestIsTopologyReadyAssignedButSecondPassPending(t *testing.T) {
	view := TopologyView{
		Configured:        true,
		Assigned:          true,
		SecondPassPending: true,
	}
	if IsTopologyReady(view) {
		t.Fatal("expected not ready when second pass pending")
	}
}

func TestParseTopologyUnknownDelayedStateDefaultsToPending(t *testing.T) {
	unknown := kueuev1beta2.DelayedTopologyRequestState("Unknown")
	assignments := []kueuev1beta2.PodSetAssignment{
		{
			Name:                   "workers",
			Count:                  ptr.To[int32](4),
			DelayedTopologyRequest: &unknown,
		},
	}

	view := ParseTopologyFromAssignments(assignments, true)
	if view.PodSetStates[0].DelayedTopologyState != DelayedTopologyPending {
		t.Fatalf("expected Pending for unknown state, got %s", view.PodSetStates[0].DelayedTopologyState)
	}
}
