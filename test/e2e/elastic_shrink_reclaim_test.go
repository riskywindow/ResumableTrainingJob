package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestElasticShrinkDynamicReclaim verifies the in-place shrink path with
// dynamic quota reclaim via Workload.status.reclaimablePods.
//
// Test flow:
//  1. Submit RTJ A (4 workers, 1000m CPU) → Running.
//  2. Verify RTJ A elasticity status shows Elastic execution mode.
//  3. Submit RTJ B (2 workers, 500m CPU) → Queued (total quota = 1250m).
//  4. Pre-patch RTJ A status: inPlaceShrinkSupported=true (fixture knob).
//  5. Patch RTJ A spec: targetWorkerCount=2.
//  6. Wait for controller to publish reclaimablePods on the Workload.
//  7. Verify RTJ A status.elasticity.reclaimablePodsPublished=true.
//  8. Verify Workload has reclaimablePods entry for "workers" PodSet.
//  9. Verify RTJ A remains Running (not fully evicted).
// 10. Verify RTJ B transitions toward admission using the reclaimed quota.
// 11. Verify child JobSets are plain runtime (Phase 2 invariant).
//
// This test exercises Phase 9 Goals:
//   - G1: Manual target-based elastic resize (shrink)
//   - G2: In-place shrink via reclaimablePods SSA patching
//   - G4: Dynamic quota reclaim (RTJ A shrinks → RTJ B admitted)
func TestElasticShrinkDynamicReclaim(t *testing.T) {
	env := setupPhase9Env(t)

	rtjAName := fmt.Sprintf("e9-shrink-a-%d", time.Now().UnixNano())
	rtjBName := fmt.Sprintf("e9-shrink-b-%d", time.Now().UnixNano())

	// ── Step 1: Submit RTJ A (4 workers) ────────────────────────────────
	rtjAManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase9/rtj-elastic-shrink-4w.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjAName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjAManifest)
	defer cleanupPhase9RTJ(t, env, rtjAName, 2)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjAManifest)

	// Wait for RTJ A to be Running.
	waitForPhase9Phase(
		t, env.repoRoot, env.namespace, rtjAName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ A is Running at 4 workers")

	// ── Step 2: Verify elasticity status ────────────────────────────────
	viewA := waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjAName,
		"elasticity status shows Elastic mode",
		1*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Elasticity != nil &&
				v.Status.Elasticity.CurrentExecutionMode == "Elastic"
		},
	)
	if viewA.Status.Elasticity.AdmittedWorkerCount != 4 {
		t.Fatalf("expected admittedWorkerCount=4, got %d", viewA.Status.Elasticity.AdmittedWorkerCount)
	}
	t.Logf("RTJ A elasticity: mode=%s admitted=%d target=%d",
		viewA.Status.Elasticity.CurrentExecutionMode,
		viewA.Status.Elasticity.AdmittedWorkerCount,
		viewA.Status.Elasticity.TargetWorkerCount,
	)

	// ── Step 3: Submit RTJ B (2 workers) — should be Queued ─────────────
	rtjBManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase9/rtj-elastic-queued-2w.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjBName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjBManifest)
	defer cleanupPhase9RTJ(t, env, rtjBName, 2)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjBManifest)

	// Verify RTJ B is Queued (insufficient quota).
	waitForPhase9Phase(
		t, env.repoRoot, env.namespace, rtjBName,
		"Queued", 2*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ B is Queued (blocked by quota exhaustion)")

	// ── Step 4: Pre-patch RTJ A status with inPlaceShrinkSupported=true ──
	// This is a deterministic fixture knob: the controller reads
	// status.elasticity.inPlaceShrinkSupported to choose the shrink path.
	patchPhase9RTJStatus(t, env.repoRoot, env.namespace, rtjAName,
		`{"status":{"elasticity":{"inPlaceShrinkSupported":true}}}`)
	t.Log("patched RTJ A status: inPlaceShrinkSupported=true")

	// ── Step 5: Patch RTJ A spec: targetWorkerCount=2 ───────────────────
	patchPhase9RTJSpec(t, env.repoRoot, env.namespace, rtjAName,
		`{"spec":{"elasticity":{"targetWorkerCount":2}}}`)
	t.Log("patched RTJ A spec: targetWorkerCount=2")

	// ── Step 6: Wait for reclaimablePods on Workload ────────────────────
	// The controller should evaluate PlanShrinkInPlace and publish
	// reclaimablePods via SSA to the Workload status.
	workload := waitForPhase9WorkloadReclaimablePods(
		t, env.repoRoot, env.namespace, rtjAName,
		"workers", 2,
		3*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Logf("Workload %s has reclaimablePods: %+v", workload.Metadata.Name, workload.Status.ReclaimablePods)

	// Verify the reclaimablePods entry content.
	found := false
	for _, rp := range workload.Status.ReclaimablePods {
		if rp.Name == "workers" && rp.Count == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reclaimablePods entry {name:workers, count:2}; got %+v",
			workload.Status.ReclaimablePods)
	}

	// ── Step 7: Verify RTJ A status.elasticity.reclaimablePodsPublished ──
	waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjAName,
		"reclaimablePodsPublished=true",
		1*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Elasticity != nil &&
				v.Status.Elasticity.ReclaimablePodsPublished
		},
	)
	t.Log("RTJ A elasticity: reclaimablePodsPublished=true")

	// ── Step 8: Verify RTJ A remains Running (not fully evicted) ────────
	viewAfterShrink, err := getPhase9RTJ(env.repoRoot, env.namespace, rtjAName)
	if err != nil {
		t.Fatalf("get RTJ A after shrink: %v", err)
	}
	if viewAfterShrink.Status.Phase != "Running" {
		t.Fatalf("expected RTJ A to remain Running after in-place shrink, got phase=%s",
			viewAfterShrink.Status.Phase)
	}
	t.Log("RTJ A remains Running after in-place shrink (not evicted)")

	// Verify resize state is InProgress (waiting for pod termination).
	if viewAfterShrink.Status.Elasticity.ResizeState != "InProgress" {
		t.Logf("note: RTJ A resizeState=%s (expected InProgress for active shrink)",
			viewAfterShrink.Status.Elasticity.ResizeState)
	}

	// Verify the resize path is InPlace.
	if viewAfterShrink.Status.Elasticity.ResizePath != "InPlace" {
		t.Fatalf("expected resizePath=InPlace, got %s",
			viewAfterShrink.Status.Elasticity.ResizePath)
	}
	t.Log("RTJ A resizePath=InPlace (correct)")

	// ── Step 9: Verify RTJ B progresses toward admission ────────────────
	// After reclaimablePods releases 500m (2 workers x 250m), Kueue should
	// have enough free quota (1250m - 500m used + 500m reclaimed = 750m free)
	// to admit RTJ B (500m). This may take a Kueue scheduling cycle.
	waitForPhase9RTJState(
		t, env.repoRoot, env.namespace, rtjBName,
		"RTJ B admitted or running (reclaimed quota)",
		4*time.Minute, env.operatorLogs, env.portForward,
		func(v phase9RTJView) bool {
			return v.Status.Phase == "Running" ||
				v.Status.Phase == "Admitted" ||
				v.Status.Phase == "Starting"
		},
	)
	t.Log("RTJ B progressed past Queued (dynamic reclaim successful)")

	// ── Step 10: Verify child JobSets are plain runtime ─────────────────
	viewAFinal, _ := getPhase9RTJ(env.repoRoot, env.namespace, rtjAName)
	if viewAFinal.Status.ActiveJobSetName != "" {
		jsA, err := getJobSetDetail(env.repoRoot, env.namespace, viewAFinal.Status.ActiveJobSetName)
		if err == nil {
			assertChildJobSetPlainRuntime(t, jsA)
			t.Logf("RTJ A child JobSet %s is plain runtime", viewAFinal.Status.ActiveJobSetName)
		}
	}

	viewBFinal, _ := getPhase9RTJ(env.repoRoot, env.namespace, rtjBName)
	if viewBFinal.Status.ActiveJobSetName != "" {
		jsB, err := getJobSetDetail(env.repoRoot, env.namespace, viewBFinal.Status.ActiveJobSetName)
		if err == nil {
			assertChildJobSetPlainRuntime(t, jsB)
			t.Logf("RTJ B child JobSet %s is plain runtime", viewBFinal.Status.ActiveJobSetName)
		}
	}

	// Final Phase 2 invariant: no Workload owned by child JobSets.
	if viewAFinal.Status.ActiveJobSetName != "" {
		assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", viewAFinal.Status.ActiveJobSetName)
	}
	if viewBFinal.Status.ActiveJobSetName != "" {
		assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", viewBFinal.Status.ActiveJobSetName)
	}
	t.Log("Phase 2 invariant preserved: no Workload owned by child JobSets")
}
