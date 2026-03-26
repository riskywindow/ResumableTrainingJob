package resume

import (
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// Setup wires both the AdmissionCheck and Workload reconcilers into the
// provided manager. Call this from the operator's main.go after creating
// the manager.
//
// The catalog parameter is optional. When nil, the Workload reconciler
// operates without a checkpoint catalog — the evaluator applies the policy's
// failurePolicy or allowInitialLaunchWithoutCheckpoint to make a decision.
// This preserves backward compatibility with the Phase 4 scaffold behavior:
// when the default policy is used (allowInitialLaunchWithoutCheckpoint=true),
// workloads are admitted without checking storage.
func Setup(mgr ctrl.Manager, catalog ...checkpoints.Catalog) error {
	acReconciler := &AdmissionCheckReconciler{
		Client: mgr.GetClient(),
	}
	if err := acReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup AdmissionCheck reconciler: %w", err)
	}

	wlReconciler := &WorkloadReconciler{
		Client: mgr.GetClient(),
	}
	if len(catalog) > 0 && catalog[0] != nil {
		wlReconciler.Catalog = catalog[0]
	}
	if err := wlReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup Workload reconciler: %w", err)
	}

	return nil
}
