package resume

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// AdmissionCheckReconciler watches AdmissionCheck objects whose
// spec.controllerName matches ControllerName. It maintains the Active
// condition on each AdmissionCheck to signal Kueue that this controller
// is healthy and its referenced ResumeReadinessPolicy exists.
type AdmissionCheckReconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=admissionchecks,verbs=get;list;watch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=admissionchecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumereadinesspolicies,verbs=get;list;watch

func (r *AdmissionCheckReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx).WithValues("admissionCheck", req.NamespacedName)

	ac := &kueuev1beta2.AdmissionCheck{}
	if err := r.Client.Get(ctx, req.NamespacedName, ac); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Only process AdmissionChecks that target this controller.
	if ac.Spec.ControllerName != ControllerName {
		return reconcile.Result{}, nil
	}

	log.V(1).Info("reconciling AdmissionCheck")

	newCondition := r.evaluateActiveCondition(ctx, ac)

	// Check if the condition already exists and is unchanged.
	existing := meta.FindStatusCondition(ac.Status.Conditions, ConditionActive)
	if existing != nil && existing.Status == newCondition.Status &&
		existing.Reason == newCondition.Reason &&
		existing.Message == newCondition.Message {
		return reconcile.Result{}, nil
	}

	// Update the Active condition on the AdmissionCheck status.
	origStatus := ac.Status.DeepCopy()
	meta.SetStatusCondition(&ac.Status.Conditions, newCondition)

	if !equality.Semantic.DeepEqual(origStatus, &ac.Status) {
		if err := r.Client.Status().Update(ctx, ac); err != nil {
			return reconcile.Result{}, fmt.Errorf("update AdmissionCheck status: %w", err)
		}
		log.Info("updated AdmissionCheck Active condition",
			"status", newCondition.Status,
			"reason", newCondition.Reason,
		)
	}

	return reconcile.Result{}, nil
}

// evaluateActiveCondition determines the Active condition based on whether
// the referenced ResumeReadinessPolicy exists and is valid.
func (r *AdmissionCheckReconciler) evaluateActiveCondition(ctx context.Context, ac *kueuev1beta2.AdmissionCheck) metav1.Condition {
	now := metav1.Now()

	// Check that parameters reference exists and points to ResumeReadinessPolicy.
	if ac.Spec.Parameters == nil {
		return metav1.Condition{
			Type:               ConditionActive,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonParametersMissing,
			Message:            "AdmissionCheck has no spec.parameters reference",
			LastTransitionTime: now,
		}
	}

	if ac.Spec.Parameters.APIGroup != trainingv1alpha1.GroupVersion.Group ||
		ac.Spec.Parameters.Kind != ResumeReadinessPolicyGVK.Kind {
		return metav1.Condition{
			Type:               ConditionActive,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonParametersMissing,
			Message:            fmt.Sprintf("AdmissionCheck parameters reference must be %s/%s, got %s/%s", trainingv1alpha1.GroupVersion.Group, ResumeReadinessPolicyGVK.Kind, ac.Spec.Parameters.APIGroup, ac.Spec.Parameters.Kind),
			LastTransitionTime: now,
		}
	}

	// Verify the referenced ResumeReadinessPolicy exists.
	policy := &trainingv1alpha1.ResumeReadinessPolicy{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: ac.Spec.Parameters.Name}, policy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.Condition{
				Type:               ConditionActive,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonPolicyNotFound,
				Message:            fmt.Sprintf("ResumeReadinessPolicy %q not found", ac.Spec.Parameters.Name),
				LastTransitionTime: now,
			}
		}
		return metav1.Condition{
			Type:               ConditionActive,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonPolicyNotFound,
			Message:            fmt.Sprintf("error fetching ResumeReadinessPolicy %q: %v", ac.Spec.Parameters.Name, err),
			LastTransitionTime: now,
		}
	}

	return metav1.Condition{
		Type:               ConditionActive,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonControllerReady,
		Message:            fmt.Sprintf("Controller is active, policy %q found", policy.Name),
		LastTransitionTime: now,
	}
}

// SetupWithManager registers the AdmissionCheck reconciler with the manager.
func (r *AdmissionCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kueuev1beta2.AdmissionCheck{}).
		Named("resume-readiness-admissioncheck").
		Complete(r)
}
