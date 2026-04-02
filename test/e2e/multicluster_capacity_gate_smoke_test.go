package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMultiClusterCapacityGateSmoke verifies that Phase 7 capacity-guaranteed
// launch integration is compatible with the Phase 6 manager/worker MultiKueue
// path.
//
// This test uses the Phase 6 three-cluster environment (manager + 2 workers).
// The Phase 6 workers do NOT have Phase 7 provisioning infrastructure, so
// Phase 7 launch gates pass through (Phase 6 backward compatibility). The
// test verifies:
//
//  1. Manager suppression invariant holds with the Phase 7 codebase.
//  2. Worker receives the dispatched RTJ and launches runtime (Phase 6 path).
//  3. Worker does NOT create child JobSet on the manager cluster.
//  4. Remote dispatch lifecycle progresses through Pending → Active.
//
// What this test does NOT cover (documented as deferred):
//   - Worker-side Phase 7 provisioning gating in multi-cluster (requires
//     ProvisioningRequest infrastructure on worker clusters).
//   - Manager observing worker provisioning transitions (requires live
//     adapter + provisioning backend).
//   - These are documented in docs/phase7/multicluster-compatibility.md.
//
// Scenario:
//   - Submit an RTJ with spec.managedBy to the manager cluster.
//   - Bias cluster selection to worker-1 (stop worker-2 CQ).
//   - Wait for the manager to report suppression + dispatch active.
//   - Wait for worker-1 to receive the RTJ and create a child JobSet.
//   - Verify the manager never creates local runtime throughout.
func TestMultiClusterCapacityGateSmoke(t *testing.T) {
	env := setupPhase6Env(t)

	rtjName := fmt.Sprintf("p7-compat-%d", time.Now().UnixNano())

	// Bias cluster selection to worker-1 by pausing worker-2's CQ.
	biasWorkerSelection(t, env)
	defer restoreWorkerSelection(t, env)

	// Render test manifest (reuse Phase 6 remote dispatch template).
	manifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase6/rtj-remote-dispatch.yaml"),
		map[string]string{
			"__RTJ_NAME__":      rtjName,
			"__DEV_NAMESPACE__": env.namespace,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(manifest)

	// Clean up at test end.
	defer cleanupPhase6RTJ(t, env, rtjName)

	// ---- Step 1: Submit RTJ to manager cluster. ----
	t.Log("Step 1: submitting RTJ to manager cluster")
	runKubectlContext(t, env.repoRoot, env.managerContext, "apply", "-f", manifest)

	// ---- Step 2: Verify manager suppression. ----
	t.Log("Step 2: waiting for manager to report local execution suppressed")
	waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"multiCluster.localExecutionSuppressed=true",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.LocalExecutionSuppressed
		},
	)

	// ---- Step 3: Verify no child JobSets on manager. ----
	t.Log("Step 3: verifying no child JobSets on manager (Phase 7 codebase does not regress)")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 4: Wait for dispatch to progress beyond Pending. ----
	t.Log("Step 4: waiting for dispatch to progress beyond Pending")
	dispatchedView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"dispatchPhase != Pending",
		5*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.DispatchPhase != "" &&
				view.Status.MultiCluster.DispatchPhase != "Pending"
		},
	)
	t.Logf("dispatch progressed: dispatchPhase=%s executionCluster=%s",
		dispatchedView.Status.MultiCluster.DispatchPhase,
		dispatchedView.Status.MultiCluster.ExecutionCluster)

	// ---- Step 5: Verify worker-1 received the RTJ and creates runtime. ----
	// In the Phase 6 environment, workers do not have Phase 7 provisioning,
	// so the Phase 7 launch gates pass through (backward compatible). The
	// worker should create a child JobSet normally.
	t.Log("Step 5: waiting for worker-1 to pick up RTJ and report activity")
	waitForPhase6RTJState(
		t, env, env.worker1Context, rtjName,
		"worker-1 RTJ phase is Starting or Running",
		5*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.Phase == "Starting" ||
				view.Status.Phase == "Running" ||
				view.Status.Phase == "Restoring"
		},
	)

	// ---- Step 6: Verify worker-1 has created a child JobSet. ----
	t.Log("Step 6: verifying child JobSet exists on worker-1")
	workerView, err := getPhase6RTJ(env.repoRoot, env.worker1Context, env.namespace, rtjName)
	if err != nil {
		t.Fatalf("get worker RTJ: %v", err)
	}
	if workerView.Status.ActiveJobSetName == "" {
		t.Fatal("expected worker to have an active child JobSet name, got empty")
	}
	t.Logf("worker-1 child JobSet: %s (run attempt %d)",
		workerView.Status.ActiveJobSetName, workerView.Status.CurrentRunAttempt)

	// ---- Step 7: Re-verify manager suppression after worker launches. ----
	// This proves the suppression invariant holds even after the worker has
	// launched runtime, which exercises the Phase 7 codebase in the manager
	// reconcile path with active remote status.
	t.Log("Step 7: re-verifying manager suppression after worker launch")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	managerView, err := getPhase6RTJ(env.repoRoot, env.managerContext, env.namespace, rtjName)
	if err != nil {
		t.Fatalf("get final manager RTJ: %v", err)
	}
	mc := managerView.Status.MultiCluster
	if mc == nil {
		t.Fatal("expected multiCluster status on manager")
	}
	if !mc.LocalExecutionSuppressed {
		t.Fatal("expected localExecutionSuppressed=true after worker launch")
	}

	// ---- Step 8: Verify the manager reflects remote phase from worker. ----
	t.Log("Step 8: waiting for manager to reflect remote active phase")
	finalView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"dispatchPhase=Active with remotePhase set",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.DispatchPhase == "Active" &&
				view.Status.MultiCluster.RemotePhase != ""
		},
	)

	t.Logf("final manager state: dispatchPhase=%s remotePhase=%s executionCluster=%s suppressed=%v",
		finalView.Status.MultiCluster.DispatchPhase,
		finalView.Status.MultiCluster.RemotePhase,
		finalView.Status.MultiCluster.ExecutionCluster,
		finalView.Status.MultiCluster.LocalExecutionSuppressed,
	)

	// ---- Step 9: Verify no child JobSets leaked to worker-2 or manager. ----
	t.Log("Step 9: final verification - no child JobSets on manager or worker-2")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.worker2Context, env.namespace, rtjName)

	t.Log("multi-cluster capacity gate smoke test passed")
	t.Log("Phase 7 backward compatibility confirmed: manager suppression holds, worker launches normally")
	t.Log("NOTE: Phase 7 provisioning-gated launch on worker requires provisioning infrastructure (deferred)")
}
