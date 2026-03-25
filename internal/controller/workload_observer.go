package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	operatormetrics "github.com/example/checkpoint-native-preemption-controller/internal/metrics"
)

// WorkloadObserver records lightweight metrics from RTJ-owned Kueue Workloads.
type WorkloadObserver struct {
	client.Client
	Metrics *operatormetrics.Recorder
}

// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch

func (r *WorkloadObserver) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("workload", req.NamespacedName)

	var workload kueuev1beta2.Workload
	if err := r.Get(ctx, req.NamespacedName, &workload); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !isRTJOwnedWorkload(&workload) {
		return ctrl.Result{}, nil
	}
	if r.Metrics != nil {
		r.Metrics.ObserveWorkload(&workload)
	}
	logger.V(1).Info("observed RTJ-owned workload for metrics", "uid", workload.UID, "admitted", workload.Status.Admission != nil)
	return ctrl.Result{}, nil
}

func (r *WorkloadObserver) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("rtj-workload-observer").
		For(&kueuev1beta2.Workload{}).
		Complete(r)
}

func isRTJOwnedWorkload(workload *kueuev1beta2.Workload) bool {
	if workload == nil {
		return false
	}
	for _, ref := range workload.OwnerReferences {
		if ref.APIVersion == trainingv1alpha1.GroupVersion.String() &&
			ref.Kind == "ResumableTrainingJob" &&
			ref.UID != types.UID("") {
			return true
		}
	}
	return false
}
