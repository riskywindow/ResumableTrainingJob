package provisioning

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

func TestParsePodSetUpdatesNilInput(t *testing.T) {
	result := ParsePodSetUpdates(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestParsePodSetUpdatesEmptyInput(t *testing.T) {
	result := ParsePodSetUpdates([]kueuev1beta2.PodSetUpdate{})
	if result != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestParsePodSetUpdatesSingleUpdate(t *testing.T) {
	updates := []kueuev1beta2.PodSetUpdate{
		{
			Name:         "workers",
			Labels:       map[string]string{"provisioned": "true"},
			Annotations:  map[string]string{"cloud.google.com/node-pool": "gpu-pool"},
			NodeSelector: map[string]string{"cloud.google.com/gke-nodepool": "gpu-pool-1"},
			Tolerations: []corev1.Toleration{
				{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
		},
	}

	result := ParsePodSetUpdates(updates)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}

	entry := result[0]
	if entry.Name != "workers" {
		t.Fatalf("expected name 'workers', got %q", entry.Name)
	}
	if entry.Labels["provisioned"] != "true" {
		t.Fatalf("expected label provisioned=true, got %v", entry.Labels)
	}
	if entry.Annotations["cloud.google.com/node-pool"] != "gpu-pool" {
		t.Fatalf("expected annotation, got %v", entry.Annotations)
	}
	if entry.NodeSelector["cloud.google.com/gke-nodepool"] != "gpu-pool-1" {
		t.Fatalf("expected nodeSelector, got %v", entry.NodeSelector)
	}
	if len(entry.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(entry.Tolerations))
	}
	if entry.Tolerations[0].Key != "nvidia.com/gpu" {
		t.Fatalf("expected toleration key 'nvidia.com/gpu', got %q", entry.Tolerations[0].Key)
	}
}

func TestParsePodSetUpdatesMultiplePodSets(t *testing.T) {
	updates := []kueuev1beta2.PodSetUpdate{
		{
			Name:         "driver",
			NodeSelector: map[string]string{"pool": "cpu"},
		},
		{
			Name:         "workers",
			NodeSelector: map[string]string{"pool": "gpu"},
		},
	}

	result := ParsePodSetUpdates(updates)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result[0].Name != "driver" {
		t.Fatalf("expected first entry 'driver', got %q", result[0].Name)
	}
	if result[1].Name != "workers" {
		t.Fatalf("expected second entry 'workers', got %q", result[1].Name)
	}
}

func TestParsePodSetUpdatesCopiesDataIndependently(t *testing.T) {
	labels := map[string]string{"key": "original"}
	nodeSelector := map[string]string{"pool": "original"}
	tolerations := []corev1.Toleration{{Key: "original"}}

	updates := []kueuev1beta2.PodSetUpdate{
		{
			Name:         "workers",
			Labels:       labels,
			NodeSelector: nodeSelector,
			Tolerations:  tolerations,
		},
	}

	result := ParsePodSetUpdates(updates)

	// Mutate originals.
	labels["key"] = "mutated"
	nodeSelector["pool"] = "mutated"
	tolerations[0].Key = "mutated"

	// Parsed result should be independent.
	if result[0].Labels["key"] != "original" {
		t.Fatal("labels were not deep-copied")
	}
	if result[0].NodeSelector["pool"] != "original" {
		t.Fatal("nodeSelector was not deep-copied")
	}
	if result[0].Tolerations[0].Key != "original" {
		t.Fatal("tolerations were not deep-copied")
	}
}

func TestParsePodSetUpdatesNilMapsPreserved(t *testing.T) {
	updates := []kueuev1beta2.PodSetUpdate{
		{Name: "workers"},
	}
	result := ParsePodSetUpdates(updates)
	if result[0].Labels != nil {
		t.Fatal("expected nil labels")
	}
	if result[0].Annotations != nil {
		t.Fatal("expected nil annotations")
	}
	if result[0].NodeSelector != nil {
		t.Fatal("expected nil nodeSelector")
	}
	if result[0].Tolerations != nil {
		t.Fatal("expected nil tolerations")
	}
}

func TestMergePodSetUpdatesNil(t *testing.T) {
	result := MergePodSetUpdates(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestMergePodSetUpdatesEmpty(t *testing.T) {
	result := MergePodSetUpdates([]PodSetUpdateSet{})
	if result != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestMergePodSetUpdatesSingleAC(t *testing.T) {
	sets := []PodSetUpdateSet{
		{
			AdmissionCheckName: "provision-ac",
			Updates: []PodSetUpdateEntry{
				{
					Name:         "workers",
					NodeSelector: map[string]string{"pool": "gpu"},
					Labels:       map[string]string{"provisioned": "true"},
				},
			},
		},
	}

	result := MergePodSetUpdates(sets)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged pod set, got %d", len(result))
	}

	entry := result["workers"]
	if entry.NodeSelector["pool"] != "gpu" {
		t.Fatalf("expected nodeSelector pool=gpu, got %v", entry.NodeSelector)
	}
	if entry.Labels["provisioned"] != "true" {
		t.Fatalf("expected label provisioned=true, got %v", entry.Labels)
	}
}

func TestMergePodSetUpdatesMultipleACsSamePodSet(t *testing.T) {
	sets := []PodSetUpdateSet{
		{
			AdmissionCheckName: "provision-ac",
			Updates: []PodSetUpdateEntry{
				{
					Name:         "workers",
					NodeSelector: map[string]string{"pool": "from-provision"},
					Labels:       map[string]string{"ac": "provision"},
				},
			},
		},
		{
			AdmissionCheckName: "resume-readiness",
			Updates: []PodSetUpdateEntry{
				{
					Name:         "workers",
					Labels:       map[string]string{"ac": "resume", "extra": "label"},
					Tolerations:  []corev1.Toleration{{Key: "resume-tol"}},
				},
			},
		},
	}

	result := MergePodSetUpdates(sets)
	entry := result["workers"]

	// Later AC (resume-readiness) takes precedence for conflicting keys.
	if entry.Labels["ac"] != "resume" {
		t.Fatalf("expected label ac=resume (later AC wins), got %q", entry.Labels["ac"])
	}
	if entry.Labels["extra"] != "label" {
		t.Fatalf("expected label extra=label, got %q", entry.Labels["extra"])
	}
	// nodeSelector from first AC preserved.
	if entry.NodeSelector["pool"] != "from-provision" {
		t.Fatalf("expected nodeSelector pool=from-provision, got %v", entry.NodeSelector)
	}
	// Tolerations concatenated.
	if len(entry.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(entry.Tolerations))
	}
}

func TestMergePodSetUpdatesDifferentPodSets(t *testing.T) {
	sets := []PodSetUpdateSet{
		{
			AdmissionCheckName: "provision-ac",
			Updates: []PodSetUpdateEntry{
				{Name: "driver", NodeSelector: map[string]string{"pool": "cpu"}},
				{Name: "workers", NodeSelector: map[string]string{"pool": "gpu"}},
			},
		},
	}

	result := MergePodSetUpdates(sets)
	if len(result) != 2 {
		t.Fatalf("expected 2 merged pod sets, got %d", len(result))
	}
	if result["driver"].NodeSelector["pool"] != "cpu" {
		t.Fatalf("expected driver pool=cpu, got %v", result["driver"].NodeSelector)
	}
	if result["workers"].NodeSelector["pool"] != "gpu" {
		t.Fatalf("expected workers pool=gpu, got %v", result["workers"].NodeSelector)
	}
}

func TestHasPodSetUpdatesTrue(t *testing.T) {
	sets := []PodSetUpdateSet{
		{Updates: []PodSetUpdateEntry{{Name: "workers"}}},
	}
	if !HasPodSetUpdates(sets) {
		t.Fatal("expected true when updates present")
	}
}

func TestHasPodSetUpdatesFalseEmpty(t *testing.T) {
	sets := []PodSetUpdateSet{
		{Updates: nil},
	}
	if HasPodSetUpdates(sets) {
		t.Fatal("expected false when no updates")
	}
}

func TestHasPodSetUpdatesFalseNil(t *testing.T) {
	if HasPodSetUpdates(nil) {
		t.Fatal("expected false for nil")
	}
}
