package kueue

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	"sigs.k8s.io/kueue/pkg/podset"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

var _ jobframework.GenericJob = (*RTJGenericJob)(nil)

type RTJGenericJob struct {
	job *trainingv1alpha1.ResumableTrainingJob
}

func NewGenericJob() jobframework.GenericJob {
	return &RTJGenericJob{job: &trainingv1alpha1.ResumableTrainingJob{}}
}

func (j *RTJGenericJob) Object() client.Object {
	return j.job
}

func (j *RTJGenericJob) IsSuspended() bool {
	return j.job.IsSuspendedForKueue()
}

func (j *RTJGenericJob) Suspend() {
	j.job.Spec.Suspend = ptr.To(true)
	j.job.SyncKueueLabels()
}

func (j *RTJGenericJob) RunWithPodSetsInfo(_ context.Context, podSetsInfo []podset.PodSetInfo) error {
	spec, err := j.decodeTemplateSpec()
	if err != nil {
		return err
	}
	if len(podSetsInfo) != len(spec.ReplicatedJobs) {
		return podset.BadPodSetsInfoLenError(len(spec.ReplicatedJobs), len(podSetsInfo))
	}

	admittedCounts := make(map[string]int32, len(spec.ReplicatedJobs))
	for i := range spec.ReplicatedJobs {
		template := &spec.ReplicatedJobs[i].Template.Spec.Template
		if err := podset.Merge(&template.ObjectMeta, &template.Spec, podSetsInfo[i]); err != nil {
			return err
		}
		admittedCounts[spec.ReplicatedJobs[i].Name] = podSetsInfo[i].Count
	}

	// Store admitted pod counts as an annotation for the controller to consume
	// when rendering the child JobSet with the admitted shape.
	if j.job.Annotations == nil {
		j.job.Annotations = map[string]string{}
	}
	raw, err := json.Marshal(admittedCounts)
	if err != nil {
		return err
	}
	j.job.Annotations[rtjjobset.AdmittedPodSetsAnnotation] = string(raw)

	j.job.Spec.Suspend = ptr.To(false)
	j.job.SyncKueueLabels()
	return j.encodeTemplateSpec(spec)
}

func (j *RTJGenericJob) RestorePodSetsInfo(podSetsInfo []podset.PodSetInfo) bool {
	if len(podSetsInfo) == 0 {
		return false
	}

	spec, err := j.decodeTemplateSpec()
	if err != nil || len(podSetsInfo) != len(spec.ReplicatedJobs) {
		return false
	}

	changed := false
	for i := range spec.ReplicatedJobs {
		template := &spec.ReplicatedJobs[i].Template.Spec.Template
		changed = podset.RestorePodSpec(&template.ObjectMeta, &template.Spec, podSetsInfo[i]) || changed
	}
	if !changed {
		return false
	}
	if err := j.encodeTemplateSpec(spec); err != nil {
		return false
	}
	return true
}

func (j *RTJGenericJob) Finished(_ context.Context) (string, bool, bool) {
	switch j.job.Status.Phase {
	case trainingv1alpha1.PhaseSucceeded:
		return j.job.Status.Message, true, true
	case trainingv1alpha1.PhaseFailed:
		return j.job.Status.Message, false, true
	default:
		return "", false, false
	}
}

func (j *RTJGenericJob) PodSets(_ context.Context) ([]kueuev1beta2.PodSet, error) {
	return PodSetsFromRTJTemplate(j.job)
}

func (j *RTJGenericJob) IsActive() bool {
	switch j.job.Status.Phase {
	case trainingv1alpha1.PhaseStarting,
		trainingv1alpha1.PhaseRunning,
		trainingv1alpha1.PhaseYieldRequested,
		trainingv1alpha1.PhaseDraining,
		trainingv1alpha1.PhaseRestoring:
		return true
	default:
		return false
	}
}

func (j *RTJGenericJob) PodsReady(_ context.Context) bool {
	return j.job.Status.Phase == trainingv1alpha1.PhaseRunning || j.job.Status.Phase == trainingv1alpha1.PhaseSucceeded
}

func (j *RTJGenericJob) GVK() schema.GroupVersionKind {
	return GroupVersionKind
}

// PriorityClass returns the WorkloadPriorityClass name for the RTJ. This tells
// Kueue's GenericJob reconciler which priority class to resolve when creating
// the Workload. Kueue sets Workload.Spec.Priority from the class value at
// creation time.
//
// Phase 5 note: The RTJ controller owns effective priority materialization.
// After Kueue creates the Workload with the base priority from the class,
// the RTJ controller patches Workload.Spec.Priority with the effective
// priority computed by the checkpoint-aware decision engine. Kueue's
// GenericJob reconciler does not overwrite Spec.Priority on subsequent
// reconciles—it only sets it at creation time from the WorkloadPriorityClass.
func (j *RTJGenericJob) PriorityClass() string {
	return j.job.Spec.WorkloadPriorityClassName
}

func (j *RTJGenericJob) decodeTemplateSpec() (rtjjobset.Spec, error) {
	return rtjjobset.ParseTemplate(j.job.Spec.Runtime.Template.Spec)
}

func (j *RTJGenericJob) encodeTemplateSpec(spec rtjjobset.Spec) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	j.job.Spec.Runtime.Template.Spec = runtime.RawExtension{Raw: raw}
	return nil
}
