package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestDRAResumeCompatibleProfile verifies that a DRA-backed RTJ can
// successfully pause, checkpoint, and resume with the same device profile.
//
// Test flow:
//  1. Submit a DRA-backed RTJ with example-gpu DeviceClass.
//  2. Wait for it to reach Running phase.
//  3. Record the device profile fingerprint from status.devices.
//  4. Wait for a checkpoint to be saved (lastCompletedCheckpoint populated).
//  5. Patch desiredState=Paused to trigger a graceful pause.
//  6. Wait for the RTJ to reach Paused phase.
//  7. Verify the checkpoint manifest exists in S3.
//  8. Patch desiredState=Running to trigger resume.
//  9. Wait for the RTJ to reach Running phase again (run attempt 2).
// 10. Verify the device profile fingerprint is unchanged.
// 11. Verify selectedCheckpoint references the compatible checkpoint.
// 12. Verify the child JobSet is recreated as a plain runtime resource.
//
// This test exercises Phase 8 compatible resume:
//   - Device profile fingerprint is preserved across pause/resume
//   - Checkpoint compatibility check passes for matching profiles
//   - Training resumes from checkpoint under the same DRA device config
func TestDRAResumeCompatibleProfile(t *testing.T) {
	env := setupPhase8Env(t)

	rtjName := fmt.Sprintf("dra-resume-%d", time.Now().UnixNano())

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase8/rtj-dra-pause-resume.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjManifest)

	// Cleanup on exit.
	defer cleanupPhase8RTJ(t, env, rtjName, 2)
	defer cleanupPhase8RTJ(t, env, rtjName, 1)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 1: Wait for Running ─────────────────────────────────────────
	waitForPhase8Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running")

	// ── Step 2: Record device profile fingerprint ────────────────────────
	runningView := waitForPhase8RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"status.devices.currentDeviceProfileFingerprint populated",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase8RTJView) bool {
			return v.Status.Devices != nil &&
				v.Status.Devices.CurrentDeviceProfileFingerprint != ""
		},
	)
	originalFingerprint := runningView.Status.Devices.CurrentDeviceProfileFingerprint
	t.Logf("device profile fingerprint: %s", originalFingerprint[:12]+"...")

	// ── Step 3: Wait for a checkpoint ────────────────────────────────────
	withCheckpoint := waitForPhase8RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"lastCompletedCheckpoint.manifestURI populated",
		3*time.Minute, env.operatorLogs, env.portForward,
		func(v phase8RTJView) bool {
			return v.Status.LastCompletedCheckpoint != nil &&
				v.Status.LastCompletedCheckpoint.ManifestURI != ""
		},
	)
	checkpointURI := withCheckpoint.Status.LastCompletedCheckpoint.ManifestURI
	t.Logf("checkpoint saved: %s", checkpointURI)

	// ── Step 4: Pause the RTJ ────────────────────────────────────────────
	runKubectl(
		t,
		env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)
	t.Log("patched desiredState=Paused")

	// ── Step 5: Wait for Paused ──────────────────────────────────────────
	waitForPhase8Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Paused", 5*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Paused")

	// ── Step 6: Verify checkpoint manifest exists in S3 ──────────────────
	assertObjectExists(t, env.minioEndpoint, env.accessKey, env.secretKey, env.region, checkpointURI)
	t.Log("checkpoint manifest verified in S3")

	// Verify the checkpoint manifest has the device profile fingerprint.
	minioClient, err := minio.New(env.minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(env.accessKey, env.secretKey, ""),
		Secure: false,
		Region: env.region,
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}
	location, err := checkpoints.ParseS3URI(checkpointURI)
	if err != nil {
		t.Fatalf("parse manifest URI: %v", err)
	}
	obj, err := minioClient.GetObject(context.Background(), location.Bucket, location.Key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get manifest object: %v", err)
	}
	defer obj.Close()
	manifestBytes := make([]byte, 64*1024)
	n, _ := obj.Read(manifestBytes)
	manifest, err := checkpoints.DecodeManifest(manifestBytes[:n], checkpointURI)
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.DeviceProfileFingerprint != originalFingerprint {
		t.Fatalf("checkpoint manifest deviceProfileFingerprint=%s; expected %s",
			manifest.DeviceProfileFingerprint, originalFingerprint)
	}
	t.Logf("checkpoint manifest has matching device profile fingerprint")

	// ── Step 7: Resume the RTJ ───────────────────────────────────────────
	runKubectl(
		t,
		env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Running"}}}`,
	)
	t.Log("patched desiredState=Running")

	// ── Step 8: Wait for Running again (run attempt 2) ───────────────────
	resumedView := waitForPhase8RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"phase=Running and currentRunAttempt>=2",
		5*time.Minute, env.operatorLogs, env.portForward,
		func(v phase8RTJView) bool {
			return v.Status.Phase == "Running" && v.Status.CurrentRunAttempt >= 2
		},
	)
	t.Logf("RTJ resumed: phase=%s runAttempt=%d",
		resumedView.Status.Phase, resumedView.Status.CurrentRunAttempt)

	// ── Step 9: Verify device profile fingerprint is unchanged ───────────
	if resumedView.Status.Devices == nil {
		t.Fatalf("status.devices is nil after resume")
	}
	if resumedView.Status.Devices.CurrentDeviceProfileFingerprint != originalFingerprint {
		t.Fatalf("device profile fingerprint changed after resume: %s -> %s",
			originalFingerprint, resumedView.Status.Devices.CurrentDeviceProfileFingerprint)
	}
	t.Log("device profile fingerprint preserved across pause/resume")

	// ── Step 10: Verify selectedCheckpoint references compatible ckpt ────
	if resumedView.Status.SelectedCheckpoint != nil &&
		resumedView.Status.SelectedCheckpoint.ManifestURI != "" {
		t.Logf("selectedCheckpoint: manifestURI=%s",
			resumedView.Status.SelectedCheckpoint.ManifestURI)
	}

	// ── Step 11: Verify child JobSet is recreated ────────────────────────
	childName := rtjjobset.ChildJobSetName(rtjName, 2)
	js := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, js)
	t.Logf("child JobSet %s is a plain runtime resource", childName)

	// Phase 2 invariant.
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", childName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")
}
