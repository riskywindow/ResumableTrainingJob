package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/elastic"
)

// reclaimFieldManager is the dedicated SSA field manager for the
// reclaimablePods field on Workload.status. Using a separate field
// manager ensures that our writes to reclaimablePods never conflict
// with Kueue's writes to other status fields (admission, conditions,
// admissionChecks, requeueState, etc.).
//
// Strategy rationale (documented in docs/phase9/elastic-planning.md):
//
// Kueue v0.15.1 uses strategic merge patch for Workload status updates.
// The Workload.status.reclaimablePods field has +listType=map with
// +listMapKey=name, meaning SSA treats each PodSet entry as an
// independently owned item. By using a dedicated field manager
// ("rtj-elastic-reclaim"), our writes are scoped to only the
// reclaimablePods list entries we create. Kueue's field manager
// (typically "kueue-controller" or the admission controller) owns
// all other status fields.
//
// This avoids the two main risks:
//  1. Our patch clobbering Kueue-owned fields (admission, conditions).
//  2. Kueue's patch clobbering our reclaimablePods entries.
//
// The SSA approach is preferred over merge-patch because:
//   - SSA field ownership is explicit and auditable.
//   - No read-modify-write race with Kueue's concurrent status writes.
//   - reclaimablePods is a map-list keyed by name, which SSA handles
//     natively without requiring full-list replacement.
const reclaimFieldManager = "rtj-elastic-reclaim"

// WorkloadReclaimPatchResult captures the outcome of a reclaimablePods
// status patch.
type WorkloadReclaimPatchResult struct {
	// Patched is true when the patch was applied (the desired state
	// differed from the current state).
	Patched bool

	// WorkloadName is the name of the Workload that was patched.
	WorkloadName string
}

// patchWorkloadReclaimablePods applies a server-side apply (SSA) patch
// to the Workload's status.reclaimablePods field using the dedicated
// field manager. This function is idempotent: if the desired state
// matches the current state, no patch is sent.
//
// When reclaimPods is nil, the patch clears any existing reclaimablePods
// entries owned by our field manager.
func (r *ResumableTrainingJobReconciler) patchWorkloadReclaimablePods(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	reclaimPods []kueuev1beta2.ReclaimablePod,
) (WorkloadReclaimPatchResult, error) {
	result := WorkloadReclaimPatchResult{}
	logger := log.FromContext(ctx)

	if job.Status.WorkloadReference == nil {
		return result, fmt.Errorf("workload reference not set on RTJ %s/%s", job.Namespace, job.Name)
	}

	workloadName := job.Status.WorkloadReference.Name
	result.WorkloadName = workloadName

	// Fetch current Workload to check if patch is needed.
	var workload kueuev1beta2.Workload
	key := types.NamespacedName{Name: workloadName, Namespace: job.Namespace}
	if err := r.Get(ctx, key, &workload); err != nil {
		return result, fmt.Errorf("get workload %s: %w", workloadName, err)
	}

	// Check if the current state already matches.
	workerPodSetName := resolveWorkerPodSetNameForJob(job)
	desiredDelta := elastic.ReclaimDelta{PodSetName: workerPodSetName}
	if len(reclaimPods) > 0 {
		desiredDelta.Count = reclaimPods[0].Count
	}
	if !elastic.NeedsReclaimUpdate(desiredDelta, workload.Status.ReclaimablePods) {
		logger.V(1).Info("reclaimablePods already matches desired state, skipping patch",
			"workload", workloadName)
		return result, nil
	}

	// Build the SSA patch payload. We only include the reclaimablePods
	// field so our field manager does not claim ownership of any other
	// status fields.
	patch, err := buildReclaimablePodsSSAPatch(reclaimPods)
	if err != nil {
		return result, fmt.Errorf("build SSA patch: %w", err)
	}

	// Apply the SSA status patch with our dedicated field manager.
	if err := r.Status().Patch(ctx, &workload, client.RawPatch(types.ApplyPatchType, patch),
		client.ForceOwnership, client.FieldOwner(reclaimFieldManager)); err != nil {
		return result, fmt.Errorf("SSA patch reclaimablePods on workload %s: %w", workloadName, err)
	}

	result.Patched = true
	logger.Info("patched Workload reclaimablePods",
		"workload", workloadName,
		"reclaimablePods", reclaimPods,
		"fieldManager", reclaimFieldManager,
	)

	return result, nil
}

// buildReclaimablePodsSSAPatch constructs the JSON payload for an SSA
// status subresource patch that only touches reclaimablePods.
func buildReclaimablePodsSSAPatch(reclaimPods []kueuev1beta2.ReclaimablePod) ([]byte, error) {
	// SSA requires apiVersion and kind in the apply configuration.
	payload := map[string]interface{}{
		"apiVersion": "kueue.x-k8s.io/v1beta2",
		"kind":       "Workload",
		"status": map[string]interface{}{
			"reclaimablePods": reclaimPodsToRaw(reclaimPods),
		},
	}
	return json.Marshal(payload)
}

// reclaimPodsToRaw converts the typed reclaimablePods slice to a raw
// representation suitable for JSON marshaling in an SSA patch.
func reclaimPodsToRaw(pods []kueuev1beta2.ReclaimablePod) interface{} {
	if len(pods) == 0 {
		// Return empty slice to clear the field via SSA.
		return []interface{}{}
	}
	raw := make([]map[string]interface{}, len(pods))
	for i, p := range pods {
		raw[i] = map[string]interface{}{
			"name":  string(p.Name),
			"count": p.Count,
		}
	}
	return raw
}

// clearWorkloadReclaimablePods removes reclaimablePods from the Workload
// status. This is a convenience wrapper around patchWorkloadReclaimablePods
// with a nil slice.
func (r *ResumableTrainingJobReconciler) clearWorkloadReclaimablePods(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (WorkloadReclaimPatchResult, error) {
	return r.patchWorkloadReclaimablePods(ctx, job, nil)
}
