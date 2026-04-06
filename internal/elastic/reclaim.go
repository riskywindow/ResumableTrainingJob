package elastic

import (
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// ComputeReclaimDelta determines the reclaimablePods delta for a given
// plan output and worker PodSet name. Returns a zero-count delta when
// no reclaim is needed.
func ComputeReclaimDelta(plan PlanOutput, workerPodSetName string) ReclaimDelta {
	if plan.Kind != PlanShrinkInPlace {
		return ReclaimDelta{PodSetName: workerPodSetName, Count: 0}
	}
	return ReclaimDelta{
		PodSetName: workerPodSetName,
		Count:      plan.ReclaimableWorkerDelta,
	}
}

// BuildReclaimablePods constructs the Kueue ReclaimablePod slice from a
// ReclaimDelta. When the delta count is zero, returns nil (clearing any
// previous reclaimablePods entry).
func BuildReclaimablePods(delta ReclaimDelta) []kueuev1beta2.ReclaimablePod {
	if delta.Count <= 0 {
		return nil
	}
	return []kueuev1beta2.ReclaimablePod{
		{
			Name:  kueuev1beta2.NewPodSetReference(delta.PodSetName),
			Count: delta.Count,
		},
	}
}

// ClearReclaimablePods returns nil, used to explicitly clear
// reclaimablePods on the Workload status after a resize completes.
func ClearReclaimablePods() []kueuev1beta2.ReclaimablePod {
	return nil
}

// ReclaimDeltaFromExisting computes a ReclaimDelta from an existing
// Workload's reclaimablePods for the given PodSet name. Useful for
// detecting whether reclaimablePods are already published.
func ReclaimDeltaFromExisting(reclaimablePods []kueuev1beta2.ReclaimablePod, workerPodSetName string) ReclaimDelta {
	for _, rp := range reclaimablePods {
		if string(rp.Name) == workerPodSetName {
			return ReclaimDelta{
				PodSetName: workerPodSetName,
				Count:      rp.Count,
			}
		}
	}
	return ReclaimDelta{PodSetName: workerPodSetName, Count: 0}
}

// NeedsReclaimUpdate returns true when the desired ReclaimDelta differs
// from the existing Workload reclaimablePods state. This avoids
// unnecessary status patches.
func NeedsReclaimUpdate(desired ReclaimDelta, existing []kueuev1beta2.ReclaimablePod) bool {
	current := ReclaimDeltaFromExisting(existing, desired.PodSetName)
	return desired.Count != current.Count
}
