package multikueue

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// -------------------------------------------------------------------------
// GVK and framework identity
// -------------------------------------------------------------------------

// RTJGroupVersionKind is the GVK for ResumableTrainingJob used by the
// MultiKueue external-framework adapter.
var RTJGroupVersionKind = trainingv1alpha1.GroupVersion.WithKind("ResumableTrainingJob")

// RTJGroupVersionResource is the GVR for ResumableTrainingJob, used for
// RBAC rule generation.
var RTJGroupVersionResource = trainingv1alpha1.GroupVersion.WithResource("resumabletrainingjobs")

// FormatExternalFrameworkName builds the Kueue external-framework name from
// a GVK using the <Kind>.<version>.<group> convention.
func FormatExternalFrameworkName(gvk schema.GroupVersionKind) string {
	return fmt.Sprintf("%s.%s.%s", gvk.Kind, gvk.Version, gvk.Group)
}

// -------------------------------------------------------------------------
// Dispatch readiness checks
// -------------------------------------------------------------------------

// IsRTJEligibleForMultiKueue checks whether an RTJ has the required fields set
// to be dispatched by MultiKueue. Specifically:
//   - spec.managedBy must be set to the MultiKueue controller name.
//
// Returns true and an empty reason when eligible, or false and a human-readable
// reason when not eligible.
func IsRTJEligibleForMultiKueue(job *trainingv1alpha1.ResumableTrainingJob) (bool, string) {
	if job == nil {
		return false, "RTJ is nil"
	}
	if job.Spec.ManagedBy == "" {
		return false, "spec.managedBy is not set; RTJ follows the single-cluster Phase 5 path"
	}
	if job.Spec.ManagedBy != trainingv1alpha1.MultiKueueControllerName {
		return false, fmt.Sprintf(
			"spec.managedBy is %q, not the MultiKueue controller %q",
			job.Spec.ManagedBy, trainingv1alpha1.MultiKueueControllerName,
		)
	}
	return true, ""
}

// -------------------------------------------------------------------------
// Remote object expectations
// -------------------------------------------------------------------------

// RemoteObjectExpectations documents what the MultiKueue generic adapter does
// when creating a remote RTJ copy on a worker cluster:
//
//  1. Deep-copies the manager-side RTJ as an unstructured object.
//  2. Clears resourceVersion.
//  3. Removes spec.managedBy so the worker-side Kueue and RTJ operator take
//     ownership without MultiKueue delegation.
//  4. Adds the prebuilt-workload-name label (for associating the remote RTJ
//     with a pre-created Workload on the worker).
//  5. Adds the multikueue-origin label (for identifying the originating
//     manager cluster and enabling garbage collection).
//
// Status mirroring flows in the opposite direction: the adapter copies the
// entire .status from the remote RTJ to the manager-side RTJ via an
// unstructured status patch.
//
// The RTJ operator does NOT need to implement any of these steps. They are
// handled entirely by Kueue's externalframeworks.Adapter.
type RemoteObjectExpectations struct{}

// -------------------------------------------------------------------------
// RBAC requirements
// -------------------------------------------------------------------------

// ManagerRBACRules returns the RBAC verbs required on the manager cluster for
// the Kueue controller to manage RTJ objects via the MultiKueue generic adapter.
//
// The manager-side Kueue controller needs:
//   - get, list, watch: to observe manager-side RTJ objects and check managedBy
//   - update/patch status: to mirror remote status back to the manager RTJ
//
// The worker-side Kueue controller needs (via the remote client):
//   - create: to create the remote RTJ copy
//   - get: to read the remote RTJ for status sync
//   - delete: to clean up the remote RTJ on completion/deletion
//   - list, watch: for the optional MultiKueueWatcher to receive events
type ManagerRBACRules struct {
	// Group is the API group for RTJ.
	Group string
	// Resource is the plural resource name.
	Resource string
	// ManagerVerbs are the RBAC verbs needed on the manager cluster.
	ManagerVerbs []string
	// WorkerVerbs are the RBAC verbs needed on the worker cluster (via
	// the MultiKueueCluster remote client).
	WorkerVerbs []string
	// StatusSubresource indicates that status update/patch is also required.
	StatusSubresource bool
}

// RTJManagerRBACRules returns the RBAC rules for RTJ MultiKueue dispatch.
func RTJManagerRBACRules() ManagerRBACRules {
	return ManagerRBACRules{
		Group:             trainingv1alpha1.GroupVersion.Group,
		Resource:          "resumabletrainingjobs",
		ManagerVerbs:      []string{"get", "list", "watch", "update", "patch"},
		WorkerVerbs:       []string{"get", "list", "watch", "create", "delete"},
		StatusSubresource: true,
	}
}
