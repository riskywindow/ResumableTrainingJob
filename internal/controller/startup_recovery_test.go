package controller

import (
	"context"
	"testing"
	"time"

	metav1api "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// ====================================================================
// Unit tests: ClassifyEviction
// ====================================================================

func TestClassifyEvictionNilWorkload(t *testing.T) {
	ec := ClassifyEviction(nil)
	if ec.Evicted {
		t.Fatal("expected no eviction for nil workload")
	}
}

func TestClassifyEvictionNoEvictionCondition(t *testing.T) {
	workload := &kueuev1beta2.Workload{
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{Type: "Admitted", Status: metav1.ConditionTrue, Reason: "QuotaReserved"},
			},
		},
	}
	ec := ClassifyEviction(workload)
	if ec.Evicted {
		t.Fatal("expected no eviction when no Evicted condition present")
	}
}

func TestClassifyEvictionPodsReadyTimeout(t *testing.T) {
	workload := &kueuev1beta2.Workload{
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPodsReadyTimeout,
					Message: "Pods did not reach Ready within 300s",
				},
			},
		},
	}
	ec := ClassifyEviction(workload)
	if !ec.Evicted {
		t.Fatal("expected eviction to be detected")
	}
	if !ec.IsPodsReadyTimeout {
		t.Fatal("expected IsPodsReadyTimeout to be true")
	}
	if ec.IsPreemption {
		t.Fatal("expected IsPreemption to be false")
	}
	if ec.Reason != kueueEvictionReasonPodsReadyTimeout {
		t.Fatalf("expected reason %q, got %q", kueueEvictionReasonPodsReadyTimeout, ec.Reason)
	}
	if ec.Message != "Pods did not reach Ready within 300s" {
		t.Fatalf("expected message preserved, got %q", ec.Message)
	}
}

func TestClassifyEvictionPreempted(t *testing.T) {
	workload := &kueuev1beta2.Workload{
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPreempted,
					Message: "Preempted by higher-priority workload",
				},
			},
		},
	}
	ec := ClassifyEviction(workload)
	if !ec.Evicted {
		t.Fatal("expected eviction to be detected")
	}
	if ec.IsPodsReadyTimeout {
		t.Fatal("expected IsPodsReadyTimeout to be false for preemption")
	}
	if !ec.IsPreemption {
		t.Fatal("expected IsPreemption to be true")
	}
}

func TestClassifyEvictionInactiveWorkload(t *testing.T) {
	workload := &kueuev1beta2.Workload{
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonInactiveWorkload,
					Message: "Workload deactivated",
				},
			},
		},
	}
	ec := ClassifyEviction(workload)
	if !ec.Evicted {
		t.Fatal("expected eviction to be detected")
	}
	if ec.IsPodsReadyTimeout {
		t.Fatal("expected IsPodsReadyTimeout to be false for inactive")
	}
	if ec.IsPreemption {
		t.Fatal("expected IsPreemption to be false for inactive")
	}
}

func TestClassifyEvictionConditionFalseIsNotEvicted(t *testing.T) {
	workload := &kueuev1beta2.Workload{
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:   kueueConditionEvicted,
					Status: metav1.ConditionFalse,
					Reason: kueueEvictionReasonPodsReadyTimeout,
				},
			},
		},
	}
	ec := ClassifyEviction(workload)
	if ec.Evicted {
		t.Fatal("expected no eviction when condition is False")
	}
}

// ====================================================================
// Unit tests: ClassifyStartupState
// ====================================================================

func TestClassifyStartupStateStartupTimeout(t *testing.T) {
	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		IsPodsReadyTimeout: true,
	}
	state := ClassifyStartupState(trainingv1alpha1.PhaseStarting, false, eviction)
	if state != trainingv1alpha1.StartupTimedOut {
		t.Fatalf("expected StartupTimedOut, got %q", state)
	}
}

func TestClassifyStartupStateRecoveryTimeout(t *testing.T) {
	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		IsPodsReadyTimeout: true,
	}
	state := ClassifyStartupState(trainingv1alpha1.PhaseRunning, true, eviction)
	if state != trainingv1alpha1.StartupRecoveryTimedOut {
		t.Fatalf("expected RecoveryTimedOut, got %q", state)
	}
}

func TestClassifyStartupStatePreemptionEvicted(t *testing.T) {
	eviction := EvictionClassification{
		Evicted:      true,
		Reason:       kueueEvictionReasonPreempted,
		IsPreemption: true,
	}
	state := ClassifyStartupState(trainingv1alpha1.PhaseRunning, true, eviction)
	if state != trainingv1alpha1.StartupEvicted {
		t.Fatalf("expected Evicted for preemption, got %q", state)
	}
}

func TestClassifyStartupStateNormalStarting(t *testing.T) {
	state := ClassifyStartupState(trainingv1alpha1.PhaseStarting, false, EvictionClassification{})
	if state != trainingv1alpha1.StartupStarting {
		t.Fatalf("expected Starting, got %q", state)
	}
}

func TestClassifyStartupStateNormalRunning(t *testing.T) {
	state := ClassifyStartupState(trainingv1alpha1.PhaseRunning, true, EvictionClassification{})
	if state != trainingv1alpha1.StartupRunning {
		t.Fatalf("expected Running, got %q", state)
	}
}

func TestClassifyStartupStateRestoring(t *testing.T) {
	state := ClassifyStartupState(trainingv1alpha1.PhaseRestoring, false, EvictionClassification{})
	if state != trainingv1alpha1.StartupStarting {
		t.Fatalf("expected Starting for Restoring phase, got %q", state)
	}
}

func TestClassifyStartupStateNotStarted(t *testing.T) {
	state := ClassifyStartupState(trainingv1alpha1.PhasePending, false, EvictionClassification{})
	if state != trainingv1alpha1.StartupNotStarted {
		t.Fatalf("expected NotStarted, got %q", state)
	}
}

// ====================================================================
// Unit tests: syncStartupRecoveryStatus
// ====================================================================

func TestSyncStartupRecoveryOnLaunch(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))

	changed := syncStartupRecoveryOnLaunch(job, now)
	if !changed {
		t.Fatal("expected status change on first launch sync")
	}
	if job.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if job.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupStarting {
		t.Fatalf("expected Starting, got %q", job.Status.StartupRecovery.StartupState)
	}
	if job.Status.StartupRecovery.PodsReadyState != trainingv1alpha1.PodsNotReady {
		t.Fatalf("expected PodsNotReady, got %q", job.Status.StartupRecovery.PodsReadyState)
	}
	if job.Status.StartupRecovery.LastTransitionTime == nil {
		t.Fatal("expected LastTransitionTime to be set")
	}
}

func TestSyncStartupRecoveryOnRunning(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))

	// Start with launch sync.
	syncStartupRecoveryOnLaunch(job, now)

	runningTime := metav1.NewTime(now.Add(30 * time.Second))
	changed := syncStartupRecoveryOnRunning(job, runningTime)
	if !changed {
		t.Fatal("expected status change when transitioning to Running")
	}
	if job.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupRunning {
		t.Fatalf("expected Running, got %q", job.Status.StartupRecovery.StartupState)
	}
	if job.Status.StartupRecovery.PodsReadyState != trainingv1alpha1.PodsReady {
		t.Fatalf("expected PodsReady, got %q", job.Status.StartupRecovery.PodsReadyState)
	}
}

func TestSyncStartupRecoveryOnEvictionRecordsReason(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	job.Status.Phase = trainingv1alpha1.PhaseStarting
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))

	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		Message:            "Pods did not reach Ready within 300s",
		IsPodsReadyTimeout: true,
	}

	changed := syncStartupRecoveryOnEviction(job, eviction, false, now)
	if !changed {
		t.Fatal("expected status change on eviction")
	}
	sr := job.Status.StartupRecovery
	if sr == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if sr.StartupState != trainingv1alpha1.StartupTimedOut {
		t.Fatalf("expected StartupTimedOut, got %q", sr.StartupState)
	}
	if sr.LastEvictionReason != kueueEvictionReasonPodsReadyTimeout {
		t.Fatalf("expected eviction reason %q, got %q", kueueEvictionReasonPodsReadyTimeout, sr.LastEvictionReason)
	}
	if sr.LastRequeueReason != reasonRequeuedAfterEviction {
		t.Fatalf("expected requeue reason %q, got %q", reasonRequeuedAfterEviction, sr.LastRequeueReason)
	}
}

func TestSyncStartupRecoveryIdempotent(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))

	// First call: should change.
	changed1 := syncStartupRecoveryOnLaunch(job, now)
	if !changed1 {
		t.Fatal("expected change on first call")
	}

	// Second call with same inputs: should NOT change.
	changed2 := syncStartupRecoveryOnLaunch(job, now)
	if changed2 {
		t.Fatal("expected no change on second call with same inputs (idempotent)")
	}

	// Third call with same inputs after operator restart: still no change.
	changed3 := syncStartupRecoveryOnLaunch(job, now)
	if changed3 {
		t.Fatal("expected no change on third call (simulating operator restart)")
	}
}

func TestSyncStartupRecoveryEvictionIdempotent(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	job.Status.Phase = trainingv1alpha1.PhaseStarting
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))

	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		IsPodsReadyTimeout: true,
	}

	changed1 := syncStartupRecoveryOnEviction(job, eviction, false, now)
	if !changed1 {
		t.Fatal("expected change on first eviction sync")
	}

	changed2 := syncStartupRecoveryOnEviction(job, eviction, false, now)
	if changed2 {
		t.Fatal("expected no change on repeated eviction sync (idempotent)")
	}
}

// ====================================================================
// Unit tests: setStartupRecoveryConditions
// ====================================================================

func TestSetStartupRecoveryConditionsStartupTimeout(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
	}
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		Message:            "Pods did not reach Ready within 300s",
		IsPodsReadyTimeout: true,
	}

	changed := setStartupRecoveryConditions(job, eviction, false, now)
	if !changed {
		t.Fatal("expected conditions to change")
	}

	cond := metav1api.FindStatusCondition(job.Status.Conditions, conditionTypeStartupTimeoutEvicted)
	if cond == nil {
		t.Fatal("expected StartupTimeoutEvicted condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected condition True, got %s", cond.Status)
	}
	if cond.Reason != reasonPodsReadyTimeout {
		t.Fatalf("expected reason %q, got %q", reasonPodsReadyTimeout, cond.Reason)
	}

	// RecoveryTimeoutEvicted should NOT be set.
	recoveryCond := metav1api.FindStatusCondition(job.Status.Conditions, conditionTypeRecoveryTimeoutEvicted)
	if recoveryCond != nil {
		t.Fatal("expected RecoveryTimeoutEvicted condition to NOT be set for startup timeout")
	}
}

func TestSetStartupRecoveryConditionsRecoveryTimeout(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
	}
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		Message:            "Pods lost Ready state and did not recover within 300s",
		IsPodsReadyTimeout: true,
	}

	changed := setStartupRecoveryConditions(job, eviction, true, now)
	if !changed {
		t.Fatal("expected conditions to change")
	}

	cond := metav1api.FindStatusCondition(job.Status.Conditions, conditionTypeRecoveryTimeoutEvicted)
	if cond == nil {
		t.Fatal("expected RecoveryTimeoutEvicted condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected condition True, got %s", cond.Status)
	}

	// StartupTimeoutEvicted should NOT be set.
	startupCond := metav1api.FindStatusCondition(job.Status.Conditions, conditionTypeStartupTimeoutEvicted)
	if startupCond != nil {
		t.Fatal("expected StartupTimeoutEvicted condition to NOT be set for recovery timeout")
	}
}

func TestSetStartupRecoveryConditionsClearedOnNonTimeout(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
	}
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))

	// First set a timeout condition.
	eviction := EvictionClassification{
		Evicted:            true,
		Reason:             kueueEvictionReasonPodsReadyTimeout,
		Message:            "timeout",
		IsPodsReadyTimeout: true,
	}
	setStartupRecoveryConditions(job, eviction, false, now)

	// Now clear with a non-timeout eviction.
	noTimeout := EvictionClassification{
		Evicted:      true,
		Reason:       kueueEvictionReasonPreempted,
		IsPreemption: true,
	}
	changed := setStartupRecoveryConditions(job, noTimeout, false, now)
	if !changed {
		t.Fatal("expected conditions to change when clearing timeout")
	}
	if metav1api.FindStatusCondition(job.Status.Conditions, conditionTypeStartupTimeoutEvicted) != nil {
		t.Fatal("expected StartupTimeoutEvicted to be cleared")
	}
}

// ====================================================================
// Unit tests: wasPhaseRunning
// ====================================================================

func TestWasPhaseRunningFromPhase(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	job.Status.Phase = trainingv1alpha1.PhaseRunning
	if !wasPhaseRunning(job) {
		t.Fatal("expected wasPhaseRunning=true when phase is Running")
	}
}

func TestWasPhaseRunningFromStartupRecoveryState(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	job.Status.Phase = trainingv1alpha1.PhaseYieldRequested
	job.Status.StartupRecovery = &trainingv1alpha1.StartupRecoveryStatus{
		StartupState: trainingv1alpha1.StartupRunning,
	}
	if !wasPhaseRunning(job) {
		t.Fatal("expected wasPhaseRunning=true when StartupRecovery records Running")
	}
}

func TestWasPhaseRunningFalseWhenStarting(t *testing.T) {
	job := &trainingv1alpha1.ResumableTrainingJob{}
	job.Status.Phase = trainingv1alpha1.PhaseStarting
	if wasPhaseRunning(job) {
		t.Fatal("expected wasPhaseRunning=false when phase is Starting")
	}
}

// ====================================================================
// Unit tests: startupRecoveryStatusEqual
// ====================================================================

func TestStartupRecoveryStatusEqualBothNil(t *testing.T) {
	if !startupRecoveryStatusEqual(nil, nil) {
		t.Fatal("expected nil == nil")
	}
}

func TestStartupRecoveryStatusEqualOneNil(t *testing.T) {
	sr := &trainingv1alpha1.StartupRecoveryStatus{StartupState: trainingv1alpha1.StartupStarting}
	if startupRecoveryStatusEqual(sr, nil) {
		t.Fatal("expected non-nil != nil")
	}
	if startupRecoveryStatusEqual(nil, sr) {
		t.Fatal("expected nil != non-nil")
	}
}

func TestStartupRecoveryStatusEqualSameValues(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	left := &trainingv1alpha1.StartupRecoveryStatus{
		StartupState:       trainingv1alpha1.StartupStarting,
		PodsReadyState:     trainingv1alpha1.PodsNotReady,
		LastEvictionReason: "PodsReadyTimeout",
		LastTransitionTime: &now,
	}
	right := &trainingv1alpha1.StartupRecoveryStatus{
		StartupState:       trainingv1alpha1.StartupStarting,
		PodsReadyState:     trainingv1alpha1.PodsNotReady,
		LastEvictionReason: "PodsReadyTimeout",
		LastTransitionTime: &now,
	}
	if !startupRecoveryStatusEqual(left, right) {
		t.Fatal("expected equal status to match")
	}
}

func TestStartupRecoveryStatusEqualDifferentState(t *testing.T) {
	left := &trainingv1alpha1.StartupRecoveryStatus{StartupState: trainingv1alpha1.StartupStarting}
	right := &trainingv1alpha1.StartupRecoveryStatus{StartupState: trainingv1alpha1.StartupRunning}
	if startupRecoveryStatusEqual(left, right) {
		t.Fatal("expected different states to not match")
	}
}

// ====================================================================
// Integration tests: Reconcile with startup/recovery
// ====================================================================

func TestReconcileStartupTimeoutClassification(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Status.Phase = trainingv1alpha1.PhaseStarting
	// Kueue has suspended the RTJ (waitForPodsReady timeout).
	rtj.Spec.Suspend = ptr.To(true)

	// Workload with Evicted=True, reason=PodsReadyTimeout.
	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPodsReadyTimeout,
					Message: "Pods did not reach Ready within 300s",
				},
			},
		},
	}
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	// Reconcile to detect eviction and enter stop flow.
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	// Verify startupRecovery status.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupTimedOut {
		t.Fatalf("expected StartupTimedOut, got %q", updated.Status.StartupRecovery.StartupState)
	}
	if updated.Status.StartupRecovery.LastEvictionReason != kueueEvictionReasonPodsReadyTimeout {
		t.Fatalf("expected eviction reason %q, got %q",
			kueueEvictionReasonPodsReadyTimeout, updated.Status.StartupRecovery.LastEvictionReason)
	}
	if updated.Status.StartupRecovery.LastRequeueReason != reasonRequeuedAfterEviction {
		t.Fatalf("expected requeue reason %q, got %q",
			reasonRequeuedAfterEviction, updated.Status.StartupRecovery.LastRequeueReason)
	}

	// Verify StartupTimeoutEvicted condition.
	cond := metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeStartupTimeoutEvicted)
	if cond == nil {
		t.Fatal("expected StartupTimeoutEvicted condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected condition True, got %s", cond.Status)
	}

	// RecoveryTimeoutEvicted should NOT be present.
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeRecoveryTimeoutEvicted) != nil {
		t.Fatal("expected RecoveryTimeoutEvicted condition to NOT be present")
	}
}

func TestReconcileRecoveryTimeoutClassification(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	// RTJ was Running when pods lost Ready state.
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Spec.Suspend = ptr.To(true)

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPodsReadyTimeout,
					Message: "Pods lost Ready state and did not recover within 300s",
				},
			},
		},
	}
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupRecoveryTimedOut {
		t.Fatalf("expected RecoveryTimedOut, got %q", updated.Status.StartupRecovery.StartupState)
	}

	// Verify RecoveryTimeoutEvicted condition.
	cond := metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeRecoveryTimeoutEvicted)
	if cond == nil {
		t.Fatal("expected RecoveryTimeoutEvicted condition")
	}

	// StartupTimeoutEvicted should NOT be present.
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeStartupTimeoutEvicted) != nil {
		t.Fatal("expected StartupTimeoutEvicted condition to NOT be present")
	}
}

func TestReconcileNormalReadyPath(t *testing.T) {
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

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}
	ctx := context.Background()

	// First reconcile: launches (Starting).
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("launch reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(ctx, req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseStarting {
		t.Fatalf("expected Starting, got %q", updated.Status.Phase)
	}

	// Verify startupRecovery is set to Starting.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated after launch")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupStarting {
		t.Fatalf("expected Starting state, got %q", updated.Status.StartupRecovery.StartupState)
	}
	if updated.Status.StartupRecovery.PodsReadyState != trainingv1alpha1.PodsNotReady {
		t.Fatalf("expected PodsNotReady, got %q", updated.Status.StartupRecovery.PodsReadyState)
	}

	// Second reconcile: active child exists → Running.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("running reconcile failed: %v", err)
	}

	must(t, client.Get(ctx, req.NamespacedName, &updated))
	if updated.Status.Phase != trainingv1alpha1.PhaseRunning {
		t.Fatalf("expected Running, got %q", updated.Status.Phase)
	}

	// Verify startupRecovery is set to Running.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated after running")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupRunning {
		t.Fatalf("expected Running state, got %q", updated.Status.StartupRecovery.StartupState)
	}
	if updated.Status.StartupRecovery.PodsReadyState != trainingv1alpha1.PodsReady {
		t.Fatalf("expected PodsReady, got %q", updated.Status.StartupRecovery.PodsReadyState)
	}

	// No eviction conditions should be present.
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeStartupTimeoutEvicted) != nil {
		t.Fatal("expected no StartupTimeoutEvicted condition on normal path")
	}
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeRecoveryTimeoutEvicted) != nil {
		t.Fatal("expected no RecoveryTimeoutEvicted condition on normal path")
	}
}

func TestReconcileManualPauseNotConfusedWithTimeout(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	// Manual pause: desiredState=Paused, suspend is NOT set by Kueue.
	rtj.Spec.Control.DesiredState = trainingv1alpha1.DesiredStatePaused
	// No Kueue suspension.
	rtj.Spec.Suspend = ptr.To(false)

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

	// Reconcile: should enter manual pause flow, NOT timeout.
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	// Should be in the yield flow, NOT showing timeout.
	if updated.Status.Phase != trainingv1alpha1.PhaseDraining && updated.Status.Phase != trainingv1alpha1.PhaseYieldRequested {
		// Phase might be YieldRequested or Draining depending on reconcile count.
		t.Logf("phase is %q (expected YieldRequested or Draining)", updated.Status.Phase)
	}

	// No timeout conditions should be present.
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeStartupTimeoutEvicted) != nil {
		t.Fatal("expected no StartupTimeoutEvicted condition on manual pause")
	}
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeRecoveryTimeoutEvicted) != nil {
		t.Fatal("expected no RecoveryTimeoutEvicted condition on manual pause")
	}

	// StartupRecovery should NOT show timeout states.
	if updated.Status.StartupRecovery != nil {
		if updated.Status.StartupRecovery.StartupState == trainingv1alpha1.StartupTimedOut {
			t.Fatal("manual pause must NOT be classified as startup timeout")
		}
		if updated.Status.StartupRecovery.StartupState == trainingv1alpha1.StartupRecoveryTimedOut {
			t.Fatal("manual pause must NOT be classified as recovery timeout")
		}
	}
}

func TestReconcileKueuePreemptionNotConfusedWithTimeout(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	// Kueue preemption: suspend=true but eviction reason is Preempted.
	rtj.Spec.Suspend = ptr.To(true)

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPreempted,
					Message: "Preempted by higher-priority workload",
				},
			},
		},
	}
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	// StartupRecovery should show Evicted, NOT timeout states.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if updated.Status.StartupRecovery.StartupState == trainingv1alpha1.StartupTimedOut {
		t.Fatal("preemption must NOT be classified as startup timeout")
	}
	if updated.Status.StartupRecovery.StartupState == trainingv1alpha1.StartupRecoveryTimedOut {
		t.Fatal("preemption must NOT be classified as recovery timeout")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupEvicted {
		t.Fatalf("expected Evicted for preemption, got %q", updated.Status.StartupRecovery.StartupState)
	}
	if updated.Status.StartupRecovery.LastEvictionReason != kueueEvictionReasonPreempted {
		t.Fatalf("expected eviction reason Preempted, got %q", updated.Status.StartupRecovery.LastEvictionReason)
	}

	// No timeout conditions should be present.
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeStartupTimeoutEvicted) != nil {
		t.Fatal("expected no StartupTimeoutEvicted condition on preemption")
	}
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeRecoveryTimeoutEvicted) != nil {
		t.Fatal("expected no RecoveryTimeoutEvicted condition on preemption")
	}
}

func TestReconcileIdempotentAfterRestart(t *testing.T) {
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Status.Phase = trainingv1alpha1.PhaseStarting
	rtj.Spec.Suspend = ptr.To(true)
	// Simulate previously recorded eviction (as if from a prior reconcile).
	rtj.Status.StartupRecovery = &trainingv1alpha1.StartupRecoveryStatus{
		StartupState:       trainingv1alpha1.StartupTimedOut,
		PodsReadyState:     trainingv1alpha1.PodsNotReady,
		LastEvictionReason: kueueEvictionReasonPodsReadyTimeout,
		LastRequeueReason:  reasonRequeuedAfterEviction,
		LastTransitionTime: &now,
	}
	rtj.Status.Conditions = append(rtj.Status.Conditions, metav1.Condition{
		Type:               conditionTypeStartupTimeoutEvicted,
		Status:             metav1.ConditionTrue,
		Reason:             reasonPodsReadyTimeout,
		Message:            "Pods did not reach Ready within 300s",
		LastTransitionTime: now,
		ObservedGeneration: rtj.Generation,
	})

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPodsReadyTimeout,
					Message: "Pods did not reach Ready within 300s",
				},
			},
		},
	}
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	// Reconcile multiple times (simulating operator restart).
	for i := 0; i < 3; i++ {
		if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	// StartupRecovery should still show the same eviction state.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be preserved")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupTimedOut {
		t.Fatalf("expected StartupTimedOut to be preserved, got %q", updated.Status.StartupRecovery.StartupState)
	}
	if updated.Status.StartupRecovery.LastEvictionReason != kueueEvictionReasonPodsReadyTimeout {
		t.Fatalf("expected eviction reason preserved, got %q", updated.Status.StartupRecovery.LastEvictionReason)
	}
}

func TestReconcileStartupRecoverySurvivesResumeAfterTimeout(t *testing.T) {
	// After a startup timeout, when the RTJ is re-admitted and launches again,
	// the startupRecovery state should reset to Starting (with eviction history preserved).
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 5, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(-1 * time.Minute))
	rtj := controllerTestRTJ()
	rtj.Finalizers = []string{resumableTrainingJobFinalizer}
	rtj.Spec.Suspend = ptr.To(false)
	rtj.Status.CurrentRunAttempt = 1
	rtj.Status.Phase = trainingv1alpha1.PhasePaused
	rtj.Status.ObservedGeneration = rtj.Generation
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:                 "ckpt-run1-step20",
		StorageURI:         "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
		ManifestURI:        "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
		CompletionTime:     &completedAt,
		SourceRunAttempt:   1,
		CompatibilityState: trainingv1alpha1.CompatibilityStateCompatible,
	}
	// Previous eviction recorded.
	rtj.Status.StartupRecovery = &trainingv1alpha1.StartupRecoveryStatus{
		StartupState:       trainingv1alpha1.StartupTimedOut,
		PodsReadyState:     trainingv1alpha1.PodsNotReady,
		LastEvictionReason: kueueEvictionReasonPodsReadyTimeout,
		LastRequeueReason:  reasonRequeuedAfterEviction,
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
				ID:                 "ckpt-run1-step20",
				StorageURI:         "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
				ManifestURI:        "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
				CompletionTime:     &completedAt,
				SourceRunAttempt:   1,
				CompatibilityState: trainingv1alpha1.CompatibilityStateCompatible,
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
		t.Fatalf("expected Restoring, got %q", updated.Status.Phase)
	}

	// StartupRecovery should be reset to Starting for the new attempt.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupStarting {
		t.Fatalf("expected Starting after resume, got %q", updated.Status.StartupRecovery.StartupState)
	}
	// Eviction history should be preserved from the previous attempt.
	if updated.Status.StartupRecovery.LastEvictionReason != kueueEvictionReasonPodsReadyTimeout {
		t.Fatalf("expected eviction history preserved, got %q", updated.Status.StartupRecovery.LastEvictionReason)
	}
}

func TestReconcileCheckpointPreservedOnRecoveryTimeout(t *testing.T) {
	// When a recovery timeout happens after the RTJ has produced checkpoints,
	// the lastCompletedCheckpoint must be preserved for later resume.
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))
	must(t, kueuev1beta2.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(-5 * time.Minute))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Status.Phase = trainingv1alpha1.PhaseRunning
	rtj.Spec.Suspend = ptr.To(true)
	// RTJ has a previously completed checkpoint.
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:                 "ckpt-run1-step100",
		StorageURI:         "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step100",
		ManifestURI:        "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step100.manifest.json",
		CompletionTime:     &completedAt,
		SourceRunAttempt:   1,
		CompatibilityState: trainingv1alpha1.CompatibilityStateCompatible,
	}

	workload := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rtj.Name + "-workload",
			Namespace: rtj.Namespace,
		},
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueueConditionEvicted,
					Status:  metav1.ConditionTrue,
					Reason:  kueueEvictionReasonPodsReadyTimeout,
					Message: "Recovery timeout",
				},
			},
		},
	}
	rtj.Status.WorkloadReference = &trainingv1alpha1.WorkloadReference{
		Name:      workload.Name,
		Namespace: workload.Namespace,
	}

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(rtj, workload).
		WithObjects(rtj, workload, controlConfigMap, childJobSet).
		Build()

	reconciler := &ResumableTrainingJobReconciler{
		Client: client,
		Scheme: scheme,
		Now:    func() metav1.Time { return now },
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: rtj.Name, Namespace: rtj.Namespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	// Checkpoint must be preserved for later resume.
	if updated.Status.LastCompletedCheckpoint == nil {
		t.Fatal("expected lastCompletedCheckpoint to be preserved on recovery timeout")
	}
	if updated.Status.LastCompletedCheckpoint.ID != "ckpt-run1-step100" {
		t.Fatalf("expected checkpoint ID preserved, got %q", updated.Status.LastCompletedCheckpoint.ID)
	}

	// Verify recovery timeout was classified correctly.
	if updated.Status.StartupRecovery == nil {
		t.Fatal("expected StartupRecovery to be populated")
	}
	if updated.Status.StartupRecovery.StartupState != trainingv1alpha1.StartupRecoveryTimedOut {
		t.Fatalf("expected RecoveryTimedOut, got %q", updated.Status.StartupRecovery.StartupState)
	}
}

func TestReconcileNoWorkloadReferenceSkipsEvictionDetection(t *testing.T) {
	// When there's no WorkloadReference (Phase 2 path), eviction detection
	// should be skipped and the normal Kueue suspend flow should proceed.
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, trainingv1alpha1.AddToScheme(scheme))

	now := metav1.NewTime(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	completedAt := metav1.NewTime(now.Add(30 * time.Second))
	rtj := controllerTestRTJ()
	primeActiveRun(rtj)
	rtj.Spec.Suspend = ptr.To(true)
	// No WorkloadReference set.

	controlConfigMap := buildControlConfigMap(rtj, rtj.Status.ActiveControlConfigMapName, rtj.Status.CurrentRunAttempt)
	childJobSet := newTestChildJobSet(rtj.Status.ActiveJobSetName, rtj.Namespace)
	fakeCatalog := &fakeCheckpointCatalog{
		observation: &checkpoints.PauseObservation{
			MarkerURI: checkpoints.YieldMarkerURI(rtj.Spec.Checkpoint.StorageURI, rtj.Status.CurrentRunAttempt),
			Checkpoint: trainingv1alpha1.CheckpointReference{
				ID:                 "ckpt-run1-step20",
				StorageURI:         "s3://phase1-checkpoints/counter/checkpoints/ckpt-run1-step20",
				ManifestURI:        "s3://phase1-checkpoints/counter/manifests/ckpt-run1-step20.manifest.json",
				CompletionTime:     &completedAt,
				SourceRunAttempt:   1,
				CompatibilityState: trainingv1alpha1.CompatibilityStateCompatible,
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

	// Should proceed through the normal Kueue suspend path without error.
	for i := 0; i < 4; i++ {
		if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}

	var updated trainingv1alpha1.ResumableTrainingJob
	must(t, client.Get(context.Background(), req.NamespacedName, &updated))

	// No timeout conditions should be present (no Workload to inspect).
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeStartupTimeoutEvicted) != nil {
		t.Fatal("expected no StartupTimeoutEvicted without WorkloadReference")
	}
	if metav1api.FindStatusCondition(updated.Status.Conditions, conditionTypeRecoveryTimeoutEvicted) != nil {
		t.Fatal("expected no RecoveryTimeoutEvicted without WorkloadReference")
	}
}

// Ensure fake types used by other test files are not duplicated.
var _ checkpoints.Catalog = &fakeCheckpointCatalog{}
var _ = (*unstructured.Unstructured)(nil)
