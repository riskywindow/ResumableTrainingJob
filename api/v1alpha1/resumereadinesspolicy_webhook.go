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
	rrpMutatingWebhookPath   = "/mutate-training-checkpoint-example-io-v1alpha1-resumereadinesspolicy"
	rrpValidatingWebhookPath = "/validate-training-checkpoint-example-io-v1alpha1-resumereadinesspolicy"
)

// +kubebuilder:webhook:path=/mutate-training-checkpoint-example-io-v1alpha1-resumereadinesspolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=training.checkpoint.example.io,resources=resumereadinesspolicies,verbs=create;update,versions=v1alpha1,name=mresumerreadinesspolicy.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-training-checkpoint-example-io-v1alpha1-resumereadinesspolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=training.checkpoint.example.io,resources=resumereadinesspolicies,verbs=create;update,versions=v1alpha1,name=vresumerreadinesspolicy.kb.io,admissionReviewVersions=v1

// ResumeReadinessPolicyWebhook handles defaulting and validation for ResumeReadinessPolicy.
type ResumeReadinessPolicyWebhook struct{}

// SetupResumeReadinessPolicyWebhookWithManager installs the RRP webhook handlers on the manager webhook server.
func SetupResumeReadinessPolicyWebhookWithManager(mgr ctrl.Manager) {
	wh := &ResumeReadinessPolicyWebhook{}
	server := mgr.GetWebhookServer()
	server.Register(rrpMutatingWebhookPath, &webhook.Admission{Handler: admission.WithCustomDefaulter(mgr.GetScheme(), &ResumeReadinessPolicy{}, wh)})
	server.Register(rrpValidatingWebhookPath, &webhook.Admission{Handler: admission.WithCustomValidator(mgr.GetScheme(), &ResumeReadinessPolicy{}, wh)})
}

var _ admission.CustomDefaulter = &ResumeReadinessPolicyWebhook{}
var _ admission.CustomValidator = &ResumeReadinessPolicyWebhook{}

func (w *ResumeReadinessPolicyWebhook) Default(_ context.Context, obj runtime.Object) error {
	policy, err := rrpFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	policy.Default()
	return nil
}

func (w *ResumeReadinessPolicyWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	policy, err := rrpFromRuntimeObject(obj)
	if err != nil {
		return nil, err
	}
	return nil, validateResumeReadinessPolicy(policy)
}

func (w *ResumeReadinessPolicyWebhook) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	policy, err := rrpFromRuntimeObject(newObj)
	if err != nil {
		return nil, err
	}
	return nil, validateResumeReadinessPolicy(policy)
}

func (w *ResumeReadinessPolicyWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func rrpFromRuntimeObject(obj runtime.Object) (*ResumeReadinessPolicy, error) {
	policy, ok := obj.(*ResumeReadinessPolicy)
	if !ok {
		return nil, fmt.Errorf("expected ResumeReadinessPolicy, got %T", obj)
	}
	return policy, nil
}

func validateResumeReadinessPolicy(p *ResumeReadinessPolicy) error {
	if p.Spec.FailurePolicy != "" &&
		p.Spec.FailurePolicy != FailurePolicyFailOpen &&
		p.Spec.FailurePolicy != FailurePolicyFailClosed {
		return fmt.Errorf("spec.failurePolicy must be FailOpen or FailClosed, got %q", p.Spec.FailurePolicy)
	}
	if p.Spec.MaxCheckpointAge != nil && p.Spec.MaxCheckpointAge.Duration < 0 {
		return fmt.Errorf("spec.maxCheckpointAge must be non-negative, got %v", p.Spec.MaxCheckpointAge.Duration)
	}
	return nil
}
