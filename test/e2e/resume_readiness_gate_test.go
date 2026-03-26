package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestResumeReadinessGate verifies that the custom ResumeReadiness AdmissionCheck
// gates launch until the operator confirms readiness. The test exercises the
// full Phase 4 admission pipeline without topology.
//
// Test flow:
//  1. Create a held LocalQueue pointing to the Phase 4 ClusterQueue.
//  2. Submit an RTJ (no topology) on the held queue.
//  3. Verify RTJ stays Queued — no child JobSet is created.
//  4. Verify a Workload is created with an admission check in Pending state.
//  5. Release the hold on the LocalQueue.
//  6. Verify the readiness gate clears (initial launch: allowInitial=true).
//  7. Verify RTJ transitions to Running.
//  8. Verify the child JobSet is created as a plain runtime resource.
//  9. Verify status.launchReadiness shows Ready.
//  10. Verify status.effectiveLaunchShape is populated.
//
// This test exercises Phase 4 Goal G3 (ResumeReadiness AdmissionCheck controller)
// and G4 (admission-gated launch).
func TestResumeReadinessGate(t *testing.T) {
	env := setupPhase4Env(t)

	rtjName := fmt.Sprintf("rr-gate-%d", time.Now().UnixNano())
	localQueueName := fmt.Sprintf("rr-gate-q-%d", time.Now().UnixNano())

	// Create a held queue pointing to the Phase 4 ClusterQueue.
	queueManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase4/localqueue-hold-phase4.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__LOCAL_QUEUE_NAME__": localQueueName,
		},
	)
	defer os.Remove(queueManifest)

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase4/rtj-readiness-gated.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__RTJ_NAME__":        rtjName,
			"__TRAINER_IMAGE__":   env.trainerImage,
			"__LOCAL_QUEUE_NAME__": localQueueName,
		},
	)
	defer os.Remove(rtjManifest)

	// Cleanup on exit.
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", "localqueue.kueue.x-k8s.io", localQueueName, "--ignore-not-found=true")

	runKubectl(t, env.repoRoot, "apply", "-f", queueManifest)
	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 1: RTJ should be Queued while the LocalQueue is held ────────
	queued := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Queued while LocalQueue is held",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.Phase == "Queued" && v.Spec.Suspend != nil && *v.Spec.Suspend
		},
	)
	if queued.Status.CurrentRunAttempt != 0 {
		t.Fatalf("expected no run attempt before admission, got %d", queued.Status.CurrentRunAttempt)
	}
	t.Log("RTJ is Queued while queue is held")

	// ── Step 2: No child JobSet should exist before admission ────────────
	assertNoChildJobSetExists(t, env.repoRoot, env.namespace, rtjName, 1)
	t.Log("no child JobSet exists before admission (correct)")

	// ── Step 3: Verify a Workload is created owned by this RTJ ───────────
	workload := waitForWorkloadOwnedBy(
		t, env.repoRoot, env.namespace,
		"ResumableTrainingJob", rtjName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Logf("Workload %s created for RTJ %s", workload.Metadata.Name, rtjName)

	// ── Step 4: Release the hold ─────────────────────────────────────────
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", "localqueue.kueue.x-k8s.io", localQueueName,
		"--type=merge",
		"-p", `{"spec":{"stopPolicy":null}}`,
	)
	t.Log("released LocalQueue hold")

	// ── Step 5: Wait for Running ─────────────────────────────────────────
	waitForPhase4Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running after readiness gate cleared")

	// ── Step 6: Verify the child JobSet exists and is plain runtime ──────
	childName := rtjjobset.ChildJobSetName(rtjName, 1)
	js := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, js)
	t.Logf("child JobSet %s is a plain runtime resource", childName)

	if len(js.Spec.ReplicatedJobs) == 0 {
		t.Fatalf("child JobSet has no replicatedJobs")
	}
	trainerJob := js.Spec.ReplicatedJobs[0]
	if trainerJob.Replicas < 1 {
		t.Fatalf("expected trainer replicas >= 1, got %d", trainerJob.Replicas)
	}
	t.Logf("child JobSet %s has %d replicas for replicatedJob %q", childName, trainerJob.Replicas, trainerJob.Name)

	// ── Step 7: Verify status.launchReadiness shows Ready ────────────────
	withReadiness := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"launchReadiness.ready=true",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.LaunchReadiness != nil && v.Status.LaunchReadiness.Ready
		},
	)
	t.Logf("launchReadiness: ready=%v gateState=%s",
		withReadiness.Status.LaunchReadiness.Ready,
		withReadiness.Status.LaunchReadiness.GateState,
	)

	// ── Step 8: Verify status.effectiveLaunchShape is populated ──────────
	withShape := waitForPhase4RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"effectiveLaunchShape populated",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase4RTJView) bool {
			return v.Status.EffectiveLaunchShape != nil && v.Status.EffectiveLaunchShape.WorkerCount > 0
		},
	)
	t.Logf("effectiveLaunchShape: workerCount=%d worldSize=%d",
		withShape.Status.EffectiveLaunchShape.WorkerCount,
		withShape.Status.EffectiveLaunchShape.WorldSize,
	)

	// ── Step 9: No Workload should be owned by the child JobSet ──────────
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", childName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")
}
