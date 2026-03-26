package jobset

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/topology"
)

func TestInjectTopologyNilResultIsNoOp(t *testing.T) {
	spec, _ := ParseTemplate(testRTJ().Spec.Runtime.Template.Spec)
	result, err := InjectTopology(&spec, "trainer", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Injected {
		t.Fatal("expected no injection for nil topology result")
	}
}

func TestInjectTopologySingleDomain(t *testing.T) {
	spec, _ := ParseTemplate(testRTJ().Spec.Runtime.Template.Spec)
	topoResult := &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			"trainer": {
				PodSetName: "trainer",
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{
						Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"},
						Count:  4,
					},
				},
			},
		},
	}

	result, err := InjectTopology(&spec, "trainer", topoResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Injected {
		t.Fatal("expected injection for single domain")
	}

	pod := podSpec(&spec.ReplicatedJobs[0])
	if pod.NodeSelector == nil {
		t.Fatal("expected nodeSelector to be set")
	}
	if got := pod.NodeSelector["topology.kubernetes.io/zone"]; got != "us-east-1a" {
		t.Fatalf("expected zone us-east-1a in nodeSelector, got %q", got)
	}
}

func TestInjectTopologyMultiDomainHomogeneous(t *testing.T) {
	spec, _ := ParseTemplate(testRTJ().Spec.Runtime.Template.Spec)
	topoResult := &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			"trainer": {
				PodSetName: "trainer",
				Levels:     []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"},
				Domains: []topology.DomainAssignment{
					{
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "us-east-1a",
							"kubernetes.io/hostname":      "node-1",
						},
						Count: 2,
					},
					{
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "us-east-1a",
							"kubernetes.io/hostname":      "node-2",
						},
						Count: 2,
					},
				},
			},
		},
	}

	result, err := InjectTopology(&spec, "trainer", topoResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Injected {
		t.Fatal("expected injection for homogeneous multi-domain")
	}

	pod := podSpec(&spec.ReplicatedJobs[0])
	if pod.NodeSelector["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatalf("expected common zone in nodeSelector, got %v", pod.NodeSelector)
	}
	// Hostname should NOT be in nodeSelector since it differs per domain.
	if _, has := pod.NodeSelector["kubernetes.io/hostname"]; has {
		t.Fatal("hostname should not be in nodeSelector when it differs per domain")
	}
}

func TestInjectTopologyFailsForNonRepresentable(t *testing.T) {
	spec, _ := ParseTemplate(testRTJ().Spec.Runtime.Template.Spec)
	topoResult := &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			"trainer": {
				PodSetName: "trainer",
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 2},
					{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1b"}, Count: 2},
				},
			},
		},
	}

	_, err := InjectTopology(&spec, "trainer", topoResult)
	if err == nil {
		t.Fatal("expected error for non-representable topology")
	}
}

func TestInjectTopologyPreservesExistingNodeSelector(t *testing.T) {
	rtj := testRTJWithNodeSelector()
	spec, _ := ParseTemplate(rtj.Spec.Runtime.Template.Spec)
	topoResult := &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			"trainer": {
				PodSetName: "trainer",
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 2},
				},
			},
		},
	}

	result, err := InjectTopology(&spec, "trainer", topoResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Injected {
		t.Fatal("expected injection")
	}

	pod := podSpec(&spec.ReplicatedJobs[0])
	if pod.NodeSelector["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatalf("expected topology zone in nodeSelector")
	}
	if pod.NodeSelector["cloud.google.com/gke-accelerator"] != "nvidia-tesla-a100" {
		t.Fatalf("expected existing nodeSelector to be preserved")
	}
}

func TestInjectTopologyLeaderAndWorker(t *testing.T) {
	rtj := testRTJWithLeaderAndWorker()
	spec, _ := ParseTemplate(rtj.Spec.Runtime.Template.Spec)
	topoResult := &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			"leader": {
				PodSetName: "leader",
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 1},
				},
			},
			"worker": {
				PodSetName: "worker",
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 4},
				},
			},
		},
	}

	result, err := InjectTopology(&spec, "worker", topoResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Injected {
		t.Fatal("expected injection")
	}

	// Both leader and worker should get topology.
	leaderPod := podSpec(&spec.ReplicatedJobs[0])
	if leaderPod.NodeSelector["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatal("expected leader to get topology nodeSelector")
	}
	workerPod := podSpec(&spec.ReplicatedJobs[1])
	if workerPod.NodeSelector["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatal("expected worker to get topology nodeSelector")
	}
}

func TestInjectTopologyNoMatchingPodSet(t *testing.T) {
	spec, _ := ParseTemplate(testRTJ().Spec.Runtime.Template.Spec)
	topoResult := &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			"nonexistent": {
				PodSetName: "nonexistent",
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{Labels: map[string]string{"topology.kubernetes.io/zone": "us-east-1a"}, Count: 4},
				},
			},
		},
	}

	result, err := InjectTopology(&spec, "trainer", topoResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Injected {
		t.Fatal("expected no injection when PodSet does not match")
	}
}

// testRTJWithNodeSelector is already defined in render_test.go via the testRTJ
// helpers. We use local minimal versions here.
func topoTestRTJ() *trainingv1alpha1.ResumableTrainingJob {
	return testRTJ()
}

func topoTestSpec(rawJSON string) runtime.RawExtension {
	return runtime.RawExtension{Raw: []byte(rawJSON)}
}
