package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFlexibleResume verifies the main Phase 3 story: an RTJ with
// allowWorldSizeChange=true can be paused, checkpointed, and resumed
// while the Phase 3 admission-aware code path is active.
//
// Test flow:
//  1. Create RTJ with allowWorldSizeChange=true on the Phase 3 queue.
//  2. Wait for Running.
//  3. Pause (desiredState=Paused).
//  4. Wait for Paused with a completed checkpoint.
//  5. Resume (desiredState=Running).
//  6. Wait for Running with selectedCheckpoint.
//  7. Let training continue, then pause again.
//  8. Verify global step is monotonically increasing.
//  9. Verify status.restore is populated with restore metadata.
//
// World-size parity:
//
// In this test, the admitted world size equals the requested world size
// (Kueue admits all-or-nothing because no MinCount is set). This exercises
// the Phase 3 admission-aware code path in same-size mode. The code path
// is identical to what runs when world sizes differ — the only difference
// is the restore mode (SameSize instead of Reshard).
//
// Different-size resume requires Kueue partial admission (PodSet.MinCount),
// which is gated behind the experimental flag. That path is validated by
// unit tests in:
//   - internal/checkpoints/compatibility_test.go (8 tests for flexible matching)
//   - internal/checkpoints/selector_test.go (5 tests for cross-size selection)
//   - internal/kueue/rtj_podsets_test.go (8 tests for MinCount synthesis)
//   - sdk/python/tests/test_resume.py (7 tests for DCP resharding)
//
// To run the full different-size e2e path, use the experimental profile:
//
//	make phase3-up PHASE3_PROFILE=experimental
//	go run ./cmd/operator --leader-elect=false --enable-experimental-partial-admission
//	# Then submit deploy/dev/samples/phase3/rtj-partial-admission.yaml
func TestFlexibleResume(t *testing.T) {
	env := setupPhase3Env(t, false)

	rtjName := fmt.Sprintf("flex-resume-%d", time.Now().UnixNano())

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase3/rtj-phase3-flexible.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__RTJ_NAME__":        rtjName,
			"__TRAINER_IMAGE__":   env.trainerImage,
			"__LOCAL_QUEUE_NAME__": "phase3-training",
		},
	)
	defer os.Remove(rtjManifest)

	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 1: Wait for Running ───────────────────────────────────────
	waitForPhase3Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running (attempt 1)")

	// ── Step 2: Pause ──────────────────────────────────────────────────
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	firstPaused := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Paused with completed checkpoint",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
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

	// ── Step 3: Resume ─────────────────────────────────────────────────
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Running"}}}`,
	)

	resumed := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"resumed from checkpoint with selectedCheckpoint matching first pause",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
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

	// Verify allowWorldSizeChange is set on the RTJ spec.
	if !resumed.Spec.Resume.AllowWorldSizeChange {
		t.Fatalf("expected spec.resume.allowWorldSizeChange=true, got false")
	}

	// ── Step 4: Verify status.restore ──────────────────────────────────
	// After resume, status.restore should be populated.
	withRestore := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"status.restore populated after resume",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
			return v.Status.Restore != nil && v.Status.Restore.RestoreMode != ""
		},
	)
	t.Logf("restore status: mode=%s checkpointWorldSize=%d restoreWorldSize=%d",
		withRestore.Status.Restore.RestoreMode,
		withRestore.Status.Restore.LastCheckpointWorldSize,
		withRestore.Status.Restore.LastRestoreWorldSize,
	)

	// In same-size mode, checkpoint and restore world sizes should match.
	if withRestore.Status.Restore.LastCheckpointWorldSize > 0 &&
		withRestore.Status.Restore.LastRestoreWorldSize > 0 &&
		withRestore.Status.Restore.LastCheckpointWorldSize == withRestore.Status.Restore.LastRestoreWorldSize {
		if withRestore.Status.Restore.RestoreMode != "SameSize" {
			t.Fatalf("expected restore mode SameSize when world sizes match, got %q",
				withRestore.Status.Restore.RestoreMode)
		}
		t.Log("restore mode is SameSize (world sizes match, as expected without partial admission)")
	}

	// ── Step 5: Let training advance, then pause again ─────────────────
	time.Sleep(5 * time.Second)

	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	secondPaused := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Paused with second checkpoint",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
			return v.Status.Phase == "Paused" &&
				v.Status.LastCompletedCheckpoint != nil &&
				v.Status.LastCompletedCheckpoint.ManifestURI != ""
		},
	)

	// ── Step 6: Verify global step monotonicity ────────────────────────
	secondManifest := loadManifestFromObjectStore(
		t, env.minioEndpoint, env.accessKey, env.secretKey, env.region,
		secondPaused.Status.LastCompletedCheckpoint.ManifestURI,
	)
	if secondManifest.GlobalStep <= firstStep {
		t.Fatalf("expected resumed training to advance beyond step %d, got step %d",
			firstStep, secondManifest.GlobalStep)
	}
	t.Logf("global step advanced: %d → %d (monotonic)", firstStep, secondManifest.GlobalStep)

	// ── Step 7: Verify admission status is populated ───────────────────
	final := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"final state with admission status",
		15*time.Second, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
			return v.Status.Admission != nil && v.Status.Admission.AdmittedWorkerCount > 0
		},
	)
	t.Logf("final admission status: admittedWorkerCount=%d",
		final.Status.Admission.AdmittedWorkerCount)
}
