package v1alpha1

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestResumeReadinessPolicyDefaultSetsFailurePolicy(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{}
	if err := wh.Default(context.Background(), policy); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if policy.Spec.FailurePolicy != DefaultFailurePolicy {
		t.Fatalf("expected failurePolicy %q, got %q", DefaultFailurePolicy, policy.Spec.FailurePolicy)
	}
	if policy.Spec.RequireCompleteCheckpoint == nil || !*policy.Spec.RequireCompleteCheckpoint {
		t.Fatalf("expected requireCompleteCheckpoint to default to true")
	}
	if policy.Spec.AllowInitialLaunchWithoutCheckpoint == nil || !*policy.Spec.AllowInitialLaunchWithoutCheckpoint {
		t.Fatalf("expected allowInitialLaunchWithoutCheckpoint to default to true")
	}
}

func TestResumeReadinessPolicyDefaultPreservesExplicitValues(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{
		Spec: ResumeReadinessPolicySpec{
			RequireCompleteCheckpoint:           ptr.To(false),
			FailurePolicy:                       FailurePolicyFailOpen,
			AllowInitialLaunchWithoutCheckpoint: ptr.To(false),
			MaxCheckpointAge:                    &metav1.Duration{Duration: 30 * time.Minute},
		},
	}
	if err := wh.Default(context.Background(), policy); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if policy.Spec.FailurePolicy != FailurePolicyFailOpen {
		t.Fatalf("expected failurePolicy to stay FailOpen, got %q", policy.Spec.FailurePolicy)
	}
	if *policy.Spec.RequireCompleteCheckpoint {
		t.Fatalf("expected requireCompleteCheckpoint to stay false")
	}
	if *policy.Spec.AllowInitialLaunchWithoutCheckpoint {
		t.Fatalf("expected allowInitialLaunchWithoutCheckpoint to stay false")
	}
	if policy.Spec.MaxCheckpointAge.Duration != 30*time.Minute {
		t.Fatalf("expected maxCheckpointAge to stay 30m, got %v", policy.Spec.MaxCheckpointAge.Duration)
	}
}

func TestResumeReadinessPolicyValidateCreateAcceptsMinimalSpec(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{}
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected minimal policy to pass validation, got %v", err)
	}
}

func TestResumeReadinessPolicyValidateCreateAcceptsFailOpen(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{
		Spec: ResumeReadinessPolicySpec{
			FailurePolicy: FailurePolicyFailOpen,
		},
	}
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected FailOpen policy to pass validation, got %v", err)
	}
}

func TestResumeReadinessPolicyValidateCreateAcceptsMaxCheckpointAge(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{
		Spec: ResumeReadinessPolicySpec{
			MaxCheckpointAge: &metav1.Duration{Duration: 1 * time.Hour},
		},
	}
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected policy with maxCheckpointAge to pass validation, got %v", err)
	}
}

func TestResumeReadinessPolicyValidateCreateRejectsNegativeAge(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{
		Spec: ResumeReadinessPolicySpec{
			MaxCheckpointAge: &metav1.Duration{Duration: -5 * time.Minute},
		},
	}
	policy.Default()

	_, err := wh.ValidateCreate(context.Background(), policy)
	if err == nil {
		t.Fatalf("expected validation to reject negative maxCheckpointAge")
	}
	if !strings.Contains(err.Error(), "maxCheckpointAge") {
		t.Fatalf("expected error about maxCheckpointAge, got %v", err)
	}
}

func TestResumeReadinessPolicyValidateCreateAcceptsZeroAge(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	// Zero means no age limit — should be accepted.
	policy := &ResumeReadinessPolicy{
		Spec: ResumeReadinessPolicySpec{
			MaxCheckpointAge: &metav1.Duration{Duration: 0},
		},
	}
	policy.Default()

	if _, err := wh.ValidateCreate(context.Background(), policy); err != nil {
		t.Fatalf("expected zero maxCheckpointAge to pass validation, got %v", err)
	}
}

func TestResumeReadinessPolicyValidateUpdateAcceptsChange(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	oldPolicy := &ResumeReadinessPolicy{
		Spec: ResumeReadinessPolicySpec{
			FailurePolicy: FailurePolicyFailClosed,
		},
	}
	oldPolicy.Default()

	newPolicy := oldPolicy.DeepCopy()
	newPolicy.Spec.FailurePolicy = FailurePolicyFailOpen

	if _, err := wh.ValidateUpdate(context.Background(), oldPolicy, newPolicy); err != nil {
		t.Fatalf("expected policy update to pass, got %v", err)
	}
}

func TestResumeReadinessPolicyValidateDeleteAllowed(t *testing.T) {
	wh := &ResumeReadinessPolicyWebhook{}

	policy := &ResumeReadinessPolicy{}
	if _, err := wh.ValidateDelete(context.Background(), policy); err != nil {
		t.Fatalf("expected delete to be allowed, got %v", err)
	}
}
