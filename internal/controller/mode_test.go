package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// --- Unit tests for mode.go ---

func TestParseOperatorModeWorker(t *testing.T) {
	mode, err := ParseOperatorMode("worker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != ModeWorker {
		t.Fatalf("expected %q, got %q", ModeWorker, mode)
	}
}

func TestParseOperatorModeManager(t *testing.T) {
	mode, err := ParseOperatorMode("manager")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != ModeManager {
		t.Fatalf("expected %q, got %q", ModeManager, mode)
	}
}

func TestParseOperatorModeInvalid(t *testing.T) {
	for _, input := range []string{"", "auto", "single", "WORKER", "Manager"} {
		_, err := ParseOperatorMode(input)
		if err == nil {
			t.Fatalf("expected error for mode %q, got nil", input)
		}
	}
}

type fakeMultiKueueChecker struct {
	managed bool
}

func (f *fakeMultiKueueChecker) IsManagedByMultiKueue() bool {
	return f.managed
}

func TestShouldSuppressRuntimeManagerModeMultiKueueManaged(t *testing.T) {
	if !ShouldSuppressRuntime(ModeManager, &fakeMultiKueueChecker{managed: true}) {
		t.Fatal("expected runtime suppression in manager mode for MultiKueue-managed RTJ")
	}
}

func TestShouldSuppressRuntimeManagerModeNotMultiKueueManaged(t *testing.T) {
	if ShouldSuppressRuntime(ModeManager, &fakeMultiKueueChecker{managed: false}) {
		t.Fatal("expected NO runtime suppression in manager mode for non-MultiKueue RTJ")
	}
}

func TestShouldSuppressRuntimeWorkerModeMultiKueueManaged(t *testing.T) {
	if ShouldSuppressRuntime(ModeWorker, &fakeMultiKueueChecker{managed: true}) {
		t.Fatal("expected NO runtime suppression in worker mode even for MultiKueue-managed RTJ")
	}
}

func TestShouldSuppressRuntimeWorkerModeNotMultiKueueManaged(t *testing.T) {
	if ShouldSuppressRuntime(ModeWorker, &fakeMultiKueueChecker{managed: false}) {
		t.Fatal("expected NO runtime suppression in worker mode for non-MultiKueue RTJ")
	}
}

// --- Integration tests: manager mode reconciliation ---

func TestManagerModeSuppressesRuntimeForMultiKueueManagedRTJ(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Mode:   ModeManager,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Run multiple reconcile loops to get past finalizer + init.
	for i := 0; i < 4; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	// Verify MultiCluster status is set correctly.
	if updated.Status.MultiCluster == nil {
		t.Fatal("expected MultiCluster status to be populated")
	}
	if !updated.Status.MultiCluster.LocalExecutionSuppressed {
		t.Fatal("expected LocalExecutionSuppressed to be true")
	}
	if updated.Status.MultiCluster.DispatchPhase != trainingv1alpha1.DispatchPhasePending {
		t.Fatalf("expected dispatch phase %q, got %q",
			trainingv1alpha1.DispatchPhasePending, updated.Status.MultiCluster.DispatchPhase)
	}

	// Verify NO child JobSet was created.
	childJobSet := rtjjobset.NewEmptyChildJobSet(
		trainingv1alpha1.DefaultJobSetAPIVersion,
		trainingv1alpha1.DefaultJobSetKind,
	)
	err := client.Get(ctx, types.NamespacedName{
		Name:      rtjjobset.ChildJobSetName(rtj.Name, 1),
		Namespace: rtj.Namespace,
	}, childJobSet)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no child JobSet for manager-mode MultiKueue RTJ, got err=%v", err)
	}

	// Verify NO control ConfigMap was created.
	var controlConfigMap corev1.ConfigMap
	err = client.Get(ctx, types.NamespacedName{
		Name:      rtjjobset.ControlConfigMapName(rtj.Name, 1),
		Namespace: rtj.Namespace,
	}, &controlConfigMap)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no control ConfigMap for manager-mode MultiKueue RTJ, got err=%v", err)
	}

	// Verify the run attempt was NOT incremented.
	if updated.Status.CurrentRunAttempt != 0 {
		t.Fatalf("expected run attempt 0 (no runtime), got %d", updated.Status.CurrentRunAttempt)
	}

	// Verify status fields.
	if updated.Status.Phase != trainingv1alpha1.PhaseQueued {
		t.Fatalf("expected phase %q, got %q", trainingv1alpha1.PhaseQueued, updated.Status.Phase)
	}
	if updated.Status.Reason != reasonLocalExecutionSuppressed {
		t.Fatalf("expected reason %q, got %q", reasonLocalExecutionSuppressed, updated.Status.Reason)
	}
}

func TestManagerModeAllowsNormalPathForNonMultiKueueRTJ(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	// No managedBy set — single-cluster RTJ on a manager cluster.

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Mode:   ModeManager,
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Run multiple reconcile loops: should reach Starting (normal path).
	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q for non-MultiKueue RTJ in manager mode, got %q",
			trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}
	if updated.Status.CurrentRunAttempt != 1 {
		t.Fatalf("expected run attempt 1, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.MultiCluster != nil {
		t.Fatal("expected MultiCluster status to be nil for non-MultiKueue RTJ")
	}

	// Verify child JobSet WAS created (normal path).
	childJobSet := rtjjobset.NewEmptyChildJobSet(
		trainingv1alpha1.DefaultJobSetAPIVersion,
		trainingv1alpha1.DefaultJobSetKind,
	)
	err := client.Get(ctx, types.NamespacedName{
		Name:      rtjjobset.ChildJobSetName(rtj.Name, 1),
		Namespace: rtj.Namespace,
	}, childJobSet)
	if err != nil {
		t.Fatalf("expected child JobSet for non-MultiKueue RTJ in manager mode, got err=%v", err)
	}
}

func TestWorkerModeLaunchesRuntimeForMultiKueueManagedRTJ(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Mode:   ModeWorker,
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Run multiple reconcile loops: should reach Starting (worker always runs runtime).
	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q in worker mode, got %q",
			trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}
	if updated.Status.CurrentRunAttempt != 1 {
		t.Fatalf("expected run attempt 1, got %d", updated.Status.CurrentRunAttempt)
	}

	// Verify child JobSet WAS created.
	childJobSet := rtjjobset.NewEmptyChildJobSet(
		trainingv1alpha1.DefaultJobSetAPIVersion,
		trainingv1alpha1.DefaultJobSetKind,
	)
	err := client.Get(ctx, types.NamespacedName{
		Name:      rtjjobset.ChildJobSetName(rtj.Name, 1),
		Namespace: rtj.Namespace,
	}, childJobSet)
	if err != nil {
		t.Fatalf("expected child JobSet in worker mode, got err=%v", err)
	}

	// Worker mode should NOT set MultiCluster status.
	if updated.Status.MultiCluster != nil {
		t.Fatal("expected MultiCluster status to be nil in worker mode")
	}
}

func TestSingleClusterBehaviorUnchangedWhenMultiKueueNotUsed(t *testing.T) {
	// This test verifies that the default worker mode with no managedBy
	// field produces identical behavior to the pre-Phase 6 controller.
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	// Explicitly ensure no managedBy is set.
	rtj.Spec.ManagedBy = ""

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	// Default mode (zero value) should behave like worker.
	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		// Mode is zero value (""), which does NOT suppress runtime.
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase %q for single-cluster (default mode), got %q",
			trainingv1alpha1.PhaseStarting, updated.Status.Phase)
	}
	if updated.Status.CurrentRunAttempt != 1 {
		t.Fatalf("expected run attempt 1, got %d", updated.Status.CurrentRunAttempt)
	}
	if updated.Status.MultiCluster != nil {
		t.Fatal("expected no MultiCluster status for single-cluster path")
	}
	if updated.Status.ActiveJobSetName == "" {
		t.Fatal("expected active JobSet name to be set")
	}
}

func TestManagerModeDoesNotCreateJobSetEvenWhenUnsuspended(t *testing.T) {
	// Even when the RTJ is unsuspended (admitted), manager mode must NOT
	// create local child resources for MultiKueue-managed RTJs.
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	rtj.Spec.Suspend = ptr.To(false) // Unsuspended — admitted.
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Mode:   ModeManager,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	// Must NOT have created a child JobSet.
	childJobSet := rtjjobset.NewEmptyChildJobSet(
		trainingv1alpha1.DefaultJobSetAPIVersion,
		trainingv1alpha1.DefaultJobSetKind,
	)
	err := client.Get(ctx, types.NamespacedName{
		Name:      rtjjobset.ChildJobSetName(rtj.Name, 1),
		Namespace: rtj.Namespace,
	}, childJobSet)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NO child JobSet for admitted MultiKueue RTJ in manager mode, got err=%v", err)
	}

	// MultiCluster status should be set.
	if updated.Status.MultiCluster == nil {
		t.Fatal("expected MultiCluster status to be populated")
	}
	if !updated.Status.MultiCluster.LocalExecutionSuppressed {
		t.Fatal("expected LocalExecutionSuppressed to be true")
	}
}

func TestManagerModeRepeatedReconcileIsIdempotent(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhasePending
	rtj.Status.ObservedGeneration = rtj.Generation

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Mode:   ModeManager,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Run many times — should converge and not create runtime resources.
	for i := 0; i < 10; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.MultiCluster == nil {
		t.Fatal("expected MultiCluster status after convergence")
	}
	if !updated.Status.MultiCluster.LocalExecutionSuppressed {
		t.Fatal("expected LocalExecutionSuppressed after convergence")
	}
	if updated.Status.CurrentRunAttempt != 0 {
		t.Fatalf("expected run attempt 0 (no runtime), got %d", updated.Status.CurrentRunAttempt)
	}
}

func TestWorkerModeDefaultForZeroValueMode(t *testing.T) {
	// A zero-value Mode field (empty string) should NOT suppress runtime,
	// preserving backward compatibility with pre-Phase 6 behavior.
	var zeroMode OperatorMode
	if ShouldSuppressRuntime(zeroMode, &fakeMultiKueueChecker{managed: true}) {
		t.Fatal("zero-value mode should not suppress runtime")
	}
	if ShouldSuppressRuntime(zeroMode, &fakeMultiKueueChecker{managed: false}) {
		t.Fatal("zero-value mode should not suppress runtime")
	}
}
