package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

const pausePollInterval = 2 * time.Second

type stopSource string

const (
	stopSourceManual stopSource = "manual"
	stopSourceKueue  stopSource = "kueue"
)

func (r *ResumableTrainingJobReconciler) now() metav1.Time {
	if r.Now != nil {
		return r.Now()
	}
	return metav1.Now()
}

func (r *ResumableTrainingJobReconciler) checkpointCatalog() checkpoints.Catalog {
	if r.Catalog != nil {
		return r.Catalog
	}
	catalog, err := checkpoints.NewCatalogFromEnv(r.Metrics)
	if err != nil {
		return &checkpoints.NoopCatalog{}
	}
	r.Catalog = catalog
	return r.Catalog
}

func (r *ResumableTrainingJobReconciler) requestedStopSource(
	job *trainingv1alpha1.ResumableTrainingJob,
	activeExists bool,
) (stopSource, bool) {
	if !activeExists {
		return "", false
	}
	if job.IsSuspendedForKueue() {
		return stopSourceKueue, true
	}
	if job.Spec.Control == nil || job.Spec.Control.DesiredState != trainingv1alpha1.DesiredStateRunning {
		return stopSourceManual, true
	}
	return "", false
}

func (r *ResumableTrainingJobReconciler) reconcileManualHold(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) (ctrl.Result, error) {
	if job.Status.LastCompletedCheckpoint != nil && job.Status.PauseRequestID != "" {
		if markPaused(job, now) {
			if err := r.Status().Update(ctx, job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	if job.Status.Phase == trainingv1alpha1.PhaseFailed {
		return ctrl.Result{}, nil
	}
	if markPendingPaused(job, now) {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *ResumableTrainingJobReconciler) reconcileStopFlow(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	activeJobSet *unstructured.Unstructured,
	source stopSource,
	now metav1.Time,
) (ctrl.Result, error) {
	requestID, requestTime, statusChanged, err := r.ensureStopRequested(ctx, job, source, now)
	if err != nil {
		return ctrl.Result{}, err
	}
	if statusChanged {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: pausePollInterval}, nil
	}

	if err := r.writePauseControl(ctx, job, requestID, requestTime); err != nil {
		return r.failLaunch(ctx, job, fmt.Sprintf("update pause control ConfigMap: %v", err))
	}

	if requestTime.Add(job.Spec.Checkpoint.MaxDrainTime.Duration).Before(now.Time) {
		if err := r.cleanupActiveJobSet(ctx, activeJobSet); err != nil {
			return ctrl.Result{}, err
		}
		if r.Metrics != nil {
			r.Metrics.IncPauseTimeout()
		}
		message := fmt.Sprintf(
			"Stop request %s for run attempt %d exceeded maxDrainTime %s before a newer yield marker and checkpoint manifest were observed.",
			requestID,
			job.Status.CurrentRunAttempt,
			job.Spec.Checkpoint.MaxDrainTime.Duration,
		)
		changed := setCondition(job, conditionTypeDegraded, metav1.ConditionTrue, reasonDrainTimedOut, message, now)
		changed = markFailed(job, reasonDrainTimedOut, message, now) || changed
		if changed {
			if err := r.Status().Update(ctx, job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	observation, ready, err := r.checkpointCatalog().ObservePause(
		ctx,
		job.Spec.Checkpoint.StorageURI,
		job.Status.CurrentRunAttempt,
		requestID,
		requestTime,
	)
	if err != nil {
		return r.failLaunch(ctx, job, fmt.Sprintf("observe pause checkpoint state: %v", err))
	}

	if !ready {
		markerURI := checkpoints.YieldMarkerURI(job.Spec.Checkpoint.StorageURI, job.Status.CurrentRunAttempt)
		if markDraining(job, markerURI, source, now) {
			if err := r.Status().Update(ctx, job); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: pausePollInterval}, nil
		}
		return ctrl.Result{RequeueAfter: pausePollInterval}, nil
	}

	if job.Status.LastCompletedCheckpoint == nil || job.Status.LastCompletedCheckpoint.ManifestURI != observation.Checkpoint.ManifestURI {
		lastCompleted := observation.Checkpoint
		selected := observation.Checkpoint
		job.Status.LastCompletedCheckpoint = &lastCompleted
		job.Status.SelectedCheckpoint = &selected
		completedAt := observation.CompletedAt
		job.Status.TransitionTimestamps.LastCheckpointCompletedAt = &completedAt
		if source == stopSourceKueue && r.Metrics != nil {
			r.Metrics.IncPreemptionYieldCompleted()
		}
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: pausePollInterval}, nil
	}

	if err := r.cleanupActiveJobSet(ctx, activeJobSet); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: pausePollInterval}, nil
}

func (r *ResumableTrainingJobReconciler) ensureStopRequested(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	source stopSource,
	now metav1.Time,
) (string, time.Time, bool, error) {
	requestID := job.Status.PauseRequestID
	requestTime := now.Time
	changed := false

	if requestID == "" {
		requestID = fmt.Sprintf("%s-run-%d-gen-%d", stopRequestPrefix(source), job.Status.CurrentRunAttempt, job.Generation)
		changed = markStopRequested(job, requestID, source, now) || changed
		if r.Metrics != nil {
			if source == stopSourceKueue {
				r.Metrics.IncKueueSuspensionObserved()
			} else {
				r.Metrics.IncPauseRequested()
			}
		}
	}

	if job.Status.TransitionTimestamps.YieldRequestedAt != nil {
		requestTime = job.Status.TransitionTimestamps.YieldRequestedAt.Time
	}
	if job.Status.ActiveControlConfigMapName == "" {
		job.Status.ActiveControlConfigMapName = rtjjobset.ControlConfigMapName(job.Name, job.Status.CurrentRunAttempt)
		changed = true
	}
	if job.Status.Phase != trainingv1alpha1.PhaseYieldRequested && job.Status.Phase != trainingv1alpha1.PhaseDraining {
		changed = markStopRequested(job, requestID, source, now) || changed
	}

	_ = ctx
	return requestID, requestTime, changed, nil
}

func stopRequestPrefix(source stopSource) string {
	if source == stopSourceKueue {
		return "kueue-suspend"
	}
	return "pause"
}

func (r *ResumableTrainingJobReconciler) writePauseControl(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	requestID string,
	requestTime time.Time,
) error {
	var controlConfigMap corev1.ConfigMap
	if err := r.Get(ctx, clientObjectKey(job.Namespace, job.Status.ActiveControlConfigMapName), &controlConfigMap); err != nil {
		return err
	}

	payload := controlPayload(trainingv1alpha1.DesiredStatePaused, requestID, requestTime)
	if controlConfigMap.Data == nil {
		controlConfigMap.Data = map[string]string{}
	}
	if controlConfigMap.Data[rtjjobset.ControlConfigKey] == payload {
		return nil
	}
	controlConfigMap.Data[rtjjobset.ControlConfigKey] = payload
	return r.Update(ctx, &controlConfigMap)
}

func controlPayload(desiredState trainingv1alpha1.DesiredState, requestID string, updatedAt time.Time) string {
	payload := map[string]string{"desiredState": string(desiredState)}
	if requestID != "" {
		payload["requestId"] = requestID
	}
	if !updatedAt.IsZero() {
		payload["updatedAt"] = updatedAt.UTC().Format(time.RFC3339)
	}
	rawPayload, _ := json.Marshal(payload)
	return string(rawPayload) + "\n"
}

func (r *ResumableTrainingJobReconciler) cleanupActiveJobSet(ctx context.Context, activeJobSet *unstructured.Unstructured) error {
	if activeJobSet == nil || activeJobSet.GetName() == "" {
		return nil
	}
	if !activeJobSet.GetDeletionTimestamp().IsZero() {
		return nil
	}
	if err := r.Delete(ctx, activeJobSet); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func clientObjectKey(namespace, name string) client.ObjectKey {
	return client.ObjectKey{Namespace: namespace, Name: name}
}
