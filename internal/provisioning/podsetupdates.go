package provisioning

import (
	corev1 "k8s.io/api/core/v1"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// PodSetUpdateSet groups podSetUpdates from a single admission check.
type PodSetUpdateSet struct {
	// AdmissionCheckName is the AC that produced these updates.
	AdmissionCheckName string

	// Updates contains the per-pod-set updates from this AC.
	Updates []PodSetUpdateEntry
}

// PodSetUpdateEntry contains the parsed updates for a single pod set
// from a single admission check.
//
// Kueue v0.15.1 fields parsed:
//   - admissionChecks[].podSetUpdates[].name         -> Name
//   - admissionChecks[].podSetUpdates[].labels       -> Labels
//   - admissionChecks[].podSetUpdates[].annotations  -> Annotations
//   - admissionChecks[].podSetUpdates[].nodeSelector -> NodeSelector
//   - admissionChecks[].podSetUpdates[].tolerations  -> Tolerations
type PodSetUpdateEntry struct {
	// Name is the pod set name (matches PodSetReference).
	Name string

	// Labels to merge into pod metadata.
	Labels map[string]string

	// Annotations to merge into pod metadata.
	Annotations map[string]string

	// NodeSelector entries to merge into the pod's nodeSelector.
	NodeSelector map[string]string

	// Tolerations to append to the pod's tolerations.
	Tolerations []corev1.Toleration
}

// ParsePodSetUpdates converts Kueue PodSetUpdate slices into the internal form.
// Returns nil for empty input. All maps and slices are deep-copied.
func ParsePodSetUpdates(updates []kueuev1beta2.PodSetUpdate) []PodSetUpdateEntry {
	if len(updates) == 0 {
		return nil
	}
	entries := make([]PodSetUpdateEntry, len(updates))
	for i, u := range updates {
		entries[i] = PodSetUpdateEntry{
			Name:         string(u.Name),
			Labels:       copyStringMap(u.Labels),
			Annotations:  copyStringMap(u.Annotations),
			NodeSelector: copyStringMap(u.NodeSelector),
			Tolerations:  copyTolerations(u.Tolerations),
		}
	}
	return entries
}

// MergePodSetUpdates merges podSetUpdates from multiple admission checks by
// pod set name. For conflicting map keys, later ACs take precedence.
// Tolerations are concatenated.
func MergePodSetUpdates(sets []PodSetUpdateSet) map[string]PodSetUpdateEntry {
	if len(sets) == 0 {
		return nil
	}

	merged := make(map[string]PodSetUpdateEntry)
	for _, set := range sets {
		for _, entry := range set.Updates {
			existing, ok := merged[entry.Name]
			if !ok {
				existing = PodSetUpdateEntry{
					Name:         entry.Name,
					Labels:       make(map[string]string),
					Annotations:  make(map[string]string),
					NodeSelector: make(map[string]string),
				}
			}
			mergeMapInto(existing.Labels, entry.Labels)
			mergeMapInto(existing.Annotations, entry.Annotations)
			mergeMapInto(existing.NodeSelector, entry.NodeSelector)
			existing.Tolerations = append(existing.Tolerations, entry.Tolerations...)
			merged[entry.Name] = existing
		}
	}
	return merged
}

// HasPodSetUpdates returns true if any AC has non-empty podSetUpdates.
func HasPodSetUpdates(sets []PodSetUpdateSet) bool {
	for _, set := range sets {
		if len(set.Updates) > 0 {
			return true
		}
	}
	return false
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

func mergeMapInto(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}
