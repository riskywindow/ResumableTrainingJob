package jobset

import (
	"k8s.io/utils/ptr"
)

// applyAdmittedReplicaCount sets the replica count on a ReplicatedJob based on
// the admitted pod count from Kueue. admittedPodCount is the total number of
// pods that Kueue admitted for this pod set. The replica count is derived as
// admittedPodCount / podsPerReplica. This preserves the job parallelism and
// completions settings from the template while adjusting only the replica count.
func applyAdmittedReplicaCount(rj *ReplicatedJob, admittedPodCount int32) {
	if admittedPodCount <= 0 {
		return
	}
	perReplica := podsPerReplica(rj)
	if perReplica <= 0 {
		perReplica = 1
	}
	replicas := admittedPodCount / perReplica
	if replicas < 1 {
		replicas = 1
	}
	rj.Replicas = &replicas
}

// podsPerReplica returns the number of pods produced by a single replica.
// This mirrors the calculation in internal/kueue/rtj_podsets.go:podsCountPerReplica.
func podsPerReplica(rj *ReplicatedJob) int32 {
	spec := &rj.Template.Spec
	parallelism := ptr.Deref(spec.Parallelism, 1)
	completions := ptr.Deref(spec.Completions, parallelism)
	if completions < parallelism {
		return completions
	}
	return parallelism
}

// stripKueuePodTemplateLabels removes Kueue management labels and annotations
// from a ReplicatedJob's pod template metadata. This prevents Kueue-injected
// metadata (from podset.Merge during RunWithPodSetsInfo) from reaching the
// child JobSet's pod templates, keeping the child JobSet a plain runtime resource.
func stripKueuePodTemplateLabels(rj *ReplicatedJob) {
	meta := &rj.Template.Spec.Template.ObjectMeta
	for key := range meta.Labels {
		if isKueueManagementLabel(key) {
			delete(meta.Labels, key)
		}
	}
	for key := range meta.Annotations {
		if isKueueManagementAnnotation(key) {
			delete(meta.Annotations, key)
		}
	}
}
