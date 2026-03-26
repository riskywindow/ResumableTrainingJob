package kueue

import (
	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

const (
	// JobCompletionIndexLabel is the Kubernetes label for indexed-job pod
	// completion index. Used as PodSetTopologyRequest.PodIndexLabel for
	// JobSet-based workloads.
	JobCompletionIndexLabel = "kubernetes.io/job-completion-index"

	// JobSetJobIndexLabel is the JobSet label for replicated job index.
	// Used as PodSetTopologyRequest.SubGroupIndexLabel so Kueue understands
	// the sub-group structure within a PodSet.
	JobSetJobIndexLabel = "jobset.sigs.k8s.io/job-index"

	// topologyColocationGroup is the PodSetGroupName shared by leader and
	// worker PodSets when LeaderWorkerColocation is requested. This ensures
	// Kueue assigns them to the same ResourceFlavor and topology domain.
	topologyColocationGroup = "rtj-topology-group"
)

// applyTopologyRequests populates TopologyRequest on each PodSet based on the
// RTJ topology spec. The worker PodSet always receives a topology request when
// topology is active. Non-worker PodSets receive a topology request only when
// LeaderWorkerColocation is true, and all topology-aware PodSets share a
// PodSetGroupName to ensure co-placement.
func applyTopologyRequests(
	podSets []kueuev1beta2.PodSet,
	topology *trainingv1alpha1.TopologySpec,
	workerPodSetName string,
	spec *rtjjobset.Spec,
) {
	if topology == nil || topology.Mode == trainingv1alpha1.TopologyModeDisabled {
		return
	}

	for i := range podSets {
		psName := string(podSets[i].Name)
		isWorker := psName == workerPodSetName

		// Only the worker PodSet gets topology by default.
		// Non-worker PodSets get topology only when colocation is requested.
		if !isWorker && !topology.LeaderWorkerColocation {
			continue
		}

		rj := findReplicatedJob(spec, psName)
		podSets[i].TopologyRequest = buildTopologyRequest(topology, rj)

		// When colocation is active, group PodSets so Kueue assigns them
		// the same ResourceFlavor and topology domain.
		if topology.LeaderWorkerColocation {
			podSets[i].TopologyRequest.PodSetGroupName = ptr.To(topologyColocationGroup)
		}
	}
}

// buildTopologyRequest constructs a PodSetTopologyRequest from the RTJ
// topology spec and the replicatedJob metadata. The topology mode maps to:
//
//   - Required      → PodSetTopologyRequest.Required = topologyLevel
//   - Preferred     → PodSetTopologyRequest.Preferred = topologyLevel
//   - Unconstrained → PodSetTopologyRequest.Unconstrained = true
//
// SubGroup metadata from the replicatedJob is included so Kueue can make
// topology-aware placement decisions that respect the JobSet sub-group
// structure (one sub-group per replicated Job).
func buildTopologyRequest(
	topology *trainingv1alpha1.TopologySpec,
	rj *rtjjobset.ReplicatedJob,
) *kueuev1beta2.PodSetTopologyRequest {
	req := &kueuev1beta2.PodSetTopologyRequest{
		PodIndexLabel: ptr.To(JobCompletionIndexLabel),
	}

	// Populate sub-group metadata from the replicatedJob replica count.
	if rj != nil {
		replicas := ptr.Deref(rj.Replicas, 1)
		if replicas > 0 {
			req.SubGroupIndexLabel = ptr.To(JobSetJobIndexLabel)
			req.SubGroupCount = ptr.To(replicas)
		}
	}

	switch topology.Mode {
	case trainingv1alpha1.TopologyModeRequired:
		req.Required = ptr.To(topology.TopologyLevel)
	case trainingv1alpha1.TopologyModePreferred:
		req.Preferred = ptr.To(topology.TopologyLevel)
	case trainingv1alpha1.TopologyModeUnconstrained:
		req.Unconstrained = ptr.To(true)
	}

	return req
}

// findReplicatedJob returns a pointer to the replicatedJob with the given
// name, or nil if not found.
func findReplicatedJob(spec *rtjjobset.Spec, name string) *rtjjobset.ReplicatedJob {
	for i := range spec.ReplicatedJobs {
		if spec.ReplicatedJobs[i].Name == name {
			return &spec.ReplicatedJobs[i]
		}
	}
	return nil
}
