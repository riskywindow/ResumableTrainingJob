package jobset

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/example/checkpoint-native-preemption-controller/internal/provisioning"
)

// PodSetUpdateConflict describes a conflict between an existing rendered value
// and a podSetUpdate from an AdmissionCheck.
type PodSetUpdateConflict struct {
	// PodSetName is the name of the PodSet with the conflict.
	PodSetName string

	// Field describes the conflicting field (e.g., "nodeSelector", "labels").
	Field string

	// Key is the specific map key that conflicts.
	Key string

	// ExistingValue is the value already present on the rendered JobSet.
	ExistingValue string

	// UpdateValue is the value from the podSetUpdate.
	UpdateValue string
}

func (c PodSetUpdateConflict) String() string {
	return fmt.Sprintf("PodSet %q: %s[%q] existing=%q update=%q",
		c.PodSetName, c.Field, c.Key, c.ExistingValue, c.UpdateValue)
}

// ApplyPodSetUpdatesResult captures the outcome of applying podSetUpdates
// to a rendered JobSet spec.
type ApplyPodSetUpdatesResult struct {
	// Applied is true when updates were successfully applied.
	Applied bool

	// Conflicts contains any detected conflicts that violate the
	// additive-only rule.
	Conflicts []PodSetUpdateConflict
}

// ConflictMessage returns a human-readable summary of all conflicts.
func (r *ApplyPodSetUpdatesResult) ConflictMessage() string {
	if len(r.Conflicts) == 0 {
		return ""
	}
	var parts []string
	for _, c := range r.Conflicts {
		parts = append(parts, c.String())
	}
	return fmt.Sprintf("podSetUpdate conflicts detected: %s", strings.Join(parts, "; "))
}

// ApplyPodSetUpdates applies AdmissionCheck-suggested podSetUpdates to the
// rendered JobSet spec additively. It modifies the spec in-place.
//
// The additive-only rule:
//   - Labels: new keys are added; existing keys with different values are conflicts.
//   - Annotations: new keys are added; existing keys with different values are conflicts.
//   - NodeSelector: new keys are added; existing keys with different values are conflicts.
//   - Tolerations: new tolerations are appended (no conflict detection for tolerations
//     since they are additive by nature).
//
// When conflicts are detected, the spec is NOT modified for the conflicting fields.
// The caller should fail clearly in status rather than silently overwriting.
func ApplyPodSetUpdates(spec *Spec, updates map[string]provisioning.PodSetUpdateEntry) *ApplyPodSetUpdatesResult {
	result := &ApplyPodSetUpdatesResult{Applied: true}

	if len(updates) == 0 {
		return result
	}

	for i := range spec.ReplicatedJobs {
		rj := &spec.ReplicatedJobs[i]
		update, ok := updates[rj.Name]
		if !ok {
			continue
		}

		pod := podSpec(rj)
		podMeta := &rj.Template.Spec.Template.ObjectMeta

		// Apply labels additively.
		conflicts := applyMapAdditive(podMeta.Labels, update.Labels, rj.Name, "labels")
		if len(conflicts) > 0 {
			result.Conflicts = append(result.Conflicts, conflicts...)
			result.Applied = false
		} else if len(update.Labels) > 0 {
			if podMeta.Labels == nil {
				podMeta.Labels = make(map[string]string)
			}
			for k, v := range update.Labels {
				podMeta.Labels[k] = v
			}
		}

		// Apply annotations additively.
		conflicts = applyMapAdditive(podMeta.Annotations, update.Annotations, rj.Name, "annotations")
		if len(conflicts) > 0 {
			result.Conflicts = append(result.Conflicts, conflicts...)
			result.Applied = false
		} else if len(update.Annotations) > 0 {
			if podMeta.Annotations == nil {
				podMeta.Annotations = make(map[string]string)
			}
			for k, v := range update.Annotations {
				podMeta.Annotations[k] = v
			}
		}

		// Apply nodeSelector additively.
		conflicts = applyMapAdditive(pod.NodeSelector, update.NodeSelector, rj.Name, "nodeSelector")
		if len(conflicts) > 0 {
			result.Conflicts = append(result.Conflicts, conflicts...)
			result.Applied = false
		} else if len(update.NodeSelector) > 0 {
			if pod.NodeSelector == nil {
				pod.NodeSelector = make(map[string]string)
			}
			for k, v := range update.NodeSelector {
				pod.NodeSelector[k] = v
			}
		}

		// Append tolerations (always additive, no conflicts).
		if len(update.Tolerations) > 0 {
			pod.Tolerations = appendUniqueTolerations(pod.Tolerations, update.Tolerations)
		}
	}

	return result
}

// applyMapAdditive checks for conflicts when merging src into dst.
// Returns conflicts for keys that exist in dst with different values.
func applyMapAdditive(
	dst map[string]string,
	src map[string]string,
	podSetName string,
	fieldName string,
) []PodSetUpdateConflict {
	if len(src) == 0 {
		return nil
	}

	var conflicts []PodSetUpdateConflict
	for k, v := range src {
		if existing, ok := dst[k]; ok && existing != v {
			conflicts = append(conflicts, PodSetUpdateConflict{
				PodSetName:    podSetName,
				Field:         fieldName,
				Key:           k,
				ExistingValue: existing,
				UpdateValue:   v,
			})
		}
	}
	return conflicts
}

// appendUniqueTolerations appends tolerations from src to dst, skipping
// exact duplicates.
func appendUniqueTolerations(dst, src []corev1.Toleration) []corev1.Toleration {
	for _, t := range src {
		if !containsToleration(dst, t) {
			dst = append(dst, t)
		}
	}
	return dst
}

// containsToleration checks if a toleration already exists in the slice.
func containsToleration(tolerations []corev1.Toleration, target corev1.Toleration) bool {
	for _, t := range tolerations {
		if t.Key == target.Key &&
			t.Operator == target.Operator &&
			t.Value == target.Value &&
			t.Effect == target.Effect &&
			((t.TolerationSeconds == nil && target.TolerationSeconds == nil) ||
				(t.TolerationSeconds != nil && target.TolerationSeconds != nil &&
					*t.TolerationSeconds == *target.TolerationSeconds)) {
			return true
		}
	}
	return false
}
