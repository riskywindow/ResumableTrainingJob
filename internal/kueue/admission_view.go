package kueue

import (
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	"sigs.k8s.io/kueue/pkg/podset"
)

// AdmissionView is an internal read-only snapshot of the Kueue admission
// state for an RTJ. It is consumed by the controller (for status updates and
// checkpoint selection) and will be consumed by the renderer (Phase 3 G2).
type AdmissionView struct {
	// PodSets holds the per-pod-set admission assignments.
	PodSets []PodSetAdmission

	// ClusterQueueName is the name of the ClusterQueue that admitted the RTJ.
	ClusterQueueName string
}

// PodSetAdmission captures the admission assignment for a single pod set.
type PodSetAdmission struct {
	// Name is the pod set name (matches the replicatedJob name).
	Name string

	// Count is the admitted pod count for this pod set.
	Count int32

	// Flavors maps resource type to the assigned ResourceFlavor name.
	// Example: {"nvidia.com/gpu": "a100-80gb"}
	Flavors map[corev1.ResourceName]string

	// NodeSelector from the assigned ResourceFlavor.
	NodeSelector map[string]string

	// Tolerations from the assigned ResourceFlavor.
	Tolerations []corev1.Toleration

	// TopologyAssignment records topology placement if present.
	// Recorded for observability; not yet enforced by the controller.
	TopologyAssignment string
}

// TotalAdmittedCount returns the sum of admitted pod counts across all pod sets.
func (v *AdmissionView) TotalAdmittedCount() int32 {
	if v == nil {
		return 0
	}
	var total int32
	for _, ps := range v.PodSets {
		total += ps.Count
	}
	return total
}

// FlavorsByPodSet returns a map of pod set name to a comma-separated list of
// assigned flavor names. When a pod set has a single resource flavor (the
// common case for GPU training), the value is just the flavor name.
func (v *AdmissionView) FlavorsByPodSet() map[string]string {
	if v == nil || len(v.PodSets) == 0 {
		return nil
	}
	result := make(map[string]string, len(v.PodSets))
	for _, ps := range v.PodSets {
		if len(ps.Flavors) == 0 {
			continue
		}
		names := make([]string, 0, len(ps.Flavors))
		for _, name := range ps.Flavors {
			names = append(names, name)
		}
		sort.Strings(names)
		result[ps.Name] = strings.Join(dedupSorted(names), ",")
	}
	return result
}

// PodSetByName returns the PodSetAdmission for the given name and true,
// or the zero value and false if no match is found.
func (v *AdmissionView) PodSetByName(name string) (PodSetAdmission, bool) {
	if v == nil {
		return PodSetAdmission{}, false
	}
	for _, ps := range v.PodSets {
		if ps.Name == name {
			return ps, true
		}
	}
	return PodSetAdmission{}, false
}

// IsEmpty returns true when no pod set assignments are present.
func (v *AdmissionView) IsEmpty() bool {
	return v == nil || len(v.PodSets) == 0
}

// FromPodSetsInfo builds an AdmissionView from Kueue PodSetInfo slices.
// podSetNames must align 1:1 with infos. This is the path used inside
// RunWithPodSetsInfo when admission mutations are applied.
// Flavor names are not available through PodSetInfo; callers must populate
// Flavors separately if needed.
func FromPodSetsInfo(podSetNames []string, infos []podset.PodSetInfo) *AdmissionView {
	if len(podSetNames) == 0 || len(podSetNames) != len(infos) {
		return nil
	}
	view := &AdmissionView{
		PodSets: make([]PodSetAdmission, len(infos)),
	}
	for i, info := range infos {
		view.PodSets[i] = PodSetAdmission{
			Name:         podSetNames[i],
			Count:        info.Count,
			NodeSelector: copyStringMap(info.NodeSelector),
			Tolerations:  copyTolerations(info.Tolerations),
		}
	}
	return view
}

// FromWorkloadAdmission builds an AdmissionView from a Kueue Workload
// admission status. This is the path used by the controller when it reads
// the Workload object to extract flavor names and counts.
func FromWorkloadAdmission(admission *kueuev1beta2.Admission) *AdmissionView {
	if admission == nil || len(admission.PodSetAssignments) == 0 {
		return nil
	}
	view := &AdmissionView{
		ClusterQueueName: string(admission.ClusterQueue),
		PodSets:          make([]PodSetAdmission, len(admission.PodSetAssignments)),
	}
	for i, psa := range admission.PodSetAssignments {
		ps := PodSetAdmission{
			Name:  string(psa.Name),
			Count: ptr.Deref(psa.Count, 0),
		}
		if len(psa.Flavors) > 0 {
			ps.Flavors = make(map[corev1.ResourceName]string, len(psa.Flavors))
			for resource, flavor := range psa.Flavors {
				ps.Flavors[resource] = string(flavor)
			}
		}
		view.PodSets[i] = ps
	}
	return view
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyTolerations(ts []corev1.Toleration) []corev1.Toleration {
	if ts == nil {
		return nil
	}
	out := make([]corev1.Toleration, len(ts))
	copy(out, ts)
	return out
}

func dedupSorted(s []string) []string {
	if len(s) <= 1 {
		return s
	}
	out := s[:1]
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1] {
			out = append(out, s[i])
		}
	}
	return out
}
