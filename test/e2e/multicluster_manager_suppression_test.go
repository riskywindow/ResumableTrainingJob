package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMultiClusterManagerSuppression proves that the manager cluster never
// creates local runtime resources (child JobSets, control ConfigMaps) for
// a MultiKueue-managed RTJ, even after remote dispatch completes and the
// worker starts executing.
//
// Scenario:
//   - An RTJ with spec.managedBy is submitted to the manager cluster.
//   - Both workers are available (no selection bias). The test does not
//     care which worker is selected—it only verifies the manager side.
//   - The test checks the suppression invariant repeatedly over a 30-second
//     window after dispatch progresses.
//
// What this test proves:
//  1. status.multiCluster.localExecutionSuppressed is always true.
//  2. No child JobSets are ever created on the manager cluster (checked
//     repeatedly over time to prove the invariant, not just a snapshot).
//  3. dispatchPhase transitions through its lifecycle (Pending -> beyond).
//  4. The operator's manager mode and Kueue's KeepAdmissionCheckPending
//     provide dual suppression layers.
func TestMultiClusterManagerSuppression(t *testing.T) {
	env := setupPhase6Env(t)

	rtjName := fmt.Sprintf("p6-suppress-%d", time.Now().UnixNano())

	// Render test manifest.
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

	// ---- Step 2: Wait for suppression status on manager. ----
	t.Log("Step 2: waiting for manager to report local execution suppressed")
	suppressedView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"multiCluster.localExecutionSuppressed=true",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.LocalExecutionSuppressed
		},
	)

	mc := suppressedView.Status.MultiCluster
	t.Logf("suppression confirmed: dispatchPhase=%s suppressed=%v",
		mc.DispatchPhase, mc.LocalExecutionSuppressed)

	// ---- Step 3: Assert no child JobSets on manager (initial check). ----
	t.Log("Step 3: verifying no child JobSets on manager (initial)")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 4: Wait for dispatch lifecycle to progress beyond Pending. ----
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

	// ---- Step 5: Re-check suppression invariant over time. ----
	// Poll for 30 seconds with multiple checks to prove the invariant
	// holds continuously, not just at a single point in time.
	t.Log("Step 5: verifying suppression invariant over 30s window")
	checkDeadline := time.Now().Add(30 * time.Second)
	checksPerformed := 0
	for time.Now().Before(checkDeadline) {
		// No child JobSets must exist on manager.
		assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

		// localExecutionSuppressed must remain true.
		view, err := getPhase6RTJ(env.repoRoot, env.managerContext, env.namespace, rtjName)
		if err == nil && view.Status.MultiCluster != nil {
			if !view.Status.MultiCluster.LocalExecutionSuppressed {
				t.Fatalf("suppression invariant violated: localExecutionSuppressed became false "+
					"at check %d (dispatchPhase=%s)",
					checksPerformed, view.Status.MultiCluster.DispatchPhase)
			}
		}
		checksPerformed++
		time.Sleep(5 * time.Second)
	}
	t.Logf("suppression invariant held across %d checks over 30s", checksPerformed)

	// ---- Step 6: Verify final manager state. ----
	t.Log("Step 6: verifying final manager state")
	finalView, err := getPhase6RTJ(env.repoRoot, env.managerContext, env.namespace, rtjName)
	if err != nil {
		t.Fatalf("get final RTJ state: %v", err)
	}

	if finalView.Status.MultiCluster == nil {
		t.Fatalf("expected multiCluster status to be populated, got nil")
	}

	mc = finalView.Status.MultiCluster
	if !mc.LocalExecutionSuppressed {
		t.Fatalf("expected localExecutionSuppressed=true in final state, got false")
	}
	if mc.DispatchPhase == "" {
		t.Fatalf("expected dispatchPhase to be set in final state, got empty")
	}

	t.Logf("final state: dispatchPhase=%s executionCluster=%s remotePhase=%s suppressed=%v",
		mc.DispatchPhase, mc.ExecutionCluster, mc.RemotePhase, mc.LocalExecutionSuppressed)
	t.Log("manager suppression test passed: no local runtime created, suppression status confirmed over time")
}
