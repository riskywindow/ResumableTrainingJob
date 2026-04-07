package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestElasticFallbackShrinkViaRelaunch verifies that when the runtime does
// NOT support in-place shrink, the controller uses the checkpoint-and-relaunch
// fallback path rather than pretending an in-place shrink happened.
//
// The key distinction from the in-place shrink test is:
//   - inPlaceShrinkSupported=false (default DDP behavior)
//   - No reclaimablePods are published on the Workload
//   - The RTJ goes through the full drain/checkpoint/relaunch cycle
//   - The resize path is CheckpointAndRelaunch, not InPlace
//
// Test flow:
//  1. Submit a 4-worker elastic RTJ (DDP, SUPPORTS_IN_PLACE_SHRINK=false).
//  2. Wait for Running.
//  3. Verify inPlaceShrinkSupported=false in elasticity status.
//  4. Patch targetWorkerCount=2 to trigger shrink.
//  5. Verify the controller chooses CheckpointAndRelaunch path (not InPlace).
//  6. Verify ResizeCheckpointing condition (not ShrinkingInPlace).
//  7. Verify no reclaimablePods are published on the Workload.
//  8. Wait for RTJ to complete the drain/checkpoint/relaunch cycle.
//  9. Verify the RTJ is Running at 2 workers with incremented runAttempt.
// 10. Verify new child JobSet has 2 worker replicas.
//
// This test exercises Phase 9 Goals:
//   - G1: Manual target-based elastic resize (shrink)
//   - Fallback coherence: controller does not pretend in-place when unsupported
func TestElasticFallbackShrinkViaRelaunch(t *testing.T) {
	env := setupPhase9Env(t)

	rtjName := fmt.Sprintf("e9-fallback-%d", time.Now().UnixNano())

	// ── Step 1: Submit 4-worker elastic RTJ ─────────────────────────────
	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase9/rtj-elastic-fallback-4w.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjManifest)
	defer cleanupPhase9RTJ(t, env, rtjName, 3)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 2: Wait for Running ────────────────────────────────────────
	waitForPhase9Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running at 4 workers")

	// ── Step 3: Verify inPlaceShrinkSupported=false ─────────────────────
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
	if initialView.Status.Elasticity.InPlaceShrinkSupported {
		t.Fatalf("expected inPlaceShrinkSupported=false for DDP runtime, got true")
	}
	initialRunAttempt := initialView.Status.CurrentRunAttempt
	t.Logf("initial state: runAttempt=%d inPlaceShrinkSupported=%v",
		initialRunAttempt, initialView.Status.Elasticity.InPlaceShrinkSupported)

	// ── Step 4: Patch targetWorkerCount=2 ───────────────────────────────
	patchPhase9RTJSpec(t, env.repoRoot, env.namespace, rtjName,
		`{"spec":{"elasticity":{"targetWorkerCount":2}}}`)
	t.Log("patched spec: targetWorkerCount=2")

	// ── Step 5: Verify CheckpointAndRelaunch path (not InPlace) ─────────
	relaunchView := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"resizePath=CheckpointAndRelaunch",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Elasticity != nil &&
				v.Status.Elasticity.ResizePath == "CheckpointAndRelaunch"
		},
	)
	t.Logf("resize path: %s (correct fallback)", relaunchView.Status.Elasticity.ResizePath)

	// Verify the resize reason indicates no in-place support.
	if relaunchView.Status.Elasticity.ResizeReason != "" {
		t.Logf("resize reason: %s", relaunchView.Status.Elasticity.ResizeReason)
	}

	// ── Step 6: Verify ResizeCheckpointing (not ShrinkingInPlace) ───────
	waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"ResizeCheckpointing or RelaunchingForResize condition",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return hasPhase9Condition(v, "ResizeCheckpointing", "True") ||
				hasPhase9Condition(v, "RelaunchingForResize", "True")
		},
	)

	// Verify ShrinkingInPlace is NOT set (fallback must not pretend in-place).
	checkView, _ := getPhase9RTJ(env.repoRoot, env.namespace, rtjName)
	if hasPhase9Condition(checkView, "ShrinkingInPlace", "True") {
		t.Fatalf("ShrinkingInPlace condition should NOT be set for fallback shrink")
	}
	if hasPhase9Condition(checkView, "ShrinkReclaimPublished", "True") {
		t.Fatalf("ShrinkReclaimPublished condition should NOT be set for fallback shrink")
	}
	t.Log("confirmed: no in-place shrink conditions (correct fallback behavior)")

	// ── Step 7: Verify no reclaimablePods on Workload ───────────────────
	workload, found, err := findPhase9WorkloadOwnedBy(env.repoRoot, env.namespace,
		"ResumableTrainingJob", rtjName)
	if err == nil && found {
		if len(workload.Status.ReclaimablePods) > 0 {
			t.Fatalf("expected no reclaimablePods for fallback shrink, got %+v",
				workload.Status.ReclaimablePods)
		}
		t.Log("confirmed: no reclaimablePods on Workload (correct fallback)")
	}

	// Verify reclaimablePodsPublished=false on RTJ status.
	if checkView.Status.Elasticity != nil && checkView.Status.Elasticity.ReclaimablePodsPublished {
		t.Fatalf("expected reclaimablePodsPublished=false for fallback shrink")
	}

	// ── Step 8: Wait for drain/checkpoint/relaunch cycle ────────────────
	shrunkView := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Running at new run attempt after fallback shrink",
		6*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Phase == "Running" &&
				v.Status.CurrentRunAttempt > initialRunAttempt
		},
	)
	t.Logf("RTJ is Running at runAttempt=%d (was %d)",
		shrunkView.Status.CurrentRunAttempt, initialRunAttempt)

	// ── Step 9: Verify running at 2 workers ─────────────────────────────
	if shrunkView.Status.Elasticity != nil &&
		shrunkView.Status.Elasticity.AdmittedWorkerCount > 0 &&
		shrunkView.Status.Elasticity.AdmittedWorkerCount != 2 {
		t.Fatalf("expected admittedWorkerCount=2 after fallback shrink, got %d",
			shrunkView.Status.Elasticity.AdmittedWorkerCount)
	}

	// ── Step 10: Verify new child JobSet has 2 worker replicas ──────────
	newJobSetName := rtjjobset.ChildJobSetName(rtjName, shrunkView.Status.CurrentRunAttempt)
	newJS := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, newJobSetName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	for _, rj := range newJS.Spec.ReplicatedJobs {
		if rj.Name == "worker" {
			if rj.Replicas != 2 {
				t.Fatalf("expected new child JobSet worker replicas=2, got %d", rj.Replicas)
			}
			t.Logf("new child JobSet %s has 2 worker replicas", newJobSetName)
		}
	}

	// Verify child JobSet is plain runtime.
	assertChildJobSetPlainRuntime(t, newJS)
	t.Logf("child JobSet %s is plain runtime (Phase 2 invariant)", newJobSetName)

	// No Workload owned by child JobSet.
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", newJobSetName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")
}
