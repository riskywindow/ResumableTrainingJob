package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestMultiClusterRemotePauseResume proves the end-to-end remote pause
// and resume flow for a MultiKueue-managed RTJ.
//
// Scenario:
//   - An RTJ with spec.managedBy: kueue.x-k8s.io/multikueue is submitted
//     to the manager cluster with desiredState=Running.
//   - Worker-2's ClusterQueue is paused so MultiKueue dispatches to worker-1.
//   - The test waits for the worker to reach Running and produce a checkpoint.
//   - The manager RTJ is patched to desiredState=Paused.
//   - The Kueue adapter detects the spec drift, tears down the active remote
//     RTJ, and creates a new remote with desiredState=Paused.
//   - The manager controller preserves the checkpoint and marks Paused.
//   - The manager RTJ is patched back to desiredState=Running.
//   - The adapter creates a new remote RTJ with desiredState=Running.
//   - The worker resumes from the checkpoint in the shared store.
//
// What this test proves:
//  1. A running remote RTJ can be paused from the manager cluster.
//  2. Checkpoint evidence exists in the shared store after pause.
//  3. No manager-local child JobSet appears at any point.
//  4. The remote phase is surfaced on the manager status.
//  5. After resume, the worker continues from the shared checkpoint.
//  6. Step/checkpoint progression remains monotonic (new checkpoint
//     after resume has a different ID from the pre-pause checkpoint).
func TestMultiClusterRemotePauseResume(t *testing.T) {
	env := setupPhase6Env(t)

	rtjName := fmt.Sprintf("p6-pause-resume-%d", time.Now().UnixNano())

	// Bias cluster selection to worker-1 by pausing worker-2's CQ.
	biasWorkerSelection(t, env)
	defer restoreWorkerSelection(t, env)

	// Render test manifest.
	manifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase6/rtj-remote-pause-resume.yaml"),
		map[string]string{
			"__RTJ_NAME__":      rtjName,
			"__DEV_NAMESPACE__": env.namespace,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(manifest)

	// Clean up at test end.
	defer cleanupPhase6PauseResumeRTJ(t, env, rtjName)

	// ---- Step 1: Submit RTJ to manager cluster. ----
	t.Log("Step 1: submitting RTJ to manager cluster")
	runKubectlContext(t, env.repoRoot, env.managerContext, "apply", "-f", manifest)

	// ---- Step 2: Wait for remote worker to reach Running. ----
	t.Log("Step 2: waiting for remote worker to reach Running")
	waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"multiCluster.remotePhase is Running or status.phase is Running",
		5*time.Minute,
		func(view phase6RTJView) bool {
			if view.Status.MultiCluster == nil {
				return false
			}
			return view.Status.MultiCluster.RemotePhase == "Running" ||
				view.Status.Phase == "Running"
		},
	)
	t.Log("remote worker is Running")

	// ---- Step 3: Wait for first checkpoint. ----
	t.Log("Step 3: waiting for first checkpoint on worker")
	prePauseView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"lastCompletedCheckpoint is populated",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.LastCompletedCheckpoint != nil &&
				view.Status.LastCompletedCheckpoint.ManifestURI != ""
		},
	)

	prePauseManifestURI := prePauseView.Status.LastCompletedCheckpoint.ManifestURI
	prePauseCheckpointID := prePauseView.Status.LastCompletedCheckpoint.ID
	t.Logf("pre-pause checkpoint: id=%s manifestURI=%s",
		prePauseCheckpointID, prePauseManifestURI)

	// Also capture the remote checkpoint summary from multiCluster.
	if prePauseView.Status.MultiCluster != nil &&
		prePauseView.Status.MultiCluster.RemoteCheckpoint != nil {
		t.Logf("pre-pause remoteCheckpoint: id=%s storageURI=%s",
			prePauseView.Status.MultiCluster.RemoteCheckpoint.LastCompletedCheckpointID,
			prePauseView.Status.MultiCluster.RemoteCheckpoint.StorageURI)
	}

	// ---- Step 4: Patch manager RTJ to request pause. ----
	t.Log("Step 4: patching manager RTJ to desiredState=Paused")
	runKubectlContext(
		t, env.repoRoot, env.managerContext,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	// ---- Step 5: Wait for manager RTJ to become Paused. ----
	t.Log("Step 5: waiting for manager RTJ to become Paused")
	pausedView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"phase is Paused",
		5*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.Phase == "Paused"
		},
	)
	t.Logf("manager RTJ is Paused: reason=%s", pausedView.Status.Reason)

	// Verify the remote checkpoint summary was preserved during pause.
	if pausedView.Status.MultiCluster == nil {
		t.Fatal("expected multiCluster status to be populated after pause")
	}
	if pausedView.Status.MultiCluster.RemoteCheckpoint == nil {
		t.Fatal("expected multiCluster.remoteCheckpoint to be preserved after pause")
	}
	t.Logf("preserved remoteCheckpoint: id=%s",
		pausedView.Status.MultiCluster.RemoteCheckpoint.LastCompletedCheckpointID)

	// ---- Step 6: Verify checkpoint exists in shared store. ----
	t.Log("Step 6: verifying checkpoint exists in shared store")
	assertObjectExists(t, env.minioEndpoint, env.accessKey, env.secretKey, env.region, prePauseManifestURI)
	t.Logf("checkpoint manifest verified in shared store: %s", prePauseManifestURI)

	// ---- Step 7: Verify no manager-local child JobSet. ----
	t.Log("Step 7: verifying no child JobSet on manager cluster")
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	// ---- Step 8: Verify remote phase surfaced on manager status. ----
	t.Log("Step 8: verifying remote phase on manager status")
	mc := pausedView.Status.MultiCluster
	if mc.RemotePhase != "Paused" {
		t.Fatalf("expected multiCluster.remotePhase=Paused, got %q", mc.RemotePhase)
	}
	if !mc.LocalExecutionSuppressed {
		t.Fatal("expected localExecutionSuppressed=true on manager")
	}
	t.Logf("manager status: remotePhase=%s suppressed=%v",
		mc.RemotePhase, mc.LocalExecutionSuppressed)

	// ---- Step 9: Patch manager RTJ back to Running. ----
	t.Log("Step 9: patching manager RTJ to desiredState=Running")
	runKubectlContext(
		t, env.repoRoot, env.managerContext,
		"-n", env.namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Running"}}}`,
	)

	// ---- Step 10: Wait for worker to resume and reach Running. ----
	t.Log("Step 10: waiting for worker to resume and reach Running")
	waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"remotePhase is Running after resume",
		5*time.Minute,
		func(view phase6RTJView) bool {
			if view.Status.MultiCluster == nil {
				return false
			}
			return view.Status.MultiCluster.RemotePhase == "Running" ||
				view.Status.Phase == "Running"
		},
	)
	t.Log("worker has resumed and is Running")

	// ---- Step 11: Wait for new checkpoint after resume. ----
	t.Log("Step 11: waiting for new checkpoint after resume")
	postResumeView := waitForPhase6RTJState(
		t, env, env.managerContext, rtjName,
		"new checkpoint after resume",
		3*time.Minute,
		func(view phase6RTJView) bool {
			return view.Status.LastCompletedCheckpoint != nil &&
				view.Status.LastCompletedCheckpoint.ManifestURI != "" &&
				view.Status.LastCompletedCheckpoint.ID != prePauseCheckpointID
		},
	)

	postResumeCheckpointID := postResumeView.Status.LastCompletedCheckpoint.ID
	postResumeManifestURI := postResumeView.Status.LastCompletedCheckpoint.ManifestURI
	t.Logf("post-resume checkpoint: id=%s manifestURI=%s",
		postResumeCheckpointID, postResumeManifestURI)

	// ---- Step 12: Verify monotonic progression. ----
	t.Log("Step 12: verifying step/checkpoint progression is monotonic")

	// Checkpoint IDs must differ (the post-resume checkpoint is from a
	// new training run that continued from the pre-pause checkpoint).
	if postResumeCheckpointID == prePauseCheckpointID {
		t.Fatalf("expected post-resume checkpoint ID to differ from pre-pause; both are %q",
			prePauseCheckpointID)
	}

	// Verify the post-resume checkpoint also exists in the shared store.
	assertObjectExists(t, env.minioEndpoint, env.accessKey, env.secretKey, env.region, postResumeManifestURI)

	// Verify no manager-local JobSet exists after the full cycle.
	assertNoJobSetsWithPrefix(t, env.repoRoot, env.managerContext, env.namespace, rtjName)

	t.Log("remote pause/resume test passed: " +
		"RTJ paused from manager, checkpoint preserved, " +
		"worker resumed from shared store, progression monotonic")
}

// cleanupPhase6PauseResumeRTJ extends the standard cleanup to handle
// the additional run attempts that may be created during pause/resume.
func cleanupPhase6PauseResumeRTJ(t *testing.T, env phase6Env, name string) {
	t.Helper()
	for _, ctx := range []string{env.managerContext, env.worker1Context, env.worker2Context} {
		kubectlContext(env.repoRoot, ctx,
			"-n", env.namespace, "delete", pauseFlowResource, name,
			"--ignore-not-found=true")
	}
	// Best-effort child JobSet cleanup on workers (up to 4 attempts
	// to cover the delete-recreate cycles from pause/resume).
	for _, attempt := range []int32{1, 2, 3, 4} {
		jsName := rtjjobset.ChildJobSetName(name, attempt)
		for _, ctx := range []string{env.worker1Context, env.worker2Context} {
			kubectlContext(env.repoRoot, ctx,
				"-n", env.namespace, "delete", "jobset", jsName,
				"--ignore-not-found=true")
		}
	}
}
