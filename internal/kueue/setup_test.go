package kueue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	"sigs.k8s.io/kueue/pkg/controller/constants"
	coreindexer "sigs.k8s.io/kueue/pkg/controller/core/indexer"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	"sigs.k8s.io/kueue/pkg/podset"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

func TestRegisterExternalFramework(t *testing.T) {
	t.Cleanup(jobframework.EnableExternalIntegrationsForTest(t))

	if err := RegisterExternalFramework(); err != nil {
		t.Fatalf("register external framework: %v", err)
	}

	child := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "child",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: trainingv1alpha1.GroupVersion.String(),
				Kind:       GroupVersionKind.Kind,
				Name:       "demo",
				Controller: ptr.To(true),
			}},
		},
	}
	if !jobframework.IsOwnerManagedByKueueForObject(child) {
		t.Fatalf("expected owner to be recognized as Kueue-manageable after external registration")
	}
}

func TestSetupIndexesRegistersRTJOwnerIndex(t *testing.T) {
	indexer := &capturingFieldIndexer{}

	if err := SetupIndexes(context.Background(), indexer); err != nil {
		t.Fatalf("setup indexes: %v", err)
	}

	wantField := jobframework.OwnerReferenceIndexKey(GroupVersionKind)
	if indexer.field != wantField {
		t.Fatalf("expected field %q, got %q", wantField, indexer.field)
	}

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: trainingv1alpha1.GroupVersion.String(),
				Kind:       GroupVersionKind.Kind,
				Name:       "demo",
				Controller: ptr.To(true),
			}},
		},
	}
	values := indexer.extract(workload)
	if len(values) != 1 || values[0] != "demo" {
		t.Fatalf("expected indexed owner name %q, got %#v", "demo", values)
	}
}

func TestGenericReconcilerCreatesWorkloadFromRTJTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, corev1.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	job := makeTestRTJ(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: job.Namespace}}
	priorityClass := &kueuev1beta2.WorkloadPriorityClass{
		ObjectMeta: metav1.ObjectMeta{Name: job.Spec.WorkloadPriorityClassName},
		Value:      1000,
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&trainingv1alpha1.ResumableTrainingJob{}, &kueuev1beta2.Workload{}).
		WithObjects(namespace, priorityClass, job).
		WithIndex(&kueuev1beta2.Workload{}, coreindexer.OwnerReferenceIndexKey(GroupVersionKind), coreindexer.WorkloadOwnerIndexFunc(GroupVersionKind)).
		Build()

	reconciler := jobframework.NewReconciler(cl, record.NewFakeRecorder(16))
	_, err := reconciler.ReconcileGenericJob(
		context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: job.Name, Namespace: job.Namespace}},
		NewGenericJob(),
	)
	if err != nil {
		t.Fatalf("reconcile generic job: %v", err)
	}

	workloads := &kueuev1beta2.WorkloadList{}
	if err := cl.List(context.Background(), workloads); err != nil {
		t.Fatalf("list workloads: %v", err)
	}
	if len(workloads.Items) != 1 {
		t.Fatalf("expected 1 workload, got %d", len(workloads.Items))
	}

	workload := workloads.Items[0]
	if workload.Name != WorkloadNameForObject(job) {
		t.Fatalf("expected workload name %q, got %q", WorkloadNameForObject(job), workload.Name)
	}
	if workload.Spec.QueueName != kueuev1beta2.LocalQueueName(job.Spec.QueueName) {
		t.Fatalf("expected queue %q, got %q", job.Spec.QueueName, workload.Spec.QueueName)
	}
	if len(workload.OwnerReferences) != 1 || workload.OwnerReferences[0].Name != job.Name {
		t.Fatalf("expected workload owner %q, got %#v", job.Name, workload.OwnerReferences)
	}
	if got := workload.Spec.PodSets; len(got) != 2 {
		t.Fatalf("expected 2 pod sets, got %d", len(got))
	}
	if workload.Spec.PodSets[0].Name != "driver" || workload.Spec.PodSets[0].Count != 1 {
		t.Fatalf("unexpected first pod set: %#v", workload.Spec.PodSets[0])
	}
	if workload.Spec.PodSets[1].Name != "worker" || workload.Spec.PodSets[1].Count != 2 {
		t.Fatalf("unexpected second pod set: %#v", workload.Spec.PodSets[1])
	}

	updated := &trainingv1alpha1.ResumableTrainingJob{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(job), updated); err != nil {
		t.Fatalf("get updated RTJ: %v", err)
	}
	if !ptr.Deref(updated.Spec.Suspend, false) {
		t.Fatalf("expected RTJ to remain suspended until admitted")
	}
	if updated.Labels[constants.QueueLabel] != job.Spec.QueueName {
		t.Fatalf("expected queue label %q, got %#v", job.Spec.QueueName, updated.Labels)
	}
}

func TestRunWithPodSetsInfoMutatesTemplateAndRestoreReverts(t *testing.T) {
	job := makeTestRTJ(t)
	genericJob := NewGenericJob().(*RTJGenericJob)
	genericJob.job = job.DeepCopy()

	originalSpec, err := genericJob.decodeTemplateSpec()
	if err != nil {
		t.Fatalf("decode original spec: %v", err)
	}

	podSets, err := genericJob.PodSets(context.Background())
	if err != nil {
		t.Fatalf("build pod sets: %v", err)
	}
	originalInfo := make([]podset.PodSetInfo, len(podSets))
	for i := range podSets {
		originalInfo[i] = podset.FromPodSet(&podSets[i])
	}

	info := []podset.PodSetInfo{
		{Name: "driver", NodeSelector: map[string]string{"node.kubernetes.io/pool": "admitted-a"}},
		{Name: "worker", NodeSelector: map[string]string{"node.kubernetes.io/pool": "admitted-b"}},
	}
	if err := genericJob.RunWithPodSetsInfo(context.Background(), info); err != nil {
		t.Fatalf("run with pod set info: %v", err)
	}
	if ptr.Deref(genericJob.job.Spec.Suspend, true) {
		t.Fatalf("expected RTJ to be unsuspended after admission info is applied")
	}

	mutatedSpec, err := genericJob.decodeTemplateSpec()
	if err != nil {
		t.Fatalf("decode mutated spec: %v", err)
	}
	if mutatedSpec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector["node.kubernetes.io/pool"] != "admitted-a" {
		t.Fatalf("expected driver node selector to be applied, got %#v", mutatedSpec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector)
	}

	if !genericJob.RestorePodSetsInfo(originalInfo) {
		t.Fatalf("expected restore to report a change")
	}
	restoredSpec, err := genericJob.decodeTemplateSpec()
	if err != nil {
		t.Fatalf("decode restored spec: %v", err)
	}
	if restoredSpec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector != nil {
		t.Fatalf("expected driver node selector to be cleared, got %#v", restoredSpec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector)
	}
	if restoredSpec.ReplicatedJobs[1].Template.Spec.Template.Spec.NodeSelector != nil {
		t.Fatalf("expected worker node selector to be cleared, got %#v", restoredSpec.ReplicatedJobs[1].Template.Spec.Template.Spec.NodeSelector)
	}
	if originalSpec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector != nil {
		t.Fatalf("expected original driver node selector to be empty, got %#v", originalSpec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector)
	}
}

type capturingFieldIndexer struct {
	field   string
	extract client.IndexerFunc
}

func (i *capturingFieldIndexer) IndexField(_ context.Context, _ client.Object, field string, extract client.IndexerFunc) error {
	i.field = field
	i.extract = extract
	return nil
}

func makeTestRTJ(t *testing.T) *trainingv1alpha1.ResumableTrainingJob {
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
						Parallelism: ptr.To[int32](1),
						Completions: ptr.To[int32](1),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								Containers: []corev1.Container{{
									Name:  "trainer",
									Image: "busybox:1.36.1",
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

	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rtj-sample",
			Namespace: "default",
			UID:       types.UID("rtj-sample-uid"),
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			Suspend:                   ptr.To(true),
			QueueName:                 "training",
			WorkloadPriorityClassName: "phase1-dev",
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "registry.example.io/trainer:latest",
				CodeVersion: "gitsha-123",
				WorldSize:   2,
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
	job.Default()
	return job
}

// --- Phase 5: Priority-aware tests ---

func TestPriorityClassReturnsWorkloadPriorityClassName(t *testing.T) {
	job := makeTestRTJ(t)
	genericJob := NewGenericJob().(*RTJGenericJob)
	genericJob.job = job

	got := genericJob.PriorityClass()
	want := job.Spec.WorkloadPriorityClassName
	if got != want {
		t.Errorf("PriorityClass() = %q, want %q", got, want)
	}
}

func TestWorkloadPrioritySetFromPriorityClass(t *testing.T) {
	// Verify that when Kueue's generic reconciler creates the Workload,
	// it resolves the WorkloadPriorityClass and sets Spec.Priority.
	scheme := runtime.NewScheme()
	must(t, corev1.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	job := makeTestRTJ(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: job.Namespace}}
	priorityClass := &kueuev1beta2.WorkloadPriorityClass{
		ObjectMeta: metav1.ObjectMeta{Name: job.Spec.WorkloadPriorityClassName},
		Value:      500,
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&trainingv1alpha1.ResumableTrainingJob{}, &kueuev1beta2.Workload{}).
		WithObjects(namespace, priorityClass, job).
		WithIndex(&kueuev1beta2.Workload{}, coreindexer.OwnerReferenceIndexKey(GroupVersionKind), coreindexer.WorkloadOwnerIndexFunc(GroupVersionKind)).
		Build()

	reconciler := jobframework.NewReconciler(cl, record.NewFakeRecorder(16))
	_, err := reconciler.ReconcileGenericJob(
		context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: job.Name, Namespace: job.Namespace}},
		NewGenericJob(),
	)
	if err != nil {
		t.Fatalf("reconcile generic job: %v", err)
	}

	workloads := &kueuev1beta2.WorkloadList{}
	if err := cl.List(context.Background(), workloads); err != nil {
		t.Fatalf("list workloads: %v", err)
	}
	if len(workloads.Items) != 1 {
		t.Fatalf("expected 1 workload, got %d", len(workloads.Items))
	}

	workload := workloads.Items[0]

	// Verify the Workload has the correct priority from the class.
	if workload.Spec.Priority == nil {
		t.Fatal("expected Workload.Spec.Priority to be set from WorkloadPriorityClass")
	}
	if *workload.Spec.Priority != 500 {
		t.Errorf("expected Workload.Spec.Priority=500, got %d", *workload.Spec.Priority)
	}

	// Verify a second reconcile does NOT overwrite the priority.
	// This is the critical anti-clobbering test for Phase 5.
	// Simulate the RTJ controller patching priority to 450 (effective priority).
	patch := client.MergeFrom(workload.DeepCopy())
	workload.Spec.Priority = ptr.To[int32](450)
	if err := cl.Patch(context.Background(), &workload, patch); err != nil {
		t.Fatalf("patch workload priority: %v", err)
	}

	// Re-reconcile the generic job.
	_, err = reconciler.ReconcileGenericJob(
		context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: job.Name, Namespace: job.Namespace}},
		NewGenericJob(),
	)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	// Re-read the workload after second reconcile.
	var updated kueuev1beta2.Workload
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(&workload), &updated); err != nil {
		t.Fatalf("get updated workload: %v", err)
	}

	// The priority should still be 450 (the operator-patched value), not
	// reverted to 500. Kueue's GenericJob reconciler only sets priority at
	// Workload creation time, not on subsequent syncs.
	if updated.Spec.Priority == nil {
		t.Fatal("expected Workload.Spec.Priority to remain set")
	}
	if *updated.Spec.Priority != 450 {
		t.Errorf("expected priority to remain 450 (operator-patched), got %d; Kueue clobbered the effective priority", *updated.Spec.Priority)
	}
}

func TestWorkloadPriorityNotSetWithoutPriorityPolicy(t *testing.T) {
	// When no priority policy is referenced, the RTJ should behave
	// exactly as Phase 4: Workload priority is set from the
	// WorkloadPriorityClass and never modified by the RTJ controller.
	job := makeTestRTJ(t)

	// Verify no priority policy ref is set.
	if job.IsPriorityShapingEnabled() {
		t.Fatal("expected priority shaping to be disabled on default test RTJ")
	}

	genericJob := NewGenericJob().(*RTJGenericJob)
	genericJob.job = job

	// PriorityClass() should still return the class name for Workload creation.
	if got := genericJob.PriorityClass(); got != "phase1-dev" {
		t.Errorf("expected PriorityClass()=phase1-dev, got %q", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
