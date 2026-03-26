package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	webhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	cppMutatingWebhookPath   = "/mutate-training-checkpoint-example-io-v1alpha1-checkpointprioritypolicy"
	cppValidatingWebhookPath = "/validate-training-checkpoint-example-io-v1alpha1-checkpointprioritypolicy"
)

// +kubebuilder:webhook:path=/mutate-training-checkpoint-example-io-v1alpha1-checkpointprioritypolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=training.checkpoint.example.io,resources=checkpointprioritypolicies,verbs=create;update,versions=v1alpha1,name=mcheckpointprioritypolicy.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-training-checkpoint-example-io-v1alpha1-checkpointprioritypolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=training.checkpoint.example.io,resources=checkpointprioritypolicies,verbs=create;update,versions=v1alpha1,name=vcheckpointprioritypolicy.kb.io,admissionReviewVersions=v1

// CheckpointPriorityPolicyWebhook handles defaulting and validation for CheckpointPriorityPolicy.
type CheckpointPriorityPolicyWebhook struct{}

// SetupCheckpointPriorityPolicyWebhookWithManager installs the CPP webhook handlers on the manager webhook server.
func SetupCheckpointPriorityPolicyWebhookWithManager(mgr ctrl.Manager) {
	wh := &CheckpointPriorityPolicyWebhook{}
	server := mgr.GetWebhookServer()
	server.Register(cppMutatingWebhookPath, &webhook.Admission{Handler: admission.WithCustomDefaulter(mgr.GetScheme(), &CheckpointPriorityPolicy{}, wh)})
	server.Register(cppValidatingWebhookPath, &webhook.Admission{Handler: admission.WithCustomValidator(mgr.GetScheme(), &CheckpointPriorityPolicy{}, wh)})
}

var _ admission.CustomDefaulter = &CheckpointPriorityPolicyWebhook{}
var _ admission.CustomValidator = &CheckpointPriorityPolicyWebhook{}

func (w *CheckpointPriorityPolicyWebhook) Default(_ context.Context, obj runtime.Object) error {
	policy, err := cppFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	policy.Default()
	return nil
}

func (w *CheckpointPriorityPolicyWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	policy, err := cppFromRuntimeObject(obj)
	if err != nil {
		return nil, err
	}
	return nil, validateCheckpointPriorityPolicy(policy)
}

func (w *CheckpointPriorityPolicyWebhook) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	policy, err := cppFromRuntimeObject(newObj)
	if err != nil {
		return nil, err
	}
	return nil, validateCheckpointPriorityPolicy(policy)
}

func (w *CheckpointPriorityPolicyWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func cppFromRuntimeObject(obj runtime.Object) (*CheckpointPriorityPolicy, error) {
	policy, ok := obj.(*CheckpointPriorityPolicy)
	if !ok {
		return nil, fmt.Errorf("expected CheckpointPriorityPolicy, got %T", obj)
	}
	return policy, nil
}

func validateCheckpointPriorityPolicy(p *CheckpointPriorityPolicy) error {
	// Required durations must be positive.
	if p.Spec.CheckpointFreshnessTarget.Duration <= 0 {
		return fmt.Errorf("spec.checkpointFreshnessTarget must be positive, got %v", p.Spec.CheckpointFreshnessTarget.Duration)
	}
	if p.Spec.StartupProtectionWindow.Duration <= 0 {
		return fmt.Errorf("spec.startupProtectionWindow must be positive, got %v", p.Spec.StartupProtectionWindow.Duration)
	}
	if p.Spec.MinRuntimeBetweenYields.Duration <= 0 {
		return fmt.Errorf("spec.minRuntimeBetweenYields must be positive, got %v", p.Spec.MinRuntimeBetweenYields.Duration)
	}

	// maxYieldsPerWindow must be non-negative.
	if p.Spec.MaxYieldsPerWindow < 0 {
		return fmt.Errorf("spec.maxYieldsPerWindow must be non-negative, got %d", p.Spec.MaxYieldsPerWindow)
	}

	// yieldWindow required when maxYieldsPerWindow is set and > 0.
	if p.Spec.MaxYieldsPerWindow > 0 {
		if p.Spec.YieldWindow == nil || p.Spec.YieldWindow.Duration <= 0 {
			return fmt.Errorf("spec.yieldWindow is required and must be positive when maxYieldsPerWindow > 0")
		}
	}

	// yieldWindow must be positive when set.
	if p.Spec.YieldWindow != nil && p.Spec.YieldWindow.Duration <= 0 {
		return fmt.Errorf("spec.yieldWindow must be positive when set, got %v", p.Spec.YieldWindow.Duration)
	}

	// Validate boost/offset bounds.
	if err := validatePriorityBound("spec.protectedBoost", p.Spec.ProtectedBoost); err != nil {
		return err
	}
	if err := validatePriorityBound("spec.cooldownBoost", p.Spec.CooldownBoost); err != nil {
		return err
	}
	if err := validatePriorityBound("spec.staleCheckpointBoost", p.Spec.StaleCheckpointBoost); err != nil {
		return err
	}
	if err := validatePriorityBound("spec.preemptibleOffset", p.Spec.PreemptibleOffset); err != nil {
		return err
	}
	if err := validatePriorityBound("spec.minEffectivePriority", p.Spec.MinEffectivePriority); err != nil {
		return err
	}
	if err := validatePriorityBound("spec.maxEffectivePriority", p.Spec.MaxEffectivePriority); err != nil {
		return err
	}

	// minEffectivePriority <= maxEffectivePriority when both are set.
	if p.Spec.MinEffectivePriority != nil && p.Spec.MaxEffectivePriority != nil {
		if *p.Spec.MinEffectivePriority > *p.Spec.MaxEffectivePriority {
			return fmt.Errorf("spec.minEffectivePriority (%d) must be <= spec.maxEffectivePriority (%d)",
				*p.Spec.MinEffectivePriority, *p.Spec.MaxEffectivePriority)
		}
	}

	return nil
}

func validatePriorityBound(fieldName string, val *int32) error {
	if val == nil {
		return nil
	}
	if *val < MinPriorityBound || *val > MaxPriorityBound {
		return fmt.Errorf("%s must be within [%d, %d], got %d", fieldName, MinPriorityBound, MaxPriorityBound, *val)
	}
	return nil
}
