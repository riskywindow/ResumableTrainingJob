package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjkueue "github.com/example/checkpoint-native-preemption-controller/internal/kueue"
	"github.com/example/checkpoint-native-preemption-controller/internal/policy/checkpointpriority"
)

// --- Test helpers ---

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		trainingv1alpha1.AddToScheme,
		kueuev1beta2.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("add to scheme: %v", err)
		}
	}
	return scheme
}

func testReconciler(t *testing.T, objects ...client.Object) *ResumableTrainingJobReconciler {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(
			&trainingv1alpha1.ResumableTrainingJob{},
			&kueuev1beta2.Workload{},
		).
		WithObjects(objects...).
		Build()

	return &ResumableTrainingJobReconciler{
		Client:  cl,
		Scheme:  scheme,
		Catalog: &fakeCatalog{found: false},
	}
}

func testPolicy() *trainingv1alpha1.CheckpointPriorityPolicy {
	policy := &trainingv1alpha1.CheckpointPriorityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-policy",
		},
		Spec: trainingv1alpha1.CheckpointPriorityPolicySpec{
			CheckpointFreshnessTarget: metav1.Duration{Duration: 10 * time.Minute},
			StartupProtectionWindow:   metav1.Duration{Duration: 5 * time.Minute},
			MinRuntimeBetweenYields:   metav1.Duration{Duration: 3 * time.Minute},
			MaxYieldsPerWindow:        3,
			YieldWindow:               &metav1.Duration{Duration: 30 * time.Minute},
		},
	}
	policy.Default()
	return policy
}

func testPriorityClass(name string, value int32) *kueuev1beta2.WorkloadPriorityClass {
	return &kueuev1beta2.WorkloadPriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Value: value,
	}
}

func testWorkload(job *trainingv1alpha1.ResumableTrainingJob, priority int32) *kueuev1beta2.Workload {
	return &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtjkueue.WorkloadNameForObject(job),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: trainingv1alpha1.GroupVersion.String(),
				Kind:       "ResumableTrainingJob",
				Name:       job.Name,
				UID:        job.UID,
				Controller: ptr.To(true),
			}},
		},
		Spec: kueuev1beta2.WorkloadSpec{
			QueueName: kueuev1beta2.LocalQueueName(job.Spec.QueueName),
			Priority:  ptr.To(priority),
		},
	}
}

func rtjForPriorityTest() *trainingv1alpha1.ResumableTrainingJob {
	rtj := rtjWithPolicy()
	rtj.UID = types.UID("test-rtj-uid-001")
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      rtjkueue.WorkloadNameForObject(rtj),
		Namespace: "default",
	}
	return rtj
}

// --- Tests: No policy attached (Phase 4 backward compat) ---

func TestReconcilePriorityState_NoPolicyNoOp(t *testing.T) {
	rtj := baseRTJ()
	rtj.UID = types.UID("test-uid")
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning

	rec := testReconciler(t, rtj)
	now := timeAt(15)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.StatusChanged {
		t.Error("expected no status change when no policy is attached")
	}
	if result.WorkloadPatched {
		t.Error("expected no workload patch when no policy is attached")
	}
	if result.Decision != nil {
		t.Error("expected nil decision when no policy is attached")
	}
	if rtj.Status.PriorityShaping != nil {
		t.Error("expected nil PriorityShaping when no policy is attached")
	}
}

func TestReconcilePriorityState_NoPolicyClearsStaleStatus(t *testing.T) {
	rtj := baseRTJ()
	rtj.UID = types.UID("test-uid")
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	// Simulate stale priority shaping from a previously attached policy.
	rtj.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{
		BasePriority:      100,
		EffectivePriority: 90,
		PreemptionState:   trainingv1alpha1.PreemptionStateActive,
	}
	rtj.Annotations = map[string]string{
		effectivePriorityAnnotation: "90",
		preemptionStateAnnotation:   "Active",
	}

	rec := testReconciler(t, rtj)
	now := timeAt(15)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if !result.StatusChanged {
		t.Error("expected status change when clearing stale priority shaping")
	}
	if rtj.Status.PriorityShaping != nil {
		t.Error("expected PriorityShaping to be cleared")
	}
	if !result.AnnotationsChanged {
		t.Error("expected annotations to be cleared")
	}
	if _, ok := rtj.Annotations[effectivePriorityAnnotation]; ok {
		t.Error("expected effective-priority annotation to be removed")
	}
}

// --- Tests: Effective priority changes with telemetry/policy state ---

func TestReconcilePriorityState_StartupProtected(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(10)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	// Now is within the 5-minute protection window (10 + 2 = 12).
	now := timeAt(12)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.Decision.State != checkpointpriority.DecisionStartupProtected {
		t.Errorf("expected StartupProtected state, got %s", result.Decision.State)
	}
	if result.Decision.PreemptionState != trainingv1alpha1.PreemptionStateProtected {
		t.Errorf("expected Protected preemption state, got %s", result.Decision.PreemptionState)
	}
	if rtj.Status.PriorityShaping == nil {
		t.Fatal("expected PriorityShaping to be set")
	}
	if rtj.Status.PriorityShaping.PreemptionState != trainingv1alpha1.PreemptionStateProtected {
		t.Errorf("expected status preemption state Protected, got %s", rtj.Status.PriorityShaping.PreemptionState)
	}
	if rtj.Status.PriorityShaping.BasePriority != 100 {
		t.Errorf("expected BasePriority=100, got %d", rtj.Status.PriorityShaping.BasePriority)
	}
}

func TestReconcilePriorityState_CheckpointFresh(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	runningAt := timeAt(1)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &runningAt

	// Checkpoint completed at minute 12, within 10-min freshness target from now=14.
	ckptTime := timeAt(12)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(14)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.Decision.State != checkpointpriority.DecisionActive {
		t.Errorf("expected Active state, got %s", result.Decision.State)
	}
	if result.Decision.EffectivePriority != 100 {
		t.Errorf("expected effective priority 100, got %d", result.Decision.EffectivePriority)
	}
}

func TestReconcilePriorityState_CheckpointStale(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	runningAt := timeAt(1)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &runningAt

	// Checkpoint completed at minute 2, stale by minute 20 (18 min > 10 min target).
	ckptTime := timeAt(2)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	// Set a negative preemptible offset.
	policy.Spec.PreemptibleOffset = ptr.To[int32](-50)
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(20)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.Decision.State != checkpointpriority.DecisionCheckpointStale {
		t.Errorf("expected CheckpointStale state, got %s", result.Decision.State)
	}
	if result.Decision.EffectivePriority != 50 {
		t.Errorf("expected effective priority 50 (100 + -50), got %d", result.Decision.EffectivePriority)
	}
	if result.Decision.PreemptionState != trainingv1alpha1.PreemptionStatePreemptible {
		t.Errorf("expected Preemptible state, got %s", result.Decision.PreemptionState)
	}
}

func TestReconcilePriorityState_EffectivePriorityChangesWorkload(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	runningAt := timeAt(1)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &runningAt

	// Stale checkpoint → preemptible with offset.
	ckptTime := timeAt(2)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	policy.Spec.PreemptibleOffset = ptr.To[int32](-30)
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100) // Workload currently has base priority.

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(20)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if !result.WorkloadPatched {
		t.Error("expected workload to be patched")
	}

	// Verify the workload was actually updated.
	var updated kueuev1beta2.Workload
	key := types.NamespacedName{
		Name:      rtjkueue.WorkloadNameForObject(rtj),
		Namespace: rtj.Namespace,
	}
	if err := rec.Get(context.Background(), key, &updated); err != nil {
		t.Fatalf("get updated workload: %v", err)
	}
	if got := ptr.Deref(updated.Spec.Priority, 0); got != 70 {
		t.Errorf("expected workload priority 70, got %d", got)
	}
}

// --- Tests: No infinite reconcile loop (idempotent) ---

func TestReconcilePriorityState_IdempotentNoStatusChurn(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	runningAt := timeAt(1)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &runningAt

	ckptTime := timeAt(12)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(14) // Active state.

	// First reconcile: sets up status.
	result1 := rec.reconcilePriorityState(context.Background(), rtj, now)
	if !result1.StatusChanged {
		t.Error("expected first reconcile to change status")
	}

	// Second reconcile with same inputs: should be idempotent.
	result2 := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result2.StatusChanged {
		t.Error("expected second reconcile to be idempotent (no status change)")
	}
	if result2.WorkloadPatched {
		t.Error("expected second reconcile to skip workload patch (already correct)")
	}
}

func TestReconcilePriorityState_NoClobberingOnRestart(t *testing.T) {
	// Simulate an operator restart where RTJ already has priority shaping
	// status from a previous operator instance. The new reconcile should
	// produce the same decision and not clobber values.
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	runningAt := timeAt(1)
	ckptTime := timeAt(12)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &runningAt
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}
	// Pre-existing status from previous operator.
	rtj.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{
		BasePriority:                100,
		EffectivePriority:           100,
		PreemptionState:             trainingv1alpha1.PreemptionStateActive,
		PreemptionStateReason:       "CheckpointFresh",
		LastCompletedCheckpointTime: &ckptTime,
		CheckpointAge:               "2m0s",
		AppliedPolicyRef:            "test-policy",
	}
	rtj.Annotations = map[string]string{
		effectivePriorityAnnotation: "100",
		preemptionStateAnnotation:   "Active",
	}

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(14)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	// The only change should be checkpoint age recomputation.
	if result.WorkloadPatched {
		t.Error("expected no workload patch on restart with same priority")
	}
	// Decision should still be Active.
	if result.Decision == nil || result.Decision.State != checkpointpriority.DecisionActive {
		t.Errorf("expected Active state after restart, got %v", result.Decision)
	}
	// Base priority should remain 100.
	if rtj.Status.PriorityShaping.BasePriority != 100 {
		t.Errorf("expected BasePriority=100, got %d", rtj.Status.PriorityShaping.BasePriority)
	}
}

// --- Tests: Workload priority consistency across reconciles ---

func TestReconcilePriorityState_TransitionProtectedToStale(t *testing.T) {
	// Simulate a job that transitions from Protected → Active → Stale
	// across multiple reconcile cycles. Verify workload priority changes
	// at each transition point.
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	policy.Spec.ProtectedBoost = ptr.To[int32](10)
	policy.Spec.PreemptibleOffset = ptr.To[int32](-20)
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)

	// Phase 1: Protected (within 5-min protection window).
	now1 := timeAt(2)
	result1 := rec.reconcilePriorityState(context.Background(), rtj, now1)
	if result1.Decision.State != checkpointpriority.DecisionStartupProtected {
		t.Fatalf("expected StartupProtected, got %s", result1.Decision.State)
	}
	if result1.Decision.EffectivePriority != 110 {
		t.Fatalf("expected effective priority 110 (100+10), got %d", result1.Decision.EffectivePriority)
	}

	// Phase 2: Active (protection expired, fresh checkpoint at minute 8).
	ckptTime := timeAt(8)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}
	now2 := timeAt(10)
	result2 := rec.reconcilePriorityState(context.Background(), rtj, now2)
	if result2.Decision.State != checkpointpriority.DecisionActive {
		t.Fatalf("expected Active, got %s", result2.Decision.State)
	}
	if result2.Decision.EffectivePriority != 100 {
		t.Fatalf("expected effective priority 100, got %d", result2.Decision.EffectivePriority)
	}

	// Phase 3: Stale (checkpoint at minute 8, now at minute 25, age=17m > 10m target).
	now3 := timeAt(25)
	result3 := rec.reconcilePriorityState(context.Background(), rtj, now3)
	if result3.Decision.State != checkpointpriority.DecisionCheckpointStale {
		t.Fatalf("expected CheckpointStale, got %s", result3.Decision.State)
	}
	if result3.Decision.EffectivePriority != 80 {
		t.Fatalf("expected effective priority 80 (100-20), got %d", result3.Decision.EffectivePriority)
	}
}

// --- Tests: Missing Workload is handled gracefully ---

func TestReconcilePriorityState_NoWorkloadYet(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	// No workload created yet.

	rec := testReconciler(t, rtj, policy, wpc)
	now := timeAt(2)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	// Should still compute decision and update status.
	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.WorkloadPatched {
		t.Error("expected no workload patch when workload doesn't exist")
	}
	// Status should still be updated.
	if !result.StatusChanged {
		t.Error("expected status change for priority shaping")
	}
}

// --- Tests: Policy resolution failure ---

func TestReconcilePriorityState_PolicyNotFound(t *testing.T) {
	rtj := rtjForPriorityTest()
	// Policy "test-policy" is referenced but not created.
	wpc := testPriorityClass("default-priority", 100)

	rec := testReconciler(t, rtj, wpc)
	now := timeAt(10)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision != nil {
		t.Error("expected nil decision on policy resolution failure")
	}
	if !result.StatusChanged {
		t.Error("expected status change for error condition")
	}
	// Should set a failure condition.
	found := false
	for _, c := range rtj.Status.Conditions {
		if c.Type == conditionTypePriorityShaping && c.Status == metav1.ConditionFalse {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected PriorityShaping condition to be False on policy not found")
	}
}

func TestReconcilePriorityState_PriorityClassNotFound(t *testing.T) {
	rtj := rtjForPriorityTest()
	policy := testPolicy()
	// WorkloadPriorityClass "default-priority" is referenced but not created.

	rec := testReconciler(t, rtj, policy)
	now := timeAt(10)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision != nil {
		t.Error("expected nil decision on base priority resolution failure")
	}
	if !result.StatusChanged {
		t.Error("expected status change for error condition")
	}
}

// --- Tests: Annotations and conditions ---

func TestReconcilePriorityState_SetsAnnotations(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(10)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(12) // Within protection window.

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if !result.AnnotationsChanged {
		t.Error("expected annotations to be set")
	}
	if rtj.Annotations[effectivePriorityAnnotation] != "100" {
		t.Errorf("expected effective-priority annotation=100, got %s",
			rtj.Annotations[effectivePriorityAnnotation])
	}
	if rtj.Annotations[preemptionStateAnnotation] != "Protected" {
		t.Errorf("expected preemption-state annotation=Protected, got %s",
			rtj.Annotations[preemptionStateAnnotation])
	}
}

func TestReconcilePriorityState_SetsCondition(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(10)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(12)

	rec.reconcilePriorityState(context.Background(), rtj, now)

	found := false
	for _, c := range rtj.Status.Conditions {
		if c.Type == conditionTypePriorityShaping {
			found = true
			if c.Status != metav1.ConditionTrue {
				t.Errorf("expected condition status True, got %s", c.Status)
			}
			break
		}
	}
	if !found {
		t.Error("expected PriorityShaping condition to be set")
	}
}

// --- Tests: RequeueAfter for protection window ---

func TestReconcilePriorityState_RequeueAfterProtectionExpiry(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(10)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(12) // 2 min into 5-min protection window → 3 min remaining.

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.RequeueAfter == 0 {
		t.Error("expected non-zero requeue interval during protection window")
	}
	// Should requeue ~3 minutes + 1 second.
	expected := 3*time.Minute + time.Second
	if result.RequeueAfter != expected {
		t.Errorf("expected requeue after %v, got %v", expected, result.RequeueAfter)
	}
}

func TestReconcilePriorityState_NoRequeueWhenActive(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	ckptTime := timeAt(12)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(14) // Active state, no protection window.

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue when active, got %v", result.RequeueAfter)
	}
}

// --- Tests: buildEvaluationInput ---

func TestBuildEvaluationInput_AllFieldsPopulated(t *testing.T) {
	ckptTime := timeAt(5)
	startTime := timeAt(1)
	resumeTime := timeAt(3)
	yieldTime := timeAt(2)
	snap := TelemetrySnapshot{
		LastCompletedCheckpointTime: &ckptTime,
		LastRunStartTime:            &startTime,
		LastResumeTime:              &resumeTime,
		LastYieldTime:               &yieldTime,
		RecentYieldCount:            2,
	}
	now := timeAt(10)

	input := buildEvaluationInput(100, snap, now)

	if input.BasePriority != 100 {
		t.Errorf("expected BasePriority=100, got %d", input.BasePriority)
	}
	if !input.Now.Equal(now.Time) {
		t.Errorf("expected Now=%v, got %v", now.Time, input.Now)
	}
	if input.LastCompletedCheckpointTime == nil || !input.LastCompletedCheckpointTime.Equal(ckptTime.Time) {
		t.Error("expected LastCompletedCheckpointTime to be set")
	}
	if input.RunStartTime == nil || !input.RunStartTime.Equal(startTime.Time) {
		t.Error("expected RunStartTime to be set")
	}
	if input.LastResumeTime == nil || !input.LastResumeTime.Equal(resumeTime.Time) {
		t.Error("expected LastResumeTime to be set")
	}
	if input.LastYieldTime == nil || !input.LastYieldTime.Equal(yieldTime.Time) {
		t.Error("expected LastYieldTime to be set")
	}
	if input.RecentYieldCount != 2 {
		t.Errorf("expected RecentYieldCount=2, got %d", input.RecentYieldCount)
	}
}

func TestBuildEvaluationInput_NilFields(t *testing.T) {
	snap := TelemetrySnapshot{}
	now := timeAt(10)

	input := buildEvaluationInput(50, snap, now)

	if input.BasePriority != 50 {
		t.Errorf("expected BasePriority=50, got %d", input.BasePriority)
	}
	if input.LastCompletedCheckpointTime != nil {
		t.Error("expected nil LastCompletedCheckpointTime")
	}
	if input.RunStartTime != nil {
		t.Error("expected nil RunStartTime")
	}
	if input.LastResumeTime != nil {
		t.Error("expected nil LastResumeTime")
	}
	if input.LastYieldTime != nil {
		t.Error("expected nil LastYieldTime")
	}
}

// --- Tests: syncDecisionToStatus ---

func TestSyncDecisionToStatus_InitializesFromDecision(t *testing.T) {
	rtj := rtjWithPolicy()
	protectedUntil := timeAt(15).Time
	decision := &checkpointpriority.Decision{
		State:             checkpointpriority.DecisionStartupProtected,
		PreemptionState:   trainingv1alpha1.PreemptionStateProtected,
		EffectivePriority: 110,
		Reason:            "WithinProtectionWindow",
		ProtectedUntil:    &protectedUntil,
	}
	now := timeAt(10)

	changed := syncDecisionToStatus(rtj, 100, decision, now)

	if !changed {
		t.Error("expected change on first sync")
	}
	ps := rtj.Status.PriorityShaping
	if ps == nil {
		t.Fatal("expected PriorityShaping to be initialized")
	}
	if ps.BasePriority != 100 {
		t.Errorf("expected BasePriority=100, got %d", ps.BasePriority)
	}
	if ps.EffectivePriority != 110 {
		t.Errorf("expected EffectivePriority=110, got %d", ps.EffectivePriority)
	}
	if ps.PreemptionState != trainingv1alpha1.PreemptionStateProtected {
		t.Errorf("expected PreemptionState=Protected, got %s", ps.PreemptionState)
	}
	if ps.PreemptionStateReason != "WithinProtectionWindow" {
		t.Errorf("expected reason WithinProtectionWindow, got %s", ps.PreemptionStateReason)
	}
	if ps.ProtectedUntil == nil || !ps.ProtectedUntil.Time.Equal(protectedUntil) {
		t.Error("expected ProtectedUntil to be set")
	}
}

func TestSyncDecisionToStatus_IdempotentWhenUnchanged(t *testing.T) {
	rtj := rtjWithPolicy()
	rtj.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{
		BasePriority:         100,
		EffectivePriority:    100,
		PreemptionState:      trainingv1alpha1.PreemptionStateActive,
		PreemptionStateReason: "CheckpointFresh",
	}

	decision := &checkpointpriority.Decision{
		State:             checkpointpriority.DecisionActive,
		PreemptionState:   trainingv1alpha1.PreemptionStateActive,
		EffectivePriority: 100,
		Reason:            "CheckpointFresh",
	}
	now := timeAt(15)

	changed := syncDecisionToStatus(rtj, 100, decision, now)

	if changed {
		t.Error("expected no change when decision matches existing status")
	}
}

// --- Tests: isActivePriorityPhase ---

func TestIsActivePriorityPhase(t *testing.T) {
	tests := []struct {
		phase trainingv1alpha1.ResumableTrainingJobPhase
		want  bool
	}{
		{trainingv1alpha1.PhaseStarting, true},
		{trainingv1alpha1.PhaseRunning, true},
		{trainingv1alpha1.PhaseRestoring, true},
		{trainingv1alpha1.PhaseYieldRequested, true},
		{trainingv1alpha1.PhaseDraining, true},
		{trainingv1alpha1.PhasePending, false},
		{trainingv1alpha1.PhaseQueued, false},
		{trainingv1alpha1.PhaseAdmitted, false},
		{trainingv1alpha1.PhasePaused, false},
		{trainingv1alpha1.PhaseSucceeded, false},
		{trainingv1alpha1.PhaseFailed, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			if got := isActivePriorityPhase(tt.phase); got != tt.want {
				t.Errorf("isActivePriorityPhase(%s) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}

// --- Tests: syncPriorityAnnotations ---

func TestSyncPriorityAnnotations_SetsAnnotations(t *testing.T) {
	rtj := rtjWithPolicy()
	decision := &checkpointpriority.Decision{
		EffectivePriority: 90,
		PreemptionState:   trainingv1alpha1.PreemptionStatePreemptible,
	}

	changed := syncPriorityAnnotations(rtj, decision)

	if !changed {
		t.Error("expected change when setting new annotations")
	}
	if rtj.Annotations[effectivePriorityAnnotation] != "90" {
		t.Errorf("expected effective-priority=90, got %s", rtj.Annotations[effectivePriorityAnnotation])
	}
	if rtj.Annotations[preemptionStateAnnotation] != "Preemptible" {
		t.Errorf("expected preemption-state=Preemptible, got %s", rtj.Annotations[preemptionStateAnnotation])
	}
}

func TestSyncPriorityAnnotations_Idempotent(t *testing.T) {
	rtj := rtjWithPolicy()
	rtj.Annotations = map[string]string{
		effectivePriorityAnnotation: "90",
		preemptionStateAnnotation:   "Preemptible",
	}
	decision := &checkpointpriority.Decision{
		EffectivePriority: 90,
		PreemptionState:   trainingv1alpha1.PreemptionStatePreemptible,
	}

	changed := syncPriorityAnnotations(rtj, decision)

	if changed {
		t.Error("expected no change when annotations already match")
	}
}

// --- Tests: clearPriorityAnnotations ---

func TestClearPriorityAnnotations_RemovesAnnotations(t *testing.T) {
	rtj := rtjWithPolicy()
	rtj.Annotations = map[string]string{
		effectivePriorityAnnotation: "90",
		preemptionStateAnnotation:   "Active",
		"other-annotation":          "keep-me",
	}

	changed := clearPriorityAnnotations(rtj)

	if !changed {
		t.Error("expected change when removing annotations")
	}
	if _, ok := rtj.Annotations[effectivePriorityAnnotation]; ok {
		t.Error("expected effective-priority annotation to be removed")
	}
	if _, ok := rtj.Annotations[preemptionStateAnnotation]; ok {
		t.Error("expected preemption-state annotation to be removed")
	}
	if rtj.Annotations["other-annotation"] != "keep-me" {
		t.Error("expected other annotation to be preserved")
	}
}

func TestClearPriorityAnnotations_NoOpWhenAbsent(t *testing.T) {
	rtj := rtjWithPolicy()
	rtj.Annotations = map[string]string{
		"other": "value",
	}

	changed := clearPriorityAnnotations(rtj)

	if changed {
		t.Error("expected no change when annotations are absent")
	}
}

// --- Tests: patchWorkloadPriority ---

func TestPatchWorkloadPriority_PatchesWhenDifferent(t *testing.T) {
	rtj := rtjForPriorityTest()
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, workload)

	patched, err := rec.patchWorkloadPriority(context.Background(), rtj, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patched {
		t.Error("expected patch to be applied")
	}

	var updated kueuev1beta2.Workload
	key := types.NamespacedName{
		Name:      rtjkueue.WorkloadNameForObject(rtj),
		Namespace: rtj.Namespace,
	}
	if err := rec.Get(context.Background(), key, &updated); err != nil {
		t.Fatalf("get updated workload: %v", err)
	}
	if got := ptr.Deref(updated.Spec.Priority, 0); got != 80 {
		t.Errorf("expected priority 80, got %d", got)
	}
}

func TestPatchWorkloadPriority_SkipsWhenEqual(t *testing.T) {
	rtj := rtjForPriorityTest()
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, workload)

	patched, err := rec.patchWorkloadPriority(context.Background(), rtj, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patched {
		t.Error("expected no patch when priority matches")
	}
}

func TestPatchWorkloadPriority_HandlesNotFound(t *testing.T) {
	rtj := rtjForPriorityTest()
	// No workload exists.

	rec := testReconciler(t, rtj)

	patched, err := rec.patchWorkloadPriority(context.Background(), rtj, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patched {
		t.Error("expected no patch when workload doesn't exist")
	}
}

func TestPatchWorkloadPriority_EmptyWorkloadName(t *testing.T) {
	rtj := rtjForPriorityTest()
	rtj.UID = "" // Will produce empty workload name.
	rtj.Name = ""

	rec := testReconciler(t, rtj)

	patched, err := rec.patchWorkloadPriority(context.Background(), rtj, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patched {
		t.Error("expected no patch when workload name is empty")
	}
}

// --- Tests: Cooldown state ---

func TestReconcilePriorityState_CooldownAfterResume(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	resumeTime := timeAt(10) // Resumed 2 min ago (at minute 10, now=12).
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &resumeTime
	rtj.Status.TransitionTimestamps.RestoringAt = &startingAt
	rtj.Status.TransitionTimestamps.RestoreCompletedAt = &resumeTime

	// Fresh checkpoint (so we'd be Active if not in cooldown).
	ckptTime := timeAt(11)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	policy.Spec.CooldownBoost = ptr.To[int32](15)
	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(12) // 2 min since resume, cooldown is 3 min.

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	// Note: protection window is checked first. Since startingAt=0 and protection=5min,
	// at time 12 the protection window (anchor=max(0,10)=10, expires at 15) is still active.
	// So we get Protected, not Cooldown. Let's adjust the test.
	// Protection window anchor = max(startingAt=0, resumeTime=10) = 10.
	// Protection expires at 10 + 5 = 15. Now=12 < 15 → Protected.
	if result.Decision.State != checkpointpriority.DecisionStartupProtected {
		t.Errorf("expected StartupProtected (protection window active after resume), got %s", result.Decision.State)
	}
}

func TestReconcilePriorityState_CooldownWhenProtectionExpired(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	resumeTime := timeAt(10)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &resumeTime
	rtj.Status.TransitionTimestamps.RestoringAt = &startingAt
	rtj.Status.TransitionTimestamps.RestoreCompletedAt = &resumeTime

	ckptTime := timeAt(14)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	// Protection window: 5 min from resume at 10 → expires at 15.
	// Cooldown: 3 min from resume at 10 → expires at 13.
	// Now=16: protection expired, cooldown expired. Should be Active.
	// To test Cooldown, set protection to 1 min (expires at 11), cooldown to 8 min (expires at 18).
	policy.Spec.StartupProtectionWindow = metav1.Duration{Duration: 1 * time.Minute}
	policy.Spec.MinRuntimeBetweenYields = metav1.Duration{Duration: 8 * time.Minute}
	policy.Spec.CooldownBoost = ptr.To[int32](20)

	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(16) // Protection expired (at 11), cooldown active (until 18).

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.Decision.State != checkpointpriority.DecisionCoolingDown {
		t.Errorf("expected CoolingDown, got %s", result.Decision.State)
	}
	if result.Decision.EffectivePriority != 120 {
		t.Errorf("expected effective priority 120 (100+20), got %d", result.Decision.EffectivePriority)
	}
}

// --- Tests: TelemetryUnknown fail-open ---

func TestReconcilePriorityState_TelemetryUnknownFailOpen(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt
	// No checkpoint data at all.

	policy := testPolicy()
	policy.Spec.StartupProtectionWindow = metav1.Duration{Duration: 0} // No protection.
	// FailOpenOnTelemetryLoss defaults to true.

	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(20)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.Decision.State != checkpointpriority.DecisionTelemetryUnknown {
		t.Errorf("expected TelemetryUnknown, got %s", result.Decision.State)
	}
	// Fail-open: keep base priority.
	if result.Decision.EffectivePriority != 100 {
		t.Errorf("expected effective priority 100 (fail-open), got %d", result.Decision.EffectivePriority)
	}
	if result.Decision.PreemptionState != trainingv1alpha1.PreemptionStateActive {
		t.Errorf("expected Active (fail-open), got %s", result.Decision.PreemptionState)
	}
}

// --- Tests: YieldBudgetExhausted ---

func TestReconcilePriorityState_YieldBudgetExhausted(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	// Record 3 yields (matching maxYieldsPerWindow=3).
	rtj.Annotations = map[string]string{
		yieldHistoryAnnotation: marshalYieldHistory([]time.Time{
			timeAt(5).Time,
			timeAt(10).Time,
			timeAt(15).Time,
		}),
	}

	policy := testPolicy()
	policy.Spec.StartupProtectionWindow = metav1.Duration{Duration: 0} // No protection.
	policy.Spec.CooldownBoost = ptr.To[int32](25)

	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(20)

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	if result.Decision == nil {
		t.Fatal("expected non-nil decision")
	}
	if result.Decision.State != checkpointpriority.DecisionYieldBudgetExhausted {
		t.Errorf("expected YieldBudgetExhausted, got %s", result.Decision.State)
	}
	if result.Decision.EffectivePriority != 125 {
		t.Errorf("expected effective priority 125 (100+25), got %d", result.Decision.EffectivePriority)
	}
	if result.Decision.PreemptionState != trainingv1alpha1.PreemptionStateCooldown {
		t.Errorf("expected Cooldown state, got %s", result.Decision.PreemptionState)
	}
}

// --- Tests: Clamping ---

func TestReconcilePriorityState_EffectivePriorityClampedToMax(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(10)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	policy := testPolicy()
	policy.Spec.ProtectedBoost = ptr.To[int32](50)
	policy.Spec.MaxEffectivePriority = ptr.To[int32](120)

	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(12) // Within protection window.

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	// 100 + 50 = 150, clamped to 120.
	if result.Decision.EffectivePriority != 120 {
		t.Errorf("expected clamped priority 120, got %d", result.Decision.EffectivePriority)
	}
}

func TestReconcilePriorityState_EffectivePriorityClampedToMin(t *testing.T) {
	rtj := rtjForPriorityTest()
	startingAt := timeAt(0)
	rtj.Status.TransitionTimestamps.StartingAt = &startingAt
	rtj.Status.TransitionTimestamps.RunningAt = &startingAt

	ckptTime := timeAt(2)
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-001",
		StorageURI:     "s3://test-bucket/checkpoints",
		CompletionTime: &ckptTime,
	}

	policy := testPolicy()
	policy.Spec.StartupProtectionWindow = metav1.Duration{Duration: 0}
	policy.Spec.PreemptibleOffset = ptr.To[int32](-80)
	policy.Spec.MinEffectivePriority = ptr.To[int32](50)

	wpc := testPriorityClass("default-priority", 100)
	workload := testWorkload(rtj, 100)

	rec := testReconciler(t, rtj, policy, wpc, workload)
	now := timeAt(20) // Stale checkpoint.

	result := rec.reconcilePriorityState(context.Background(), rtj, now)

	// 100 + (-80) = 20, clamped to 50.
	if result.Decision.EffectivePriority != 50 {
		t.Errorf("expected clamped priority 50, got %d", result.Decision.EffectivePriority)
	}
}

// --- Helpers (ensure no import cycle) ---

func init() {
	// Verify the test helpers compile without import cycles.
	_ = checkpointpriority.DecisionActive
	_ = checkpoints.CheckpointInfo{}
	_ = rtjkueue.WorkloadNameForObject
	_ = apierrors.IsNotFound
	_ = fmt.Sprintf
}
