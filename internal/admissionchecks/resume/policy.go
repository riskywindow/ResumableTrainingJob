package resume

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// ResolvedPolicy holds the effective policy values with all nil pointers
// resolved to their defaults. The evaluator works with this struct so that
// decision logic never has to dereference optional pointers.
type ResolvedPolicy struct {
	RequireCompleteCheckpoint           bool
	MaxCheckpointAge                    *time.Duration
	FailurePolicy                       trainingv1alpha1.FailurePolicy
	AllowInitialLaunchWithoutCheckpoint bool
}

// ResolvePolicy converts a ResumeReadinessPolicy into concrete values.
// If the policy is nil, defaults are returned.
func ResolvePolicy(policy *trainingv1alpha1.ResumeReadinessPolicy) ResolvedPolicy {
	if policy == nil {
		return ResolvedPolicy{
			RequireCompleteCheckpoint:           trainingv1alpha1.DefaultRequireCompleteCheckpoint,
			MaxCheckpointAge:                    nil,
			FailurePolicy:                       trainingv1alpha1.DefaultFailurePolicy,
			AllowInitialLaunchWithoutCheckpoint: trainingv1alpha1.DefaultAllowInitialLaunchWithoutCheckpoint,
		}
	}

	resolved := ResolvedPolicy{
		RequireCompleteCheckpoint:           trainingv1alpha1.DefaultRequireCompleteCheckpoint,
		FailurePolicy:                       trainingv1alpha1.DefaultFailurePolicy,
		AllowInitialLaunchWithoutCheckpoint: trainingv1alpha1.DefaultAllowInitialLaunchWithoutCheckpoint,
	}

	if policy.Spec.RequireCompleteCheckpoint != nil {
		resolved.RequireCompleteCheckpoint = *policy.Spec.RequireCompleteCheckpoint
	}
	if policy.Spec.MaxCheckpointAge != nil && policy.Spec.MaxCheckpointAge.Duration > 0 {
		d := policy.Spec.MaxCheckpointAge.Duration
		resolved.MaxCheckpointAge = &d
	}
	if policy.Spec.FailurePolicy != "" {
		resolved.FailurePolicy = policy.Spec.FailurePolicy
	}
	if policy.Spec.AllowInitialLaunchWithoutCheckpoint != nil {
		resolved.AllowInitialLaunchWithoutCheckpoint = *policy.Spec.AllowInitialLaunchWithoutCheckpoint
	}

	return resolved
}

// LoadPolicyForCheck resolves the ResumeReadinessPolicy referenced by an
// AdmissionCheck's spec.parameters. Returns the policy, or an error if the
// parameters are missing, point to the wrong kind, or the policy is not found.
func LoadPolicyForCheck(ctx context.Context, c client.Client, ac *kueuev1beta2.AdmissionCheck) (*trainingv1alpha1.ResumeReadinessPolicy, error) {
	if ac.Spec.Parameters == nil {
		return nil, fmt.Errorf("AdmissionCheck %q has no spec.parameters", ac.Name)
	}
	if ac.Spec.Parameters.APIGroup != trainingv1alpha1.GroupVersion.Group ||
		ac.Spec.Parameters.Kind != ResumeReadinessPolicyGVK.Kind {
		return nil, fmt.Errorf("AdmissionCheck %q parameters reference %s/%s, expected %s/%s",
			ac.Name,
			ac.Spec.Parameters.APIGroup, ac.Spec.Parameters.Kind,
			trainingv1alpha1.GroupVersion.Group, ResumeReadinessPolicyGVK.Kind)
	}

	policy := &trainingv1alpha1.ResumeReadinessPolicy{}
	if err := c.Get(ctx, types.NamespacedName{Name: ac.Spec.Parameters.Name}, policy); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("ResumeReadinessPolicy %q not found", ac.Spec.Parameters.Name)
		}
		return nil, fmt.Errorf("fetch ResumeReadinessPolicy %q: %w", ac.Spec.Parameters.Name, err)
	}

	return policy, nil
}
