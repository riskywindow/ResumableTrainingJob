package controller

import "fmt"

// OperatorMode determines how the RTJ controller handles reconciliation
// in relation to MultiKueue multi-cluster dispatch.
//
// The mode is set at startup via the --mode flag and is fixed for the
// lifetime of the process. There is no runtime mode switching.
type OperatorMode string

const (
	// ModeWorker is the default mode. The operator runs the full Phase 5
	// runtime path: launch gating, child JobSet creation, checkpoint I/O,
	// graceful yield, and resume. Used for single-cluster deployments and
	// for worker clusters in a MultiKueue setup.
	ModeWorker OperatorMode = "worker"

	// ModeManager is the control-plane-only mode. For RTJs managed by
	// MultiKueue (spec.managedBy == MultiKueueControllerName), the operator
	// suppresses local child JobSet creation and delegates runtime execution
	// to a remote worker cluster via MultiKueue dispatch.
	//
	// RTJs not managed by MultiKueue that happen to exist on a manager
	// cluster continue to follow the normal Phase 5 path to avoid data loss.
	ModeManager OperatorMode = "manager"
)

// ParseOperatorMode validates and returns the OperatorMode for the given
// string. Returns an error for unrecognized values.
func ParseOperatorMode(s string) (OperatorMode, error) {
	switch OperatorMode(s) {
	case ModeWorker:
		return ModeWorker, nil
	case ModeManager:
		return ModeManager, nil
	default:
		return "", fmt.Errorf("unsupported operator mode %q: must be %q or %q", s, ModeWorker, ModeManager)
	}
}

// multiKueueChecker is satisfied by any type that reports whether it is
// managed by MultiKueue. Decouples mode logic from the concrete RTJ type.
type multiKueueChecker interface {
	IsManagedByMultiKueue() bool
}

// ShouldSuppressRuntime returns true when the operator must NOT create
// local child JobSets or control ConfigMaps for this RTJ.
//
// Suppression occurs only when ALL of the following hold:
//  1. The operator is running in ModeManager.
//  2. The RTJ has spec.managedBy set to the MultiKueue controller value.
//
// In all other cases the full Phase 5 runtime path is preserved.
func ShouldSuppressRuntime(mode OperatorMode, job multiKueueChecker) bool {
	return mode == ModeManager && job.IsManagedByMultiKueue()
}
