package topology

import (
	"testing"

	"k8s.io/utils/ptr"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

func TestParseFromAdmissionNilAdmission(t *testing.T) {
	result, err := ParseFromAdmission(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for nil admission, got %v", result)
	}
}

func TestParseFromAdmissionNoTopology(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "worker",
				Count: ptr.To[int32](4),
			},
		},
	}
	result, err := ParseFromAdmission(admission)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result when no topology assignment present, got %v", result)
	}
}

func TestParseFromAdmissionSingleDomainUniversal(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "worker",
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
		},
	}

	result, err := ParseFromAdmission(admission)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	pst, ok := result.PodSets["worker"]
	if !ok {
		t.Fatal("expected worker PodSet in result")
	}
	if len(pst.Levels) != 1 {
		t.Fatalf("expected 1 level, got %d", len(pst.Levels))
	}
	if pst.Levels[0] != "topology.kubernetes.io/zone" {
		t.Fatalf("expected zone level, got %s", pst.Levels[0])
	}
	if len(pst.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(pst.Domains))
	}
	if pst.Domains[0].Labels["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatalf("expected zone us-east-1a, got %s", pst.Domains[0].Labels["topology.kubernetes.io/zone"])
	}
	if pst.Domains[0].Count != 4 {
		t.Fatalf("expected 4 pods, got %d", pst.Domains[0].Count)
	}
}

func TestParseFromAdmissionMultiDomainIndividual(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "worker",
				Count: ptr.To[int32](6),
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{
						{
							DomainCount: 2,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Universal: ptr.To("us-east-1a")},
								{Individual: &kueuev1beta2.TopologyAssignmentSliceLevelIndividualValues{
									Roots: []string{"node-1", "node-2"},
								}},
							},
							PodCounts: kueuev1beta2.TopologyAssignmentSlicePodCounts{
								Individual: []int32{3, 3},
							},
						},
					},
				},
			},
		},
	}

	result, err := ParseFromAdmission(admission)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	pst := result.PodSets["worker"]
	if len(pst.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(pst.Domains))
	}

	// Both domains should share the same zone.
	for i, domain := range pst.Domains {
		if domain.Labels["topology.kubernetes.io/zone"] != "us-east-1a" {
			t.Fatalf("domain[%d]: expected zone us-east-1a, got %s", i, domain.Labels["topology.kubernetes.io/zone"])
		}
	}

	if pst.Domains[0].Labels["kubernetes.io/hostname"] != "node-1" {
		t.Fatalf("domain[0]: expected hostname node-1, got %s", pst.Domains[0].Labels["kubernetes.io/hostname"])
	}
	if pst.Domains[1].Labels["kubernetes.io/hostname"] != "node-2" {
		t.Fatalf("domain[1]: expected hostname node-2, got %s", pst.Domains[1].Labels["kubernetes.io/hostname"])
	}
}

func TestParseFromAdmissionIndividualWithPrefixSuffix(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "worker",
				Count: ptr.To[int32](4),
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{"kubernetes.io/hostname"},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{
						{
							DomainCount: 2,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Individual: &kueuev1beta2.TopologyAssignmentSliceLevelIndividualValues{
									Prefix: ptr.To("gpu-node-"),
									Suffix: ptr.To("-us1"),
									Roots:  []string{"01", "02"},
								}},
							},
							PodCounts: kueuev1beta2.TopologyAssignmentSlicePodCounts{
								Universal: ptr.To[int32](2),
							},
						},
					},
				},
			},
		},
	}

	result, err := ParseFromAdmission(admission)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pst := result.PodSets["worker"]
	if pst.Domains[0].Labels["kubernetes.io/hostname"] != "gpu-node-01-us1" {
		t.Fatalf("expected gpu-node-01-us1, got %s", pst.Domains[0].Labels["kubernetes.io/hostname"])
	}
	if pst.Domains[1].Labels["kubernetes.io/hostname"] != "gpu-node-02-us1" {
		t.Fatalf("expected gpu-node-02-us1, got %s", pst.Domains[1].Labels["kubernetes.io/hostname"])
	}
}

func TestParseFromAdmissionMultipleSlices(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "worker",
				Count: ptr.To[int32](6),
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
						{
							DomainCount: 1,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Universal: ptr.To("us-east-1b")},
							},
							PodCounts: kueuev1beta2.TopologyAssignmentSlicePodCounts{
								Universal: ptr.To[int32](2),
							},
						},
					},
				},
			},
		},
	}

	result, err := ParseFromAdmission(admission)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pst := result.PodSets["worker"]
	if len(pst.Domains) != 2 {
		t.Fatalf("expected 2 domains from 2 slices, got %d", len(pst.Domains))
	}
	if pst.Domains[0].Count != 4 {
		t.Fatalf("expected 4 pods in first domain, got %d", pst.Domains[0].Count)
	}
	if pst.Domains[1].Count != 2 {
		t.Fatalf("expected 2 pods in second domain, got %d", pst.Domains[1].Count)
	}
}

func TestToTopologyStatusNil(t *testing.T) {
	result := ToTopologyStatus(nil)
	if result != nil {
		t.Fatalf("expected nil status for nil topology, got %v", result)
	}
}

func TestToTopologyStatusConvertsCorrectly(t *testing.T) {
	pst := &PodSetTopology{
		PodSetName: "worker",
		Levels:     []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"},
		Domains: []DomainAssignment{
			{
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "us-east-1a",
					"kubernetes.io/hostname":      "node-1",
				},
				Count: 3,
			},
			{
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "us-east-1a",
					"kubernetes.io/hostname":      "node-2",
				},
				Count: 3,
			},
		},
	}

	status := ToTopologyStatus(pst)
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if len(status.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(status.Levels))
	}
	if len(status.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(status.Domains))
	}
	if status.Domains[0].Values[0] != "us-east-1a" {
		t.Fatalf("expected zone us-east-1a, got %s", status.Domains[0].Values[0])
	}
	if status.Domains[0].Values[1] != "node-1" {
		t.Fatalf("expected hostname node-1, got %s", status.Domains[0].Values[1])
	}
	if status.Domains[0].Count != 3 {
		t.Fatalf("expected count 3, got %d", status.Domains[0].Count)
	}
}

func TestIsSingleDomain(t *testing.T) {
	if IsSingleDomain(nil) {
		t.Fatal("nil should not be single domain")
	}
	if IsSingleDomain(&PodSetTopology{Domains: []DomainAssignment{}}) {
		t.Fatal("empty domains should not be single domain")
	}
	if !IsSingleDomain(&PodSetTopology{Domains: []DomainAssignment{{Count: 4}}}) {
		t.Fatal("one domain should be single domain")
	}
	if IsSingleDomain(&PodSetTopology{Domains: []DomainAssignment{{Count: 2}, {Count: 2}}}) {
		t.Fatal("two domains should not be single domain")
	}
}

func TestCanRepresentInJobSetSingleDomain(t *testing.T) {
	pst := &PodSetTopology{
		Levels: []string{"topology.kubernetes.io/zone"},
		Domains: []DomainAssignment{
			{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 4},
		},
	}
	ok, reason := CanRepresentInJobSet(pst)
	if !ok {
		t.Fatalf("single domain should be representable, reason: %s", reason)
	}
}

func TestCanRepresentInJobSetMultiDomainSingleLevel(t *testing.T) {
	pst := &PodSetTopology{
		Levels: []string{"topology.kubernetes.io/zone"},
		Domains: []DomainAssignment{
			{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 2},
			{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1b"}, Count: 2},
		},
	}
	ok, reason := CanRepresentInJobSet(pst)
	if ok {
		t.Fatal("multi-domain single-level should not be representable")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCanRepresentInJobSetMultiDomainHomogeneousHigherLevels(t *testing.T) {
	pst := &PodSetTopology{
		Levels: []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"},
		Domains: []DomainAssignment{
			{Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"kubernetes.io/hostname":      "node-1",
			}, Count: 2},
			{Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"kubernetes.io/hostname":      "node-2",
			}, Count: 2},
		},
	}
	ok, reason := CanRepresentInJobSet(pst)
	if !ok {
		t.Fatalf("homogeneous higher levels should be representable, reason: %s", reason)
	}
}

func TestCanRepresentInJobSetMultiDomainHeterogeneousHigherLevels(t *testing.T) {
	pst := &PodSetTopology{
		Levels: []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"},
		Domains: []DomainAssignment{
			{Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"kubernetes.io/hostname":      "node-1",
			}, Count: 2},
			{Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1b",
				"kubernetes.io/hostname":      "node-3",
			}, Count: 2},
		},
	}
	ok, _ := CanRepresentInJobSet(pst)
	if ok {
		t.Fatal("heterogeneous higher levels should not be representable")
	}
}

func TestCommonNodeSelectorSingleDomain(t *testing.T) {
	pst := &PodSetTopology{
		Levels: []string{"topology.kubernetes.io/zone"},
		Domains: []DomainAssignment{
			{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 4},
		},
	}
	ns := CommonNodeSelector(pst)
	if ns == nil {
		t.Fatal("expected non-nil nodeSelector for single domain")
	}
	if ns["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatalf("expected zone us-east-1a, got %s", ns["topology.kubernetes.io/zone"])
	}
}

func TestCommonNodeSelectorMultiDomainHomogeneous(t *testing.T) {
	pst := &PodSetTopology{
		Levels: []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"},
		Domains: []DomainAssignment{
			{Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"kubernetes.io/hostname":      "node-1",
			}, Count: 2},
			{Labels: map[string]string{
				"topology.kubernetes.io/zone": "us-east-1a",
				"kubernetes.io/hostname":      "node-2",
			}, Count: 2},
		},
	}
	ns := CommonNodeSelector(pst)
	if ns == nil {
		t.Fatal("expected non-nil nodeSelector")
	}
	if ns["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatalf("expected common zone us-east-1a, got %s", ns["topology.kubernetes.io/zone"])
	}
	if _, has := ns["kubernetes.io/hostname"]; has {
		t.Fatal("hostname should not be in common nodeSelector (differs per domain)")
	}
}

func TestCommonNodeSelectorNoDomains(t *testing.T) {
	ns := CommonNodeSelector(nil)
	if ns != nil {
		t.Fatalf("expected nil nodeSelector for nil topology, got %v", ns)
	}
}

func TestParseErrorNoLevels(t *testing.T) {
	admission := &kueuev1beta2.Admission{
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name: "worker",
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{},
				},
			},
		},
	}
	_, err := ParseFromAdmission(admission)
	if err == nil {
		t.Fatal("expected error for empty levels")
	}
}
