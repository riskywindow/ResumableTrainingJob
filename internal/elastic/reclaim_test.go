package elastic

import (
	"testing"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// --- ComputeReclaimDelta tests ---

func TestComputeReclaimDelta_ShrinkInPlace(t *testing.T) {
	plan := PlanOutput{
		Kind:                   PlanShrinkInPlace,
		ReclaimableWorkerDelta: 4,
	}

	delta := ComputeReclaimDelta(plan, "worker")
	if delta.PodSetName != "worker" {
		t.Errorf("expected PodSetName 'worker', got %q", delta.PodSetName)
	}
	if delta.Count != 4 {
		t.Errorf("expected count 4, got %d", delta.Count)
	}
	if !delta.IsReclaim() {
		t.Error("expected IsReclaim() true")
	}
}

func TestComputeReclaimDelta_NonShrinkIsZero(t *testing.T) {
	plans := []PlanOutput{
		{Kind: PlanNoResize},
		{Kind: PlanGrowViaRelaunch},
		{Kind: PlanShrinkViaRelaunch},
		{Kind: PlanResizeBlocked},
		{Kind: PlanResizeInProgress},
		{Kind: PlanReclaimPublished},
	}

	for _, plan := range plans {
		delta := ComputeReclaimDelta(plan, "worker")
		if delta.Count != 0 {
			t.Errorf("expected zero count for plan kind %s, got %d", plan.Kind, delta.Count)
		}
		if delta.IsReclaim() {
			t.Errorf("expected IsReclaim() false for %s", plan.Kind)
		}
		if !delta.IsClear() {
			t.Errorf("expected IsClear() true for %s", plan.Kind)
		}
	}
}

// --- BuildReclaimablePods tests ---

func TestBuildReclaimablePods_WithCount(t *testing.T) {
	delta := ReclaimDelta{PodSetName: "worker", Count: 3}

	pods := BuildReclaimablePods(delta)
	if len(pods) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(pods))
	}
	if string(pods[0].Name) != "worker" {
		t.Errorf("expected name 'worker', got %q", pods[0].Name)
	}
	if pods[0].Count != 3 {
		t.Errorf("expected count 3, got %d", pods[0].Count)
	}
}

func TestBuildReclaimablePods_ZeroReturnsNil(t *testing.T) {
	delta := ReclaimDelta{PodSetName: "worker", Count: 0}

	pods := BuildReclaimablePods(delta)
	if pods != nil {
		t.Errorf("expected nil for zero count, got %v", pods)
	}
}

func TestClearReclaimablePods(t *testing.T) {
	pods := ClearReclaimablePods()
	if pods != nil {
		t.Errorf("expected nil, got %v", pods)
	}
}

// --- ReclaimDeltaFromExisting tests ---

func TestReclaimDeltaFromExisting_Found(t *testing.T) {
	existing := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 5},
	}

	delta := ReclaimDeltaFromExisting(existing, "worker")
	if delta.Count != 5 {
		t.Errorf("expected count 5, got %d", delta.Count)
	}
	if delta.PodSetName != "worker" {
		t.Errorf("expected PodSetName 'worker', got %q", delta.PodSetName)
	}
}

func TestReclaimDeltaFromExisting_NotFound(t *testing.T) {
	existing := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("other"), Count: 5},
	}

	delta := ReclaimDeltaFromExisting(existing, "worker")
	if delta.Count != 0 {
		t.Errorf("expected count 0 for missing PodSet, got %d", delta.Count)
	}
}

func TestReclaimDeltaFromExisting_EmptySlice(t *testing.T) {
	delta := ReclaimDeltaFromExisting(nil, "worker")
	if delta.Count != 0 {
		t.Errorf("expected count 0 for nil slice, got %d", delta.Count)
	}
}

func TestReclaimDeltaFromExisting_MultiplePodSets(t *testing.T) {
	existing := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("launcher"), Count: 1},
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 3},
	}

	delta := ReclaimDeltaFromExisting(existing, "worker")
	if delta.Count != 3 {
		t.Errorf("expected count 3, got %d", delta.Count)
	}
}

// --- NeedsReclaimUpdate tests ---

func TestNeedsReclaimUpdate_NewReclaim(t *testing.T) {
	desired := ReclaimDelta{PodSetName: "worker", Count: 4}
	existing := []kueuev1beta2.ReclaimablePod{}

	if !NeedsReclaimUpdate(desired, existing) {
		t.Error("expected update needed for new reclaim")
	}
}

func TestNeedsReclaimUpdate_SameCount(t *testing.T) {
	desired := ReclaimDelta{PodSetName: "worker", Count: 4}
	existing := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 4},
	}

	if NeedsReclaimUpdate(desired, existing) {
		t.Error("expected no update when count matches")
	}
}

func TestNeedsReclaimUpdate_ClearExisting(t *testing.T) {
	desired := ReclaimDelta{PodSetName: "worker", Count: 0}
	existing := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 4},
	}

	if !NeedsReclaimUpdate(desired, existing) {
		t.Error("expected update needed to clear existing reclaim")
	}
}

func TestNeedsReclaimUpdate_BothZeroNoUpdate(t *testing.T) {
	desired := ReclaimDelta{PodSetName: "worker", Count: 0}

	if NeedsReclaimUpdate(desired, nil) {
		t.Error("expected no update when both desired and existing are zero")
	}
}

func TestNeedsReclaimUpdate_DifferentCount(t *testing.T) {
	desired := ReclaimDelta{PodSetName: "worker", Count: 3}
	existing := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 5},
	}

	if !NeedsReclaimUpdate(desired, existing) {
		t.Error("expected update when counts differ")
	}
}
