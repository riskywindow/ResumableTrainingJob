package jobset

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kueueconstants "sigs.k8s.io/kueue/pkg/controller/constants"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

func TestRenderChildJobSetInjectsOperatorLabelsEnvAndVolumes(t *testing.T) {
	rtj := testRTJ()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           2,
		JobSetName:           "counter-run-2",
		ControlConfigMapName: "counter-run-2-control",
		ResumeManifestURI:    "s3://bucket/demo/manifests/ckpt-2.manifest.json",
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	if _, found := rendered.Metadata.Labels[QueueLabelKey]; found {
		t.Fatalf("expected queue label to be removed from child JobSet, got %#v", rendered.Metadata.Labels)
	}
	if _, found := rendered.Metadata.Labels[WorkloadPriorityLabelKey]; found {
		t.Fatalf("expected priority label to be removed from child JobSet, got %#v", rendered.Metadata.Labels)
	}
	if got := rendered.Metadata.Labels[ManagedByLabelKey]; got != ManagedByLabelValue {
		t.Fatalf("expected managed-by label %q, got %q", ManagedByLabelValue, got)
	}

	replicatedJob := rendered.Spec.ReplicatedJobs[0]
	pod := replicatedJob.Template.Spec.Template.Spec
	if len(pod.Volumes) != 2 {
		t.Fatalf("expected 2 injected volumes, got %d", len(pod.Volumes))
	}

	container := pod.Containers[0]
	assertEnvValue(t, container.Env, EnvStorageURI, rtj.Spec.Checkpoint.StorageURI)
	assertEnvValue(t, container.Env, EnvControlFile, ControlFilePath)
	assertEnvValue(t, container.Env, EnvRunAttempt, "2")
	assertEnvValue(t, container.Env, EnvRestoreManifestURI, "s3://bucket/demo/manifests/ckpt-2.manifest.json")
	assertEnvValue(t, container.Env, EnvStagingRoot, DefaultStagingRoot)
	assertEnvValue(t, container.Env, EnvRestoreRoot, DefaultRestoreRoot)
	assertEnvValue(t, container.Env, EnvYieldMarkerPath, DefaultYieldMarkerPath)
	assertEnvValue(t, container.Env, EnvYieldMarkerURI, "s3://phase1-checkpoints/counter/yield-markers/run-2.json")

	assertVolumeMount(t, container.VolumeMounts, ControlVolumeName, ControlMountDir)
	assertVolumeMount(t, container.VolumeMounts, StagingVolumeName, StagingMountDir)
}

func TestRenderChildJobSetStripsKueueManagementMetadata(t *testing.T) {
	rtj := testRTJ()
	rtj.Spec.Runtime.Template.Metadata = &trainingv1alpha1.EmbeddedObjectMetadata{
		Labels: map[string]string{
			"app.kubernetes.io/name":                  "counter",
			kueueconstants.QueueLabel:                 "queue-a",
			kueueconstants.WorkloadPriorityClassLabel: "priority-a",
			kueueconstants.PrebuiltWorkloadLabel:      "prebuilt-demo",
		},
		Annotations: map[string]string{
			kueueconstants.ProvReqAnnotationPrefix + "flavor":     "gpu-a",
			kueueconstants.SafeToForcefullyTerminateAnnotationKey: kueueconstants.SafeToForcefullyTerminateAnnotationValue,
			"example.com/keep": "true",
		},
	}

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	if got := rendered.Metadata.Labels["app.kubernetes.io/name"]; got != "counter" {
		t.Fatalf("expected non-Kueue label to be preserved, got %q", got)
	}
	if _, found := rendered.Metadata.Labels[kueueconstants.QueueLabel]; found {
		t.Fatalf("expected queue label to be stripped, got %#v", rendered.Metadata.Labels)
	}
	if _, found := rendered.Metadata.Labels[kueueconstants.WorkloadPriorityClassLabel]; found {
		t.Fatalf("expected workload priority label to be stripped, got %#v", rendered.Metadata.Labels)
	}
	if _, found := rendered.Metadata.Labels[kueueconstants.PrebuiltWorkloadLabel]; found {
		t.Fatalf("expected prebuilt workload label to be stripped, got %#v", rendered.Metadata.Labels)
	}
	if _, found := rendered.Metadata.Annotations[kueueconstants.ProvReqAnnotationPrefix+"flavor"]; found {
		t.Fatalf("expected provisioning annotation to be stripped, got %#v", rendered.Metadata.Annotations)
	}
	if _, found := rendered.Metadata.Annotations[kueueconstants.SafeToForcefullyTerminateAnnotationKey]; found {
		t.Fatalf("expected Kueue annotation to be stripped, got %#v", rendered.Metadata.Annotations)
	}
	if got := rendered.Metadata.Annotations["example.com/keep"]; got != "true" {
		t.Fatalf("expected non-Kueue annotation to be preserved, got %q", got)
	}
}

func TestRenderChildJobSetUnstructuredRoundTrip(t *testing.T) {
	rtj := testRTJ()

	unstructuredObj, err := RenderChildJobSetUnstructured(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
	})
	if err != nil {
		t.Fatalf("render child JobSet as unstructured: %v", err)
	}

	decoded, err := FromUnstructured(unstructuredObj)
	if err != nil {
		t.Fatalf("decode rendered child JobSet: %v", err)
	}
	if decoded.Metadata.Name != "counter-run-1" {
		t.Fatalf("expected rendered name counter-run-1, got %s", decoded.Metadata.Name)
	}
}

func assertEnvValue(t *testing.T, envVars []corev1.EnvVar, name, want string) {
	t.Helper()
	for _, envVar := range envVars {
		if envVar.Name == name {
			if envVar.Value != want {
				t.Fatalf("expected env %s=%q, got %q", name, want, envVar.Value)
			}
			return
		}
	}
	t.Fatalf("missing env var %s", name)
}

func assertVolumeMount(t *testing.T, mounts []corev1.VolumeMount, name, mountPath string) {
	t.Helper()
	for _, mount := range mounts {
		if mount.Name == name {
			if mount.MountPath != mountPath {
				t.Fatalf("expected volume mount %s at %q, got %q", name, mountPath, mount.MountPath)
			}
			return
		}
	}
	t.Fatalf("missing volume mount %s", name)
}

func TestRenderChildJobSetAppliesAdmittedWorkerCount(t *testing.T) {
	rtj := testRTJMultiReplica()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		AdmittedCounts:       map[string]int32{"trainer": 4},
		OriginalWorldSize:    2,
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	rj := rendered.Spec.ReplicatedJobs[0]
	if rj.Replicas == nil {
		t.Fatalf("expected replicas to be set")
	}
	if *rj.Replicas != 4 {
		t.Fatalf("expected 4 replicas from admitted count, got %d", *rj.Replicas)
	}
}

func TestRenderChildJobSetPreservesLeaderCountWithAdmission(t *testing.T) {
	rtj := testRTJWithLeaderAndWorker()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		AdmittedCounts:       map[string]int32{"leader": 1, "worker": 4},
		OriginalWorldSize:    3,
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	// Leader should be 1 replica (admitted count 1).
	leader := rendered.Spec.ReplicatedJobs[0]
	if leader.Replicas == nil || *leader.Replicas != 1 {
		t.Fatalf("expected leader replicas=1, got %v", leader.Replicas)
	}
	// Worker should be 4 replicas (admitted count 4).
	worker := rendered.Spec.ReplicatedJobs[1]
	if worker.Replicas == nil || *worker.Replicas != 4 {
		t.Fatalf("expected worker replicas=4, got %v", worker.Replicas)
	}
}

func TestRenderChildJobSetInjectsPhase3EnvVars(t *testing.T) {
	rtj := testRTJ()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		AdmittedCounts:       map[string]int32{"trainer": 4},
		OriginalWorldSize:    2,
		AllowWorldSizeChange: true,
		AdmittedFlavor:       "a100-80gb",
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	container := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers[0]
	assertEnvValue(t, container.Env, EnvWorldSize, "4")
	assertEnvValue(t, container.Env, EnvOriginalWorldSize, "2")
	assertEnvValue(t, container.Env, EnvAllowWorldSizeChange, "true")
	assertEnvValue(t, container.Env, EnvAdmittedFlavor, "a100-80gb")
}

func TestRenderChildJobSetOmitsPhase3EnvVarsWhenNotSet(t *testing.T) {
	rtj := testRTJ()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	container := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers[0]
	assertEnvNotPresent(t, container.Env, EnvWorldSize)
	assertEnvNotPresent(t, container.Env, EnvOriginalWorldSize)
	assertEnvNotPresent(t, container.Env, EnvAllowWorldSizeChange)
	assertEnvNotPresent(t, container.Env, EnvAdmittedFlavor)
}

func TestRenderChildJobSetStripsPodTemplateKueueLabels(t *testing.T) {
	rtj := testRTJWithKueuePodLabels()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	podLabels := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Labels
	if _, found := podLabels["kueue.x-k8s.io/managed"]; found {
		t.Fatalf("expected Kueue label to be stripped from pod template, got %v", podLabels)
	}
	if got := podLabels["app"]; got != "counter" {
		t.Fatalf("expected non-Kueue label to be preserved on pod template, got %q", got)
	}
}

func TestRenderChildJobSetPreservesFlavorNodeSelectorFromTemplate(t *testing.T) {
	rtj := testRTJWithNodeSelector()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		AdmittedCounts:       map[string]int32{"trainer": 2},
		OriginalWorldSize:    2,
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	nodeSelector := rendered.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector
	if got := nodeSelector["cloud.google.com/gke-accelerator"]; got != "nvidia-tesla-a100" {
		t.Fatalf("expected nodeSelector to be preserved from template, got %v", nodeSelector)
	}
}

func TestRenderChildJobSetUsesOriginalReplicaCountWhenNoAdmission(t *testing.T) {
	rtj := testRTJMultiReplica()

	rendered, err := RenderChildJobSet(RenderInput{
		RTJ:                  rtj,
		RunAttempt:           1,
		JobSetName:           "counter-run-1",
		ControlConfigMapName: "counter-run-1-control",
		// No AdmittedCounts → Phase 2 behavior.
	})
	if err != nil {
		t.Fatalf("render child JobSet: %v", err)
	}

	rj := rendered.Spec.ReplicatedJobs[0]
	if rj.Replicas == nil {
		t.Fatalf("expected replicas to be set")
	}
	if *rj.Replicas != 2 {
		t.Fatalf("expected original 2 replicas to be preserved, got %d", *rj.Replicas)
	}
}

func assertEnvNotPresent(t *testing.T, envVars []corev1.EnvVar, name string) {
	t.Helper()
	for _, envVar := range envVars {
		if envVar.Name == name {
			t.Fatalf("expected env %s to not be present, but found value %q", name, envVar.Value)
		}
	}
}

func testRTJMultiReplica() *trainingv1alpha1.ResumableTrainingJob {
	rtj := testRTJ()
	// Override template to have 2 replicas.
	rtj.Spec.Runtime.Template.Spec = runtime.RawExtension{
		Raw: []byte(`{
			"replicatedJobs":[
				{
					"name":"trainer",
					"replicas":2,
					"template":{
						"spec":{
							"parallelism":1,
							"completions":1,
							"template":{
								"spec":{
									"restartPolicy":"Never",
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

func testRTJWithLeaderAndWorker() *trainingv1alpha1.ResumableTrainingJob {
	rtj := testRTJ()
	rtj.Spec.Runtime.Template.Spec = runtime.RawExtension{
		Raw: []byte(`{
			"replicatedJobs":[
				{
					"name":"leader",
					"replicas":1,
					"template":{
						"spec":{
							"parallelism":1,
							"completions":1,
							"template":{
								"spec":{
									"restartPolicy":"Never",
									"containers":[{"name":"leader","image":"counter:latest"}]
								}
							}
						}
					}
				},
				{
					"name":"worker",
					"replicas":2,
					"template":{
						"spec":{
							"parallelism":1,
							"completions":1,
							"template":{
								"spec":{
									"restartPolicy":"Never",
									"containers":[{"name":"worker","image":"counter:latest"}]
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

func testRTJWithKueuePodLabels() *trainingv1alpha1.ResumableTrainingJob {
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
								"metadata":{
									"labels":{
										"app":"counter",
										"kueue.x-k8s.io/managed":"true"
									}
								},
								"spec":{
									"restartPolicy":"Never",
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

func testRTJWithNodeSelector() *trainingv1alpha1.ResumableTrainingJob {
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
									"nodeSelector":{
										"cloud.google.com/gke-accelerator":"nvidia-tesla-a100"
									},
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

func testRTJ() *trainingv1alpha1.ResumableTrainingJob {
	rtj := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "counter",
			Namespace: "default",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			QueueName:                 "training",
			WorkloadPriorityClassName: "phase1-dev",
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "registry.example.com/training/counter:sha256-1234",
				CodeVersion: "git:1234",
				WorldSize:   2,
				GPUShape:    "cpu",
			},
			Runtime: trainingv1alpha1.ResumableTrainingJobRuntime{
				Mode:          trainingv1alpha1.RuntimeModeDDP,
				OptimizerMode: "adamw",
				ShardingMode:  "none",
				Template: trainingv1alpha1.JobSetTemplate{
					APIVersion: trainingv1alpha1.DefaultJobSetAPIVersion,
					Kind:       trainingv1alpha1.DefaultJobSetKind,
					Metadata: &trainingv1alpha1.EmbeddedObjectMetadata{
						Labels: map[string]string{"app.kubernetes.io/name": "counter"},
					},
					Spec: runtime.RawExtension{
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
													"containers":[{"name":"trainer","image":"counter:latest"}]
												}
											}
										}
									}
								}
							]
						}`),
					},
				},
			},
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI:      "s3://phase1-checkpoints/counter/",
				Interval:        metav1.Duration{Duration: 5 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 10 * time.Minute},
				MaxDrainTime:    metav1.Duration{Duration: 15 * time.Minute},
				SafePointMode:   trainingv1alpha1.SafePointModeStepBoundary,
			},
			Resume: trainingv1alpha1.ResumePolicy{
				SourcePolicy:     trainingv1alpha1.ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3,
			},
			Control: &trainingv1alpha1.ControlSpec{DesiredState: trainingv1alpha1.DesiredStateRunning},
		},
	}
	rtj.Default()
	return rtj
}
