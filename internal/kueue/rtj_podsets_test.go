package kueue

import (
	"encoding/json"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

func TestPodSetsFromRTJTemplateSynthesizesSupportedRuntimeShape(t *testing.T) {
	rtj := testRTJForPodSets(t)

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}
	if len(podSets) != 2 {
		t.Fatalf("expected 2 pod sets, got %d", len(podSets))
	}

	driver := podSets[0]
	if driver.Name != "driver" {
		t.Fatalf("expected first pod set to be driver, got %q", driver.Name)
	}
	if driver.Count != 1 {
		t.Fatalf("expected driver count 1, got %d", driver.Count)
	}
	if got := driver.Template.Spec.Containers[0].Resources.Requests.Cpu().String(); got != "500m" {
		t.Fatalf("expected driver cpu request 500m, got %q", got)
	}
	if got := driver.Template.Spec.Containers[0].Resources.Requests.Memory().String(); got != "1Gi" {
		t.Fatalf("expected driver memory request 1Gi, got %q", got)
	}

	worker := podSets[1]
	if worker.Name != "worker" {
		t.Fatalf("expected second pod set to be worker, got %q", worker.Name)
	}
	if worker.Count != 6 {
		t.Fatalf("expected worker count 6, got %d", worker.Count)
	}
	if got := worker.Template.Spec.Containers[0].Resources.Requests.Cpu().String(); got != "2" {
		t.Fatalf("expected worker cpu request 2, got %q", got)
	}
	if got := worker.Template.Spec.Containers[0].Resources.Requests.Memory().String(); got != "8Gi" {
		t.Fatalf("expected worker memory request 8Gi, got %q", got)
	}
	if got := worker.Template.Spec.NodeSelector["node.kubernetes.io/instance-type"]; got != "gpu" {
		t.Fatalf("expected worker node selector to be preserved, got %#v", worker.Template.Spec.NodeSelector)
	}
}

func TestPodSetsFromRTJTemplateRejectsBlankReplicatedJobName(t *testing.T) {
	rtj := testRTJForPodSets(t)
	spec, err := rtjjobset.ParseTemplate(rtj.Spec.Runtime.Template.Spec)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	spec.ReplicatedJobs[0].Name = ""

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	rtj.Spec.Runtime.Template.Spec = runtime.RawExtension{Raw: raw}

	if _, err := PodSetsFromRTJTemplate(rtj); err == nil {
		t.Fatalf("expected blank replicated job name to fail")
	}
}

func TestPodSetsFromRTJTemplateDefaultModeDoesNotEmitMinCount(t *testing.T) {
	// Operator flag off, per-job flag on → no MinCount.
	SetExperimentalPartialAdmission(false)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Resume.AllowWorldSizeChange = true
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "worker",
		EnablePartialAdmission: true,
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	for _, ps := range podSets {
		if ps.MinCount != nil {
			t.Fatalf("expected no MinCount when operator flag is off, pod set %q has MinCount=%d", ps.Name, *ps.MinCount)
		}
	}
}

func TestPodSetsFromRTJTemplateExperimentalModeEmitsMinCountForWorkerOnly(t *testing.T) {
	SetExperimentalPartialAdmission(true)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Resume.AllowWorldSizeChange = true
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "worker",
		EnablePartialAdmission: true,
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}
	if len(podSets) != 2 {
		t.Fatalf("expected 2 pod sets, got %d", len(podSets))
	}

	driver := podSets[0]
	if driver.Name != "driver" {
		t.Fatalf("expected first pod set to be driver, got %q", driver.Name)
	}
	if driver.MinCount != nil {
		t.Fatalf("expected driver to have no MinCount, got %d", *driver.MinCount)
	}

	worker := podSets[1]
	if worker.Name != "worker" {
		t.Fatalf("expected second pod set to be worker, got %q", worker.Name)
	}
	if worker.Count != 8 {
		t.Fatalf("expected worker count=8 (preferredCount), got %d", worker.Count)
	}
	if worker.MinCount == nil {
		t.Fatalf("expected worker MinCount to be set")
	}
	if *worker.MinCount != 4 {
		t.Fatalf("expected worker MinCount=4, got %d", *worker.MinCount)
	}
}

func TestPodSetsFromRTJTemplatePreferredCountOverridesTemplateCount(t *testing.T) {
	SetExperimentalPartialAdmission(false)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount: 16,
		PodSetName:     "worker",
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Driver count should remain unchanged from template.
	if podSets[0].Name != "driver" || podSets[0].Count != 1 {
		t.Fatalf("expected driver count=1 (unchanged), got %d", podSets[0].Count)
	}

	// Worker count should use preferredCount, not template-derived count.
	if podSets[1].Name != "worker" || podSets[1].Count != 16 {
		t.Fatalf("expected worker count=16 (preferredCount), got %d", podSets[1].Count)
	}
}

func TestPodSetsFromRTJTemplateFixedSizeRTJUnchanged(t *testing.T) {
	SetExperimentalPartialAdmission(true)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	// No Parallelism spec → Phase 2 behavior.

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// Counts should match template-derived values exactly.
	if podSets[0].Count != 1 {
		t.Fatalf("expected driver count=1, got %d", podSets[0].Count)
	}
	if podSets[1].Count != 6 {
		t.Fatalf("expected worker count=6 (from template 2*3), got %d", podSets[1].Count)
	}
	for _, ps := range podSets {
		if ps.MinCount != nil {
			t.Fatalf("expected no MinCount for fixed-size RTJ, pod set %q has MinCount=%d", ps.Name, *ps.MinCount)
		}
	}
}

func TestPodSetsFromRTJTemplateDefaultsWorkerToFirstReplicatedJob(t *testing.T) {
	SetExperimentalPartialAdmission(true)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Resume.AllowWorldSizeChange = true
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount:         10,
		MinCount:               ptr.To[int32](2),
		EnablePartialAdmission: true,
		// PodSetName not set → defaults to first replicatedJob ("driver").
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	// First pod set (driver) should get the override.
	if podSets[0].Count != 10 {
		t.Fatalf("expected first pod set count=10 (preferredCount), got %d", podSets[0].Count)
	}
	if podSets[0].MinCount == nil || *podSets[0].MinCount != 2 {
		t.Fatalf("expected first pod set MinCount=2, got %v", podSets[0].MinCount)
	}

	// Second pod set (worker) should be unchanged.
	if podSets[1].MinCount != nil {
		t.Fatalf("expected second pod set to have no MinCount, got %d", *podSets[1].MinCount)
	}
}

func TestPodSetsFromRTJTemplatePartialAdmissionDisabledPerJobIgnoresMinCount(t *testing.T) {
	SetExperimentalPartialAdmission(true)
	defer SetExperimentalPartialAdmission(false)

	rtj := testRTJForPodSets(t)
	rtj.Spec.Parallelism = &trainingv1alpha1.ParallelismSpec{
		PreferredCount: 8,
		MinCount:       ptr.To[int32](4),
		PodSetName:     "worker",
		// EnablePartialAdmission is false → EffectiveMinCount returns nil.
	}

	podSets, err := PodSetsFromRTJTemplate(rtj)
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}

	worker := podSets[1]
	if worker.Count != 8 {
		t.Fatalf("expected worker count=8, got %d", worker.Count)
	}
	if worker.MinCount != nil {
		t.Fatalf("expected no MinCount when per-job enablePartialAdmission is false, got %d", *worker.MinCount)
	}
}

func testRTJForPodSets(t *testing.T) *trainingv1alpha1.ResumableTrainingJob {
	t.Helper()

	templateSpec := rtjjobset.Spec{
		ReplicatedJobs: []rtjjobset.ReplicatedJob{
			{
				Name:     "driver",
				Replicas: ptr.To[int32](1),
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Parallelism: ptr.To[int32](1),
						Completions: ptr.To[int32](1),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{{
									Name:  "trainer",
									Image: "busybox:1.36.1",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("1Gi"),
										},
									},
								}},
							},
						},
					},
				},
			},
			{
				Name:     "worker",
				Replicas: ptr.To[int32](2),
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Parallelism: ptr.To[int32](4),
						Completions: ptr.To[int32](3),
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"role": "worker"},
							},
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								NodeSelector: map[string]string{
									"node.kubernetes.io/instance-type": "gpu",
								},
								Containers: []corev1.Container{{
									Name:  "trainer",
									Image: "busybox:1.36.1",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("2"),
											corev1.ResourceMemory: resource.MustParse("8Gi"),
										},
									},
								}},
							},
						},
					},
				},
			},
		},
	}
	rawTemplate, err := json.Marshal(templateSpec)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}

	rtj := &trainingv1alpha1.ResumableTrainingJob{
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			QueueName:                 "training",
			WorkloadPriorityClassName: "phase2-dev",
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "registry.example.io/trainer:latest",
				CodeVersion: "gitsha-123",
				WorldSize:   4,
				GPUShape:    "nvidia-l4",
			},
			Runtime: trainingv1alpha1.ResumableTrainingJobRuntime{
				Mode:          trainingv1alpha1.RuntimeModeDDP,
				OptimizerMode: "adamw",
				ShardingMode:  "none",
				Template: trainingv1alpha1.JobSetTemplate{
					Spec: runtime.RawExtension{Raw: rawTemplate},
				},
			},
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI:      "s3://rtj-checkpoints/demo",
				Interval:        metav1.Duration{Duration: time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 2 * time.Minute},
				MaxDrainTime:    metav1.Duration{Duration: 5 * time.Minute},
			},
			Resume: trainingv1alpha1.ResumePolicy{
				MaxResumeRetries: 3,
			},
			Control: &trainingv1alpha1.ControlSpec{
				DesiredState: trainingv1alpha1.DesiredStateRunning,
			},
		},
	}
	rtj.Default()
	return rtj
}
