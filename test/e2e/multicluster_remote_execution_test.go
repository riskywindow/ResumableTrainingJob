package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestMultiClusterRemoteExecution proves the end-to-end remote dispatch
// and execution flow for a MultiKueue-managed RTJ.
//
// Scenario:
//   - An RTJ with spec.managedBy: kueue.x-k8s.io/multikueue is submitted
//     to the manager cluster.
//   - Worker-2's ClusterQueue is paused (spec.stopPolicy=Hold) so that
//     MultiKueue deterministically dispatches to worker-1.
//   - The test verifies that the RTJ is dispatched, a mirror copy exists
//     on worker-1, a child JobSet is created on worker-1, and NO local
//     runtime exists on the manager or worker-2.
//
// What this test proves:
//  1. An RTJ with spec.managedBy submitted to the manager cluster becomes
//     a MultiKueue-managed object.
//  2. MultiKueue selects worker-1 (the only admitting worker) and dispatches.
//  3. A mirror RTJ copy exists on worker-1 with spec.managedBy stripped
//     (the adapter removes it so the worker treats it as a local job).
//  4. A child JobSet exists only on worker-1.
//  5. No child JobSet exists on the manager cluster.
//  6. No child JobSet exists on worker-2.
//  7. The manager-side RTJ reflects remote execution status
//     (executionCluster set, localExecutionSuppressed=true).
func TestMultiClusterRemoteExecution(t *testing.T) {
	env := setupPhase6Env(t)

	rtjName := fmt.Sprintf("p6-remote-exec-%d", time.Now().UnixNano())
	childJobSetName := rtjjobset.ChildJobSetName(rtjName, 1)

	// Bias cluster selection to worker-1 by pausing worker-2's CQ.
	biasWorkerSelection(t, env)
	defer restoreWorkerSelection(t, env)

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

	// ---- Step 2: Verify manager-side RTJ shows MultiKueue dispatch state. ----
	t.Log("Step 2: waiting for manager-side RTJ to show MultiKueue dispatch state")
	managerView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"multiCluster.dispatchPhase is set",
		5*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.DispatchPhase != ""
		},
	)

	if managerView.Spec.ManagedBy != "kueue.x-k8s.io/multikueue" {
		t.Fatalf("expected spec.managedBy=%q, got %q",
			"kueue.x-k8s.io/multikueue", managerView.Spec.ManagedBy)
	}
	t.Logf("manager RTJ: phase=%s dispatchPhase=%s",
		managerView.Status.Phase,
		managerView.Status.MultiCluster.DispatchPhase)

	// ---- Step 3: Wait for mirror RTJ on worker-1. ----
	t.Log("Step 3: waiting for mirror RTJ on worker-1")
	workerView := waitForPhase6RTJState(
		t, env, env.worker1Context, rtjName,
		"RTJ exists on worker-1",
		5*time.Minute,
		func(view phase6RTJView) bool {
			return view.Metadata.UID != ""
		},
	)
	t.Logf("mirror RTJ on worker-1: phase=%s managedBy=%q",
		workerView.Status.Phase, workerView.Spec.ManagedBy)

	// Worker-side RTJ must NOT have spec.managedBy (stripped by adapter).
	if workerView.Spec.ManagedBy != "" {
		t.Fatalf("expected worker-side RTJ to have spec.managedBy stripped, got %q",
			workerView.Spec.ManagedBy)
	}

	// ---- Step 4: Wait for child JobSet on worker-1. ----
	t.Log("Step 4: waiting for child JobSet on worker-1")
	waitForJobSetOnCluster(t, env, env.worker1Context, childJobSetName, 5*time.Minute)
	t.Logf("child JobSet %s exists on worker-1", childJobSetName)

	// ---- Step 5: Assert NO child JobSet on manager cluster. ----
	t.Log("Step 5: verifying no child JobSet on manager cluster")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 6: Assert NO child JobSet on worker-2. ----
	t.Log("Step 6: verifying no child JobSet on worker-2")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.worker2Context, env.namespace, rtjName)

	// ---- Step 7: Verify manager status reflects remote execution. ----
	t.Log("Step 7: verifying manager-side remote execution status")
	managerFinal := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"multiCluster.executionCluster is set",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.ExecutionCluster != ""
		},
	)

	mc := managerFinal.Status.MultiCluster
	t.Logf("manager status: dispatchPhase=%s executionCluster=%s remotePhase=%s suppressed=%v",
		mc.DispatchPhase, mc.ExecutionCluster, mc.RemotePhase, mc.LocalExecutionSuppressed)

	if !mc.LocalExecutionSuppressed {
		t.Fatalf("expected localExecutionSuppressed=true on manager, got false")
	}

	t.Log("remote execution test passed: RTJ dispatched to worker-1, child JobSet on worker-1 only, no local runtime on manager")
}
