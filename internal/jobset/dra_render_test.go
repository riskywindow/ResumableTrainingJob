package jobset

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// --- InjectDRAClaims tests ---

func TestInjectDRAClaims_SingleClaim(t *testing.T) {
	spec := testDRASpecSingleContainer()
	claims := []DRAClaimInjection{
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: []string{"trainer"}},
	}

	InjectDRAClaims(&spec, claims)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.ResourceClaims) != 1 {
		t.Fatalf("expected 1 PodResourceClaim, got %d", len(pod.ResourceClaims))
	}
	if pod.ResourceClaims[0].Name != "gpu" {
		t.Fatalf("expected claim name 'gpu', got %q", pod.ResourceClaims[0].Name)
	}
	if pod.ResourceClaims[0].ResourceClaimTemplateName == nil {
		t.Fatal("expected ResourceClaimTemplateName to be set")
	}
	if *pod.ResourceClaims[0].ResourceClaimTemplateName != "my-rtj-gpu" {
		t.Fatalf("expected template name 'my-rtj-gpu', got %q", *pod.ResourceClaims[0].ResourceClaimTemplateName)
	}

	container := pod.Containers[0]
	if len(container.Resources.Claims) != 1 {
		t.Fatalf("expected 1 container ResourceClaim, got %d", len(container.Resources.Claims))
	}
	if container.Resources.Claims[0].Name != "gpu" {
		t.Fatalf("expected container claim name 'gpu', got %q", container.Resources.Claims[0].Name)
	}
}

func TestInjectDRAClaims_MultipleClaims(t *testing.T) {
	spec := testDRASpecSingleContainer()
	claims := []DRAClaimInjection{
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: []string{"trainer"}},
		{ClaimName: "rdma", TemplateName: "my-rtj-rdma", Containers: []string{"trainer"}},
	}

	InjectDRAClaims(&spec, claims)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.ResourceClaims) != 2 {
		t.Fatalf("expected 2 PodResourceClaims, got %d", len(pod.ResourceClaims))
	}

	container := pod.Containers[0]
	if len(container.Resources.Claims) != 2 {
		t.Fatalf("expected 2 container ResourceClaims, got %d", len(container.Resources.Claims))
	}
}

func TestInjectDRAClaims_TargetedContainers(t *testing.T) {
	spec := testDRASpecTwoContainers()
	claims := []DRAClaimInjection{
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: []string{"trainer"}},
	}

	InjectDRAClaims(&spec, claims)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec

	// PodResourceClaim should be present (pod-level).
	if len(pod.ResourceClaims) != 1 {
		t.Fatalf("expected 1 PodResourceClaim, got %d", len(pod.ResourceClaims))
	}

	// Only "trainer" container should have the claim attached.
	trainer := pod.Containers[0]
	if trainer.Name != "trainer" {
		t.Fatalf("expected first container to be 'trainer', got %q", trainer.Name)
	}
	if len(trainer.Resources.Claims) != 1 {
		t.Fatalf("expected trainer to have 1 claim, got %d", len(trainer.Resources.Claims))
	}

	// "sidecar" container should NOT have the claim.
	sidecar := pod.Containers[1]
	if sidecar.Name != "sidecar" {
		t.Fatalf("expected second container to be 'sidecar', got %q", sidecar.Name)
	}
	if len(sidecar.Resources.Claims) != 0 {
		t.Fatalf("expected sidecar to have 0 claims, got %d", len(sidecar.Resources.Claims))
	}
}

func TestInjectDRAClaims_NoMatchingContainers(t *testing.T) {
	spec := testDRASpecSingleContainer()
	claims := []DRAClaimInjection{
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: []string{"nonexistent"}},
	}

	InjectDRAClaims(&spec, claims)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.ResourceClaims) != 0 {
		t.Fatalf("expected 0 PodResourceClaims when no containers match, got %d", len(pod.ResourceClaims))
	}
}

func TestInjectDRAClaims_EmptyClaimsIsNoOp(t *testing.T) {
	spec := testDRASpecSingleContainer()

	InjectDRAClaims(&spec, nil)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.ResourceClaims) != 0 {
		t.Fatalf("expected 0 PodResourceClaims, got %d", len(pod.ResourceClaims))
	}
}

func TestInjectDRAClaims_Idempotent(t *testing.T) {
	spec := testDRASpecSingleContainer()
	claims := []DRAClaimInjection{
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: []string{"trainer"}},
	}

	InjectDRAClaims(&spec, claims)
	InjectDRAClaims(&spec, claims)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.ResourceClaims) != 1 {
		t.Fatalf("expected 1 PodResourceClaim after double injection, got %d", len(pod.ResourceClaims))
	}
	container := pod.Containers[0]
	if len(container.Resources.Claims) != 1 {
		t.Fatalf("expected 1 container ResourceClaim after double injection, got %d", len(container.Resources.Claims))
	}
}

func TestInjectDRAClaims_MultipleReplicatedJobs(t *testing.T) {
	spec := testDRASpecLeaderWorker()
	claims := []DRAClaimInjection{
		// Only target "worker" container, which exists in the worker replicatedJob.
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: []string{"worker"}},
	}

	InjectDRAClaims(&spec, claims)

	// Leader pod should NOT have the claim (no matching container).
	leaderPod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(leaderPod.ResourceClaims) != 0 {
		t.Fatalf("expected leader to have 0 PodResourceClaims, got %d", len(leaderPod.ResourceClaims))
	}

	// Worker pod should have the claim.
	workerPod := spec.ReplicatedJobs[1].Template.Spec.Template.Spec
	if len(workerPod.ResourceClaims) != 1 {
		t.Fatalf("expected worker to have 1 PodResourceClaim, got %d", len(workerPod.ResourceClaims))
	}
	if workerPod.Containers[0].Resources.Claims[0].Name != "gpu" {
		t.Fatalf("expected worker container claim name 'gpu', got %q", workerPod.Containers[0].Resources.Claims[0].Name)
	}
}

func TestInjectDRAClaims_AllContainersWhenTargetsEmpty(t *testing.T) {
	spec := testDRASpecTwoContainers()
	claims := []DRAClaimInjection{
		// Empty Containers means all containers get the claim.
		{ClaimName: "gpu", TemplateName: "my-rtj-gpu", Containers: nil},
	}

	InjectDRAClaims(&spec, claims)

	pod := spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if len(pod.ResourceClaims) != 1 {
		t.Fatalf("expected 1 PodResourceClaim, got %d", len(pod.ResourceClaims))
	}
	for _, c := range pod.Containers {
		if len(c.Resources.Claims) != 1 {
			t.Fatalf("expected all containers to have 1 claim, container %q has %d", c.Name, len(c.Resources.Claims))
		}
	}
}

// --- BuildDRAClaimInjections tests ---

func TestBuildDRAClaimInjections_Disabled(t *testing.T) {
	rtj := testRTJForDRA()
	rtj.Spec.Devices = nil

	injections := BuildDRAClaimInjections(rtj)
	if injections != nil {
		t.Fatalf("expected nil injections when devices disabled, got %d", len(injections))
	}
}

func TestBuildDRAClaimInjections_NoStatus(t *testing.T) {
	rtj := testRTJForDRA()
	rtj.Spec.Devices = &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{Name: "gpu", Containers: []string{"trainer"}, Request: trainingv1alpha1.DeviceRequestSpec{DeviceClassName: "gpu.example.com", Count: 8}},
		},
	}
	rtj.Status.Devices = nil

	injections := BuildDRAClaimInjections(rtj)
	if injections != nil {
		t.Fatalf("expected nil injections when no status, got %d", len(injections))
	}
}

func TestBuildDRAClaimInjections_Builds(t *testing.T) {
	rtj := testRTJForDRA()
	rtj.Spec.Devices = &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{Name: "gpu", Containers: []string{"trainer"}, Request: trainingv1alpha1.DeviceRequestSpec{DeviceClassName: "gpu.example.com", Count: 8}},
			{Name: "rdma", Containers: []string{"trainer"}, Request: trainingv1alpha1.DeviceRequestSpec{DeviceClassName: "rdma.example.com", Count: 1}},
		},
	}
	rtj.Status.Devices = &trainingv1alpha1.DeviceStatus{
		DeviceMode: trainingv1alpha1.DeviceModeDRA,
		ResourceClaimTemplateRefs: []trainingv1alpha1.ResourceClaimTemplateReference{
			{Name: "my-rtj-gpu", ClaimName: "gpu"},
			{Name: "my-rtj-rdma", ClaimName: "rdma"},
		},
	}

	injections := BuildDRAClaimInjections(rtj)
	if len(injections) != 2 {
		t.Fatalf("expected 2 injections, got %d", len(injections))
	}

	if injections[0].ClaimName != "gpu" || injections[0].TemplateName != "my-rtj-gpu" {
		t.Fatalf("unexpected first injection: %+v", injections[0])
	}
	if injections[1].ClaimName != "rdma" || injections[1].TemplateName != "my-rtj-rdma" {
		t.Fatalf("unexpected second injection: %+v", injections[1])
	}
}

func TestBuildDRAClaimInjections_SkipsMissingRef(t *testing.T) {
	rtj := testRTJForDRA()
	rtj.Spec.Devices = &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{Name: "gpu", Containers: []string{"trainer"}, Request: trainingv1alpha1.DeviceRequestSpec{DeviceClassName: "gpu.example.com", Count: 8}},
			{Name: "rdma", Containers: []string{"trainer"}, Request: trainingv1alpha1.DeviceRequestSpec{DeviceClassName: "rdma.example.com", Count: 1}},
		},
	}
	// Only gpu has a template ref in status.
	rtj.Status.Devices = &trainingv1alpha1.DeviceStatus{
		DeviceMode: trainingv1alpha1.DeviceModeDRA,
		ResourceClaimTemplateRefs: []trainingv1alpha1.ResourceClaimTemplateReference{
			{Name: "my-rtj-gpu", ClaimName: "gpu"},
		},
	}

	injections := BuildDRAClaimInjections(rtj)
	if len(injections) != 1 {
		t.Fatalf("expected 1 injection (rdma ref missing), got %d", len(injections))
	}
	if injections[0].ClaimName != "gpu" {
		t.Fatalf("expected gpu injection, got %q", injections[0].ClaimName)
	}
}

func TestBuildDRAClaimInjections_DisabledMode(t *testing.T) {
	rtj := testRTJForDRA()
	rtj.Spec.Devices = &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDisabled,
	}

	injections := BuildDRAClaimInjections(rtj)
	if injections != nil {
		t.Fatalf("expected nil injections when mode disabled, got %d", len(injections))
	}
}

// --- Helper functions for DRA render tests ---

func testDRASpecSingleContainer() Spec {
	return Spec{
		ReplicatedJobs: []ReplicatedJob{
			testDRAReplicatedJob("trainer", "trainer"),
		},
	}
}

func testDRASpecTwoContainers() Spec {
	rj := testDRAReplicatedJob("trainer", "trainer")
	rj.Template.Spec.Template.Spec.Containers = append(
		rj.Template.Spec.Template.Spec.Containers,
		corev1.Container{Name: "sidecar", Image: "sidecar:latest"},
	)
	return Spec{
		ReplicatedJobs: []ReplicatedJob{rj},
	}
}

func testDRASpecLeaderWorker() Spec {
	return Spec{
		ReplicatedJobs: []ReplicatedJob{
			testDRAReplicatedJob("leader", "leader"),
			testDRAReplicatedJob("worker", "worker"),
		},
	}
}

func testDRAReplicatedJob(name, containerName string) ReplicatedJob {
	return ReplicatedJob{
		Name:     name,
		Replicas: ptr.To[int32](1),
		Template: batchv1.JobTemplateSpec{
			Spec: batchv1.JobSpec{
				Parallelism: ptr.To[int32](1),
				Completions: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{Name: containerName, Image: containerName + ":latest"},
						},
					},
				},
			},
		},
	}
}

func testRTJForDRA() *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rtj",
			Namespace: "default",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			QueueName: "training",
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "trainer:latest",
				CodeVersion: "v1",
				WorldSize:   2,
				GPUShape:    "a100",
			},
			Runtime: trainingv1alpha1.ResumableTrainingJobRuntime{
				Mode: trainingv1alpha1.RuntimeModeDDP,
				Template: trainingv1alpha1.JobSetTemplate{
					APIVersion: trainingv1alpha1.DefaultJobSetAPIVersion,
					Kind:       trainingv1alpha1.DefaultJobSetKind,
					Spec: runtime.RawExtension{
						Raw: []byte(`{
							"replicatedJobs":[{
								"name":"trainer",
								"replicas":1,
								"template":{
									"spec":{
										"parallelism":1,
										"completions":1,
										"template":{
											"spec":{
												"restartPolicy":"Never",
												"containers":[{"name":"trainer","image":"trainer:latest"}]
											}
										}
									}
								}
							}]
						}`),
					},
				},
			},
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI: "s3://checkpoints/",
			},
			Resume: trainingv1alpha1.ResumePolicy{
				MaxResumeRetries: 3,
			},
		},
	}
}
