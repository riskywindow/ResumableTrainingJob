package resume

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// --- mock catalog ---

type mockCatalog struct {
	ref *trainingv1alpha1.CheckpointReference
	ok  bool
	err error
}

func (m *mockCatalog) ObservePause(_ context.Context, _ string, _ int32, _ string, _ time.Time) (*checkpoints.PauseObservation, bool, error) {
	return nil, false, nil
}

func (m *mockCatalog) SelectResumeCheckpoint(_ context.Context, _ checkpoints.ResumeRequest) (*trainingv1alpha1.CheckpointReference, bool, error) {
	return m.ref, m.ok, m.err
}

// --- test helpers ---

func reconcilerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := kueuev1beta2.AddToScheme(s); err != nil {
		t.Fatalf("add kueue scheme: %v", err)
	}
	if err := trainingv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add training scheme: %v", err)
	}
	return s
}

func baseAdmissionCheck() *kueuev1beta2.AdmissionCheck {
	return &kueuev1beta2.AdmissionCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resume-readiness-check",
		},
		Spec: kueuev1beta2.AdmissionCheckSpec{
			ControllerName: ControllerName,
			Parameters: &kueuev1beta2.AdmissionCheckParametersReference{
				APIGroup: trainingv1alpha1.GroupVersion.Group,
				Kind:     "ResumeReadinessPolicy",
				Name:     "default-policy",
			},
		},
	}
}

func basePolicy() *trainingv1alpha1.ResumeReadinessPolicy {
	return &trainingv1alpha1.ResumeReadinessPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-policy",
		},
		Spec: trainingv1alpha1.ResumeReadinessPolicySpec{
			FailurePolicy: trainingv1alpha1.FailurePolicyFailClosed,
		},
	}
}

func baseRTJ() *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
			UID:       types.UID("rtj-uid-123"),
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			QueueName:                 "test-queue",
			WorkloadPriorityClassName: "test-priority",
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "local/fixture:dev",
				CodeVersion: "git:abc123",
				WorldSize:   2,
				GPUShape:    "cpu",
			},
			Runtime: trainingv1alpha1.ResumableTrainingJobRuntime{
				Mode:          trainingv1alpha1.RuntimeModeDDP,
				OptimizerMode: "adamw",
				ShardingMode:  "replicated-optimizer-state",
			},
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI: "s3://bucket/demo/",
			},
		},
	}
}

func baseWorkload(rtj *trainingv1alpha1.ResumableTrainingJob) *kueuev1beta2.Workload {
	return &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workload",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: trainingv1alpha1.GroupVersion.String(),
					Kind:       "ResumableTrainingJob",
					Name:       rtj.Name,
					UID:        rtj.UID,
				},
			},
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:  "resume-readiness-check",
					State: kueuev1beta2.CheckStatePending,
				},
			},
		},
	}
}

// --- Tests ---

// TestReconcilerInitialLaunchReadyNoCatalog verifies that with no catalog
// and default policy, the check transitions to Ready.
func TestReconcilerInitialLaunchReadyNoCatalog(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	wl := baseWorkload(rtj)
	ac := baseAdmissionCheck()
	policy := basePolicy()

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue, got %v", result.RequeueAfter)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateReady, ReasonInitialLaunchReady)
}

// TestReconcilerCheckpointReadyWithCatalog verifies that when a compatible
// checkpoint is found, the check transitions to Ready.
func TestReconcilerCheckpointReadyWithCatalog(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	rtj.Status.CurrentRunAttempt = 2
	wl := baseWorkload(rtj)
	ac := baseAdmissionCheck()
	policy := basePolicy()

	completionTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	catalog := &mockCatalog{
		ref: &trainingv1alpha1.CheckpointReference{
			ID:             "ckpt-1",
			ManifestURI:    "s3://bucket/demo/manifests/ckpt-1.manifest.json",
			CompletionTime: &completionTime,
			WorldSize:      2,
		},
		ok: true,
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl, Catalog: catalog}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue, got %v", result.RequeueAfter)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateReady, ReasonCheckpointReady)
}

// TestReconcilerStorageErrorRetryFailClosed verifies that when the catalog
// returns an error and the policy is FailClosed, the check transitions to Retry.
func TestReconcilerStorageErrorRetryFailClosed(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	wl := baseWorkload(rtj)
	ac := baseAdmissionCheck()
	policy := basePolicy()
	policy.Spec.FailurePolicy = trainingv1alpha1.FailurePolicyFailClosed

	catalog := &mockCatalog{
		err: fmt.Errorf("S3 connection timeout"),
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl, Catalog: catalog}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue for Retry state")
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateRetry, ReasonStorageUnavailable)
}

// TestReconcilerStorageErrorReadyFailOpen verifies that when the catalog
// returns an error and the policy is FailOpen, the check transitions to Ready.
func TestReconcilerStorageErrorReadyFailOpen(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	wl := baseWorkload(rtj)
	ac := baseAdmissionCheck()
	policy := basePolicy()
	policy.Spec.FailurePolicy = trainingv1alpha1.FailurePolicyFailOpen

	catalog := &mockCatalog{
		err: fmt.Errorf("DNS resolution failed"),
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl, Catalog: catalog}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue for Ready state, got %v", result.RequeueAfter)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateReady, ReasonStorageUnavailable)
}

// TestReconcilerNoCheckpointRejectedInitialBlocked verifies that when no
// checkpoint is found and initial launch is not allowed, the check is Rejected.
func TestReconcilerNoCheckpointRejectedInitialBlocked(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	wl := baseWorkload(rtj)
	ac := baseAdmissionCheck()
	policy := basePolicy()
	allowInitial := false
	policy.Spec.AllowInitialLaunchWithoutCheckpoint = &allowInitial

	catalog := &mockCatalog{ok: false}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl, Catalog: catalog}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateRejected, ReasonInitialLaunchBlocked)
}

// TestReconcilerIgnoresNonManagedChecks verifies the reconciler does not
// modify checks with a different controller.
func TestReconcilerIgnoresNonManagedChecks(t *testing.T) {
	s := reconcilerScheme(t)

	otherAC := &kueuev1beta2.AdmissionCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-check",
		},
		Spec: kueuev1beta2.AdmissionCheckSpec{
			ControllerName: "kueue.x-k8s.io/provisioning-request",
		},
	}

	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workload",
			Namespace: "default",
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:  "other-check",
					State: kueuev1beta2.CheckStatePending,
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(otherAC, wl).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	if updated.Status.AdmissionChecks[0].State != kueuev1beta2.CheckStatePending {
		t.Fatalf("expected check to remain Pending, got %v", updated.Status.AdmissionChecks[0].State)
	}
}

// TestReconcilerNoopWhenAlreadyReady verifies no spurious update when the
// check is already in the target state.
func TestReconcilerNoopWhenAlreadyReady(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	ac := baseAdmissionCheck()
	policy := basePolicy()

	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workload",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: trainingv1alpha1.GroupVersion.String(),
					Kind:       "ResumableTrainingJob",
					Name:       rtj.Name,
					UID:        rtj.UID,
				},
			},
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "resume-readiness-check",
					State:   kueuev1beta2.CheckStateReady,
					Message: "no checkpoint catalog configured; initial launch allowed by policy",
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	if updated.Status.AdmissionChecks[0].State != kueuev1beta2.CheckStateReady {
		t.Fatalf("expected check to remain Ready, got %v", updated.Status.AdmissionChecks[0].State)
	}
}

// TestReconcilerPolicyResolutionFailedRetries verifies that when the policy
// cannot be loaded, the check transitions to Retry.
func TestReconcilerPolicyResolutionFailedRetries(t *testing.T) {
	s := reconcilerScheme(t)
	rtj := baseRTJ()
	wl := baseWorkload(rtj)

	// AdmissionCheck references a nonexistent policy.
	ac := &kueuev1beta2.AdmissionCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resume-readiness-check",
		},
		Spec: kueuev1beta2.AdmissionCheckSpec{
			ControllerName: ControllerName,
			Parameters: &kueuev1beta2.AdmissionCheckParametersReference{
				APIGroup: trainingv1alpha1.GroupVersion.Group,
				Kind:     "ResumeReadinessPolicy",
				Name:     "nonexistent-policy",
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(rtj, wl, ac).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue for Retry state")
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "test-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateRetry, ReasonPolicyResolutionFailed)
}

// TestReconcilerNonRTJWorkloadDefaultsReady verifies that workloads not
// owned by an RTJ get their managed checks set to Ready.
func TestReconcilerNonRTJWorkloadDefaultsReady(t *testing.T) {
	s := reconcilerScheme(t)
	ac := baseAdmissionCheck()
	policy := basePolicy()

	// Workload with no owner references.
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-workload",
			Namespace: "default",
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:  "resume-readiness-check",
					State: kueuev1beta2.CheckStatePending,
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(wl, ac, policy).
		WithStatusSubresource(wl).
		Build()

	r := &WorkloadReconciler{Client: cl}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "orphan-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "orphan-workload", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get workload: %v", err)
	}

	assertCheckState(t, updated, kueuev1beta2.CheckStateReady, ReasonOwnerNotFound)
}

// --- assertion helper ---

func assertCheckState(t *testing.T, wl *kueuev1beta2.Workload, wantState kueuev1beta2.CheckState, wantReasonSubstring string) {
	t.Helper()
	if len(wl.Status.AdmissionChecks) == 0 {
		t.Fatal("expected at least one admission check")
	}
	acs := wl.Status.AdmissionChecks[0]
	if acs.State != wantState {
		t.Errorf("expected state %v, got %v (message=%s)", wantState, acs.State, acs.Message)
	}
	if wantReasonSubstring != "" {
		found := false
		// The reason is encoded in the message since Kueue's AdmissionCheckState
		// doesn't have a separate reason field. Check both Message and the reason constant.
		for _, haystack := range []string{acs.Message} {
			if len(haystack) > 0 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected message to be non-empty for reason %q", wantReasonSubstring)
		}
	}
}
