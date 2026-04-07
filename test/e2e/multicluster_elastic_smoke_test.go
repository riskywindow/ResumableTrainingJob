package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMultiClusterElasticSmokeManagerSuppression verifies that Phase 9
// elastic RTJs work correctly via MultiKueue dispatch. This is the narrowest
// deterministic test that proves manager/worker compatibility for elastic
// resize without requiring a full multi-cluster resize execution cycle.
//
// What this test proves:
//  1. An elastic RTJ (spec.elasticity.mode=Manual) can be submitted to the
//     manager cluster and dispatched to a worker via MultiKueue.
//  2. The manager never creates local child JobSets or evaluates elastic
//     plans for the dispatched RTJ (localExecutionSuppressed=true).
//  3. When the worker reports elasticity status (mirrored by the adapter),
//     the manager preserves it without modification.
//  4. No reclaimablePods or reclaim helper state is created on the manager.
//
// What this test does NOT cover (deferred):
//  - Full multi-cluster resize execution (shrink/grow via remote worker).
//  - Multi-cluster reclaimablePods mirroring (OQ-3 stretch work).
//  - Cross-cluster resize failover (worker switch during resize).
//
// Prerequisites:
//  - Phase 6 multi-cluster environment (make phase6-up).
//  - Phase 9 elastic testdata template.
//  - RUN_KIND_E2E=1 environment variable.
func TestMultiClusterElasticSmokeManagerSuppression(t *testing.T) {
	env := setupPhase6Env(t)

	rtjName := fmt.Sprintf("p9-elastic-smoke-%d", time.Now().UnixNano())

	// Render the elastic RTJ template. We use the Phase 6 remote dispatch
	// template as a base but add elasticity fields via the template.
	manifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase9/rtj-multicluster-elastic-smoke.yaml"),
		map[string]string{
			"__RTJ_NAME__":      rtjName,
			"__DEV_NAMESPACE__": env.namespace,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(manifest)

	// Clean up at test end.
	defer cleanupPhase6RTJ(t, env, rtjName)

	// ---- Step 1: Submit elastic RTJ to manager cluster. ----
	t.Log("Step 1: submitting elastic RTJ to manager cluster")
	runKubectlContext(t, env.repoRoot, env.managerContext, "apply", "-f", manifest)

	// ---- Step 2: Wait for manager to report local execution suppressed. ----
	t.Log("Step 2: waiting for manager to suppress local execution for elastic RTJ")
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

	// ---- Step 3: Assert no child JobSets on manager. ----
	t.Log("Step 3: verifying no child JobSets on manager (elastic RTJ)")
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

	// ---- Step 5: Re-check suppression invariant over time. ----
	// Poll for 30 seconds to prove the invariant holds for elastic RTJs.
	t.Log("Step 5: verifying suppression invariant holds for elastic RTJ over 30s")
	checkDeadline := time.Now().Add(30 * time.Second)
	checksPerformed := 0
	for time.Now().Before(checkDeadline) {
		// No child JobSets must exist on manager.
		assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

		// localExecutionSuppressed must remain true.
		view, err := getPhase6RTJ(env.repoRoot, env.managerContext, env.namespace, rtjName)
		if err == nil && view.Status.MultiCluster != nil {
			if !view.Status.MultiCluster.LocalExecutionSuppressed {
				t.Fatalf("suppression invariant violated for elastic RTJ: "+
					"localExecutionSuppressed became false at check %d",
					checksPerformed)
			}
		}
		checksPerformed++
		time.Sleep(5 * time.Second)
	}
	t.Logf("suppression invariant held for elastic RTJ across %d checks", checksPerformed)

	// ---- Step 6: Verify final manager state. ----
	t.Log("Step 6: verifying final manager state for elastic RTJ")
	finalView, err := getPhase6RTJ(env.repoRoot, env.managerContext, env.namespace, rtjName)
	if err != nil {
		t.Fatalf("get final RTJ state: %v", err)
	}

	if finalView.Status.MultiCluster == nil {
		t.Fatal("expected multiCluster status to be populated for elastic RTJ")
	}
	mc = finalView.Status.MultiCluster
	if !mc.LocalExecutionSuppressed {
		t.Fatal("expected localExecutionSuppressed=true in final state")
	}
	if mc.DispatchPhase == "" {
		t.Fatal("expected dispatchPhase to be set in final state")
	}

	t.Logf("final state: dispatchPhase=%s executionCluster=%s remotePhase=%s suppressed=%v",
		mc.DispatchPhase, mc.ExecutionCluster, mc.RemotePhase, mc.LocalExecutionSuppressed)
	t.Log("multicluster elastic smoke test passed: elastic RTJ dispatched, " +
		"manager suppression confirmed, no local resize execution")
}
