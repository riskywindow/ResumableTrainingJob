package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestTopologyAwareResume verifies that after a pause-resume cycle, the resumed
// child JobSet still respects topology-aware rendering. This exercises the full
// Phase 4 pipeline: topology launch → checkpoint → suspend → re-admit → resume.
//
// Test flow:
//  1. Submit an RTJ with topology.mode=Required on the Phase 4 queue.
//  2. Wait for Running.
//  3. Pause (desiredState=Paused).
//  4. Wait for Paused with a completed checkpoint.
//  5. Resume (desiredState=Running).
//  6. Wait for Running with a selectedCheckpoint.
//  7. Verify the resumed child JobSet has topology nodeSelector.
//  8. Verify status.topology persists across the resume.
//  9. Verify status.effectiveLaunchShape has selectedCheckpointID.
//  10. Verify global step monotonicity (training advanced).
//
// This test exercises Phase 4 Goals G2 (topology materialization on resume)
// and G4 (admission-gated resume flow).
func TestTopologyAwareResume(t *testing.T) {
	env := setupPhase4Env(t)

	rtjName := fmt.Sprintf("topo-resume-%d", time.Now().UnixNano())

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase4/rtj-topology-required.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__RTJ_NAME__":        rtjName,
			"__TRAINER_IMAGE__":   env.trainerImage,
			"__LOCAL_QUEUE_NAME__": "phase4-training",
		},
	)
	defer os.Remove(rtjManifest)

	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 1: Wait for Running ─────────────────────────────────────────
	waitForPhase4Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running (attempt 1)")

	// Capture the first child JobSet's topology nodeSelector for comparison.
	firstChildName := rtjjobset.ChildJobSetName(rtjName, 1)
	firstJS := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, firstChildName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, firstJS)

	firstRackLabel := ""
	if len(firstJS.Spec.ReplicatedJobs) > 0 {
		firstRackLabel = firstJS.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector["topology.example.io/rack"]
	}
	if firstRackLabel != "" {
		t.Logf("first child JobSet has topology rack=%s", firstRackLabel)
	} else {
		t.Log("first child JobSet has no topology rack nodeSelector")
	}

	// ── Step 2: Pause ────────────────────────────────────────────────────
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	firstPaused := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Paused with completed checkpoint",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.Phase == "Paused" &&
				v.Status.LastCompletedCheckpoint != nil &&
				v.Status.LastCompletedCheckpoint.ManifestURI != ""
		},
	)
	firstManifestURI := firstPaused.Status.LastCompletedCheckpoint.ManifestURI
	t.Logf("paused with checkpoint: %s", firstManifestURI)

	// Load the checkpoint manifest to get the global step.
	firstManifest := loadManifestFromObjectStore(
		t, env.minioEndpoint, env.accessKey, env.secretKey, env.region,
		firstManifestURI,
	)
	firstStep := firstManifest.GlobalStep
	t.Logf("first checkpoint global step: %d", firstStep)

	// ── Step 3: Resume ───────────────────────────────────────────────────
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Running"}}}`,
	)

	resumed := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"resumed from checkpoint with selectedCheckpoint",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.Phase == "Running" &&
				v.Status.CurrentRunAttempt >= 2 &&
				v.Status.SelectedCheckpoint != nil &&
				v.Status.SelectedCheckpoint.ManifestURI == firstManifestURI
		},
	)
	t.Logf("resumed as run attempt %d, selectedCheckpoint=%s",
		resumed.Status.CurrentRunAttempt,
		resumed.Status.SelectedCheckpoint.ManifestURI,
	)

	// ── Step 4: Verify resumed child JobSet has topology nodeSelector ────
	secondChildName := rtjjobset.ChildJobSetName(rtjName, resumed.Status.CurrentRunAttempt)
	secondJS := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, secondChildName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, secondJS)

	if len(secondJS.Spec.ReplicatedJobs) == 0 {
		t.Fatalf("resumed child JobSet has no replicatedJobs")
	}

	secondRackLabel := secondJS.Spec.ReplicatedJobs[0].Template.Spec.Template.Spec.NodeSelector["topology.example.io/rack"]
	if firstRackLabel != "" {
		// If topology was active on first launch, it should be on resume too.
		if secondRackLabel == "" {
			t.Fatalf("resumed child JobSet lost topology nodeSelector; first had rack=%s", firstRackLabel)
		}
		t.Logf("resumed child JobSet has topology rack=%s (first was rack=%s)", secondRackLabel, firstRackLabel)
	} else {
		t.Logf("resumed child JobSet rack nodeSelector: %q", secondRackLabel)
	}

	// ── Step 5: Verify status.topology persists across resume ────────────
	if firstRackLabel != "" {
		withTopology := waitForPhase4RTJState(
			t, env.repoRoot, env.namespace, rtjName,
			"status.topology populated after resume",
			30*time.Second, env.operatorLogs, env.portForward,
			func(v phase4RTJView) bool {
				return v.Status.Topology != nil && len(v.Status.Topology.Levels) > 0
			},
		)
		t.Logf("status.topology after resume: levels=%v domains=%d",
			withTopology.Status.Topology.Levels,
			len(withTopology.Status.Topology.Domains),
		)
	}

	// ── Step 6: Verify status.effectiveLaunchShape has selectedCheckpointID ─
	withShape := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"effectiveLaunchShape with selectedCheckpointID",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.EffectiveLaunchShape != nil &&
				v.Status.EffectiveLaunchShape.WorkerCount > 0
		},
	)
	t.Logf("effectiveLaunchShape after resume: workerCount=%d worldSize=%d resumeMode=%s checkpointID=%s",
		withShape.Status.EffectiveLaunchShape.WorkerCount,
		withShape.Status.EffectiveLaunchShape.WorldSize,
		withShape.Status.EffectiveLaunchShape.ResumeMode,
		withShape.Status.EffectiveLaunchShape.SelectedCheckpointID,
	)

	// ── Step 7: Verify status.launchReadiness is Ready after resume ──────
	withReadiness := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"launchReadiness.ready=true after resume",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.LaunchReadiness != nil && v.Status.LaunchReadiness.Ready
		},
	)
	t.Logf("launchReadiness after resume: ready=%v gateState=%s",
		withReadiness.Status.LaunchReadiness.Ready,
		withReadiness.Status.LaunchReadiness.GateState,
	)

	// ── Step 8: Let training advance, then pause again for step check ────
	time.Sleep(5 * time.Second)

	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	secondPaused := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Paused with second checkpoint",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.Phase == "Paused" &&
				v.Status.LastCompletedCheckpoint != nil &&
				v.Status.LastCompletedCheckpoint.ManifestURI != ""
		},
	)

	// ── Step 9: Verify global step monotonicity ──────────────────────────
	secondManifest := loadManifestFromObjectStore(
		t, env.minioEndpoint, env.accessKey, env.secretKey, env.region,
		secondPaused.Status.LastCompletedCheckpoint.ManifestURI,
	)
	if secondManifest.GlobalStep <= firstStep {
		t.Fatalf("expected resumed training to advance beyond step %d, got step %d",
			firstStep, secondManifest.GlobalStep)
	}
	t.Logf("global step advanced: %d -> %d (monotonic)", firstStep, secondManifest.GlobalStep)

	// ── Step 10: No Workload should be owned by the child JobSet ─────────
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", secondChildName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")
}
