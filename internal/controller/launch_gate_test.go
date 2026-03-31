package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	corev1 "k8s.io/api/core/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// --- Phase 7 Launch Gate Tests ---

func TestReconcileDoesNotCreateChildJobSetBeforeProvisioningReady(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Create a workload with a Pending provisioning AC.
	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "test-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "trainer", Count: ptr.To[int32](2)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "provisioning-check",
					State:   kueuev1beta2.CheckStatePending,
					Message: "Provisioning in progress",
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

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		ProvisioningACNames: map[string]bool{
			"provisioning-check": true,
		},
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

	// Should NOT have created a child JobSet.
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet while provisioning is pending, got %q", updated.Status.ActiveJobSetName)
	}

	// Should have set LaunchGate to Blocked.
	if updated.Status.LaunchGate == nil {
		t.Fatal("expected LaunchGate status to be set")
	}
	if updated.Status.LaunchGate.State != trainingv1alpha1.LaunchGateBlocked {
		t.Fatalf("expected LaunchGate.State=Blocked, got %q", updated.Status.LaunchGate.State)
	}
	if updated.Status.LaunchGate.Reason != reasonProvisioningInProgress {
		t.Fatalf("expected reason %q, got %q", reasonProvisioningInProgress, updated.Status.LaunchGate.Reason)
	}

	// Should have set Provisioning status.
	if updated.Status.Provisioning == nil {
		t.Fatal("expected Provisioning status to be set")
	}
	if updated.Status.Provisioning.State != trainingv1alpha1.ProvisioningPending {
		t.Fatalf("expected provisioning state Pending, got %q", updated.Status.Provisioning.State)
	}

	// Should have set Capacity status.
	if updated.Status.Capacity == nil {
		t.Fatal("expected Capacity status to be set")
	}
	if updated.Status.Capacity.GuaranteeActive {
		t.Fatal("expected capacity guarantee NOT active while provisioning pending")
	}

	// No child JobSet should exist.
	childJobSet := rtjjobset.NewEmptyChildJobSet(trainingv1alpha1.DefaultJobSetAPIVersion, trainingv1alpha1.DefaultJobSetKind)
	err := client.Get(ctx, types.NamespacedName{Name: rtjjobset.ChildJobSetName(rtj.Name, 1), Namespace: rtj.Namespace}, childJobSet)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected no child JobSet before provisioning ready, got err=%v", err)
	}
}

func TestReconcileDoesNotCreateChildJobSetBeforeDelayedTopologyAssignment(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJWithTopology()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Create a workload where all ACs are Ready but topology has a
	// delayedTopologyRequest that is Pending (second pass not done).
	pendingState := kueuev1beta2.DelayedTopologyRequestStatePending
	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "test-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{
						Name:                    "trainer",
						Count:                   ptr.To[int32](2),
						DelayedTopologyRequest:  &pendingState,
						// No TopologyAssignment yet — second pass pending.
					},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "resume-readiness",
					State:   kueuev1beta2.CheckStateReady,
					Message: "InitialLaunchReady",
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

	// Should NOT have created a child JobSet.
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet while topology second pass pending, got %q", updated.Status.ActiveJobSetName)
	}

	// Should have set LaunchGate to Blocked with TopologyPendingSecondPass.
	if updated.Status.LaunchGate == nil {
		t.Fatal("expected LaunchGate status to be set")
	}
	if updated.Status.LaunchGate.Reason != reasonTopologyPendingSecondPass {
		t.Fatalf("expected reason %q, got %q", reasonTopologyPendingSecondPass, updated.Status.LaunchGate.Reason)
	}
	if updated.Status.LaunchGate.TopologyGateState != trainingv1alpha1.TopologyGatePending {
		t.Fatalf("expected topology gate state Pending, got %q", updated.Status.LaunchGate.TopologyGateState)
	}
}

func TestReconcileProvisioningReadyLaunchesWithPodSetUpdates(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Workload with a Ready provisioning AC that has podSetUpdates.
	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "test-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "trainer", Count: ptr.To[int32](2)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "provisioning-check",
					State:   kueuev1beta2.CheckStateReady,
					Message: "Capacity confirmed",
					PodSetUpdates: []kueuev1beta2.PodSetUpdate{
						{
							Name:         "trainer",
							NodeSelector: map[string]string{"provisioning.example.com/pool": "reserved-gpu"},
							Tolerations: []corev1.Toleration{
								{Key: "provisioning.example.com/reserved", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
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

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		ProvisioningACNames: map[string]bool{
			"provisioning-check": true,
		},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Reconcile to launch.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("launch reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase Starting, got %q", updated.Status.Phase)
	}
	if updated.Status.ActiveJobSetName == "" {
		t.Fatal("expected active child JobSet to be created")
	}

	// Verify LaunchGate is Open.
	if updated.Status.LaunchGate == nil {
		t.Fatal("expected LaunchGate status to be set")
	}
	if updated.Status.LaunchGate.State != trainingv1alpha1.LaunchGateOpen {
		t.Fatalf("expected LaunchGate.State=Open, got %q", updated.Status.LaunchGate.State)
	}

	// Verify Provisioning status.
	if updated.Status.Provisioning == nil {
		t.Fatal("expected Provisioning status to be set")
	}
	if updated.Status.Provisioning.State != trainingv1alpha1.ProvisioningProvisioned {
		t.Fatalf("expected provisioning state Provisioned, got %q", updated.Status.Provisioning.State)
	}

	// Verify Capacity guarantee is active.
	if updated.Status.Capacity == nil {
		t.Fatal("expected Capacity status to be set")
	}
	if !updated.Status.Capacity.GuaranteeActive {
		t.Fatal("expected capacity guarantee active when provisioning is satisfied")
	}

	// Verify the child JobSet has the podSetUpdate nodeSelector and toleration.
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
	if pod.NodeSelector["provisioning.example.com/pool"] != "reserved-gpu" {
		t.Fatalf("expected provisioning nodeSelector, got %v", pod.NodeSelector)
	}
	if len(pod.Tolerations) == 0 {
		t.Fatal("expected tolerations on child JobSet pod template")
	}
	found := false
	for _, tol := range pod.Tolerations {
		if tol.Key == "provisioning.example.com/reserved" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected provisioning toleration, got %v", pod.Tolerations)
	}
}

func TestReconcileProvisioningFailedBlocksLaunch(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "test-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "trainer", Count: ptr.To[int32](2)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "provisioning-check",
					State:   kueuev1beta2.CheckStateRejected,
					Message: "Capacity unavailable",
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

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		ProvisioningACNames: map[string]bool{
			"provisioning-check": true,
		},
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

	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet when provisioning failed, got %q", updated.Status.ActiveJobSetName)
	}
	if updated.Status.LaunchGate == nil {
		t.Fatal("expected LaunchGate status to be set")
	}
	if updated.Status.LaunchGate.Reason != reasonProvisioningFailed {
		t.Fatalf("expected reason %q, got %q", reasonProvisioningFailed, updated.Status.LaunchGate.Reason)
	}
	if updated.Status.Provisioning == nil || updated.Status.Provisioning.State != trainingv1alpha1.ProvisioningFailed {
		t.Fatal("expected provisioning state Failed")
	}
}

func TestReconcilePhase6BehaviorPreservedWhenProvisioningAbsent(t *testing.T) {
	// When no provisioning AC names are configured and no topology is enabled,
	// the controller should behave exactly as Phase 6.
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation
	// No WorkloadReference, no topology, no ProvisioningACNames.

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj).
		WithObjects(rtj).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		// No ProvisioningACNames set — Phase 6 behavior.
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Single reconcile: should launch immediately.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("launch reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected phase Starting for Phase 6 path, got %q", updated.Status.Phase)
	}
	// Phase 7 status sections should NOT be populated in Phase 6 path.
	if updated.Status.LaunchGate != nil {
		t.Fatal("expected LaunchGate to be nil in Phase 6 path")
	}
	if updated.Status.Provisioning != nil {
		t.Fatal("expected Provisioning to be nil in Phase 6 path")
	}
	if updated.Status.Capacity != nil {
		t.Fatal("expected Capacity to be nil in Phase 6 path")
	}
}

func TestReconcileConflictingPodSetUpdateFailsClearly(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	// RTJ with an existing nodeSelector on the pod template.
	rtj := controllerTestRTJWithExistingNodeSelector()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	// Workload with a Ready provisioning AC that has conflicting nodeSelector.
	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "test-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "trainer", Count: ptr.To[int32](2)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "provisioning-check",
					State:   kueuev1beta2.CheckStateReady,
					Message: "Capacity confirmed",
					PodSetUpdates: []kueuev1beta2.PodSetUpdate{
						{
							Name: "trainer",
							// This conflicts with the existing "cloud.google.com/gke-accelerator"
							// which is set to "nvidia-tesla-a100" in the template.
							NodeSelector: map[string]string{
								"cloud.google.com/gke-accelerator": "nvidia-h100",
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

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		ProvisioningACNames: map[string]bool{
			"provisioning-check": true,
		},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// Reconcile should return an error for the conflict.
	_, err := reconciler.Reconcile(ctx, req)
	if err == nil {
		t.Fatal("expected error for conflicting podSetUpdate, got nil")
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))

	// Should be Failed due to conflict.
	if updated.Status.Phase != trainingv1alpha1.PhaseFailed {
		t.Fatalf("expected phase Failed for conflicting podSetUpdate, got %q", updated.Status.Phase)
	}

	// No child JobSet should exist.
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no active child JobSet when podSetUpdate conflicts, got %q", updated.Status.ActiveJobSetName)
	}
}

func TestReconcileMultipleACsOneNotReadyBlocksLaunch(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	rtj := controllerTestRTJ()
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Status.Phase = trainingv1alpha1.PhaseQueued
	rtj.Status.ObservedGeneration = rtj.Generation

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "test-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "trainer", Count: ptr.To[int32](2)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:    "resume-readiness",
					State:   kueuev1beta2.CheckStateReady,
					Message: "InitialLaunchReady",
				},
				{
					Name:    "provisioning-check",
					State:   kueuev1beta2.CheckStatePending,
					Message: "Waiting for backend",
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

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		ProvisioningACNames: map[string]bool{
			"provisioning-check": true,
		},
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

	// Should NOT launch — one AC is still pending.
	if updated.Status.ActiveJobSetName != "" {
		t.Fatalf("expected no child JobSet with one AC pending, got %q", updated.Status.ActiveJobSetName)
	}
	if updated.Status.LaunchGate == nil {
		t.Fatal("expected LaunchGate to be set")
	}
	if updated.Status.LaunchGate.State != trainingv1alpha1.LaunchGateBlocked {
		t.Fatalf("expected LaunchGate.State=Blocked, got %q", updated.Status.LaunchGate.State)
	}

	// Verify admission check summary has both checks.
	if len(updated.Status.LaunchGate.AdmissionCheckSummary) != 2 {
		t.Fatalf("expected 2 ACs in summary, got %d", len(updated.Status.LaunchGate.AdmissionCheckSummary))
	}
	if updated.Status.LaunchGate.AdmissionCheckSummary["resume-readiness"] != trainingv1alpha1.AdmissionCheckReady {
		t.Fatalf("expected resume-readiness=Ready in summary, got %q", updated.Status.LaunchGate.AdmissionCheckSummary["resume-readiness"])
	}
	if updated.Status.LaunchGate.AdmissionCheckSummary["provisioning-check"] != trainingv1alpha1.AdmissionCheckPending {
		t.Fatalf("expected provisioning-check=Pending in summary, got %q", updated.Status.LaunchGate.AdmissionCheckSummary["provisioning-check"])
	}
}

// --- Phase 7 Test Helpers ---

func controllerTestRTJWithExistingNodeSelector() *trainingv1alpha1.ResumableTrainingJob {
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
				GPUShape:    "a100",
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
					},
				},
			},
			Checkpoint: trainingv1alpha1.CheckpointPolicy{
				StorageURI:      "s3://phase1-checkpoints/counter/",
				Interval:        metav1.Duration{Duration: 5 * 60000000000},
				FreshnessBudget: metav1.Duration{Duration: 10 * 60000000000},
				MaxDrainTime:    metav1.Duration{Duration: 15 * 60000000000},
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
