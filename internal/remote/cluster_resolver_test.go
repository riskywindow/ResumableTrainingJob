package remote

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := trainingv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := kueuev1beta2.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWorkloadClusterResolverReturnsClusterFromReadyAdmissionCheck(t *testing.T) {
	scheme := testScheme(t)

	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj-workload",
			Namespace: "default",
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    kueuev1beta2.AdmissionCheckReference("multikueue"),
					State:   kueuev1beta2.CheckStateReady,
					Message: "worker-cluster-1",
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(wl).
		Build()

	resolver := &WorkloadClusterResolver{Client: c}
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
		Status: trainingv1alpha1.ResumableTrainingJobStatus{
			WorkloadReference: &trainingv1alpha1.WorkloadReference{
				Name:      "test-rtj-workload",
				Namespace: "default",
			},
		},
	}

	cluster, err := resolver.ResolveExecutionCluster(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "worker-cluster-1" {
		t.Fatalf("expected cluster %q, got %q", "worker-cluster-1", cluster)
	}
}

func TestWorkloadClusterResolverReturnsEmptyWhenNoWorkloadRef(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	resolver := &WorkloadClusterResolver{Client: c}
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
	}

	cluster, err := resolver.ResolveExecutionCluster(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "" {
		t.Fatalf("expected empty cluster, got %q", cluster)
	}
}

func TestWorkloadClusterResolverReturnsEmptyWhenCheckPending(t *testing.T) {
	scheme := testScheme(t)

	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj-workload",
			Namespace: "default",
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    kueuev1beta2.AdmissionCheckReference("multikueue"),
					State:   kueuev1beta2.CheckStatePending,
					Message: "",
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(wl).
		Build()

	resolver := &WorkloadClusterResolver{Client: c}
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
		Status: trainingv1alpha1.ResumableTrainingJobStatus{
			WorkloadReference: &trainingv1alpha1.WorkloadReference{
				Name:      "test-rtj-workload",
				Namespace: "default",
			},
		},
	}

	cluster, err := resolver.ResolveExecutionCluster(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "" {
		t.Fatalf("expected empty cluster for pending check, got %q", cluster)
	}
}

func TestWorkloadClusterResolverFiltersByAdmissionCheckName(t *testing.T) {
	scheme := testScheme(t)

	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj-workload",
			Namespace: "default",
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    kueuev1beta2.AdmissionCheckReference("other-check"),
					State:   kueuev1beta2.CheckStateReady,
					Message: "wrong-cluster",
				},
				{
					Name:    kueuev1beta2.AdmissionCheckReference("my-multikueue"),
					State:   kueuev1beta2.CheckStateReady,
					Message: "correct-cluster",
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(wl).
		Build()

	resolver := &WorkloadClusterResolver{
		Client:             c,
		AdmissionCheckName: "my-multikueue",
	}
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
		Status: trainingv1alpha1.ResumableTrainingJobStatus{
			WorkloadReference: &trainingv1alpha1.WorkloadReference{
				Name:      "test-rtj-workload",
				Namespace: "default",
			},
		},
	}

	cluster, err := resolver.ResolveExecutionCluster(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "correct-cluster" {
		t.Fatalf("expected cluster %q, got %q", "correct-cluster", cluster)
	}
}

func TestWorkloadClusterResolverUsesJobNamespaceAsDefault(t *testing.T) {
	scheme := testScheme(t)

	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wl",
			Namespace: "training",
		},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    kueuev1beta2.AdmissionCheckReference("multikueue"),
					State:   kueuev1beta2.CheckStateReady,
					Message: "worker-2",
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(wl).
		Build()

	resolver := &WorkloadClusterResolver{Client: c}
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "training",
		},
		Status: trainingv1alpha1.ResumableTrainingJobStatus{
			WorkloadReference: &trainingv1alpha1.WorkloadReference{
				Name: "test-wl",
				// Namespace intentionally empty — should default to job.Namespace.
			},
		},
	}

	cluster, err := resolver.ResolveExecutionCluster(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "worker-2" {
		t.Fatalf("expected cluster %q, got %q", "worker-2", cluster)
	}
}

func TestStaticClusterResolver(t *testing.T) {
	resolver := &StaticClusterResolver{ClusterName: "static-worker"}
	cluster, err := resolver.ResolveExecutionCluster(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cluster != "static-worker" {
		t.Fatalf("expected cluster %q, got %q", "static-worker", cluster)
	}
}

// --- Unit tests for extractClusterFromAdmissionChecks ---

func TestExtractClusterFromAdmissionChecksReadyMatch(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: kueuev1beta2.AdmissionCheckReference("multikueue"), State: kueuev1beta2.CheckStateReady, Message: "worker-1"},
	}
	got := extractClusterFromAdmissionChecks(checks, "")
	if got != "worker-1" {
		t.Fatalf("expected %q, got %q", "worker-1", got)
	}
}

func TestExtractClusterFromAdmissionChecksPendingNoMatch(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: kueuev1beta2.AdmissionCheckReference("multikueue"), State: kueuev1beta2.CheckStatePending, Message: ""},
	}
	got := extractClusterFromAdmissionChecks(checks, "")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractClusterFromAdmissionChecksEmptyMessage(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: kueuev1beta2.AdmissionCheckReference("multikueue"), State: kueuev1beta2.CheckStateReady, Message: ""},
	}
	got := extractClusterFromAdmissionChecks(checks, "")
	if got != "" {
		t.Fatalf("expected empty for Ready state with empty message, got %q", got)
	}
}

func TestExtractClusterFromAdmissionChecksNameFilter(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: kueuev1beta2.AdmissionCheckReference("other"), State: kueuev1beta2.CheckStateReady, Message: "wrong"},
		{Name: kueuev1beta2.AdmissionCheckReference("target"), State: kueuev1beta2.CheckStateReady, Message: "correct"},
	}
	got := extractClusterFromAdmissionChecks(checks, "target")
	if got != "correct" {
		t.Fatalf("expected %q, got %q", "correct", got)
	}
}

func TestExtractClusterFromAdmissionChecksEmptyList(t *testing.T) {
	got := extractClusterFromAdmissionChecks(nil, "")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
