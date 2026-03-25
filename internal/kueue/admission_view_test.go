package kueue

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	"sigs.k8s.io/kueue/pkg/podset"
)

func TestFromPodSetsInfoBuildsPodSetAdmissions(t *testing.T) {
	names := []string{"driver", "workers"}
	infos := []podset.PodSetInfo{
		{
			Count:        1,
			NodeSelector: map[string]string{"pool": "cpu"},
		},
		{
			Count:        8,
			NodeSelector: map[string]string{"pool": "a100"},
			Tolerations: []corev1.Toleration{
				{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
		},
	}

	view := FromPodSetsInfo(names, infos)
	if view == nil {
		t.Fatal("expected non-nil admission view")
	}
	if len(view.PodSets) != 2 {
		t.Fatalf("expected 2 pod sets, got %d", len(view.PodSets))
	}

	driver := view.PodSets[0]
	if driver.Name != "driver" {
		t.Fatalf("expected pod set name 'driver', got %q", driver.Name)
	}
	if driver.Count != 1 {
		t.Fatalf("expected driver count 1, got %d", driver.Count)
	}

	workers := view.PodSets[1]
	if workers.Name != "workers" {
		t.Fatalf("expected pod set name 'workers', got %q", workers.Name)
	}
	if workers.Count != 8 {
		t.Fatalf("expected workers count 8, got %d", workers.Count)
	}
	if workers.NodeSelector["pool"] != "a100" {
		t.Fatalf("expected workers nodeSelector pool=a100, got %v", workers.NodeSelector)
	}
	if len(workers.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(workers.Tolerations))
	}
}

func TestFromPodSetsInfoReturnsNilOnMismatchedLengths(t *testing.T) {
	names := []string{"workers"}
	infos := []podset.PodSetInfo{
		{Count: 4},
		{Count: 8},
	}
	if view := FromPodSetsInfo(names, infos); view != nil {
		t.Fatal("expected nil admission view for mismatched lengths")
	}
}

func TestFromPodSetsInfoReturnsNilOnEmptyInput(t *testing.T) {
	if view := FromPodSetsInfo(nil, nil); view != nil {
		t.Fatal("expected nil admission view for nil input")
	}
	if view := FromPodSetsInfo([]string{}, []podset.PodSetInfo{}); view != nil {
		t.Fatal("expected nil admission view for empty input")
	}
}

func TestFromWorkloadAdmissionExtractsCountsAndFlavors(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		ClusterQueue: "gpu-cluster-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "workers",
				Count: ptr.To[int32](4),
				Flavors: map[corev1.ResourceName]kueuev1beta2.ResourceFlavorReference{
					"nvidia.com/gpu": "a100-80gb",
				},
			},
		},
	}

	view := FromWorkloadAdmission(admission)
	if view == nil {
		t.Fatal("expected non-nil admission view")
	}
	if view.ClusterQueueName != "gpu-cluster-queue" {
		t.Fatalf("expected cluster queue 'gpu-cluster-queue', got %q", view.ClusterQueueName)
	}
	if len(view.PodSets) != 1 {
		t.Fatalf("expected 1 pod set, got %d", len(view.PodSets))
	}
	ps := view.PodSets[0]
	if ps.Name != "workers" {
		t.Fatalf("expected pod set name 'workers', got %q", ps.Name)
	}
	if ps.Count != 4 {
		t.Fatalf("expected count 4, got %d", ps.Count)
	}
	if ps.Flavors["nvidia.com/gpu"] != "a100-80gb" {
		t.Fatalf("expected flavor 'a100-80gb', got %q", ps.Flavors["nvidia.com/gpu"])
	}
}

func TestFromWorkloadAdmissionReturnsNilForNilInput(t *testing.T) {
	if view := FromWorkloadAdmission(nil); view != nil {
		t.Fatal("expected nil for nil admission")
	}
}

func TestFromWorkloadAdmissionReturnsNilForEmptyAssignments(t *testing.T) {
	if view := FromWorkloadAdmission(&kueuev1beta2.Admission{}); view != nil {
		t.Fatal("expected nil for empty assignments")
	}
}

func TestFromWorkloadAdmissionMultiplePodSets(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		ClusterQueue: "training-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "driver",
				Count: ptr.To[int32](1),
			},
			{
				Name:  "workers",
				Count: ptr.To[int32](8),
				Flavors: map[corev1.ResourceName]kueuev1beta2.ResourceFlavorReference{
					"nvidia.com/gpu": "h100-80gb",
					"cpu":            "h100-cpu",
				},
			},
		},
	}

	view := FromWorkloadAdmission(admission)
	if view == nil {
		t.Fatal("expected non-nil admission view")
	}
	if len(view.PodSets) != 2 {
		t.Fatalf("expected 2 pod sets, got %d", len(view.PodSets))
	}
	if view.PodSets[0].Count != 1 {
		t.Fatalf("expected driver count 1, got %d", view.PodSets[0].Count)
	}
	if view.PodSets[1].Count != 8 {
		t.Fatalf("expected workers count 8, got %d", view.PodSets[1].Count)
	}
}

func TestTotalAdmittedCount(t *testing.T) {
	view := &AdmissionView{
		PodSets: []PodSetAdmission{
			{Name: "driver", Count: 1},
			{Name: "workers", Count: 8},
		},
	}
	if total := view.TotalAdmittedCount(); total != 9 {
		t.Fatalf("expected total 9, got %d", total)
	}
}

func TestTotalAdmittedCountNilView(t *testing.T) {
	var view *AdmissionView
	if total := view.TotalAdmittedCount(); total != 0 {
		t.Fatalf("expected total 0 for nil view, got %d", total)
	}
}

func TestFlavorsByPodSet(t *testing.T) {
	view := &AdmissionView{
		PodSets: []PodSetAdmission{
			{
				Name: "driver",
				// no flavors
			},
			{
				Name: "workers",
				Flavors: map[corev1.ResourceName]string{
					"nvidia.com/gpu": "a100-80gb",
				},
			},
		},
	}
	flavors := view.FlavorsByPodSet()
	if len(flavors) != 1 {
		t.Fatalf("expected 1 entry in flavors map, got %d", len(flavors))
	}
	if flavors["workers"] != "a100-80gb" {
		t.Fatalf("expected workers flavor 'a100-80gb', got %q", flavors["workers"])
	}
}

func TestFlavorsByPodSetMultipleResources(t *testing.T) {
	view := &AdmissionView{
		PodSets: []PodSetAdmission{
			{
				Name: "workers",
				Flavors: map[corev1.ResourceName]string{
					"nvidia.com/gpu": "a100-80gb",
					"cpu":            "a100-cpu",
				},
			},
		},
	}
	flavors := view.FlavorsByPodSet()
	// flavors are sorted and deduped
	got := flavors["workers"]
	if got != "a100-80gb,a100-cpu" && got != "a100-cpu,a100-80gb" {
		// since they're sorted alphabetically:
		if got != "a100-80gb,a100-cpu" {
			t.Fatalf("expected sorted flavors 'a100-80gb,a100-cpu', got %q", got)
		}
	}
}

func TestFlavorsByPodSetNilView(t *testing.T) {
	var view *AdmissionView
	if flavors := view.FlavorsByPodSet(); flavors != nil {
		t.Fatalf("expected nil flavors for nil view, got %v", flavors)
	}
}

func TestPodSetByName(t *testing.T) {
	view := &AdmissionView{
		PodSets: []PodSetAdmission{
			{Name: "driver", Count: 1},
			{Name: "workers", Count: 4},
		},
	}
	ps, ok := view.PodSetByName("workers")
	if !ok {
		t.Fatal("expected to find 'workers' pod set")
	}
	if ps.Count != 4 {
		t.Fatalf("expected count 4, got %d", ps.Count)
	}

	_, ok = view.PodSetByName("nonexistent")
	if ok {
		t.Fatal("expected not to find 'nonexistent'")
	}
}

func TestPodSetByNameNilView(t *testing.T) {
	var view *AdmissionView
	_, ok := view.PodSetByName("workers")
	if ok {
		t.Fatal("expected false for nil view")
	}
}

func TestIsEmpty(t *testing.T) {
	var nilView *AdmissionView
	if !nilView.IsEmpty() {
		t.Fatal("expected nil view to be empty")
	}

	emptyView := &AdmissionView{}
	if !emptyView.IsEmpty() {
		t.Fatal("expected empty view to be empty")
	}

	view := &AdmissionView{
		PodSets: []PodSetAdmission{{Name: "w", Count: 1}},
	}
	if view.IsEmpty() {
		t.Fatal("expected non-empty view")
	}
}

func TestFromPodSetsInfoCopiesDataIndependently(t *testing.T) {
	ns := map[string]string{"pool": "a100"}
	tols := []corev1.Toleration{{Key: "test"}}
	infos := []podset.PodSetInfo{
		{Count: 4, NodeSelector: ns, Tolerations: tols},
	}

	view := FromPodSetsInfo([]string{"workers"}, infos)

	// Mutate original data
	ns["pool"] = "mutated"
	tols[0].Key = "mutated"

	// View should be independent
	if view.PodSets[0].NodeSelector["pool"] != "a100" {
		t.Fatal("admission view nodeSelector was mutated")
	}
	if view.PodSets[0].Tolerations[0].Key != "test" {
		t.Fatal("admission view tolerations were mutated")
	}
}
