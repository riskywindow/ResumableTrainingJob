package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1api "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

func TestReconcileCreatesLaunchResourcesWhenNoActiveRunExists(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.CurrentRunAttempt != 1 {
		t.Fatalf("expected current run attempt 1, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q after creation, got %q", trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}

	controlName := rtjjobset.ControlConfigMapName(rtj.Name, 1)
	jobSetName := rtjjobset.ChildJobSetName(rtj.Name, 1)
	if updated.Status.ActiveControlConfigMapName != controlName {
		t.Fatalf("expected active control ConfigMap %q, got %q", controlName, updated.Status.ActiveControlConfigMapName)
	}
	if updated.Status.ActiveJobSetName != jobSetName {
		t.Fatalf("expected active JobSet %q, got %q", jobSetName, updated.Status.ActiveJobSetName)
	}

	var controlConfigMap corev1.ConfigMap
	must(t, client.Get(ctx, types.NamespacedName{Name: controlName, Namespace: rtj.Namespace}, &controlConfigMap))
	assertControllerOwnerReference(t, controlConfigMap.OwnerReferences, rtj)

	childJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	must(t, client.Get(ctx, types.NamespacedName{Name: jobSetName, Namespace: rtj.Namespace}, childJobSet))
	assertOwnerReference(t, childJobSet.GetOwnerReferences(), rtj)
	if ref := childJobSet.GetOwnerReferences()[0]; ref.Controller != nil && *ref.Controller {
		t.Fatalf("expected child JobSet owner reference to be non-controller to avoid Kueue ancestor management, got %#v", ref)
	}
}

func TestReconcileDoesNotCreateChildJobSetBeforeAdmission(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(true)
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseQueued {
		t.Fatalf("expected phase %q while suspended, got %q", trainingv1alpha1.PhaseQueued, updated.Status.Phase)
	}
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet while suspended, got %q", updated.Status.ActiveJobSetName)
	}
	if updated.Status.CurrentRunAttempt != 0 {
		t.Fatalf("expected run attempt to remain 0 before admission, got %d", updated.Status.CurrentRunAttempt)
	}

	childJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	err := client.Get(ctx, types.NamespacedName{Name: rtjjobset.ChildJobSetName(rtj.Name, 1), Namespace: rtj.Namespace}, childJobSet)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no child JobSet before admission, got err=%v", err)
	}

	var controlConfigMap corev1.ConfigMap
	err = client.Get(ctx, types.NamespacedName{Name: rtjjobset.ControlConfigMapName(rtj.Name, 1), Namespace: rtj.Namespace}, &controlConfigMap)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no control ConfigMap before admission, got err=%v", err)
	}
}

func TestReconcileUnsuspendedQueuedRTJLaunchesRunAttempt(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("launch reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}
	if updated.Status.CurrentRunAttempt != 1 {
		t.Fatalf("expected run attempt 1, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.ActiveJobSetName != rtjjobset.ChildJobSetName(rtj.Name, 1) {
		t.Fatalf("expected active child JobSet %q, got %q", rtjjobset.ChildJobSetName(rtj.Name, 1), updated.Status.ActiveJobSetName)
	}
}

func TestReconcileMarksRunningWhenActiveChildExists(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.CurrentRunAttempt = 1
	rtj.Status.ActiveControlConfigMapName = rtjjobset.ControlConfigMapName(rtj.Name, 1)
	rtj.Status.ActiveJobSetName = rtjjobset.ChildJobSetName(rtj.Name, 1)
	rtj.Status.Phase = trainingv1alpha1.PhaseStarting
	rtj.Status.ObservedGeneration = rtj.Generation

	childJobSet := &unstructured.Unstructured{}
	childJobSet.SetAPIVersion(trainingv1alpha1.DefaultJobSetAPIVersion)
	childJobSet.SetKind(trainingv1alpha1.DefaultJobSetKind)
	childJobSet.SetName(rtj.Status.ActiveJobSetName)
	childJobSet.SetNamespace(rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseRunning, updated.Status.Phase)
	}
}

func TestReconcilePausePublishesYieldRequestAndControlPayload(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Spec.Control.DesiredState = trainingv1alpha1.DesiredStatePaused

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	for i := 0; i < 2; i++ {
		result, err := reconciler.Reconcile(context.Background(), req)
		if err != nil {
			t.Fatalf("pause reconcile %d failed: %v", i+1, err)
		}
		if result.RequeueAfter != pausePollInterval {
			t.Fatalf("expected requeueAfter %s on reconcile %d, got %s", pausePollInterval, i+1, result.RequeueAfter)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseDraining {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseDraining, updated.Status.Phase)
	}
	if updated.Status.PauseRequestID != "pause-run-1-gen-1" {
		t.Fatalf("expected pause request id pause-run-1-gen-1, got %q", updated.Status.PauseRequestID)
	}
	if updated.Status.TransitionTimestamps.YieldRequestedAt == nil {
		t.Fatalf("expected yield requested timestamp to be recorded")
	}
	if !updated.Status.TransitionTimestamps.YieldRequestedAt.Time.Equal(now.Time) {
		t.Fatalf("expected yield requested time %s, got %s", now.Time.Format(time.RFC3339), updated.Status.TransitionTimestamps.YieldRequestedAt.Time.Format(time.RFC3339))
	}

	var updatedConfigMap corev1.ConfigMap
	must(t, client.Get(context.Background(), types.NamespacedName{Name: rtj.Status.ActiveControlConfigMapName, Namespace: rtj.Namespace}, &updatedConfigMap))
	gotPayload := updatedConfigMap.Data[rtjjobset.ControlConfigKey]
	wantPayload := controlPayload(trainingv1alpha1.DesiredStatePaused, updated.Status.PauseRequestID, now.Time)
	if gotPayload != wantPayload {
		t.Fatalf("expected pause control payload %q, got %q", wantPayload, gotPayload)
	}
}

func TestReconcilePauseCompletesAfterCheckpointObservation(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(30 * time.Second))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Spec.Control.DesiredState = trainingv1alpha1.DesiredStatePaused

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)
	fakeCatalog := &fakeCheckpointCatalog{
		observation: &checkpoints.PauseObservation{
			MarkerURI: checkpoints.YieldMarkerURI(rtj.Spec.Checkpoint.StorageURI, rtj.Status.CurrentRunAttempt),
			Checkpoint: trainingv1alpha1.CheckpointReference{
				ID:                  "ckpt-run1-step20",
				StorageURI:          "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
				ManifestURI:         "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
				CompletionTime:      &completedAt,
				SourceRunAttempt:    1,
				CompatibilityState:  trainingv1alpha1.CompatibilityStateCompatible,
				CompatibilityReason: "test fixture",
			},
			RequestID:   "pause-run-1-gen-1",
			GlobalStep:  20,
			CompletedAt: completedAt,
		},
		ready: true,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:  client,
		Scheme:  scheme,
		Now:     func() metav1.Time { return now },
		Catalog: fakeCatalog,
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	for i := 0; i < 4; i++ {
		if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("pause-complete reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhasePaused {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhasePaused, updated.Status.Phase)
	}
	if updated.Status.LastCompletedCheckpoint == nil {
		t.Fatalf("expected lastCompletedCheckpoint to be recorded")
	}
	if updated.Status.LastCompletedCheckpoint.ManifestURI != fakeCatalog.observation.Checkpoint.ManifestURI {
		t.Fatalf("expected manifest uri %q, got %q", fakeCatalog.observation.Checkpoint.ManifestURI, updated.Status.LastCompletedCheckpoint.ManifestURI)
	}
	if updated.Status.SelectedCheckpoint == nil || updated.Status.SelectedCheckpoint.ManifestURI != fakeCatalog.observation.Checkpoint.ManifestURI {
		t.Fatalf("expected selected checkpoint manifest uri %q, got %#v", fakeCatalog.observation.Checkpoint.ManifestURI, updated.Status.SelectedCheckpoint)
	}
	if updated.Status.TransitionTimestamps.LastCheckpointCompletedAt == nil {
		t.Fatalf("expected last checkpoint completed timestamp to be recorded")
	}

	deletedChild := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)
	err := client.Get(context.Background(), types.NamespacedName{Name: rtj.Status.ActiveJobSetName, Namespace: rtj.Namespace}, deletedChild)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected child JobSet to be deleted, got err=%v", err)
	}
}

func TestReconcileKueueSuspendYieldsCheckpointAndQueues(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(30 * time.Second))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Spec.Suspend = ptr.To(true)

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)
	fakeCatalog := &fakeCheckpointCatalog{
		observation: &checkpoints.PauseObservation{
			MarkerURI: checkpoints.YieldMarkerURI(rtj.Spec.Checkpoint.StorageURI, rtj.Status.CurrentRunAttempt),
			Checkpoint: trainingv1alpha1.CheckpointReference{
				ID:                  "ckpt-run1-step20",
				StorageURI:          "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
				ManifestURI:         "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
				CompletionTime:      &completedAt,
				SourceRunAttempt:    1,
				CompatibilityState:  trainingv1alpha1.CompatibilityStateCompatible,
				CompatibilityReason: "test fixture",
			},
			RequestID:   "kueue-suspend-run-1-gen-1",
			GlobalStep:  20,
			CompletedAt: completedAt,
		},
		ready: true,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:  client,
		Scheme:  scheme,
		Now:     func() metav1.Time { return now },
		Catalog: fakeCatalog,
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	for i := 0; i < 4; i++ {
		result, err := reconciler.Reconcile(context.Background(), req)
		if err != nil {
			t.Fatalf("kueue suspend reconcile %d failed: %v", i+1, err)
		}
		if i < 3 && result.RequeueAfter != pausePollInterval {
			t.Fatalf("expected requeueAfter %s on reconcile %d, got %s", pausePollInterval, i+1, result.RequeueAfter)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseQueued {
		t.Fatalf("expected phase %q after Kueue suspension, got %q", trainingv1alpha1.PhaseQueued, updated.Status.Phase)
	}
	if updated.Status.LastCompletedCheckpoint == nil || updated.Status.LastCompletedCheckpoint.ManifestURI != fakeCatalog.observation.Checkpoint.ManifestURI {
		t.Fatalf("expected last completed checkpoint to be recorded, got %#v", updated.Status.LastCompletedCheckpoint)
	}
	if updated.Status.CurrentSuspension == nil || updated.Status.CurrentSuspension.Source != trainingv1alpha1.SuspensionSourceKueue {
		t.Fatalf("expected current suspension source Kueue, got %#v", updated.Status.CurrentSuspension)
	}
	condition := metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeKueueSuspended)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected KueueSuspended condition to be true, got %#v", condition)
	}

	var updatedConfigMap corev1.ConfigMap
	must(t, client.Get(context.Background(), types.NamespacedName{Name: rtj.Status.ActiveControlConfigMapName, Namespace: rtj.Namespace}, &updatedConfigMap))
	wantPayload := controlPayload(trainingv1alpha1.DesiredStatePaused, "kueue-suspend-run-1-gen-1", now.Time)
	if updatedConfigMap.Data[rtjjobset.ControlConfigKey] != wantPayload {
		t.Fatalf("expected Kueue suspend control payload %q, got %q", wantPayload, updatedConfigMap.Data[rtjjobset.ControlConfigKey])
	}

	deletedChild := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)
	err := client.Get(context.Background(), types.NamespacedName{Name: rtj.Status.ActiveJobSetName, Namespace: rtj.Namespace}, deletedChild)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected child JobSet to be deleted after Kueue suspend, got err=%v", err)
	}
}

func TestReconcilePauseTimeoutDeletesChildAndMarksDegraded(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	requestedAt := metav1.NewTime(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC))
	now := metav1.NewTime(requestedAt.Add(16 * time.Minute))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Spec.Control.DesiredState = trainingv1alpha1.DesiredStatePaused
	rtj.Status.PauseRequestID = "pause-run-1-gen-1"
	rtj.Status.Phase = trainingv1alpha1.PhaseYieldRequested
	rtj.Status.TransitionTimestamps.YieldRequestedAt = &requestedAt

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("timeout reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseFailed {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseFailed, updated.Status.Phase)
	}
	condition := metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeDegraded)
	if condition == nil {
		t.Fatalf("expected degraded condition")
	}
	if condition.Reason != reasonDrainTimedOut {
		t.Fatalf("expected degraded reason %q, got %q", reasonDrainTimedOut, condition.Reason)
	}

	deletedChild := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)
	err := client.Get(context.Background(), types.NamespacedName{Name: rtj.Status.ActiveJobSetName, Namespace: rtj.Namespace}, deletedChild)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected child JobSet to be deleted after timeout, got err=%v", err)
	}

	var updatedConfigMap corev1.ConfigMap
	must(t, client.Get(context.Background(), types.NamespacedName{Name: rtj.Status.ActiveControlConfigMapName, Namespace: rtj.Namespace}, &updatedConfigMap))
	wantPayload := controlPayload(trainingv1alpha1.DesiredStatePaused, rtj.Status.PauseRequestID, requestedAt.Time)
	if updatedConfigMap.Data[rtjjobset.ControlConfigKey] != wantPayload {
		t.Fatalf("expected timeout path to preserve pause payload %q, got %q", wantPayload, updatedConfigMap.Data[rtjjobset.ControlConfigKey])
	}
}

func TestReconcileResumeCreatesNewAttemptFromLatestCompatibleCheckpoint(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 21, 12, 5, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(-1 * time.Minute))
	rtj := controllerTestRTJ()
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.CurrentRunAttempt = 1
	rtj.Status.Phase = trainingv1alpha1.PhasePaused
	rtj.Status.ObservedGeneration = rtj.Generation
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:                  "ckpt-run1-step20",
		StorageURI:          "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
		ManifestURI:         "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
		CompletionTime:      &completedAt,
		SourceRunAttempt:    1,
		CompatibilityState:  trainingv1alpha1.CompatibilityStateCompatible,
		CompatibilityReason: "test fixture",
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
		Catalog: &fakeCheckpointCatalog{
			selectedCheckpoint: &trainingv1alpha1.CheckpointReference{
				ID:                  "ckpt-run1-step20",
				StorageURI:          "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
				ManifestURI:         "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
				CompletionTime:      &completedAt,
				SourceRunAttempt:    1,
				CompatibilityState:  trainingv1alpha1.CompatibilityStateCompatible,
				CompatibilityReason: "latest compatible complete checkpoint selected for resume",
			},
			selected: true,
		},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("resume reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseRestoring {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseRestoring, updated.Status.Phase)
	}
	if updated.Status.CurrentRunAttempt != 2 {
		t.Fatalf("expected current run attempt 2, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.SelectedCheckpoint == nil || updated.Status.SelectedCheckpoint.ManifestURI == "" {
		t.Fatalf("expected selected checkpoint to be recorded")
	}
	if updated.Status.PauseRequestID != "" {
		t.Fatalf("expected pauseRequestID to be cleared on resume, got %q", updated.Status.PauseRequestID)
	}

	childJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	must(t, client.Get(context.Background(), types.NamespacedName{Name: updated.Status.ActiveJobSetName, Namespace: rtj.Namespace}, childJobSet))
	decoded, err := rtjjobset.FromUnstructured(childJobSet)
	if err != nil {
		t.Fatalf("decode resumed child JobSet: %v", err)
	}
	container := decoded.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.Containers[0]
	assertEnvValue(t, container.Env, rtjjobset.EnvRestoreManifestURI, updated.Status.SelectedCheckpoint.ManifestURI)

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("follow-up resume reconcile failed: %v", err)
	}
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected phase %q after the active resumed child exists, got %q", trainingv1alpha1.PhaseRunning, updated.Status.Phase)
	}
}

func TestReconcileUnsuspendAfterKueueSuspensionResumesFromLatestCheckpoint(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 21, 12, 5, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(-1 * time.Minute))
	rtj := controllerTestRTJ()
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Status.CurrentRunAttempt = 1
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:                  "ckpt-run1-step20",
		StorageURI:          "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
		ManifestURI:         "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
		CompletionTime:      &completedAt,
		SourceRunAttempt:    1,
		CompatibilityState:  trainingv1alpha1.CompatibilityStateCompatible,
		CompatibilityReason: "test fixture",
	}
	rtj.Status.CurrentSuspension = &trainingv1alpha1.SuspensionStatus{
		Suspended: true,
		Source:    trainingv1alpha1.SuspensionSourceKueue,
	}
	rtj.Status.Conditions = append(rtj.Status.Conditions, metav1.Condition{
		Type:               conditionTypeKueueSuspended,
		Status:             metav1.ConditionTrue,
		Reason:             reasonKueueSuspended,
		Message:            "test fixture",
		LastTransitionTime: now,
		ObservedGeneration: rtj.Generation,
	})

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
		Catalog: &fakeCheckpointCatalog{
			selectedCheckpoint: &trainingv1alpha1.CheckpointReference{
				ID:                  "ckpt-run1-step20",
				StorageURI:          "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
				ManifestURI:         "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
				CompletionTime:      &completedAt,
				SourceRunAttempt:    1,
				CompatibilityState:  trainingv1alpha1.CompatibilityStateCompatible,
				CompatibilityReason: "latest compatible complete checkpoint selected for resume",
			},
			selected: true,
		},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("resume reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseRestoring {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseRestoring, updated.Status.Phase)
	}
	if updated.Status.CurrentRunAttempt != 2 {
		t.Fatalf("expected current run attempt 2, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.CurrentSuspension != nil {
		t.Fatalf("expected current suspension to be cleared after unsuspend, got %#v", updated.Status.CurrentSuspension)
	}
	if condition := metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeKueueSuspended); condition != nil {
		t.Fatalf("expected KueueSuspended condition to be cleared after unsuspend, got %#v", condition)
	}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("follow-up resume reconcile failed: %v", err)
	}
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected phase %q after resumed child exists, got %q", trainingv1alpha1.PhaseRunning, updated.Status.Phase)
	}
}

func TestReconcileRepeatedUnsuspendedLaunchDoesNotCreateDuplicateChildJobSets(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))
	if updated.Status.CurrentRunAttempt != 1 {
		t.Fatalf("expected current run attempt 1 after repeated reconcile, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.ActiveJobSetName != rtjjobset.ChildJobSetName(rtj.Name, 1) {
		t.Fatalf("expected active JobSet %q, got %q", rtjjobset.ChildJobSetName(rtj.Name, 1), updated.Status.ActiveJobSetName)
	}

	runTwoJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	err := client.Get(ctx, types.NamespacedName{Name: rtjjobset.ChildJobSetName(rtj.Name, 2), Namespace: rtj.Namespace}, runTwoJobSet)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no second child JobSet, got err=%v", err)
	}

	var runTwoConfigMap corev1.ConfigMap
	err = client.Get(ctx, types.NamespacedName{Name: rtjjobset.ControlConfigMapName(rtj.Name, 2), Namespace: rtj.Namespace}, &runTwoConfigMap)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no second control ConfigMap, got err=%v", err)
	}
}

func assertEnvValue(t *testing.T, envVars []corev1.EnvVar, name, want string) {
	t.Helper()
	for _, envVar := range envVars {
		if envVar.Name != name {
			continue
		}
		if envVar.Value != want {
			t.Fatalf("expected env %s=%q, got %q", name, want, envVar.Value)
		}
		return
	}
	t.Fatalf("missing env var %s", name)
}

func assertOwnerReference(t *testing.T, refs []metav1.OwnerReference, owner *trainingv1alpha1.ResumableTrainingJob) {
	t.Helper()
	if len(refs) != 1 {
		t.Fatalf("expected exactly one owner reference, got %d", len(refs))
	}
	ref := refs[0]
	if ref.Name != owner.Name {
		t.Fatalf("expected owner reference name %q, got %q", owner.Name, ref.Name)
	}
	if ref.Kind != "ResumableTrainingJob" {
		t.Fatalf("expected owner reference kind ResumableTrainingJob, got %q", ref.Kind)
	}
}

func assertControllerOwnerReference(t *testing.T, refs []metav1.OwnerReference, owner *trainingv1alpha1.ResumableTrainingJob) {
	t.Helper()
	assertOwnerReference(t, refs, owner)
	ref := refs[0]
	if ref.Controller == nil || !*ref.Controller {
		t.Fatalf("expected controller owner reference")
	}
}

func controllerTestRTJ() *trainingv1alpha1.ResumableTrainingJob {
	rtj := &trainingv1alpha1.ResumableTrainingJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: trainingv1alpha1.GroupVersion.String(),
			Kind:       "ResumableTrainingJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "counter",
			Namespace:  "default",
			UID:        "rtj-uid-1",
			Generation: 1,
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

func primeActiveRun(rtj *trainingv1alpha1.ResumableTrainingJob) {
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.CurrentRunAttempt = 1
	rtj.Status.ActiveControlConfigMapName = rtjjobset.ControlConfigMapName(rtj.Name, 1)
	rtj.Status.ActiveJobSetName = rtjjobset.ChildJobSetName(rtj.Name, 1)
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Status.ObservedGeneration = rtj.Generation
}

func newTestChildJobSet(name, namespace string) *unstructured.Unstructured {
	childJobSet := &unstructured.Unstructured{}
	childJobSet.SetAPIVersion(trainingv1alpha1.DefaultJobSetAPIVersion)
	childJobSet.SetKind(trainingv1alpha1.DefaultJobSetKind)
	childJobSet.SetName(name)
	childJobSet.SetNamespace(namespace)
	return childJobSet
}

// --- Phase 4 Tests ---

func TestReconcileDoesNotCreateChildJobSetBeforeReadinessGatePasses(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Create a workload with a Pending admission check.
	workload := testWorkloadForRTJ(rtj, kueuev1beta2.CheckStatePending)
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	// Should NOT have created a child JobSet.
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet while readiness gate is pending, got %q", updated.Status.ActiveJobSetName)
	}

	// Should have set launch readiness to pending.
	if updated.Status.LaunchReadiness == nil {
		t.Fatal("expected LaunchReadiness to be set")
	}
	if updated.Status.LaunchReadiness.Ready {
		t.Fatal("expected LaunchReadiness.Ready to be false")
	}
	if updated.Status.LaunchReadiness.GateState != trainingv1alpha1.ReadinessGatePending {
		t.Fatalf("expected gate state Pending, got %q", updated.Status.LaunchReadiness.GateState)
	}
}

func TestReconcileDoesNotCreateChildJobSetBeforeTopologyAssignment(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Create a workload that is admitted but has NO topology assignment.
	workload := testWorkloadForRTJ(rtj, kueuev1beta2.CheckStateReady)
	workload.Status.Admission = &kueuev1beta2.Admission{
		ClusterQueue: "test-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "trainer",
				Count: ptr.To[int32](2),
				// No TopologyAssignment — this is the key point.
			},
		},
	}
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	// Should NOT have created a child JobSet.
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet while topology assignment is missing, got %q", updated.Status.ActiveJobSetName)
	}
	if updated.Status.LaunchReadiness == nil {
		t.Fatal("expected LaunchReadiness to be set")
	}
	if updated.Status.LaunchReadiness.Reason != reasonWaitingForTopology {
		t.Fatalf("expected reason %q, got %q", reasonWaitingForTopology, updated.Status.LaunchReadiness.Reason)
	}
}

func TestReconcileTopologyAdmittedLaunchCreatesChildWithNodeSelector(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Create a workload with topology assignment.
	workload := testWorkloadForRTJ(rtj, kueuev1beta2.CheckStateReady)
	workload.Status.Admission = &kueuev1beta2.Admission{
		ClusterQueue: "test-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "trainer",
				Count: ptr.To[int32](2),
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{"topology.kubernetes.io/zone"},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{
						{
							DomainCount: 1,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Universal: ptr.To("us-east-1a")},
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
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// First reconcile: evaluates gates and launches.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("launch reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}
	if updated.Status.ActiveJobSetName == "" {
		t.Fatal("expected active child JobSet to be created")
	}

	// Verify topology status was populated.
	if updated.Status.Topology == nil {
		t.Fatal("expected Topology status to be populated")
	}
	if len(updated.Status.Topology.Levels) != 1 {
		t.Fatalf("expected 1 topology level, got %d", len(updated.Status.Topology.Levels))
	}
	if updated.Status.Topology.Levels[0] != "topology.kubernetes.io/zone" {
		t.Fatalf("expected zone level, got %s", updated.Status.Topology.Levels[0])
	}

	// Verify the child JobSet has topology nodeSelector.
	childJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	must(t, client.Get(ctx, types.NamespacedName{Name: updated.Status.ActiveJobSetName, Namespace: rtj.Namespace}, childJobSet))
	decoded, err := rtjjobset.FromUnstructured(childJobSet)
	if err != nil {
		t.Fatalf("decode child JobSet: %v", err)
	}
	pod := decoded.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec
	if pod.NodeSelector == nil {
		t.Fatal("expected nodeSelector on child JobSet pod template")
	}
	if pod.NodeSelector["topology.kubernetes.io/zone"] != "us-east-1a" {
		t.Fatalf("expected topology zone in nodeSelector, got %v", pod.NodeSelector)
	}

	// Verify launch readiness is Ready.
	if updated.Status.LaunchReadiness == nil {
		t.Fatal("expected LaunchReadiness to be populated")
	}
	if !updated.Status.LaunchReadiness.Ready {
		t.Fatal("expected LaunchReadiness.Ready to be true")
	}

	// Verify effective launch shape.
	if updated.Status.EffectiveLaunchShape == nil {
		t.Fatal("expected EffectiveLaunchShape to be populated")
	}
	if updated.Status.EffectiveLaunchShape.WorkerCount != 2 {
		t.Fatalf("expected worker count 2, got %d", updated.Status.EffectiveLaunchShape.WorkerCount)
	}
}

func TestReconcileFlavorInjectionStillWorksWithTopology(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	workload := testWorkloadForRTJ(rtj, kueuev1beta2.CheckStateReady)
	workload.Status.Admission = &kueuev1beta2.Admission{
		ClusterQueue: "test-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "trainer",
				Count: ptr.To[int32](2),
				Flavors: map[corev1.ResourceName]kueuev1beta2.ResourceFlavorReference{
					"nvidia.com/gpu": "a100-80gb",
				},
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{"topology.kubernetes.io/zone"},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{
						{
							DomainCount: 1,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Universal: ptr.To("us-east-1a")},
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
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	// Verify flavor was recorded in admission status.
	if updated.Status.Admission == nil {
		t.Fatal("expected admission status to be populated")
	}
	if updated.Status.Admission.AdmittedFlavors == nil {
		t.Fatal("expected admitted flavors to be populated")
	}
	if updated.Status.Admission.AdmittedFlavors["trainer"] != "a100-80gb" {
		t.Fatalf("expected flavor a100-80gb for trainer, got %v", updated.Status.Admission.AdmittedFlavors)
	}
}

func TestReconcileNonRepresentableTopologyFailsClearly(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Create a workload with multi-domain single-level topology (not representable).
	workload := testWorkloadForRTJ(rtj, kueuev1beta2.CheckStateReady)
	workload.Status.Admission = &kueuev1beta2.Admission{
		ClusterQueue: "test-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "trainer",
				Count: ptr.To[int32](4),
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{"topology.kubernetes.io/zone"},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{
						{
							DomainCount: 2,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Individual: &kueuev1beta2.TopologyAssignmentSliceLevelIndividualValues{
									Roots: []string{"us-east-1a", "us-east-1b"},
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
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// The reconcile should fail and mark the RTJ as failed.
	for i := 0; i < 3; i++ {
		_, _ = reconciler.Reconcile(ctx, req)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseFailed {
		t.Fatalf("expected phase Failed for non-representable topology, got %q", updated.Status.Phase)
	}
}

func TestReconcilePhase3BehaviorPreservedWithoutTopology(t *testing.T) {
	// When topology is not enabled and no workload reference exists,
	// launch should proceed as in Phase 3 without any gate checks.
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation
	// No WorkloadReference, no topology — pure Phase 3 path.

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Single reconcile: should launch immediately without gate checks.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("launch reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q for Phase 3 path, got %q", trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}
	if updated.Status.LaunchReadiness != nil {
		t.Fatal("expected LaunchReadiness to be nil in Phase 3 path")
	}
	if updated.Status.Topology != nil {
		t.Fatal("expected Topology to be nil in Phase 3 path")
	}
}

func TestReconcileChildJobSetHasNoKueueManagementLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	workload := testWorkloadForRTJ(rtj, kueuev1beta2.CheckStateReady)
	workload.Status.Admission = &kueuev1beta2.Admission{
		ClusterQueue: "test-queue",
		PodSetAssignments: []kueuev1beta2.PodSetAssignment{
			{
				Name:  "trainer",
				Count: ptr.To[int32](2),
				TopologyAssignment: &kueuev1beta2.TopologyAssignment{
					Levels: []string{"topology.kubernetes.io/zone"},
					Slices: []kueuev1beta2.TopologyAssignmentSlice{
						{
							DomainCount: 1,
							ValuesPerLevel: []kueuev1beta2.TopologyAssignmentSliceLevelValues{
								{Universal: ptr.To("us-east-1a")},
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
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload).
		Build()

	reconciler := &ResumableTrainingJobReconciler{Client: client, Scheme: scheme}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	childJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	must(t, client.Get(ctx, types.NamespacedName{Name: updated.Status.ActiveJobSetName, Namespace: rtj.Namespace}, childJobSet))

	labels := childJobSet.GetLabels()
	for key := range labels {
		if key == "kueue.x-k8s.io/queue-name" ||
			key == "kueue.x-k8s.io/priority-class" ||
			key == "kueue.x-k8s.io/managed" {
			t.Fatalf("found Kueue management label %q on child JobSet, expected none", key)
		}
	}
}

// --- Phase 4 Test Helpers ---

func controllerTestRTJWithTopology() *trainingv1alpha1.ResumableTrainingJob {
	rtj := controllerTestRTJ()
	rtj.Spec.Topology = &trainingv1alpha1.TopologySpec{
		Mode:          trainingv1alpha1.TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	return rtj
}

func testWorkloadForRTJ(rtj *trainingv1alpha1.ResumableTrainingJob, checkState kueuev1beta2.CheckState) *kueuev1beta2.Workload {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: trainingv1alpha1.GroupVersion.String(),
					Kind:       "ResumableTrainingJob",
					Name:       rtj.Name,
					UID:        rtj.UID,
				},
			},
		},
		Status: kueuev1beta2.WorkloadStatus{},
	}

	// Add a resume-readiness admission check state.
	wl.Status.AdmissionChecks = []kueuev1beta2.AdmissionCheckState{
		{
			Name:    "resume-readiness",
			State:   checkState,
			Message: "InitialLaunchReady",
		},
	}

	return wl
}

type fakeCheckpointCatalog struct {
	observation        *checkpoints.PauseObservation
	ready              bool
	err                error
	selectedCheckpoint *trainingv1alpha1.CheckpointReference
	selected           bool
}

func (f *fakeCheckpointCatalog) ObservePause(context.Context, string, int32, string, time.Time) (*checkpoints.PauseObservation, bool, error) {
	return f.observation, f.ready, f.err
}

func (f *fakeCheckpointCatalog) SelectResumeCheckpoint(context.Context, checkpoints.ResumeRequest) (*trainingv1alpha1.CheckpointReference, bool, error) {
	return f.selectedCheckpoint, f.selected, f.err
}

func (f *fakeCheckpointCatalog) LatestCheckpointInfo(context.Context, string) (*checkpoints.CheckpointInfo, bool, error) {
	return nil, false, nil
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
