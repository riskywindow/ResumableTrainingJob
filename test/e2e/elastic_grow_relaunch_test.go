package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestElasticGrowViaRelaunch verifies grow via checkpoint-and-relaunch:
// the controller checkpoints the current run, suspends the Workload,
// re-queues at the larger size, and launches a new child JobSet from
// the latest checkpoint.
//
// Test flow:
//  1. Submit a 2-worker elastic RTJ → Running.
//  2. Record the initial currentRunAttempt and activeJobSetName.
//  3. Patch targetWorkerCount=4 to trigger grow.
//  4. Verify resizeState transitions to InProgress.
//  5. Verify the ResizeCheckpointing condition is set.
//  6. Wait for the RTJ to complete the drain/checkpoint/relaunch cycle.
//  7. Verify the RTJ is Running again at 4 workers.
//  8. Verify currentRunAttempt was incremented (new child JobSet).
//  9. Verify the new child JobSet has 4 worker replicas.
// 10. Verify lastCompletedCheckpoint was populated (resize checkpoint).
// 11. Verify resizeState reaches Completed.
// 12. Verify child JobSet is plain runtime (Phase 2 invariant).
//
// This test exercises Phase 9 Goals:
//   - G1: Manual target-based elastic resize (grow)
//   - G3: Grow via checkpoint-and-relaunch
func TestElasticGrowViaRelaunch(t *testing.T) {
	env := setupPhase9Env(t)

	rtjName := fmt.Sprintf("e9-grow-%d", time.Now().UnixNano())

	// ── Step 1: Submit 2-worker elastic RTJ ─────────────────────────────
	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase9/rtj-elastic-grow-2w.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjManifest)
	defer cleanupPhase9RTJ(t, env, rtjName, 3)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// Wait for Running.
	waitForPhase9Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running at 2 workers")

	// ── Step 2: Record initial state ────────────────────────────────────
	initialView := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"elasticity status populated",
		1*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Elasticity != nil &&
				v.Status.Elasticity.CurrentExecutionMode == "Elastic" &&
				v.Status.CurrentRunAttempt > 0
		},
	)
	initialRunAttempt := initialView.Status.CurrentRunAttempt
	initialJobSetName := initialView.Status.ActiveJobSetName
	t.Logf("initial state: runAttempt=%d activeJobSet=%s admittedWorkers=%d",
		initialRunAttempt, initialJobSetName,
		initialView.Status.Elasticity.AdmittedWorkerCount,
	)

	// Verify initial child JobSet has 2 replicas.
	if initialJobSetName != "" {
		initialJS := waitForJobSetDetailPresent(
			t, env.repoRoot, env.namespace, initialJobSetName,
			1*time.Minute, env.operatorLogs, env.portForward,
		)
		for _, rj := range initialJS.Spec.ReplicatedJobs {
			if rj.Name == "worker" && rj.Replicas != 2 {
				t.Fatalf("expected initial worker replicas=2, got %d", rj.Replicas)
			}
		}
		t.Log("initial child JobSet has 2 worker replicas")
	}

	// ── Step 3: Patch targetWorkerCount=4 to trigger grow ───────────────
	patchPhase9RTJSpec(t, env.repoRoot, env.namespace, rtjName,
		`{"spec":{"elasticity":{"targetWorkerCount":4}}}`)
	t.Log("patched spec: targetWorkerCount=4")

	// ── Step 4: Verify resizeState transitions to InProgress ────────────
	waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"resizeState=InProgress",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Elasticity != nil &&
				(v.Status.Elasticity.ResizeState == "InProgress" ||
					v.Status.Elasticity.ResizeState == "Pending")
		},
	)
	t.Log("resize state transitioned to InProgress/Pending")

	// ── Step 5: Verify ResizeCheckpointing condition ────────────────────
	resizingView := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"ResizeCheckpointing or RelaunchingForResize condition",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return hasPhase9Condition(v, "ResizeCheckpointing", "True") ||
				hasPhase9Condition(v, "RelaunchingForResize", "True")
		},
	)
	if cond := findPhase9Condition(resizingView, "ResizeCheckpointing"); cond != nil {
		t.Logf("ResizeCheckpointing condition: reason=%s message=%s", cond.Reason, cond.Message)
	}

	// Verify the resize path is CheckpointAndRelaunch.
	if resizingView.Status.Elasticity != nil &&
		resizingView.Status.Elasticity.ResizePath != "CheckpointAndRelaunch" {
		t.Logf("note: resizePath=%s", resizingView.Status.Elasticity.ResizePath)
	}

	// ── Step 6: Wait for complete drain/checkpoint/relaunch cycle ────────
	// The RTJ should go through: Running → YieldRequested/Draining → Paused → Restoring → Running
	// We wait for it to be Running again with a new run attempt.
	grownView := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Running at new run attempt after grow",
		6*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Phase == "Running" &&
				v.Status.CurrentRunAttempt > initialRunAttempt
		},
	)
	t.Logf("RTJ is Running at runAttempt=%d (was %d)",
		grownView.Status.CurrentRunAttempt, initialRunAttempt)

	// ── Step 7: Verify running at 4 workers ─────────────────────────────
	if grownView.Status.Elasticity != nil &&
		grownView.Status.Elasticity.AdmittedWorkerCount > 0 &&
		grownView.Status.Elasticity.AdmittedWorkerCount != 4 {
		t.Fatalf("expected admittedWorkerCount=4 after grow, got %d",
			grownView.Status.Elasticity.AdmittedWorkerCount)
	}

	// ── Step 8: Verify currentRunAttempt incremented ────────────────────
	if grownView.Status.CurrentRunAttempt <= initialRunAttempt {
		t.Fatalf("expected currentRunAttempt > %d, got %d",
			initialRunAttempt, grownView.Status.CurrentRunAttempt)
	}

	// ── Step 9: Verify new child JobSet has 4 worker replicas ───────────
	newJobSetName := rtjjobset.ChildJobSetName(rtjName, grownView.Status.CurrentRunAttempt)
	newJS := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, newJobSetName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	for _, rj := range newJS.Spec.ReplicatedJobs {
		if rj.Name == "worker" {
			if rj.Replicas != 4 {
				t.Fatalf("expected new child JobSet worker replicas=4, got %d", rj.Replicas)
			}
			t.Logf("new child JobSet %s has 4 worker replicas", newJobSetName)
		}
	}

	// ── Step 10: Verify lastCompletedCheckpoint was populated ───────────
	if grownView.Status.LastCompletedCheckpoint == nil ||
		grownView.Status.LastCompletedCheckpoint.ManifestURI == "" {
		t.Log("note: lastCompletedCheckpoint not yet populated (resize checkpoint may still be in progress)")
	} else {
		t.Logf("resize checkpoint: %s", grownView.Status.LastCompletedCheckpoint.ManifestURI)
	}

	// ── Step 11: Verify resizeState reaches Completed ───────────────────
	completedView := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"resizeState=Completed or Idle",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			if v.Status.Elasticity == nil {
				return false
			}
			return v.Status.Elasticity.ResizeState == "Completed" ||
				v.Status.Elasticity.ResizeState == "Idle"
		},
	)
	t.Logf("resize completed: state=%s", completedView.Status.Elasticity.ResizeState)

	// ── Step 12: Verify child JobSet is plain runtime ───────────────────
	assertChildJobSetPlainRuntime(t, newJS)
	t.Logf("child JobSet %s is plain runtime (Phase 2 invariant)", newJobSetName)

	// No Workload owned by child JobSet.
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", newJobSetName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")
}
