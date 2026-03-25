package controller

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

func (r *ResumableTrainingJobReconciler) shouldResume(job *trainingv1alpha1.ResumableTrainingJob) bool {
	if job.Status.Phase == trainingv1alpha1.PhaseRestoring && job.Status.SelectedCheckpoint != nil {
		return true
	}
	if job.Status.LastCompletedCheckpoint == nil {
		return false
	}
	return job.Status.Phase == trainingv1alpha1.PhasePaused || job.Status.Phase == trainingv1alpha1.PhaseQueued
}

func (r *ResumableTrainingJobReconciler) reconcileLaunch(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) (ctrl.Result, error) {
	runAttempt := job.Status.CurrentRunAttempt + 1
	if runAttempt < 1 {
		runAttempt = 1
	}

	controlConfigMapName, childJobSetName, err := r.createRunAttemptResources(ctx, job, runAttempt, checkpoints.ResumeManifestURI(job.Status.SelectedCheckpoint))
	if err != nil {
		return r.failLaunch(ctx, job, err.Error())
	}

	changed := markStarting(job, runAttempt, controlConfigMapName, childJobSetName, now)

	// Phase 3: sync admission status when admitted counts are available.
	admittedCounts := parseAdmittedCounts(job)
	if admittedCounts != nil {
		workerCount := totalAdmittedCount(admittedCounts)
		changed = syncAdmissionStatus(job, workerCount, job.EffectivePreferredCount(), nil) || changed
		if r.Metrics != nil {
			r.Metrics.ObserveAdmissionComparison(workerCount, job.EffectivePreferredCount())
		}
	}

	if changed {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *ResumableTrainingJobReconciler) reconcileResume(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) (ctrl.Result, error) {
	selectedCheckpoint, selected, err := r.resumeCheckpointForAttempt(ctx, job)
	if err != nil {
		if r.Metrics != nil {
			r.Metrics.IncResumeFailed()
		}
		return r.failLaunch(ctx, job, fmt.Sprintf("select resume checkpoint: %v", err))
	}
	if !selected || selectedCheckpoint == nil {
		if r.Metrics != nil {
			r.Metrics.IncResumeFailed()
		}
		message := "No compatible complete checkpoint was available for resume."
		changed := setCondition(job, conditionTypeDegraded, metav1.ConditionTrue, reasonNoCompatibleCheckpoint, message, now)
		changed = markFailed(job, reasonNoCompatibleCheckpoint, message, now) || changed
		if changed {
			if err := r.Status().Update(ctx, job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	runAttempt := job.Status.CurrentRunAttempt + 1
	if job.Status.Phase == trainingv1alpha1.PhaseRestoring && job.Status.ActiveJobSetName != "" {
		runAttempt = job.Status.CurrentRunAttempt
	}
	if runAttempt < 1 {
		runAttempt = 1
	}
	if r.Metrics != nil && job.Status.Phase != trainingv1alpha1.PhaseRestoring {
		r.Metrics.IncResumeAttempted()
	}

	controlConfigMapName, childJobSetName, err := r.createRunAttemptResources(ctx, job, runAttempt, selectedCheckpoint.ManifestURI)
	if err != nil {
		if r.Metrics != nil {
			r.Metrics.IncResumeFailed()
		}
		return r.failLaunch(ctx, job, err.Error())
	}

	changed := markRestoring(job, runAttempt, controlConfigMapName, childJobSetName, selectedCheckpoint, now)

	// Phase 3: sync restore status with checkpoint and admitted world sizes.
	if selectedCheckpoint != nil && selectedCheckpoint.WorldSize > 0 {
		restoreWorldSize := admittedWorldSize(job)
		changed = syncRestoreStatus(job, selectedCheckpoint.WorldSize, restoreWorldSize) || changed
		if r.Metrics != nil {
			r.Metrics.ObserveResumeWorldSize(selectedCheckpoint.WorldSize, restoreWorldSize)
		}
	}

	// Phase 3: sync admission status when admitted counts are available.
	admittedCounts := parseAdmittedCounts(job)
	if admittedCounts != nil {
		workerCount := totalAdmittedCount(admittedCounts)
		changed = syncAdmissionStatus(job, workerCount, job.EffectivePreferredCount(), nil) || changed
		if r.Metrics != nil {
			r.Metrics.ObserveAdmissionComparison(workerCount, job.EffectivePreferredCount())
		}
	}

	if changed {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *ResumableTrainingJobReconciler) resumeCheckpointForAttempt(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (*trainingv1alpha1.CheckpointReference, bool, error) {
	if job.Status.Phase == trainingv1alpha1.PhaseRestoring && job.Status.SelectedCheckpoint != nil {
		selected := *job.Status.SelectedCheckpoint
		return &selected, true, nil
	}
	return r.checkpointCatalog().SelectResumeCheckpoint(ctx, checkpoints.ResumeRequest{
		StorageRootURI:          job.Spec.Checkpoint.StorageURI,
		ClusterIdentity:         rtjjobset.DefaultClusterIdentity,
		RTJIdentity:             job.Name,
		RuntimeMode:             string(job.Spec.Runtime.Mode),
		WorldSize:               admittedWorldSize(job),
		GPUShape:                job.Spec.Identity.GPUShape,
		ImageIdentity:           job.Spec.Identity.Image,
		CodeVersionIdentity:     job.Spec.Identity.CodeVersion,
		OptimizerMode:           job.Spec.Runtime.OptimizerMode,
		ShardingMode:            job.Spec.Runtime.ShardingMode,
		SupportedFormatVersions: []string{checkpoints.SupportedManifestFormatVersion},
		AllowWorldSizeChange:    job.Spec.Resume.AllowWorldSizeChange,
	})
}

func (r *ResumableTrainingJobReconciler) createRunAttemptResources(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	runAttempt int32,
	resumeManifestURI string,
) (string, string, error) {
	controlConfigMapName := rtjjobset.ControlConfigMapName(job.Name, runAttempt)
	childJobSetName := rtjjobset.ChildJobSetName(job.Name, runAttempt)

	controlConfigMap := buildControlConfigMap(job, controlConfigMapName, runAttempt)
	if err := controllerutil.SetControllerReference(job, controlConfigMap, r.Scheme); err != nil {
		return "", "", fmt.Errorf("set control ConfigMap owner reference: %w", err)
	}
	if _, err := createIfMissing(ctx, r.Client, controlConfigMap); err != nil {
		return "", "", fmt.Errorf("create control ConfigMap: %w", err)
	}

	admittedCounts := parseAdmittedCounts(job)
	renderInput := rtjjobset.RenderInput{
		RTJ:                  job,
		RunAttempt:           runAttempt,
		JobSetName:           childJobSetName,
		ControlConfigMapName: controlConfigMapName,
		ResumeManifestURI:    resumeManifestURI,
	}
	if admittedCounts != nil {
		renderInput.AdmittedCounts = admittedCounts
		renderInput.OriginalWorldSize = job.Spec.Identity.WorldSize
		renderInput.AllowWorldSizeChange = job.Spec.Resume.AllowWorldSizeChange
	}
	renderedJobSet, err := rtjjobset.RenderChildJobSetUnstructured(renderInput)
	if err != nil {
		return "", "", fmt.Errorf("render child JobSet: %w", err)
	}
	if err := controllerutil.SetOwnerReference(job, renderedJobSet, r.Scheme); err != nil {
		return "", "", fmt.Errorf("set child JobSet owner reference: %w", err)
	}
	created, err := createIfMissing(ctx, r.Client, renderedJobSet)
	if err != nil {
		return "", "", fmt.Errorf("create child JobSet: %w", err)
	}
	if !created && r.Metrics != nil {
		r.Metrics.IncDuplicateChildJobSetPrevention()
	}

	return controlConfigMapName, childJobSetName, nil
}

// parseAdmittedCounts reads the admitted pod counts annotation set by
// RunWithPodSetsInfo. Returns nil when the annotation is absent or invalid.
func parseAdmittedCounts(job *trainingv1alpha1.ResumableTrainingJob) map[string]int32 {
	raw, ok := job.Annotations[rtjjobset.AdmittedPodSetsAnnotation]
	if !ok || raw == "" {
		return nil
	}
	var counts map[string]int32
	if err := json.Unmarshal([]byte(raw), &counts); err != nil {
		return nil
	}
	return counts
}

// totalAdmittedCount returns the sum of all admitted pod counts.
func totalAdmittedCount(counts map[string]int32) int32 {
	var total int32
	for _, c := range counts {
		total += c
	}
	return total
}

// admittedWorldSize returns the admitted world size (total admitted pod count)
// or falls back to spec.identity.worldSize when no admission data is present.
func admittedWorldSize(job *trainingv1alpha1.ResumableTrainingJob) int32 {
	counts := parseAdmittedCounts(job)
	if counts != nil {
		total := totalAdmittedCount(counts)
		if total > 0 {
			return total
		}
	}
	return job.Spec.Identity.WorldSize
}
