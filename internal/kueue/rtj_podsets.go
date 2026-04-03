package kueue

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/dra"
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

	// Phase 8: when DRA devices are configured, inject DRA resource claims
	// into pod templates so Kueue can account device classes via
	// deviceClassMappings. Template names are computed deterministically
	// from the RTJ name and claim name.
	if job.IsDevicesEnabled() {
		injectDRAIntoPodSets(podSets, job)
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

// injectDRAIntoPodSets adds DRA resource claim references to PodSet pod
// templates for Kueue deviceClassMappings-based accounting. For each claim
// in spec.devices.claims, a PodResourceClaim and container ResourceClaim
// are added to PodSets where at least one container matches.
func injectDRAIntoPodSets(podSets []kueuev1beta2.PodSet, job *trainingv1alpha1.ResumableTrainingJob) {
	if job.Spec.Devices == nil || len(job.Spec.Devices.Claims) == 0 {
		return
	}

	for _, claim := range job.Spec.Devices.Claims {
		templateName := dra.TemplateNameForClaim(job.Name, claim.Name)

		for i := range podSets {
			pod := &podSets[i].Template.Spec

			// Only inject if at least one container matches.
			if !podHasTargetContainer(pod, claim.Containers) {
				continue
			}

			pod.ResourceClaims = append(pod.ResourceClaims, corev1.PodResourceClaim{
				Name:                      claim.Name,
				ResourceClaimTemplateName: ptr.To(templateName),
			})

			for ci := range pod.Containers {
				if isTargetContainer(pod.Containers[ci].Name, claim.Containers) {
					pod.Containers[ci].Resources.Claims = append(
						pod.Containers[ci].Resources.Claims,
						corev1.ResourceClaim{Name: claim.Name},
					)
				}
			}
		}
	}
}

// podHasTargetContainer returns true if at least one container in the pod
// matches the target container list. When targets is empty, returns true
// if there are any containers.
func podHasTargetContainer(pod *corev1.PodSpec, targets []string) bool {
	if len(targets) == 0 {
		return len(pod.Containers) > 0
	}
	for _, t := range targets {
		for _, c := range pod.Containers {
			if c.Name == t {
				return true
			}
		}
	}
	return false
}

// isTargetContainer returns true if the container name appears in the
// target container list. When targets is empty, all containers match.
func isTargetContainer(name string, targets []string) bool {
	if len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		if t == name {
			return true
		}
	}
	return false
}
