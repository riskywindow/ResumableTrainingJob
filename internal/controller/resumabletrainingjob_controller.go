package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
	operatormetrics "github.com/example/checkpoint-native-preemption-controller/internal/metrics"
)

const resumableTrainingJobFinalizer = "training.checkpoint.example.io/finalizer"

// ResumableTrainingJobReconciler reconciles a ResumableTrainingJob object.
type ResumableTrainingJobReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Catalog checkpoints.Catalog
	Metrics *operatormetrics.Recorder
	Now     func() metav1.Time
}

// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch;create;update;patch;delete

func (r *ResumableTrainingJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("resumableTrainingJob", req.NamespacedName)

	var job trainingv1alpha1.ResumableTrainingJob
	if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
		if apierrors.IsNotFound(err) && r.Metrics != nil {
			r.Metrics.RemoveRTJ(req.NamespacedName.String())
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !job.DeletionTimestamp.IsZero() {
		if r.Metrics != nil {
			r.Metrics.RemoveRTJ(req.NamespacedName.String())
		}
		if controllerutil.ContainsFinalizer(&job, resumableTrainingJobFinalizer) {
			controllerutil.RemoveFinalizer(&job, resumableTrainingJobFinalizer)
			if err := r.Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	defer r.observePhase(&job)

	if controllerutil.AddFinalizer(&job, resumableTrainingJobFinalizer) {
		if err := r.Update(ctx, &job); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if job.InitializePhase1Status(r.now()) {
		if err := r.Status().Update(ctx, &job); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	activeJobSet, activeExists, err := r.getActiveJobSet(ctx, &job)
	if err != nil {
		return r.failLaunch(ctx, &job, fmt.Sprintf("lookup child JobSet: %v", err))
	}
	now := r.now()
	if stopSource, shouldStop := r.requestedStopSource(&job, activeExists); shouldStop {
		return r.reconcileStopFlow(ctx, &job, activeJobSet, stopSource, now)
	}
	if job.Spec.Control == nil || job.Spec.Control.DesiredState != trainingv1alpha1.DesiredStateRunning {
		return r.reconcileManualHold(ctx, &job, now)
	}
	if !activeExists && job.IsSuspendedForKueue() {
		if markQueuedForAdmission(&job, now) {
			if err := r.Status().Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("deferring child JobSet creation until RTJ admission is granted")
		return ctrl.Result{}, nil
	}
	if activeExists {
		wasRestoring := job.Status.Phase == trainingv1alpha1.PhaseRestoring
		if markRunning(&job, now) {
			if wasRestoring && r.Metrics != nil {
				r.Metrics.IncResumeSucceeded()
			}
			if err := r.Status().Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("observed active child JobSet", "jobSet", activeJobSet.GetName(), "phase", job.Status.Phase)
		return ctrl.Result{}, nil
	}
	if r.shouldResume(&job) {
		return r.reconcileResume(ctx, &job, now)
	}
	return r.reconcileLaunch(ctx, &job, now)
}

func (r *ResumableTrainingJobReconciler) observePhase(job *trainingv1alpha1.ResumableTrainingJob) {
	if r.Metrics == nil || job == nil || job.Status.Phase == "" {
		return
	}
	r.Metrics.ObservePhase(client.ObjectKeyFromObject(job).String(), string(job.Status.Phase))
}

func createIfMissing(ctx context.Context, c client.Client, obj client.Object) (bool, error) {
	key := client.ObjectKeyFromObject(obj)
	current := obj.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, key, current); err == nil {
		return false, nil
	} else if !apierrors.IsNotFound(err) {
		return false, err
	}
	if err := c.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func buildControlConfigMap(job *trainingv1alpha1.ResumableTrainingJob, name string, runAttempt int32) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: job.Namespace,
			Labels: map[string]string{
				rtjjobset.ManagedByLabelKey:  rtjjobset.ManagedByLabelValue,
				rtjjobset.RTJNameLabelKey:    job.Name,
				rtjjobset.RunAttemptLabelKey: fmt.Sprintf("%d", runAttempt),
			},
		},
		Data: map[string]string{
			rtjjobset.ControlConfigKey: controlPayload(trainingv1alpha1.DesiredStateRunning, "", time.Time{}),
		},
	}
}

func (r *ResumableTrainingJobReconciler) getActiveJobSet(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (*unstructured.Unstructured, bool, error) {
	if job.Status.ActiveJobSetName == "" {
		return nil, false, nil
	}

	child := rtjjobset.NewEmptyChildJobSet(job.Spec.Runtime.Template.APIVersion, job.Spec.Runtime.Template.Kind)
	key := types.NamespacedName{Name: job.Status.ActiveJobSetName, Namespace: job.Namespace}
	if err := r.Get(ctx, key, child); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !child.GetDeletionTimestamp().IsZero() {
		return nil, false, nil
	}
	return child, true, nil
}

func (r *ResumableTrainingJobReconciler) failLaunch(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	message string,
) (ctrl.Result, error) {
	if markFailed(job, reasonLaunchFailed, message, r.now()) {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, errors.New(message)
}

func (r *ResumableTrainingJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trainingv1alpha1.ResumableTrainingJob{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
