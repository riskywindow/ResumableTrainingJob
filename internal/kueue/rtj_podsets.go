package kueue

import (
	"fmt"

	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// experimentalPartialAdmissionEnabled is the operator-level gate for the
// experimental partial-admission path. It must be explicitly enabled via
// --enable-experimental-partial-admission. Even when enabled, each RTJ must
// also opt in via spec.parallelism.enablePartialAdmission.
var experimentalPartialAdmissionEnabled bool

// SetExperimentalPartialAdmission sets the operator-level gate for partial admission.
func SetExperimentalPartialAdmission(enabled bool) {
	experimentalPartialAdmissionEnabled = enabled
}

// ExperimentalPartialAdmissionEnabled returns the current operator-level gate state.
func ExperimentalPartialAdmissionEnabled() bool {
	return experimentalPartialAdmissionEnabled
}

// PodSetsFromRTJTemplate derives the Kueue workload shape directly from the
// RTJ's embedded JobSet template without creating the child runtime object.
func PodSetsFromRTJTemplate(job *trainingv1alpha1.ResumableTrainingJob) ([]kueuev1beta2.PodSet, error) {
	if job == nil {
		return nil, fmt.Errorf("resumable training job is nil")
	}

	spec, err := rtjjobset.ParseTemplate(job.Spec.Runtime.Template.Spec)
	if err != nil {
		return nil, err
	}

	podSets := make([]kueuev1beta2.PodSet, len(spec.ReplicatedJobs))
	for i, replicatedJob := range spec.ReplicatedJobs {
		if replicatedJob.Name == "" {
			return nil, fmt.Errorf("replicatedJobs[%d].name is required", i)
		}
		podSets[i] = kueuev1beta2.PodSet{
			Name:     kueuev1beta2.NewPodSetReference(replicatedJob.Name),
			Template: *replicatedJob.Template.Spec.Template.DeepCopy(),
			Count:    podsCount(&replicatedJob),
		}
	}

	// Phase 3: when parallelism is configured, override the worker pod set's
	// count with the effective preferred count and optionally set MinCount
	// for partial admission.
	if job.Spec.Parallelism != nil {
		workerName := resolveWorkerPodSetName(job, &spec)
		for i := range podSets {
			if string(podSets[i].Name) == workerName {
				podSets[i].Count = job.EffectivePreferredCount()
				if experimentalPartialAdmissionEnabled {
					podSets[i].MinCount = job.EffectiveMinCount()
				}
				break
			}
		}
	}

	// Phase 4: when topology is enabled, apply topology requests to PodSets.
	// The worker PodSet gets a TopologyRequest matching the RTJ topology mode.
	// Non-worker PodSets get topology only when LeaderWorkerColocation is true.
	if job.IsTopologyEnabled() {
		workerName := resolveWorkerPodSetName(job, &spec)
		applyTopologyRequests(podSets, job.Spec.Topology, workerName, &spec)
	}

	return podSets, nil
}

// resolveWorkerPodSetName returns the scalable worker pod set name.
// Defaults to the first replicatedJob if not explicitly configured.
func resolveWorkerPodSetName(job *trainingv1alpha1.ResumableTrainingJob, spec *rtjjobset.Spec) string {
	name := job.EffectivePodSetName()
	if name == "" && len(spec.ReplicatedJobs) > 0 {
		name = spec.ReplicatedJobs[0].Name
	}
	return name
}

func podsCountPerReplica(rj *rtjjobset.ReplicatedJob) int32 {
	spec := &rj.Template.Spec
	jobPodsCount := ptr.Deref(spec.Parallelism, 1)
	if completions := ptr.Deref(spec.Completions, jobPodsCount); completions < jobPodsCount {
		jobPodsCount = completions
	}
	return jobPodsCount
}

func podsCount(rj *rtjjobset.ReplicatedJob) int32 {
	return ptr.Deref(rj.Replicas, 1) * podsCountPerReplica(rj)
}
