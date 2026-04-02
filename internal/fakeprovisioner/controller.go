package fakeprovisioner

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// Provisioning class names — used in ProvisioningRequestConfig.spec.provisioningClassName.
	ClassDelayedSuccess   = "check-capacity.fake.dev"
	ClassPermanentFailure = "failed.fake.dev"
	ClassBookingExpiry    = "booking-expiry.fake.dev"

	// Default timing.
	DefaultSuccessDelay = 10 * time.Second
	DefaultExpiryDelay  = 60 * time.Second

	// Parameter keys read from spec.parameters.
	ParamDelay          = "fake.dev/delay"
	ParamExpiry         = "fake.dev/expiry"
	ParamFailureMessage = "fake.dev/failure-message"

	// Condition types matching the cluster-autoscaler ProvisioningRequest API.
	ConditionProvisioned     = "Provisioned"
	ConditionFailed          = "Failed"
	ConditionBookingExpired  = "BookingExpired"
	ConditionCapacityRevoked = "CapacityRevoked"
)

// ProvisioningRequestGVK is the GVK for ProvisioningRequest objects.
var ProvisioningRequestGVK = schema.GroupVersionKind{
	Group:   "autoscaling.x-k8s.io",
	Version: "v1beta1",
	Kind:    "ProvisioningRequest",
}

// Action describes the state change the reconciler should apply.
type Action struct {
	// Conditions to set on the ProvisioningRequest status.
	Conditions []metav1.Condition
	// RequeueAfter requests delayed re-reconciliation.
	RequeueAfter time.Duration
	// Done means no status update is needed.
	Done bool
}

// ComputeAction is a pure function that determines the next action for a
// ProvisioningRequest based on its current state. This is the core logic
// and is easily testable without a Kubernetes client.
func ComputeAction(
	className string,
	conditions []metav1.Condition,
	createdAt time.Time,
	params map[string]string,
	now time.Time,
) Action {
	switch className {
	case ClassDelayedSuccess:
		return computeDelayedSuccess(conditions, createdAt, params, now)
	case ClassPermanentFailure:
		return computePermanentFailure(conditions, params, now)
	case ClassBookingExpiry:
		return computeBookingExpiry(conditions, createdAt, params, now)
	default:
		return Action{Done: true}
	}
}

func computeDelayedSuccess(
	conditions []metav1.Condition,
	createdAt time.Time,
	params map[string]string,
	now time.Time,
) Action {
	if HasConditionTrue(conditions, ConditionProvisioned) {
		return Action{Done: true}
	}

	delay := GetParamDuration(params, ParamDelay, DefaultSuccessDelay)
	readyTime := createdAt.Add(delay)

	if now.Before(readyTime) {
		return Action{RequeueAfter: readyTime.Sub(now)}
	}

	cond := metav1.Condition{
		Type:               ConditionProvisioned,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(now),
		Reason:             "FakeBackendProvisioned",
		Message:            "Fake backend: capacity provisioned after delay",
	}
	return Action{Conditions: []metav1.Condition{cond}}
}

func computePermanentFailure(
	conditions []metav1.Condition,
	params map[string]string,
	now time.Time,
) Action {
	if HasConditionTrue(conditions, ConditionFailed) {
		return Action{Done: true}
	}

	msg := GetParamString(params, ParamFailureMessage, "Fake backend: provisioning permanently failed")
	cond := metav1.Condition{
		Type:               ConditionFailed,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(now),
		Reason:             "FakeBackendFailed",
		Message:            msg,
	}
	return Action{Conditions: []metav1.Condition{cond}}
}

func computeBookingExpiry(
	conditions []metav1.Condition,
	createdAt time.Time,
	params map[string]string,
	now time.Time,
) Action {
	// Already revoked — done.
	if HasConditionTrue(conditions, ConditionCapacityRevoked) {
		return Action{Done: true}
	}

	// Already provisioned — check for expiry.
	if HasConditionTrue(conditions, ConditionProvisioned) {
		provCond := FindCondition(conditions, ConditionProvisioned)
		if provCond == nil {
			return Action{Done: true}
		}
		expiry := GetParamDuration(params, ParamExpiry, DefaultExpiryDelay)
		expiryTime := provCond.LastTransitionTime.Time.Add(expiry)

		if now.Before(expiryTime) {
			return Action{RequeueAfter: expiryTime.Sub(now)}
		}

		cond := metav1.Condition{
			Type:               ConditionCapacityRevoked,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(now),
			Reason:             "FakeBackendBookingExpired",
			Message:            "Fake backend: booking expired, capacity revoked",
		}
		return Action{Conditions: []metav1.Condition{cond}}
	}

	// Not yet provisioned — same delay logic as delayed success.
	delay := GetParamDuration(params, ParamDelay, DefaultSuccessDelay)
	readyTime := createdAt.Add(delay)

	if now.Before(readyTime) {
		return Action{RequeueAfter: readyTime.Sub(now)}
	}

	expiry := GetParamDuration(params, ParamExpiry, DefaultExpiryDelay)
	cond := metav1.Condition{
		Type:               ConditionProvisioned,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(now),
		Reason:             "FakeBackendProvisioned",
		Message:            "Fake backend: capacity provisioned (booking will expire)",
	}
	return Action{
		Conditions:   []metav1.Condition{cond},
		RequeueAfter: expiry,
	}
}

// Reconciler reconciles ProvisioningRequest objects for the fake backend.
type Reconciler struct {
	client.Client
}

// Reconcile processes a single ProvisioningRequest.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(ProvisioningRequestGVK)
	if err := r.Get(ctx, req.NamespacedName, pr); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	className := GetProvisioningClassName(pr)
	if className == "" {
		return reconcile.Result{}, nil
	}

	// Skip unknown classes silently.
	if className != ClassDelayedSuccess && className != ClassPermanentFailure && className != ClassBookingExpiry {
		log.V(1).Info("skipping ProvisioningRequest with unknown class", "class", className)
		return reconcile.Result{}, nil
	}

	conditions, err := GetConditions(pr)
	if err != nil {
		return reconcile.Result{}, err
	}

	params := GetParameters(pr)
	now := time.Now()
	action := ComputeAction(className, conditions, pr.GetCreationTimestamp().Time, params, now)

	if action.Done {
		return reconcile.Result{}, nil
	}

	if len(action.Conditions) == 0 {
		return reconcile.Result{RequeueAfter: action.RequeueAfter}, nil
	}

	// Apply condition updates.
	for _, c := range action.Conditions {
		SetCondition(&conditions, c)
	}
	if err := SetConditions(pr, conditions); err != nil {
		return reconcile.Result{}, err
	}

	log.Info("updating ProvisioningRequest conditions",
		"name", req.Name,
		"namespace", req.Namespace,
		"class", className,
		"conditionCount", len(action.Conditions),
	)
	if err := r.Status().Update(ctx, pr); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{RequeueAfter: action.RequeueAfter}, nil
}

// Setup registers the fake provisioner controller with the manager.
func Setup(mgr ctrl.Manager) error {
	r := &Reconciler{
		Client: mgr.GetClient(),
	}

	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(ProvisioningRequestGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(pr).
		Named("fake-provisioner").
		Complete(r)
}
