package jobset

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/provisioning"
	"github.com/example/checkpoint-native-preemption-controller/internal/topology"
)

func TestApplyPodSetUpdatesAdditiveLabels(t *testing.T) {
	rtj := testRTJ()
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name:   "trainer",
				Labels: map[string]string{"provisioning.example.com/pool": "reserved"},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	podMeta := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.ObjectMeta
	if podMeta.Labels == nil {
		t.Fatal("expected pod labels to be set")
	}
	if podMeta.Labels["provisioning.example.com/pool"] != "reserved" {
		t.Fatalf("expected label provisioning.example.com/pool=reserved, got %v", podMeta.Labels)
	}
}

func TestApplyPodSetUpdatesAdditiveNodeSelector(t *testing.T) {
	rtj := testRTJ()
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name:         "trainer",
				NodeSelector: map[string]string{"provisioning.example.com/pool": "reserved-gpu"},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if pod.NodeSelector == nil {
		t.Fatal("expected nodeSelector to be set")
	}
	if pod.NodeSelector["provisioning.example.com/pool"] != "reserved-gpu" {
		t.Fatalf("expected nodeSelector key, got %v", pod.NodeSelector)
	}
}

func TestApplyPodSetUpdatesAdditiveAnnotations(t *testing.T) {
	rtj := testRTJ()
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name:        "trainer",
				Annotations: map[string]string{"provisioning.example.com/request-id": "pr-123"},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	podMeta := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.ObjectMeta
	if podMeta.Annotations == nil {
		t.Fatal("expected pod annotations to be set")
	}
	if podMeta.Annotations["provisioning.example.com/request-id"] != "pr-123" {
		t.Fatalf("expected annotation, got %v", podMeta.Annotations)
	}
}

func TestApplyPodSetUpdatesAdditiveTolerations(t *testing.T) {
	rtj := testRTJ()
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name: "trainer",
				Tolerations: []corev1.Toleration{
					{Key: "provisioning.example.com/reserved", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(pod.Tolerations))
	}
	if pod.Tolerations[0].Key != "provisioning.example.com/reserved" {
		t.Fatalf("expected toleration key, got %v", pod.Tolerations[0])
	}
}

func TestApplyPodSetUpdatesConflictingNodeSelectorFails(t *testing.T) {
	rtj := testRTJWithNodeSelector() // Has "cloud.google.com/gke-accelerator":"nvidia-tesla-a100"
	_, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name: "trainer",
				// Conflicts with existing nodeSelector value.
				NodeSelector: map[string]string{
					"cloud.google.com/gke-accelerator": "nvidia-h100",
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for conflicting nodeSelector update")
	}
}

func TestApplyPodSetUpdatesSameValueNotConflict(t *testing.T) {
	rtj := testRTJWithNodeSelector() // Has "cloud.google.com/gke-accelerator":"nvidia-tesla-a100"
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name: "trainer",
				// Same value as existing — not a conflict.
				NodeSelector: map[string]string{
					"cloud.google.com/gke-accelerator": "nvidia-tesla-a100",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error for same-value nodeSelector update: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if pod.NodeSelector["cloud.google.com/gke-accelerator"] != "nvidia-tesla-a100" {
		t.Fatalf("expected preserved nodeSelector, got %v", pod.NodeSelector)
	}
}

func TestApplyPodSetUpdatesPreservesExistingWithNewKeys(t *testing.T) {
	rtj := testRTJWithNodeSelector() // Has "cloud.google.com/gke-accelerator":"nvidia-tesla-a100"
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name: "trainer",
				// New key, no conflict.
				NodeSelector: map[string]string{
					"provisioning.example.com/pool": "reserved",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	// Both existing and new keys should be present.
	if pod.NodeSelector["cloud.google.com/gke-accelerator"] != "nvidia-tesla-a100" {
		t.Fatal("expected existing nodeSelector to be preserved")
	}
	if pod.NodeSelector["provisioning.example.com/pool"] != "reserved" {
		t.Fatal("expected new nodeSelector to be added")
	}
}

func TestApplyPodSetUpdatesNoPodSetMatch(t *testing.T) {
	rtj := testRTJ()
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"nonexistent-podset": {
				Name:         "nonexistent-podset",
				NodeSelector: map[string]string{"key": "value"},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	// Should succeed without applying anything.
	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if pod.NodeSelector != nil {
		t.Fatalf("expected no nodeSelector when PodSet doesn't match, got %v", pod.NodeSelector)
	}
}

func TestApplyPodSetUpdatesEmptyUpdatesIsNoop(t *testing.T) {
	rtj := testRTJ()
	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates:        map[string]provisioning.PodSetUpdateEntry{},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if pod.NodeSelector != nil {
		t.Fatalf("expected no nodeSelector with empty updates, got %v", pod.NodeSelector)
	}
}

func TestApplyPodSetUpdatesWithTopologyCoexist(t *testing.T) {
	rtj := testRTJ()
	topoResult := testSingleZoneTopology("trainer", "us-east-1a")

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		TopologyResult:       topoResult,
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name:         "trainer",
				NodeSelector: map[string]string{"provisioning.example.com/pool": "reserved"},
				Tolerations: []corev1.Toleration{
					{Key: "provisioning.example.com/reserved", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	// Both topology nodeSelector and podSetUpdate nodeSelector should be present.
	if pod.NodeSelector["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatal("expected topology nodeSelector to be present")
	}
	if pod.NodeSelector["provisioning.example.com/pool"] != "reserved" {
		t.Fatal("expected provisioning nodeSelector to be present")
	}
	if len(pod.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(pod.Tolerations))
	}
}

func TestApplyPodSetUpdatesConflictMessage(t *testing.T) {
	spec := &Spec{
		ReplicatedJobs: []ReplicatedJob{
			testReplicatedJobWithNodeSelector("trainer", map[string]string{
				"existing-key": "existing-value",
			}),
		},
	}

	updates := map[string]provisioning.PodSetUpdateEntry{
		"trainer": {
			Name:         "trainer",
			NodeSelector: map[string]string{"existing-key": "different-value"},
		},
	}

	result := ApplyPodSetUpdates(spec, updates)
	if result.Applied {
		t.Fatal("expected Applied=false for conflicting update")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}
	msg := result.ConflictMessage()
	if msg == "" {
		t.Fatal("expected non-empty conflict message")
	}
}

func TestApplyPodSetUpdatesDuplicateTolerationDeduped(t *testing.T) {
	rtj := testRTJWithToleration()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		PodSetUpdates: map[string]provisioning.PodSetUpdateEntry{
			"trainer": {
				Name: "trainer",
				// Same toleration that already exists in the template.
				Tolerations: []corev1.Toleration{
					{Key: "existing-toleration", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	pod := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	// Should not duplicate the existing toleration.
	count := 0
	for _, tol := range pod.Tolerations {
		if tol.Key == "existing-toleration" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 existing-toleration, got %d", count)
	}
}

// --- Test helpers ---

func testReplicatedJobWithNodeSelector(name string, nodeSelector map[string]string) ReplicatedJob {
	replicas := int32(1)
	return ReplicatedJob{
		Name:     name,
		Replicas: &replicas,
		Template: batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						NodeSelector:  nodeSelector,
						Containers: []corev1.Container{
							{Name: name, Image: "counter:latest"},
						},
					},
				},
			},
		},
	}
}

func testSingleZoneTopology(podSetName, zone string) *topology.ParseResult {
	return &topology.ParseResult{
		PodSets: map[string]*topology.PodSetTopology{
			podSetName: {
				PodSetName: podSetName,
				Levels:     []string{"topology.kubernetes.io/zone"},
				Domains: []topology.DomainAssignment{
					{
						Labels: map[string]string{"topology.kubernetes.io/zone": zone},
						Count:  2,
					},
				},
			},
		},
	}
}

func testRTJWithToleration() *trainingv1alpha1.ResumableTrainingJob {
	rtj := testRTJ()
	rtj.Spec.Runtime.Template.Spec = runtime.RawExtension{
		Raw: []byte(`{
			"replicatedJobs":[
				{
					"name":"trainer",
					"replicas":1,
					"template":{
						"spec":{
							"parallelism":1,
							"completions":1,
							"template":{
								"spec":{
									"restartPolicy":"Never",
									"tolerations":[
										{"key":"existing-toleration","operator":"Exists","effect":"NoSchedule"}
									],
									"containers":[{"name":"trainer","image":"counter:latest"}]
								}
							}
						}
					}
				}
			]
		}`),
	}
	return rtj
}
