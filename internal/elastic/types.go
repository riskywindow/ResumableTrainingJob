// Package elastic implements the controller-side elasticity planning model
// for Phase 9 hybrid elastic RTJs.
//
// The planning model is a pure-function evaluator: given an input snapshot
// of the current RTJ state, it produces a deterministic plan output with
// no side effects. The controller integration layer is responsible for
// reading inputs from Kubernetes objects and applying plan outputs.
package elastic

import (
	"fmt"
	"time"
)

// PlanKind is the discrete outcome of the elasticity planner.
// Each value maps to a single controller action path.
type PlanKind string

const (
	// PlanNoResize means no resize is needed. Target equals current, or
	// elasticity is disabled.
	PlanNoResize PlanKind = "NoResize"

	// PlanShrinkInPlace means the controller can reduce the worker count
	// in-place by patching the child JobSet replicas and writing
	// reclaimablePods to the Workload status.
	PlanShrinkInPlace PlanKind = "ShrinkInPlace"

	// PlanShrinkViaRelaunch means the shrink requires a full
	// checkpoint-and-relaunch cycle because the runtime does not
	// support in-place replica reduction.
	PlanShrinkViaRelaunch PlanKind = "ShrinkViaRelaunch"

	// PlanGrowViaRelaunch means the worker count must increase, which
	// always requires checkpoint-and-relaunch (new Kueue admission).
	PlanGrowViaRelaunch PlanKind = "GrowViaRelaunch"

	// PlanResizeBlocked means a resize is desired but cannot proceed.
	// Reasons include: target out of bounds, workload not admitted,
	// concurrent preemption in progress, or DRA/topology constraints.
	PlanResizeBlocked PlanKind = "ResizeBlocked"

	// PlanResizeInProgress means a resize was previously planned and
	// is still being executed (e.g., checkpoint in progress, waiting
	// for pod termination, waiting for re-admission).
	PlanResizeInProgress PlanKind = "ResizeInProgress"

	// PlanReclaimPublished means an in-place shrink has been committed
	// and reclaimablePods have been written. The controller is waiting
	// for surplus pods to terminate and for cleanup.
	PlanReclaimPublished PlanKind = "ReclaimPublished"
)

// PlanInput captures the snapshot of state the planner needs to evaluate.
// All fields are read-only inputs; the planner never mutates them.
type PlanInput struct {
	// ElasticityEnabled is true when spec.elasticity.mode != Disabled.
	ElasticityEnabled bool

	// TargetWorkerCount is the desired worker count from
	// spec.elasticity.targetWorkerCount (or 0 when unset).
	TargetWorkerCount int32

	// CurrentWorkerCount is the number of workers currently admitted
	// by Kueue (from the Workload admission PodSet count).
	CurrentWorkerCount int32

	// ActiveWorkerCount is the observed number of running worker pods.
	ActiveWorkerCount int32

	// MinWorkerCount is the lower bound (from parallelism.minCount or 1).
	MinWorkerCount int32

	// MaxWorkerCount is the upper bound (from parallelism.preferredCount
	// or identity.worldSize).
	MaxWorkerCount int32

	// InPlaceShrinkPolicy is the user's shrink policy preference.
	// "IfSupported" or "Never".
	InPlaceShrinkPolicy string

	// RuntimeSupportsInPlaceShrink is true when the child JobSet
	// advertises support for live replica reduction via the
	// training.io/supports-in-place-shrink annotation.
	RuntimeSupportsInPlaceShrink bool

	// WorkloadAdmitted is true when the Kueue Workload has an active
	// admission (Workload.Status.Admission != nil).
	WorkloadAdmitted bool

	// WorkloadExists is true when the Kueue Workload object exists.
	WorkloadExists bool

	// CurrentResizeState is the resize state from status.elasticity.resizeState.
	CurrentResizeState string

	// ReclaimablePodsPublished is true when reclaimablePods have already
	// been written to the Workload status for the current cycle.
	ReclaimablePodsPublished bool

	// CheckpointReady is true when the latest checkpoint is available
	// and compatible for a relaunch.
	CheckpointReady bool

	// LastResizeCheckpointExists is true when a resize-specific checkpoint
	// reference exists in status.
	LastResizeCheckpointExists bool

	// PreemptionInProgress is true when the RTJ is currently being
	// preempted by Kueue (suspend field set, drain in progress).
	PreemptionInProgress bool

	// DRAConstraintsBlock is true when DRA/topology constraints prevent
	// the planned resize from being satisfied.
	DRAConstraintsBlock bool

	// Now is the evaluation timestamp.
	Now time.Time
}

// PlanOutput is the deterministic result of plan evaluation.
// It describes what the controller should do next without performing
// any mutations.
type PlanOutput struct {
	// Kind is the discrete plan action.
	Kind PlanKind

	// ReclaimableWorkerDelta is the number of worker pods to declare
	// reclaimable (positive for shrink, zero otherwise).
	ReclaimableWorkerDelta int32

	// CheckpointRequired is true when the plan requires a checkpoint
	// before proceeding (always true for relaunch paths).
	CheckpointRequired bool

	// RelaunchRequired is true when the plan requires tearing down
	// the current run and relaunching with a new worker count.
	RelaunchRequired bool

	// NewWorkerCount is the target worker count after the plan executes.
	// Equal to TargetWorkerCount for active plans, CurrentWorkerCount
	// for no-op/blocked.
	NewWorkerCount int32

	// Reason is a machine-readable reason string for status.elasticity.resizeReason.
	Reason string

	// Message is a human-readable explanation for observability.
	Message string
}

// String returns a compact representation for logging.
func (p PlanOutput) String() string {
	return fmt.Sprintf("Plan{kind=%s, delta=%d, checkpoint=%v, relaunch=%v, target=%d, reason=%s}",
		p.Kind, p.ReclaimableWorkerDelta, p.CheckpointRequired, p.RelaunchRequired,
		p.NewWorkerCount, p.Reason)
}

// ReclaimDelta describes the reclaimablePods patch to apply to a Workload.
type ReclaimDelta struct {
	// PodSetName is the name of the scalable worker PodSet.
	PodSetName string

	// Count is the number of pods to declare reclaimable.
	// Zero means clear the reclaimablePods entry.
	Count int32
}

// IsReclaim returns true when this delta signals pods to reclaim.
func (d ReclaimDelta) IsReclaim() bool {
	return d.Count > 0
}

// IsClear returns true when this delta clears a previous reclaim signal.
func (d ReclaimDelta) IsClear() bool {
	return d.Count == 0
}
