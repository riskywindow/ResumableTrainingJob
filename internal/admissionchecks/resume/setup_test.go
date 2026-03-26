package resume

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// TestControllerNameConstant verifies the controller name constant matches
// the expected convention.
func TestControllerNameConstant(t *testing.T) {
	if ControllerName != "training.checkpoint.example.io/resume-readiness" {
		t.Fatalf("expected controller name %q, got %q",
			"training.checkpoint.example.io/resume-readiness", ControllerName)
	}
}

// TestResumeReadinessPolicyGVK verifies the GVK constant is correct.
func TestResumeReadinessPolicyGVK(t *testing.T) {
	if ResumeReadinessPolicyGVK.Group != "training.checkpoint.example.io" {
		t.Fatalf("expected group %q, got %q", "training.checkpoint.example.io", ResumeReadinessPolicyGVK.Group)
	}
	if ResumeReadinessPolicyGVK.Version != "v1alpha1" {
		t.Fatalf("expected version %q, got %q", "v1alpha1", ResumeReadinessPolicyGVK.Version)
	}
	if ResumeReadinessPolicyGVK.Kind != "ResumeReadinessPolicy" {
		t.Fatalf("expected kind %q, got %q", "ResumeReadinessPolicy", ResumeReadinessPolicyGVK.Kind)
	}
}

// TestResumeReadinessPolicySchemeRegistration verifies that ResumeReadinessPolicy
// is registered in the scheme alongside ResumableTrainingJob.
func TestResumeReadinessPolicySchemeRegistration(t *testing.T) {
	scheme := testScheme(t)

	// Verify ResumeReadinessPolicy is registered.
	gvks, _, err := scheme.ObjectKinds(&trainingv1alpha1.ResumeReadinessPolicy{})
	if err != nil {
		t.Fatalf("expected ResumeReadinessPolicy to be registered in scheme, got error: %v", err)
	}
	found := false
	for _, gvk := range gvks {
		if gvk.Kind == "ResumeReadinessPolicy" && gvk.Group == trainingv1alpha1.GroupVersion.Group {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ResumeReadinessPolicy not found in scheme GVKs: %v", gvks)
	}

	// Verify ResumableTrainingJob is still registered.
	rtjGVKs, _, err := scheme.ObjectKinds(&trainingv1alpha1.ResumableTrainingJob{})
	if err != nil {
		t.Fatalf("expected ResumableTrainingJob to be registered in scheme, got error: %v", err)
	}
	if len(rtjGVKs) == 0 {
		t.Fatalf("ResumableTrainingJob not found in scheme")
	}
}

// TestAdmissionCheckReconcilerSetsActiveWhenPolicyExists verifies the
// AdmissionCheck reconciler sets Active=True when the referenced policy exists.
func TestAdmissionCheckReconcilerSetsActiveWhenPolicyExists(t *testing.T) {
	scheme := testScheme(t)

	policy := &trainingv1alpha1.ResumeReadinessPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-policy",
		},
	}

	ac := &kueuev1beta2.AdmissionCheck{
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

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy, ac).
		WithStatusSubresource(ac).
		Build()

	reconciler := &AdmissionCheckReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "resume-readiness-check"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.AdmissionCheck{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("get updated AC: %v", err)
	}

	cond := findCondition(updated.Status.Conditions, ConditionActive)
	if cond == nil {
		t.Fatalf("expected Active condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected Active=True, got %v", cond.Status)
	}
	if cond.Reason != ReasonControllerReady {
		t.Fatalf("expected reason %q, got %q", ReasonControllerReady, cond.Reason)
	}
}

// TestAdmissionCheckReconcilerSetsInactiveWhenPolicyMissing verifies the
// reconciler sets Active=False when the referenced policy does not exist.
func TestAdmissionCheckReconcilerSetsInactiveWhenPolicyMissing(t *testing.T) {
	scheme := testScheme(t)

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
		WithScheme(scheme).
		WithObjects(ac).
		WithStatusSubresource(ac).
		Build()

	reconciler := &AdmissionCheckReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "resume-readiness-check"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.AdmissionCheck{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("get updated AC: %v", err)
	}

	cond := findCondition(updated.Status.Conditions, ConditionActive)
	if cond == nil {
		t.Fatalf("expected Active condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected Active=False, got %v", cond.Status)
	}
	if cond.Reason != ReasonPolicyNotFound {
		t.Fatalf("expected reason %q, got %q", ReasonPolicyNotFound, cond.Reason)
	}
}

// TestAdmissionCheckReconcilerSetsInactiveWhenNoParameters verifies the
// reconciler sets Active=False when spec.parameters is nil.
func TestAdmissionCheckReconcilerSetsInactiveWhenNoParameters(t *testing.T) {
	scheme := testScheme(t)

	ac := &kueuev1beta2.AdmissionCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name: "resume-readiness-check",
		},
		Spec: kueuev1beta2.AdmissionCheckSpec{
			ControllerName: ControllerName,
			Parameters:     nil,
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ac).
		WithStatusSubresource(ac).
		Build()

	reconciler := &AdmissionCheckReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "resume-readiness-check"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.AdmissionCheck{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("get updated AC: %v", err)
	}

	cond := findCondition(updated.Status.Conditions, ConditionActive)
	if cond == nil {
		t.Fatalf("expected Active condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected Active=False, got %v", cond.Status)
	}
	if cond.Reason != ReasonParametersMissing {
		t.Fatalf("expected reason %q, got %q", ReasonParametersMissing, cond.Reason)
	}
}

// TestAdmissionCheckReconcilerIgnoresOtherControllers verifies the reconciler
// skips AdmissionChecks with a different controllerName.
func TestAdmissionCheckReconcilerIgnoresOtherControllers(t *testing.T) {
	scheme := testScheme(t)

	ac := &kueuev1beta2.AdmissionCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name: "provisioning-check",
		},
		Spec: kueuev1beta2.AdmissionCheckSpec{
			ControllerName: "kueue.x-k8s.io/provisioning-request",
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ac).
		WithStatusSubresource(ac).
		Build()

	reconciler := &AdmissionCheckReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "provisioning-check"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.AdmissionCheck{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(ac), updated); err != nil {
		t.Fatalf("get updated AC: %v", err)
	}

	// Should not have touched the status.
	if len(updated.Status.Conditions) != 0 {
		t.Fatalf("expected no conditions on non-managed AdmissionCheck, got %v", updated.Status.Conditions)
	}
}

// TestWorkloadReconcilerSetsCheckReady verifies the Workload reconciler
// sets the managed check state to Ready for an RTJ-owned workload with a
// valid policy and no catalog (initial launch allowed by default policy).
func TestWorkloadReconcilerSetsCheckReady(t *testing.T) {
	scheme := testScheme(t)

	policy := &trainingv1alpha1.ResumeReadinessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-policy"},
		Spec:       trainingv1alpha1.ResumeReadinessPolicySpec{},
	}

	ac := &kueuev1beta2.AdmissionCheck{
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

	rtj := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
			UID:       "rtj-uid-1",
		},
	}

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
					Name:  "resume-readiness-check",
					State: kueuev1beta2.CheckStatePending,
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy, ac, rtj, wl).
		WithStatusSubresource(wl).
		Build()

	reconciler := &WorkloadReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(wl), updated); err != nil {
		t.Fatalf("get updated workload: %v", err)
	}

	if len(updated.Status.AdmissionChecks) != 1 {
		t.Fatalf("expected 1 admission check, got %d", len(updated.Status.AdmissionChecks))
	}
	if updated.Status.AdmissionChecks[0].State != kueuev1beta2.CheckStateReady {
		t.Fatalf("expected check state Ready, got %v", updated.Status.AdmissionChecks[0].State)
	}
}

// TestWorkloadReconcilerIgnoresNonManagedChecks verifies the reconciler
// does not modify checks with a different controller.
func TestWorkloadReconcilerIgnoresNonManagedChecks(t *testing.T) {
	scheme := testScheme(t)

	ac := &kueuev1beta2.AdmissionCheck{
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
		WithScheme(scheme).
		WithObjects(ac, wl).
		WithStatusSubresource(wl).
		Build()

	reconciler := &WorkloadReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(wl), updated); err != nil {
		t.Fatalf("get updated workload: %v", err)
	}

	// Should not have modified the check state.
	if updated.Status.AdmissionChecks[0].State != kueuev1beta2.CheckStatePending {
		t.Fatalf("expected check state to remain Pending, got %v", updated.Status.AdmissionChecks[0].State)
	}
}

// TestWorkloadReconcilerNoopWhenAlreadyReady verifies the reconciler
// does not issue a status update when the check is already Ready with
// the same message.
func TestWorkloadReconcilerNoopWhenAlreadyReady(t *testing.T) {
	scheme := testScheme(t)

	policy := &trainingv1alpha1.ResumeReadinessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-policy"},
		Spec:       trainingv1alpha1.ResumeReadinessPolicySpec{},
	}

	ac := &kueuev1beta2.AdmissionCheck{
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

	rtj := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
			UID:       "rtj-uid-1",
		},
	}

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
		WithScheme(scheme).
		WithObjects(policy, ac, rtj, wl).
		WithStatusSubresource(wl).
		Build()

	reconciler := &WorkloadReconciler{Client: cl}
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-workload", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	// The state should remain Ready without error — no unnecessary update.
	updated := &kueuev1beta2.Workload{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(wl), updated); err != nil {
		t.Fatalf("get updated workload: %v", err)
	}
	if updated.Status.AdmissionChecks[0].State != kueuev1beta2.CheckStateReady {
		t.Fatalf("expected check to remain Ready, got %v", updated.Status.AdmissionChecks[0].State)
	}
}

// --- helpers ---

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := kueuev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add kueue scheme: %v", err)
	}
	if err := trainingv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add training scheme: %v", err)
	}
	return scheme
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
