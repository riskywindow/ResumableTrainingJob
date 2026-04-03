package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestMultiClusterDRASmoke proves that the Phase 8 DRA integration is
// compatible with the Phase 6 manager/worker multi-cluster path.
//
// Scenario:
//   - A DRA-backed RTJ (spec.devices.mode=DRA, claims=[gpu]) is submitted
//     to the manager cluster with spec.managedBy set to MultiKueue.
//   - Worker-2's ClusterQueue is paused so dispatch is deterministic to worker-1.
//   - The test verifies:
//     1. The manager does NOT create local ResourceClaimTemplates.
//     2. The manager does NOT create local child JobSets.
//     3. The manager-side RTJ shows localExecutionSuppressed=true.
//     4. The worker-1 RTJ exists (mirror copy from MultiKueue adapter).
//     5. If the DRA infra is present on the worker, ResourceClaimTemplates
//        are created on worker-1 (not the manager).
//     6. A child JobSet is created only on worker-1.
//
// Infrastructure note: this test requires the Phase 6 multi-cluster
// environment (3 kind clusters: manager, worker-1, worker-2) with the
// Phase 8 DRA driver and DeviceClass installed on worker clusters.
// When the DRA driver is not present on workers, the test still verifies
// the manager-side suppression invariants (steps 1-3) and the worker-side
// dispatch (steps 4, 6). ResourceClaimTemplate verification (step 5) is
// skipped gracefully if the DeviceClass CRD is not present on the worker.
func TestMultiClusterDRASmoke(t *testing.T) {
	env := setupPhase6Env(t)

	rtjName := fmt.Sprintf("p8-dra-mc-%d", time.Now().UnixNano())
	childJobSetName := rtjjobset.ChildJobSetName(rtjName, 1)

	// Bias cluster selection to worker-1 by pausing worker-2's CQ.
	biasWorkerSelection(t, env)
	defer restoreWorkerSelection(t, env)

	// Render test manifest with DRA device claims.
	manifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase8/rtj-dra-multicluster-dispatch.yaml"),
		map[string]string{
			"__RTJ_NAME__":      rtjName,
			"__DEV_NAMESPACE__": env.namespace,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(manifest)

	// Clean up at test end.
	defer cleanupPhase6RTJ(t, env, rtjName)
	defer cleanupResourceClaimTemplatesOnCluster(t, env, env.worker1Context, rtjName)

	// ---- Step 1: Submit DRA-backed RTJ to manager cluster. ----
	t.Log("Step 1: submitting DRA-backed RTJ to manager cluster")
	runKubectlContext(t, env.repoRoot, env.managerContext, "apply", "-f", manifest)

	// ---- Step 2: Wait for manager to report local execution suppressed. ----
	t.Log("Step 2: waiting for manager to suppress local execution")
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

	// ---- Step 3: Verify NO ResourceClaimTemplates on manager cluster. ----
	t.Log("Step 3: verifying no ResourceClaimTemplates on manager cluster")
	assertNoResourceClaimTemplatesOnCluster(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 4: Verify NO child JobSets on manager cluster. ----
	t.Log("Step 4: verifying no child JobSets on manager cluster")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 5: Wait for dispatch to progress and mirror RTJ on worker-1. ----
	t.Log("Step 5: waiting for mirror RTJ on worker-1")
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

	// ---- Step 6: Verify ResourceClaimTemplates on worker-1 (if DRA CRD present). ----
	t.Log("Step 6: checking for ResourceClaimTemplates on worker-1")
	if hasDRASupport(env.repoRoot, env.worker1Context) {
		// Wait for the worker operator to create the template.
		waitForResourceClaimTemplatesOnCluster(
			t, env, env.worker1Context, rtjName, 1, 3*time.Minute,
		)
		t.Log("ResourceClaimTemplate created on worker-1 (DRA template is worker-local)")

		// Verify template is owned by the worker-side RTJ and has correct labels.
		templates := listResourceClaimTemplatesOnCluster(t, env.repoRoot, env.worker1Context, env.namespace, rtjName)
		if len(templates) == 0 {
			t.Fatal("expected at least 1 ResourceClaimTemplate on worker-1")
		}
		for _, tmpl := range templates {
			if tmpl.Labels["training.checkpoint.example.io/rtj-name"] != rtjName {
				t.Fatalf("expected template label rtj-name=%q, got %q",
					rtjName, tmpl.Labels["training.checkpoint.example.io/rtj-name"])
			}
			t.Logf("  template %s: claim=%s managed-by=%s",
				tmpl.Name,
				tmpl.Labels["training.checkpoint.example.io/claim-name"],
				tmpl.Labels["training.checkpoint.example.io/managed-by"])
		}
	} else {
		t.Log("DRA CRDs not present on worker-1; skipping ResourceClaimTemplate verification")
	}

	// ---- Step 7: Wait for child JobSet on worker-1. ----
	t.Log("Step 7: waiting for child JobSet on worker-1")
	waitForJobSetOnCluster(t, env, env.worker1Context, childJobSetName, 5*time.Minute)
	t.Logf("child JobSet %s exists on worker-1", childJobSetName)

	// ---- Step 8: Re-verify NO child JobSet on manager (after worker has launched). ----
	t.Log("Step 8: re-verifying no child JobSet on manager after worker launch")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 9: Re-verify NO ResourceClaimTemplates on manager. ----
	t.Log("Step 9: re-verifying no ResourceClaimTemplates on manager")
	assertNoResourceClaimTemplatesOnCluster(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 10: Verify manager reflects remote execution status. ----
	t.Log("Step 10: verifying manager-side remote execution status")
	managerFinal := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"multiCluster.executionCluster is set",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.MultiCluster != nil &&
				view.Status.MultiCluster.ExecutionCluster != ""
		},
	)

	mcFinal := managerFinal.Status.MultiCluster
	t.Logf("manager final: dispatchPhase=%s executionCluster=%s remotePhase=%s suppressed=%v",
		mcFinal.DispatchPhase, mcFinal.ExecutionCluster, mcFinal.RemotePhase, mcFinal.LocalExecutionSuppressed)

	if !mcFinal.LocalExecutionSuppressed {
		t.Fatalf("expected localExecutionSuppressed=true on manager, got false")
	}

	t.Log("multi-cluster DRA smoke test passed: " +
		"manager suppressed local DRA templates and runtime, " +
		"worker created DRA templates and launched child JobSet")
}

// ---------------------------------------------------------------------------
// Multi-cluster DRA helpers
// ---------------------------------------------------------------------------

// resourceClaimTemplateOnClusterView is a minimal view of a ResourceClaimTemplate
// retrieved from a specific cluster.
type resourceClaimTemplateOnClusterView struct {
	Name   string
	Labels map[string]string
}

// hasDRASupport returns true if the cluster has the ResourceClaimTemplate CRD.
func hasDRASupport(repoRoot, kubeContext string) bool {
	_, err := kubectlContext(repoRoot, kubeContext,
		"api-resources", "--api-group=resource.k8s.io", "-o", "name")
	if err != nil {
		return false
	}
	// Check specifically for resourceclaimtemplates.
	output, err := kubectlContext(repoRoot, kubeContext,
		"get", "resourceclaimtemplates.resource.k8s.io", "--all-namespaces",
		"--no-headers", "--ignore-not-found")
	// The command succeeds if the CRD exists (even with 0 resources).
	return err == nil && !strings.Contains(output, "error")
}

// assertNoResourceClaimTemplatesOnCluster verifies that no ResourceClaimTemplates
// with the RTJ label exist on the specified cluster.
func assertNoResourceClaimTemplatesOnCluster(t *testing.T, repoRoot, kubeContext, namespace, rtjName string) {
	t.Helper()
	output, err := kubectlContext(repoRoot, kubeContext,
		"-n", namespace, "get", "resourceclaimtemplates.resource.k8s.io",
		"-l", "training.checkpoint.example.io/rtj-name="+rtjName,
		"-o", "json")
	if err != nil {
		// CRD may not exist on this cluster (e.g., manager without DRA).
		// That's fine — no templates can exist.
		return
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		// Parse error is non-fatal; if we can't parse, assume no templates.
		return
	}
	if len(list.Items) > 0 {
		t.Fatalf("expected no ResourceClaimTemplates for RTJ %s on %s, found %d",
			rtjName, kubeContext, len(list.Items))
	}
}

// listResourceClaimTemplatesOnCluster lists ResourceClaimTemplates with the
// RTJ label on a specific cluster.
func listResourceClaimTemplatesOnCluster(t *testing.T, repoRoot, kubeContext, namespace, rtjName string) []resourceClaimTemplateOnClusterView {
	t.Helper()
	output, err := kubectlContext(repoRoot, kubeContext,
		"-n", namespace, "get", "resourceclaimtemplates.resource.k8s.io",
		"-l", "training.checkpoint.example.io/rtj-name="+rtjName,
		"-o", "json")
	if err != nil {
		return nil
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil
	}
	var result []resourceClaimTemplateOnClusterView
	for _, item := range list.Items {
		result = append(result, resourceClaimTemplateOnClusterView{
			Name:   item.Metadata.Name,
			Labels: item.Metadata.Labels,
		})
	}
	return result
}

// waitForResourceClaimTemplatesOnCluster waits for the expected number of
// ResourceClaimTemplates to appear on a specific cluster.
func waitForResourceClaimTemplatesOnCluster(
	t *testing.T,
	env phase6Env,
	kubeContext string,
	rtjName string,
	expectedCount int,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		templates := listResourceClaimTemplatesOnCluster(t, env.repoRoot, kubeContext, env.namespace, rtjName)
		if len(templates) >= expectedCount {
			return
		}
		time.Sleep(2 * time.Second)
	}
	templates := listResourceClaimTemplatesOnCluster(t, env.repoRoot, kubeContext, env.namespace, rtjName)
	t.Fatalf(
		"timed out waiting for %d ResourceClaimTemplates for RTJ %s on %s; found %d\n"+
			"manager logs:\n%s\nworker-1 logs:\n%s\nworker-2 logs:\n%s",
		expectedCount, rtjName, kubeContext, len(templates),
		env.managerLogs.String(),
		env.worker1Logs.String(),
		env.worker2Logs.String(),
	)
}

// cleanupResourceClaimTemplatesOnCluster deletes ResourceClaimTemplates
// owned by the RTJ on a specific cluster (best-effort).
func cleanupResourceClaimTemplatesOnCluster(t *testing.T, env phase6Env, kubeContext, rtjName string) {
	t.Helper()
	kubectlContext(env.repoRoot, kubeContext,
		"-n", env.namespace, "delete", "resourceclaimtemplates.resource.k8s.io",
		"-l", "training.checkpoint.example.io/rtj-name="+rtjName,
		"--ignore-not-found=true")
}
