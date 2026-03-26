package controller

import (
	"encoding/json"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
	"github.com/example/checkpoint-native-preemption-controller/internal/topology"
)

// LaunchPlan captures the computed launch parameters derived from admission
// decisions, topology assignments, and checkpoint state. It is used to
// populate RenderInput and the RTJ status fields.
type LaunchPlan struct {
	// AdmittedCounts maps PodSet name to admitted pod count.
	AdmittedCounts map[string]int32

	// AdmittedFlavors maps PodSet name to the ResourceFlavor name.
	AdmittedFlavors map[string]string

	// TopologyResult is the parsed topology assignment.
	TopologyResult *topology.ParseResult

	// WorkerCount is the effective number of worker pods.
	WorkerCount int32

	// WorldSize is the effective world size for this launch.
	WorldSize int32
}

// buildLaunchPlan computes the launch parameters from the Workload admission
// and topology assignment. When no Workload is available (Phase 2/3 path),
// it falls back to annotation-based counts.
func buildLaunchPlan(
	job *trainingv1alpha1.ResumableTrainingJob,
	gateResult *LaunchGateResult,
) *LaunchPlan {
	plan := &LaunchPlan{
		TopologyResult: gateResult.TopologyResult,
	}

	// Try to extract admission info from the Workload object first.
	if gateResult.Workload != nil && gateResult.Workload.Status.Admission != nil {
		plan.AdmittedCounts = extractAdmittedCounts(gateResult.Workload.Status.Admission)
		plan.AdmittedFlavors = extractAdmittedFlavors(gateResult.Workload.Status.Admission)
	}

	// Fall back to annotation-based counts (Phase 3 path).
	if plan.AdmittedCounts == nil {
		plan.AdmittedCounts = parseAdmittedCounts(job)
	}

	// Compute effective worker count and world size.
	if plan.AdmittedCounts != nil {
		plan.WorkerCount = totalAdmittedCount(plan.AdmittedCounts)
		plan.WorldSize = plan.WorkerCount
	}
	if plan.WorldSize == 0 {
		plan.WorldSize = job.Spec.Identity.WorldSize
		plan.WorkerCount = job.EffectivePreferredCount()
	}

	return plan
}

// extractAdmittedCounts extracts per-PodSet admitted pod counts from the
// Workload admission.
func extractAdmittedCounts(admission *kueuev1beta2.Admission) map[string]int32 {
	if admission == nil || len(admission.PodSetAssignments) == 0 {
		return nil
	}
	counts := make(map[string]int32)
	for _, psa := range admission.PodSetAssignments {
		if psa.Count != nil {
			counts[string(psa.Name)] = *psa.Count
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

// extractAdmittedFlavors extracts per-PodSet ResourceFlavor assignments from
// the Workload admission. Returns a map from PodSet name to the flavor name
// of the first resource type (typically the dominant GPU resource).
func extractAdmittedFlavors(admission *kueuev1beta2.Admission) map[string]string {
	if admission == nil || len(admission.PodSetAssignments) == 0 {
		return nil
	}
	flavors := make(map[string]string)
	for _, psa := range admission.PodSetAssignments {
		for _, flavorRef := range psa.Flavors {
			flavors[string(psa.Name)] = string(flavorRef)
			break // Take the first flavor only.
		}
	}
	if len(flavors) == 0 {
		return nil
	}
	return flavors
}

// toRenderInput builds a RenderInput from the launch plan, incorporating
// topology information when available.
func (plan *LaunchPlan) toRenderInput(
	job *trainingv1alpha1.ResumableTrainingJob,
	runAttempt int32,
	childJobSetName string,
	controlConfigMapName string,
	resumeManifestURI string,
) rtjjobset.RenderInput {
	input := rtjjobset.RenderInput{
		RTJ:                  job,
		RunAttempt:           runAttempt,
		JobSetName:           childJobSetName,
		ControlConfigMapName: controlConfigMapName,
		ResumeManifestURI:    resumeManifestURI,
		TopologyResult:       plan.TopologyResult,
	}

	if plan.AdmittedCounts != nil {
		input.AdmittedCounts = plan.AdmittedCounts
		input.OriginalWorldSize = job.Spec.Identity.WorldSize
		input.AllowWorldSizeChange = job.Spec.Resume.AllowWorldSizeChange
	}

	// Set the admitted flavor for the first non-empty flavor.
	if plan.AdmittedFlavors != nil {
		for _, flavor := range plan.AdmittedFlavors {
			if flavor != "" {
				input.AdmittedFlavor = flavor
				break
			}
		}
	}

	return input
}

// buildEffectiveLaunchShape computes the EffectiveLaunchShape status from the
// launch plan and checkpoint selection.
func (plan *LaunchPlan) buildEffectiveLaunchShape(
	selectedCheckpoint *trainingv1alpha1.CheckpointReference,
) *trainingv1alpha1.EffectiveLaunchShape {
	shape := &trainingv1alpha1.EffectiveLaunchShape{
		WorkerCount: plan.WorkerCount,
		WorldSize:   plan.WorldSize,
	}

	if selectedCheckpoint != nil {
		shape.SelectedCheckpointID = selectedCheckpoint.ID
		if selectedCheckpoint.WorldSize > 0 && selectedCheckpoint.WorldSize != plan.WorldSize {
			shape.ResumeMode = trainingv1alpha1.RestoreModeReshard
		} else if selectedCheckpoint.WorldSize > 0 {
			shape.ResumeMode = trainingv1alpha1.RestoreModeSameSize
		}
	}

	return shape
}

// syncLaunchReadinessStatus updates the RTJ's LaunchReadiness status based on
// the gate evaluation result. Returns true if the status changed.
func syncLaunchReadinessStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	gateResult *LaunchGateResult,
) bool {
	if gateResult == nil {
		// No gate evaluation — clear status.
		if job.Status.LaunchReadiness == nil {
			return false
		}
		job.Status.LaunchReadiness = nil
		return true
	}

	desired := &trainingv1alpha1.LaunchReadinessStatus{
		Ready:   gateResult.Ready,
		Reason:  gateResult.Reason,
		Message: gateResult.Message,
	}

	if gateResult.Ready {
		desired.GateState = trainingv1alpha1.ReadinessGateReady
	} else {
		switch gateResult.Reason {
		case reasonReadinessGateRejected:
			desired.GateState = trainingv1alpha1.ReadinessGateRejected
		default:
			desired.GateState = trainingv1alpha1.ReadinessGatePending
		}
	}

	if launchReadinessEqual(job.Status.LaunchReadiness, desired) {
		return false
	}
	job.Status.LaunchReadiness = desired
	return true
}

// syncTopologyStatus updates the RTJ's Topology status from the parsed
// topology assignment. Returns true if the status changed.
func syncTopologyStatus(
	job *trainingv1alpha1.ResumableTrainingJob,
	topoResult *topology.ParseResult,
	workerPodSetName string,
) bool {
	if topoResult == nil {
		if job.Status.Topology == nil {
			return false
		}
		job.Status.Topology = nil
		return true
	}

	// Use the worker PodSet's topology for the RTJ status.
	pst, ok := topoResult.PodSets[workerPodSetName]
	if !ok {
		// Try first PodSet if worker not found.
		for _, ps := range topoResult.PodSets {
			pst = ps
			break
		}
	}

	desired := topology.ToTopologyStatus(pst)
	if topologyStatusEqual(job.Status.Topology, desired) {
		return false
	}
	job.Status.Topology = desired
	return true
}

// syncEffectiveLaunchShape updates the RTJ's EffectiveLaunchShape status.
// Returns true if the status changed.
func syncEffectiveLaunchShape(
	job *trainingv1alpha1.ResumableTrainingJob,
	shape *trainingv1alpha1.EffectiveLaunchShape,
) bool {
	if effectiveLaunchShapeEqual(job.Status.EffectiveLaunchShape, shape) {
		return false
	}
	job.Status.EffectiveLaunchShape = shape
	return true
}

func launchReadinessEqual(left, right *trainingv1alpha1.LaunchReadinessStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	return left.Ready == right.Ready &&
		left.GateState == right.GateState &&
		left.Reason == right.Reason &&
		left.Message == right.Message
}

func topologyStatusEqual(left, right *trainingv1alpha1.TopologyStatus) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}

	if len(left.Levels) != len(right.Levels) {
		return false
	}
	for i := range left.Levels {
		if left.Levels[i] != right.Levels[i] {
			return false
		}
	}
	if len(left.Domains) != len(right.Domains) {
		return false
	}
	for i := range left.Domains {
		if left.Domains[i].Count != right.Domains[i].Count {
			return false
		}
		if len(left.Domains[i].Values) != len(right.Domains[i].Values) {
			return false
		}
		for j := range left.Domains[i].Values {
			if left.Domains[i].Values[j] != right.Domains[i].Values[j] {
				return false
			}
		}
	}
	return true
}

func effectiveLaunchShapeEqual(left, right *trainingv1alpha1.EffectiveLaunchShape) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	return left.WorkerCount == right.WorkerCount &&
		left.WorldSize == right.WorldSize &&
		left.ResumeMode == right.ResumeMode &&
		left.SelectedCheckpointID == right.SelectedCheckpointID
}

// parseAdmittedFlavorsAnnotation reads the admitted flavors from an annotation,
// falling back to the annotation-based approach when Workload is not available.
func parseAdmittedFlavorsAnnotation(job *trainingv1alpha1.ResumableTrainingJob) map[string]string {
	const admittedFlavorsAnnotation = "training.checkpoint.example.io/admitted-flavors"
	raw, ok := job.Annotations[admittedFlavorsAnnotation]
	if !ok || raw == "" {
		return nil
	}
	var flavors map[string]string
	if err := json.Unmarshal([]byte(raw), &flavors); err != nil {
		return nil
	}
	return flavors
}
