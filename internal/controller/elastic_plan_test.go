package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/elastic"
)

var elasticTestNow = metav1.NewTime(time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC))

func elasticTestRTJ() *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-elastic-rtj",
			Namespace: "default",
			UID:       "test-uid-elastic",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "train:latest",
				CodeVersion: "v1",
				WorldSize:   8,
				GPUShape:    "A100",
			},
			Elasticity: &trainingv1alpha1.ElasticitySpec{
				Mode:                trainingv1alpha1.ElasticityModeManual,
				TargetWorkerCount:   ptr.To[int32](4),
				InPlaceShrinkPolicy: trainingv1alpha1.InPlaceShrinkPolicyIfSupported,
				ReclaimMode:         trainingv1alpha1.ReclaimModeReclaimablePods,
			},
		},
		Status: trainingv1alpha1.ResumableTrainingJobStatus{
			Phase: trainingv1alpha1.PhaseRunning,
			Elasticity: &trainingv1alpha1.ElasticityStatus{
				AdmittedWorkerCount:  8,
				ActiveWorkerCount:    8,
				ResizeState:          trainingv1alpha1.ResizeStateIdle,
				CurrentExecutionMode: trainingv1alpha1.ExecutionModeElastic,
				InPlaceShrinkSupported: true,
			},
		},
	}
}

// --- buildElasticPlanInput tests ---

func TestBuildElasticPlanInput_ManualMode(t *testing.T) {
	job := elasticTestRTJ()
	input := buildElasticPlanInput(job, true, true, elasticTestNow)

	if !input.ElasticityEnabled {
		t.Error("expected ElasticityEnabled=true")
	}
	if input.TargetWorkerCount != 4 {
		t.Errorf("expected target=4, got %d", input.TargetWorkerCount)
	}
	if input.CurrentWorkerCount != 8 {
		t.Errorf("expected current=8, got %d", input.CurrentWorkerCount)
	}
	if input.MinWorkerCount != 1 {
		t.Errorf("expected min=1, got %d", input.MinWorkerCount)
	}
	if input.MaxWorkerCount != 8 {
		t.Errorf("expected max=8, got %d", input.MaxWorkerCount)
	}
	if input.InPlaceShrinkPolicy != "IfSupported" {
		t.Errorf("expected policy=IfSupported, got %s", input.InPlaceShrinkPolicy)
	}
	if !input.RuntimeSupportsInPlaceShrink {
		t.Error("expected RuntimeSupportsInPlaceShrink=true")
	}
	if !input.WorkloadAdmitted {
		t.Error("expected WorkloadAdmitted=true")
	}
}

func TestBuildElasticPlanInput_DisabledMode(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Elasticity.Mode = trainingv1alpha1.ElasticityModeDisabled

	input := buildElasticPlanInput(job, true, true, elasticTestNow)

	if input.ElasticityEnabled {
		t.Error("expected ElasticityEnabled=false for Disabled mode")
	}
}

func TestBuildElasticPlanInput_NilElasticity(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Elasticity = nil
	job.Status.Elasticity = nil

	input := buildElasticPlanInput(job, true, true, elasticTestNow)

	if input.ElasticityEnabled {
		t.Error("expected ElasticityEnabled=false for nil elasticity")
	}
	if input.TargetWorkerCount != 0 {
		t.Errorf("expected target=0, got %d", input.TargetWorkerCount)
	}
}

func TestBuildElasticPlanInput_PreemptionDetection(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Suspend = ptr.To(true)
	job.Status.Phase = trainingv1alpha1.PhaseDraining

	input := buildElasticPlanInput(job, false, true, elasticTestNow)

	if !input.PreemptionInProgress {
		t.Error("expected PreemptionInProgress=true when suspended and draining")
	}
}

func TestBuildElasticPlanInput_NoPreemption(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Suspend = ptr.To(false)
	job.Status.Phase = trainingv1alpha1.PhaseRunning

	input := buildElasticPlanInput(job, true, true, elasticTestNow)

	if input.PreemptionInProgress {
		t.Error("expected PreemptionInProgress=false when not suspended")
	}
}

func TestBuildElasticPlanInput_FallbackToPreferredCount(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity = nil
	job.Spec.Identity.WorldSize = 16

	input := buildElasticPlanInput(job, true, true, elasticTestNow)

	if input.CurrentWorkerCount != 16 {
		t.Errorf("expected current=16 (fallback to preferred), got %d", input.CurrentWorkerCount)
	}
}

// --- evaluateElasticPlan tests ---

func TestEvaluateElasticPlan_ShrinkInPlace(t *testing.T) {
	job := elasticTestRTJ()
	ctx := context.Background()

	result := evaluateElasticPlan(ctx, job, true, true, elasticTestNow)

	if result.Plan.Kind != elastic.PlanShrinkInPlace {
		t.Errorf("expected ShrinkInPlace, got %s", result.Plan.Kind)
	}
	if !result.StatusChanged {
		t.Error("expected status to change on first evaluation")
	}
	if job.Status.Elasticity.ResizeState != trainingv1alpha1.ResizeStatePending {
		t.Errorf("expected ResizeState=Pending, got %s", job.Status.Elasticity.ResizeState)
	}
	if job.Status.Elasticity.ResizePath != trainingv1alpha1.ResizePathInPlace {
		t.Errorf("expected ResizePath=InPlace, got %s", job.Status.Elasticity.ResizePath)
	}
}

func TestEvaluateElasticPlan_GrowViaRelaunch(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Elasticity.TargetWorkerCount = ptr.To[int32](16)
	job.Spec.Identity.WorldSize = 16
	ctx := context.Background()

	result := evaluateElasticPlan(ctx, job, true, true, elasticTestNow)

	if result.Plan.Kind != elastic.PlanGrowViaRelaunch {
		t.Errorf("expected GrowViaRelaunch, got %s", result.Plan.Kind)
	}
	if job.Status.Elasticity.ResizePath != trainingv1alpha1.ResizePathCheckpointAndRelaunch {
		t.Errorf("expected ResizePath=CheckpointAndRelaunch, got %s", job.Status.Elasticity.ResizePath)
	}
}

func TestEvaluateElasticPlan_NoOpWhenTargetEqualsCurrent(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Elasticity.TargetWorkerCount = ptr.To[int32](8)
	ctx := context.Background()

	result := evaluateElasticPlan(ctx, job, true, true, elasticTestNow)

	if result.Plan.Kind != elastic.PlanNoResize {
		t.Errorf("expected NoResize, got %s", result.Plan.Kind)
	}
}

func TestEvaluateElasticPlan_DisabledIsNoOp(t *testing.T) {
	job := elasticTestRTJ()
	job.Spec.Elasticity.Mode = trainingv1alpha1.ElasticityModeDisabled
	ctx := context.Background()

	result := evaluateElasticPlan(ctx, job, true, true, elasticTestNow)

	if result.Plan.Kind != elastic.PlanNoResize {
		t.Errorf("expected NoResize when disabled, got %s", result.Plan.Kind)
	}
}

func TestEvaluateElasticPlan_BlockedWhenNotAdmitted(t *testing.T) {
	job := elasticTestRTJ()
	ctx := context.Background()

	result := evaluateElasticPlan(ctx, job, false, true, elasticTestNow)

	if result.Plan.Kind != elastic.PlanResizeBlocked {
		t.Errorf("expected ResizeBlocked, got %s", result.Plan.Kind)
	}
}

// --- syncElasticityStatus tests ---

func TestSyncElasticityStatus_InitializesNilStatus(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity = nil

	plan := elastic.PlanOutput{
		Kind:   elastic.PlanNoResize,
		Reason: "TestReason",
	}
	input := elastic.PlanInput{
		MaxWorkerCount: 8,
	}

	changed := syncElasticityStatus(job, plan, input, elasticTestNow)

	if !changed {
		t.Error("expected change when initializing nil status")
	}
	if job.Status.Elasticity == nil {
		t.Fatal("expected elasticity status to be initialized")
	}
}

func TestSyncElasticityStatus_Idempotent(t *testing.T) {
	job := elasticTestRTJ()

	plan := elastic.PlanOutput{
		Kind:   elastic.PlanNoResize,
		Reason: "TargetEqualsAdmitted",
		Message: "target worker count matches current admitted count",
	}
	input := elastic.PlanInput{
		MaxWorkerCount:  8,
		CurrentWorkerCount: 8,
		ElasticityEnabled: true,
	}

	// First call initializes.
	syncElasticityStatus(job, plan, input, elasticTestNow)

	// Second call with same inputs should be idempotent.
	changed := syncElasticityStatus(job, plan, input, elasticTestNow)

	if changed {
		t.Error("expected no change on idempotent call")
	}
}

func TestSyncElasticityStatus_SetsExecutionMode(t *testing.T) {
	job := elasticTestRTJ()
	job.Status.Elasticity = &trainingv1alpha1.ElasticityStatus{}

	plan := elastic.PlanOutput{Kind: elastic.PlanNoResize}
	enabledInput := elastic.PlanInput{ElasticityEnabled: true}

	syncElasticityStatus(job, plan, enabledInput, elasticTestNow)
	if job.Status.Elasticity.CurrentExecutionMode != trainingv1alpha1.ExecutionModeElastic {
		t.Errorf("expected Elastic, got %s", job.Status.Elasticity.CurrentExecutionMode)
	}

	disabledInput := elastic.PlanInput{ElasticityEnabled: false}
	syncElasticityStatus(job, plan, disabledInput, elasticTestNow)
	if job.Status.Elasticity.CurrentExecutionMode != trainingv1alpha1.ExecutionModeFixed {
		t.Errorf("expected Fixed, got %s", job.Status.Elasticity.CurrentExecutionMode)
	}
}

func TestSyncElasticityStatus_TransitionTimestamp(t *testing.T) {
	job := elasticTestRTJ()

	plan := elastic.PlanOutput{
		Kind:   elastic.PlanShrinkInPlace,
		Reason: "InPlaceShrinkSupported",
		ReclaimableWorkerDelta: 4,
		Message: "shrink",
	}
	input := elastic.PlanInput{
		ElasticityEnabled: true,
		MaxWorkerCount:    8,
		CurrentWorkerCount: 8,
		TargetWorkerCount: 4,
		ActiveWorkerCount: 8,
		RuntimeSupportsInPlaceShrink: true,
	}

	syncElasticityStatus(job, plan, input, elasticTestNow)

	if job.Status.Elasticity.LastElasticTransitionTime == nil {
		t.Error("expected transition time to be set")
	}
}

// --- planKindToResizeState tests ---

func TestPlanKindToResizeState(t *testing.T) {
	tests := []struct {
		kind elastic.PlanKind
		want trainingv1alpha1.ResizeState
	}{
		{elastic.PlanNoResize, trainingv1alpha1.ResizeStateIdle},
		{elastic.PlanShrinkInPlace, trainingv1alpha1.ResizeStatePending},
		{elastic.PlanShrinkViaRelaunch, trainingv1alpha1.ResizeStatePending},
		{elastic.PlanGrowViaRelaunch, trainingv1alpha1.ResizeStatePending},
		{elastic.PlanResizeBlocked, trainingv1alpha1.ResizeStateBlocked},
		{elastic.PlanResizeInProgress, trainingv1alpha1.ResizeStateInProgress},
		{elastic.PlanReclaimPublished, trainingv1alpha1.ResizeStateInProgress},
	}

	for _, tt := range tests {
		got := planKindToResizeState(tt.kind)
		if got != tt.want {
			t.Errorf("planKindToResizeState(%s) = %s, want %s", tt.kind, got, tt.want)
		}
	}
}

// --- planKindToResizePath tests ---

func TestPlanKindToResizePath(t *testing.T) {
	tests := []struct {
		kind elastic.PlanKind
		want trainingv1alpha1.ResizePath
	}{
		{elastic.PlanShrinkInPlace, trainingv1alpha1.ResizePathInPlace},
		{elastic.PlanReclaimPublished, trainingv1alpha1.ResizePathInPlace},
		{elastic.PlanShrinkViaRelaunch, trainingv1alpha1.ResizePathCheckpointAndRelaunch},
		{elastic.PlanGrowViaRelaunch, trainingv1alpha1.ResizePathCheckpointAndRelaunch},
		{elastic.PlanNoResize, ""},
		{elastic.PlanResizeBlocked, ""},
	}

	for _, tt := range tests {
		got := planKindToResizePath(tt.kind)
		if got != tt.want {
			t.Errorf("planKindToResizePath(%s) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
