package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProtectedPriorityBlocksPreemption proves that a running RTJ with a
// CheckpointPriorityPolicy in the Protected state (within its startup
// protection window) resists same-tier preemption.
//
// Scenario:
//   - RTJ A: phase5-low (base priority 100) with dev-checkpoint-priority policy.
//     While in the 30s protection window, A's effective priority = 100 + 50 = 150.
//   - RTJ B: phase5-low (base priority 100) with NO policy.
//     B's Workload priority is the raw base priority = 100.
//   - The queue quota (500m CPU / 512Mi) fits exactly one RTJ.
//   - B cannot preempt A because 100 < 150 (Kueue LowerPriority).
//
// What this test proves:
//   1. The priority shaping engine evaluates correctly during startup protection.
//   2. The effective priority (base + protectedBoost) is written to the Workload.
//   3. Kueue's LowerPriority preemption respects the effective priority.
//   4. The RTJ status surfaces the correct preemption state and annotations.
//   5. A same-tier competitor workload stays pending while the running job is protected.
func TestProtectedPriorityBlocksPreemption(t *testing.T) {
	env := setupPhase5Env(t)

	protectedName := fmt.Sprintf("p5-protected-%d", time.Now().UnixNano())
	competitorName := fmt.Sprintf("p5-competitor-%d", time.Now().UnixNano())

	// Render the protected RTJ (with policy).
	protectedManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase5/rtj-with-policy.yaml"),
		map[string]string{
			"__RTJ_NAME__":         protectedName,
			"__DEV_NAMESPACE__":    env.namespace,
			"__TRAINER_IMAGE__":    env.trainerImage,
			"__PRIORITY_CLASS__":   "phase5-low",
			"__POLICY_NAME__":      "dev-checkpoint-priority",
			"__SLEEP_PER_STEP__":   "5",
			"__CHECKPOINT_EVERY__": "3",
		},
	)
	defer os.Remove(protectedManifest)

	// Render the competitor RTJ (no policy — raw base priority only).
	competitorManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase5/rtj-no-policy.yaml"),
		map[string]string{
			"__RTJ_NAME__":         competitorName,
			"__DEV_NAMESPACE__":    env.namespace,
			"__TRAINER_IMAGE__":    env.trainerImage,
			"__PRIORITY_CLASS__":   "phase5-low",
			"__SLEEP_PER_STEP__":   "5",
			"__CHECKPOINT_EVERY__": "3",
		},
	)
	defer os.Remove(competitorManifest)

	// Cleanup both RTJs at test end.
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, protectedName, "--ignore-not-found=true")
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, competitorName, "--ignore-not-found=true")
	defer cleanupPhase5RTJ(t, env, protectedName, 1)
	defer cleanupPhase5RTJ(t, env, competitorName, 1)

	// ---- Step 1: Submit RTJ A and wait for it to reach Running + Protected. ----
	t.Log("Step 1: submitting protected RTJ A")
	runKubectl(t, env.repoRoot, "apply", "-f", protectedManifest)

	waitForPhase5WorkloadOwnedBy(t, env.repoRoot, env.namespace,
		"ResumableTrainingJob", protectedName,
		2*time.Minute, env.operatorLogs, env.portForward)

	protectedRunning := waitForPhase5RTJState(
		t, env.repoRoot, env.namespace, protectedName,
		"Running with Protected preemption state",
		4*time.Minute, env.operatorLogs, env.portForward,
		func(view phase5RTJView) bool {
			return view.Status.Phase == "Running" &&
				view.Status.PriorityShaping != nil &&
				view.Status.PriorityShaping.PreemptionState == "Protected"
		},
	)

	// ---- Step 2: Verify priority shaping status and observability. ----
	t.Log("Step 2: verifying priority shaping status on RTJ A")

	assertPriorityShapingState(t, protectedRunning, "Protected")

	// Effective priority should be base (100) + protectedBoost (50) = 150.
	if protectedRunning.Status.PriorityShaping.BasePriority != 100 {
		t.Fatalf("expected basePriority 100, got %d",
			protectedRunning.Status.PriorityShaping.BasePriority)
	}
	assertEffectivePriorityAbove(t, protectedRunning, 100)

	// Verify the PriorityShaping condition is set.
	if !hasPriorityShapingCondition(protectedRunning, "True") {
		t.Fatalf("expected PriorityShaping condition with status=True, conditions=%+v",
			protectedRunning.Status.Conditions)
	}

	// Verify annotations reflect the priority shaping state.
	epAnnotation := protectedRunning.Metadata.Annotations["training.checkpoint.example.io/effective-priority"]
	if epAnnotation == "" {
		t.Fatalf("expected effective-priority annotation to be set")
	}
	psAnnotation := protectedRunning.Metadata.Annotations["training.checkpoint.example.io/preemption-state"]
	if psAnnotation != "Protected" {
		t.Fatalf("expected preemption-state annotation to be Protected, got %q", psAnnotation)
	}

	// ---- Step 3: Verify the Workload.Spec.Priority reflects the effective priority. ----
	t.Log("Step 3: verifying Workload.Spec.Priority is the effective (boosted) priority")

	workloadA, _, err := findPhase5WorkloadOwnedBy(env.repoRoot, env.namespace,
		"ResumableTrainingJob", protectedName)
	if err != nil {
		t.Fatalf("find workload for protected RTJ: %v", err)
	}
	if workloadA.Spec.Priority == nil {
		t.Fatalf("expected Workload.Spec.Priority to be set")
	}
	if *workloadA.Spec.Priority != protectedRunning.Status.PriorityShaping.EffectivePriority {
		t.Fatalf("expected Workload priority %d to match effective priority %d",
			*workloadA.Spec.Priority,
			protectedRunning.Status.PriorityShaping.EffectivePriority)
	}

	// ---- Step 4: Submit competitor RTJ B at same base priority. ----
	t.Log("Step 4: submitting competitor RTJ B (same base priority, no policy)")
	runKubectl(t, env.repoRoot, "apply", "-f", competitorManifest)

	waitForPhase5WorkloadOwnedBy(t, env.repoRoot, env.namespace,
		"ResumableTrainingJob", competitorName,
		2*time.Minute, env.operatorLogs, env.portForward)

	// ---- Step 5: Verify B remains pending while A is in the protection window. ----
	t.Log("Step 5: verifying competitor RTJ B stays Queued (cannot preempt protected A)")

	// Wait briefly, then check B is still queued and A is still running.
	// The protection window is 30s. We check over a 15s window to be safe.
	checkDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(checkDeadline) {
		competitorView, err := getPhase5RTJ(env.repoRoot, env.namespace, competitorName)
		if err == nil {
			// B must not be Running — it should be Pending or Queued.
			if competitorView.Status.Phase == "Running" {
				t.Fatalf("competitor RTJ B should not be Running while A is protected; "+
					"B phase=%s, A effective priority=%d",
					competitorView.Status.Phase,
					protectedRunning.Status.PriorityShaping.EffectivePriority)
			}
		}

		// A must still be Running.
		protectedView, err := getPhase5RTJ(env.repoRoot, env.namespace, protectedName)
		if err == nil {
			if protectedView.Status.Phase != "Running" &&
				protectedView.Status.Phase != "Starting" {
				// A should not have been preempted during protection.
				if protectedView.Status.Phase == "YieldRequested" ||
					protectedView.Status.Phase == "Draining" ||
					protectedView.Status.Phase == "Queued" {
					t.Fatalf("protected RTJ A was preempted during protection window; "+
						"A phase=%s, expected Running",
						protectedView.Status.Phase)
				}
			}
		}

		time.Sleep(3 * time.Second)
	}

	// Final state check.
	competitorFinal, err := getPhase5RTJ(env.repoRoot, env.namespace, competitorName)
	if err != nil {
		t.Fatalf("get competitor RTJ B: %v", err)
	}
	if competitorFinal.Status.Phase == "Running" {
		t.Fatalf("competitor RTJ B should remain pending, but reached Running")
	}

	protectedFinal, err := getPhase5RTJ(env.repoRoot, env.namespace, protectedName)
	if err != nil {
		t.Fatalf("get protected RTJ A: %v", err)
	}
	if protectedFinal.Status.Phase != "Running" {
		// Allow Starting as well, since protection applies during Starting too.
		if protectedFinal.Status.Phase != "Starting" {
			t.Fatalf("expected protected RTJ A to still be Running, got %s",
				protectedFinal.Status.Phase)
		}
	}

	// Verify A's priority shaping is still reporting Protected (if still within window)
	// or Active (if protection just expired — checkpoints every 15s keep it fresh).
	if protectedFinal.Status.PriorityShaping != nil {
		state := protectedFinal.Status.PriorityShaping.PreemptionState
		if state != "Protected" && state != "Active" {
			t.Fatalf("expected preemptionState Protected or Active, got %s", state)
		}
	}

	t.Log("Protection window test passed: competitor B remained pending while A was protected")

	// ---- Step 6: Verify B has no priority shaping status (no policy). ----
	if competitorFinal.Status.PriorityShaping != nil {
		t.Fatalf("expected competitor B (no policy) to have nil priorityShaping status, got %+v",
			competitorFinal.Status.PriorityShaping)
	}

	// Verify B's Workload uses raw base priority.
	workloadB, found, err := findPhase5WorkloadOwnedBy(env.repoRoot, env.namespace,
		"ResumableTrainingJob", competitorName)
	if err != nil {
		t.Fatalf("find workload for competitor RTJ B: %v", err)
	}
	if found && workloadB.Spec.Priority != nil {
		// B should have base priority 100 (no shaping).
		if *workloadB.Spec.Priority > 100 {
			t.Fatalf("expected competitor B Workload priority <= 100, got %d",
				*workloadB.Spec.Priority)
		}
	}

	// Log the final priority comparison for diagnostic clarity.
	t.Logf("Final priority comparison: A effective=%d (state=%s), B base=100",
		protectedFinal.Status.PriorityShaping.EffectivePriority,
		protectedFinal.Status.PriorityShaping.PreemptionState)
	if found && workloadB.Spec.Priority != nil {
		t.Logf("Workload priorities: A=%d, B=%d",
			*workloadA.Spec.Priority, *workloadB.Spec.Priority)
	}

	// The test intentionally does NOT wait for protection to expire. The
	// lifecycle from protected → stale → preempted is covered by
	// TestPriorityDropEnablesPreemption.

	// Verify the protected RTJ has the ProtectedUntil timestamp set.
	if protectedRunning.Status.PriorityShaping.ProtectedUntil == "" {
		t.Log("Warning: protectedUntil was not populated; may be a timing issue")
	}

	// Verify the policy ref is reflected in the status.
	if protectedRunning.Status.PriorityShaping.AppliedPolicyRef == "" {
		t.Log("Warning: appliedPolicyRef not populated in priorityShaping status")
	}

	// Log key annotations for diagnosis.
	for k, v := range protectedRunning.Metadata.Annotations {
		if strings.HasPrefix(k, "training.checkpoint.example.io/") {
			t.Logf("Annotation %s = %s", k, v)
		}
	}
}
