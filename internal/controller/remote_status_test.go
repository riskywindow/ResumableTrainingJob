package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/remote"
)

// -------------------------------------------------------------------------
// Unit tests for remote_status.go
// -------------------------------------------------------------------------

func TestSyncRemoteStatusInitializesMultiCluster(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	changed := syncRemoteStatus(job, "", now)
	if !changed {
		t.Fatal("expected change on first sync")
	}
	if job.Status.MultiCluster == nil {
		t.Fatal("expected MultiCluster to be initialized")
	}
	if !job.Status.MultiCluster.LocalExecutionSuppressed {
		t.Fatal("expected LocalExecutionSuppressed to be true")
	}
	if job.Status.MultiCluster.DispatchPhase != trainingv1alpha1.DispatchPhasePending {
		t.Fatalf("expected dispatch phase %q, got %q",
			trainingv1alpha1.DispatchPhasePending, job.Status.MultiCluster.DispatchPhase)
	}
}

func TestSyncRemoteStatusDetectsRemotePhase(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	// Simulate adapter mirroring remote status: the worker has an active
	// JobSet and is in Running phase.
	job.Status.Phase = trainingv1alpha1.PhaseRunning
	job.Status.ActiveJobSetName = "counter-run-1"
	job.Status.CurrentRunAttempt = 1
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	changed := syncRemoteStatus(job, "worker-cluster-1", now)
	if !changed {
		t.Fatal("expected change")
	}

	mc := job.Status.MultiCluster
	if mc.DispatchPhase != trainingv1alpha1.DispatchPhaseActive {
		t.Fatalf("expected dispatch phase %q, got %q",
			trainingv1alpha1.DispatchPhaseActive, mc.DispatchPhase)
	}
	if mc.RemotePhase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected remote phase %q, got %q",
			trainingv1alpha1.PhaseRunning, mc.RemotePhase)
	}
	if mc.ExecutionCluster != "worker-cluster-1" {
		t.Fatalf("expected execution cluster %q, got %q",
			"worker-cluster-1", mc.ExecutionCluster)
	}
}

func TestSyncRemoteStatusMirrorsCheckpointSummary(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	completionTime := metav1.NewTime(time.Date(2026, 3, 30, 13, 30, 0, 0, time.UTC))
	job.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-42",
		StorageURI:     "s3://shared-bucket/training/counter",
		CompletionTime: &completionTime,
	}
	job.Status.ActiveJobSetName = "counter-run-1"
	job.Status.CurrentRunAttempt = 1
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	syncRemoteStatus(job, "worker-1", now)

	rc := job.Status.MultiCluster.RemoteCheckpoint
	if rc == nil {
		t.Fatal("expected remote checkpoint summary to be populated")
	}
	if rc.LastCompletedCheckpointID != "ckpt-42" {
		t.Fatalf("expected checkpoint ID %q, got %q", "ckpt-42", rc.LastCompletedCheckpointID)
	}
	if rc.StorageURI != "s3://shared-bucket/training/counter" {
		t.Fatalf("expected storage URI %q, got %q", "s3://shared-bucket/training/counter", rc.StorageURI)
	}
	if rc.LastCompletedCheckpointTime == nil || !rc.LastCompletedCheckpointTime.Equal(&completionTime) {
		t.Fatalf("expected checkpoint time %v, got %v", completionTime, rc.LastCompletedCheckpointTime)
	}
}

func TestSyncRemoteStatusDispatchedButNoMirroredStatus(t *testing.T) {
	// Execution cluster is known but no mirrored status signal yet.
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	syncRemoteStatus(job, "worker-cluster-1", now)

	mc := job.Status.MultiCluster
	if mc.DispatchPhase != trainingv1alpha1.DispatchPhaseDispatched {
		t.Fatalf("expected dispatch phase %q, got %q",
			trainingv1alpha1.DispatchPhaseDispatched, mc.DispatchPhase)
	}
	if mc.RemotePhase != "" {
		t.Fatalf("expected empty remote phase before adapter sync, got %q", mc.RemotePhase)
	}
	if mc.ExecutionCluster != "worker-cluster-1" {
		t.Fatalf("expected execution cluster %q, got %q", "worker-cluster-1", mc.ExecutionCluster)
	}
}

func TestSyncRemoteStatusBuildsRemoteObjectRef(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	syncRemoteStatus(job, "worker-1", now)

	ref := job.Status.MultiCluster.RemoteObjectRef
	if ref == nil {
		t.Fatal("expected remote object ref to be populated")
	}
	if ref.Cluster != "worker-1" {
		t.Fatalf("expected cluster %q, got %q", "worker-1", ref.Cluster)
	}
	if ref.Name != job.Name {
		t.Fatalf("expected name %q, got %q", job.Name, ref.Name)
	}
	if ref.Namespace != job.Namespace {
		t.Fatalf("expected namespace %q, got %q", job.Namespace, ref.Namespace)
	}
}

func TestSyncRemoteStatusClearsRefWhenNoCluster(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	job.Status.MultiCluster = &trainingv1alpha1.MultiClusterStatus{
		RemoteObjectRef: &trainingv1alpha1.RemoteObjectReference{
			Cluster: "old-worker",
			Name:    job.Name,
		},
	}
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	changed := syncRemoteStatus(job, "", now)
	if !changed {
		t.Fatal("expected change when clearing remote object ref")
	}
	if job.Status.MultiCluster.RemoteObjectRef != nil {
		t.Fatal("expected remote object ref to be cleared")
	}
}

func TestSyncRemoteStatusIdempotent(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	job.Status.Phase = trainingv1alpha1.PhaseRunning
	job.Status.ActiveJobSetName = "counter-run-1"
	job.Status.CurrentRunAttempt = 1
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	// First call should change.
	if !syncRemoteStatus(job, "worker-1", now) {
		t.Fatal("expected change on first call")
	}
	// Second call with same inputs should be idempotent.
	if syncRemoteStatus(job, "worker-1", now) {
		t.Fatal("expected no change on idempotent call")
	}
}

func TestSyncRemoteStatusClearsCheckpointWhenNil(t *testing.T) {
	job := controllerTestRTJ()
	job.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	job.Status.MultiCluster = &trainingv1alpha1.MultiClusterStatus{
		RemoteCheckpoint: &trainingv1alpha1.RemoteCheckpointSummary{
			LastCompletedCheckpointID: "old-ckpt",
		},
	}
	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))

	changed := syncRemoteStatus(job, "", now)
	if !changed {
		t.Fatal("expected change when clearing checkpoint")
	}
	if job.Status.MultiCluster.RemoteCheckpoint != nil {
		t.Fatal("expected remote checkpoint to be cleared")
	}
}

func TestHasRemoteStatusSignal(t *testing.T) {
	tests := []struct {
		name     string
		job      func() *trainingv1alpha1.ResumableTrainingJob
		expected bool
	}{
		{
			name: "no signal",
			job: func() *trainingv1alpha1.ResumableTrainingJob {
				return controllerTestRTJ()
			},
			expected: false,
		},
		{
			name: "active job set name set",
			job: func() *trainingv1alpha1.ResumableTrainingJob {
				j := controllerTestRTJ()
				j.Status.ActiveJobSetName = "counter-run-1"
				return j
			},
			expected: true,
		},
		{
			name: "current run attempt > 0",
			job: func() *trainingv1alpha1.ResumableTrainingJob {
				j := controllerTestRTJ()
				j.Status.CurrentRunAttempt = 1
				return j
			},
			expected: true,
		},
		{
			name: "both signals",
			job: func() *trainingv1alpha1.ResumableTrainingJob {
				j := controllerTestRTJ()
				j.Status.ActiveJobSetName = "counter-run-1"
				j.Status.CurrentRunAttempt = 2
				return j
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasRemoteStatusSignal(tc.job())
			if got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Integration tests: reconcileManagerIntent with remote status
// -------------------------------------------------------------------------

func TestManagerModeReflectsRemoteExecutionCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:          c,
		Scheme:          scheme,
		Mode:            ModeManager,
		Now:             func() metav1.Time { return now },
		ClusterResolver: &remote.StaticClusterResolver{ClusterName: "worker-eu-1"},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Run through init + finalizer + manager path.
	for i := 0; i < 4; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, c.Get(ctx, req.NamespacedName, &updated))

	mc := updated.Status.MultiCluster
	if mc == nil {
		t.Fatal("expected MultiCluster status to be populated")
	}
	if mc.ExecutionCluster != "worker-eu-1" {
		t.Fatalf("expected execution cluster %q, got %q", "worker-eu-1", mc.ExecutionCluster)
	}
	if !mc.LocalExecutionSuppressed {
		t.Fatal("expected LocalExecutionSuppressed to be true")
	}
}

func TestManagerModeReflectsRemotePhaseAfterAdapterSync(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.ObservedGeneration = rtj.Generation
	// Simulate post-adapter state: the adapter has mirrored the remote
	// worker's status, setting phase to Running and activeJobSetName.
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Status.ActiveJobSetName = "counter-run-1"
	rtj.Status.CurrentRunAttempt = 1

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:          c,
		Scheme:          scheme,
		Mode:            ModeManager,
		Now:             func() metav1.Time { return now },
		ClusterResolver: &remote.StaticClusterResolver{ClusterName: "worker-us-1"},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, c.Get(ctx, req.NamespacedName, &updated))

	mc := updated.Status.MultiCluster
	if mc == nil {
		t.Fatal("expected MultiCluster status")
	}
	if mc.DispatchPhase != trainingv1alpha1.DispatchPhaseActive {
		t.Fatalf("expected dispatch phase %q, got %q",
			trainingv1alpha1.DispatchPhaseActive, mc.DispatchPhase)
	}
	if mc.RemotePhase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected remote phase %q, got %q",
			trainingv1alpha1.PhaseRunning, mc.RemotePhase)
	}
}

func TestManagerModeReflectsRemoteCheckpointData(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))
	ckptTime := metav1.NewTime(time.Date(2026, 3, 30, 13, 55, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.ObservedGeneration = rtj.Generation
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Status.ActiveJobSetName = "counter-run-2"
	rtj.Status.CurrentRunAttempt = 2
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-100",
		StorageURI:     "s3://shared-checkpoints/counter",
		CompletionTime: &ckptTime,
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:          c,
		Scheme:          scheme,
		Mode:            ModeManager,
		Now:             func() metav1.Time { return now },
		ClusterResolver: &remote.StaticClusterResolver{ClusterName: "worker-1"},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, c.Get(ctx, req.NamespacedName, &updated))

	mc := updated.Status.MultiCluster
	if mc == nil {
		t.Fatal("expected MultiCluster status")
	}
	rc := mc.RemoteCheckpoint
	if rc == nil {
		t.Fatal("expected remote checkpoint summary")
	}
	if rc.LastCompletedCheckpointID != "ckpt-100" {
		t.Fatalf("expected checkpoint ID %q, got %q", "ckpt-100", rc.LastCompletedCheckpointID)
	}
	if rc.StorageURI != "s3://shared-checkpoints/counter" {
		t.Fatalf("expected storage URI %q, got %q", "s3://shared-checkpoints/counter", rc.StorageURI)
	}
}

func TestManagerModeRemoteStatusSurvivesRepeatedReconciles(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.ObservedGeneration = rtj.Generation
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Status.ActiveJobSetName = "counter-run-1"
	rtj.Status.CurrentRunAttempt = 1

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:          c,
		Scheme:          scheme,
		Mode:            ModeManager,
		Now:             func() metav1.Time { return now },
		ClusterResolver: &remote.StaticClusterResolver{ClusterName: "worker-1"},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Run many reconciles — should converge and be stable.
	for i := 0; i < 10; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, c.Get(ctx, req.NamespacedName, &updated))

	mc := updated.Status.MultiCluster
	if mc == nil {
		t.Fatal("expected MultiCluster status after convergence")
	}
	if mc.DispatchPhase != trainingv1alpha1.DispatchPhaseActive {
		t.Fatalf("expected dispatch phase %q after convergence, got %q",
			trainingv1alpha1.DispatchPhaseActive, mc.DispatchPhase)
	}
	if mc.RemotePhase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected remote phase %q after convergence, got %q",
			trainingv1alpha1.PhaseRunning, mc.RemotePhase)
	}
	if mc.ExecutionCluster != "worker-1" {
		t.Fatalf("expected execution cluster %q after convergence, got %q",
			"worker-1", mc.ExecutionCluster)
	}
	if !mc.LocalExecutionSuppressed {
		t.Fatal("expected LocalExecutionSuppressed after convergence")
	}
}

func TestManagerModeNilClusterResolverIsGraceful(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 30, 14, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	rtj.Spec.ManagedBy = trainingv1alpha1.MultiKueueControllerName

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client:          c,
		Scheme:          scheme,
		Mode:            ModeManager,
		Now:             func() metav1.Time { return now },
		ClusterResolver: nil, // Nil resolver.
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile %d failed with nil resolver: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, c.Get(ctx, req.NamespacedName, &updated))

	mc := updated.Status.MultiCluster
	if mc == nil {
		t.Fatal("expected MultiCluster status even with nil resolver")
	}
	if mc.DispatchPhase != trainingv1alpha1.DispatchPhasePending {
		t.Fatalf("expected dispatch phase %q with nil resolver, got %q",
			trainingv1alpha1.DispatchPhasePending, mc.DispatchPhase)
	}
	if mc.ExecutionCluster != "" {
		t.Fatalf("expected empty execution cluster with nil resolver, got %q", mc.ExecutionCluster)
	}
}

// -------------------------------------------------------------------------
// Unit tests for equality helpers
// -------------------------------------------------------------------------

func TestRemoteObjectRefEqual(t *testing.T) {
	ref := &trainingv1alpha1.RemoteObjectReference{
		Cluster: "w1", Namespace: "default", Name: "rtj-1",
	}
	same := &trainingv1alpha1.RemoteObjectReference{
		Cluster: "w1", Namespace: "default", Name: "rtj-1",
	}
	diff := &trainingv1alpha1.RemoteObjectReference{
		Cluster: "w2", Namespace: "default", Name: "rtj-1",
	}

	if !remoteObjectRefEqual(nil, nil) {
		t.Fatal("nil == nil should be true")
	}
	if remoteObjectRefEqual(nil, ref) {
		t.Fatal("nil != non-nil should be false")
	}
	if !remoteObjectRefEqual(ref, same) {
		t.Fatal("same refs should be equal")
	}
	if remoteObjectRefEqual(ref, diff) {
		t.Fatal("different refs should not be equal")
	}
}

func TestRemoteCheckpointSummaryEqual(t *testing.T) {
	ts := metav1.NewTime(time.Date(2026, 3, 30, 13, 0, 0, 0, time.UTC))
	a := &trainingv1alpha1.RemoteCheckpointSummary{
		LastCompletedCheckpointID:   "ckpt-1",
		LastCompletedCheckpointTime: &ts,
		StorageURI:                  "s3://bucket/path",
	}
	same := &trainingv1alpha1.RemoteCheckpointSummary{
		LastCompletedCheckpointID:   "ckpt-1",
		LastCompletedCheckpointTime: &ts,
		StorageURI:                  "s3://bucket/path",
	}
	diff := &trainingv1alpha1.RemoteCheckpointSummary{
		LastCompletedCheckpointID:   "ckpt-2",
		LastCompletedCheckpointTime: &ts,
		StorageURI:                  "s3://bucket/path",
	}

	if !remoteCheckpointSummaryEqual(nil, nil) {
		t.Fatal("nil == nil should be true")
	}
	if remoteCheckpointSummaryEqual(nil, a) {
		t.Fatal("nil != non-nil should be false")
	}
	if !remoteCheckpointSummaryEqual(a, same) {
		t.Fatal("same summaries should be equal")
	}
	if remoteCheckpointSummaryEqual(a, diff) {
		t.Fatal("different summaries should not be equal")
	}
}
