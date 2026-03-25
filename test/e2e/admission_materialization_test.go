package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestAdmissionMaterialization verifies that when an RTJ is admitted through
// the Phase 3 multi-flavor queue:
//
//  1. The RTJ stays in Queued while the LocalQueue is held.
//  2. After the hold is released, the RTJ transitions to Running.
//  3. The child JobSet is created with the admitted replica count.
//  4. The child JobSet is a plain runtime resource (no Kueue management metadata).
//  5. The admitted-pod-sets annotation is present on the RTJ.
//  6. status.admission.admittedWorkerCount is populated.
//
// This test exercises Phase 3 Goals G1 (admission-aware launch) and the
// child-JobSet-as-plain-runtime invariant.
func TestAdmissionMaterialization(t *testing.T) {
	env := setupPhase3Env(t, false)

	rtjName := fmt.Sprintf("adm-mat-%d", time.Now().UnixNano())
	localQueueName := fmt.Sprintf("adm-mat-%d", time.Now().UnixNano())

	// Create a hold queue pointing to the Phase 3 ClusterQueue.
	queueManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase3/localqueue-hold-phase3.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__LOCAL_QUEUE_NAME__": localQueueName,
		},
	)
	defer os.Remove(queueManifest)

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase3/rtj-phase3.yaml"),
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

	// Step 1: RTJ should be Queued while the LocalQueue is held.
	queued := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Queued while LocalQueue is held",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
			return v.Status.Phase == "Queued" && v.Spec.Suspend != nil && *v.Spec.Suspend
		},
	)
	if queued.Status.CurrentRunAttempt != 0 {
		t.Fatalf("expected no run attempt before admission, got %d", queued.Status.CurrentRunAttempt)
	}

	// Step 2: No child JobSet should exist before admission.
	childName := rtjjobset.ChildJobSetName(rtjName, 1)
	if _, err := getJobSetDetail(env.repoRoot, env.namespace, childName); err == nil {
		t.Fatalf("child JobSet %s exists before admission", childName)
	}

	// Step 3: Release the hold.
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", "localqueue.kueue.x-k8s.io", localQueueName,
		"--type=merge",
		"-p", `{"spec":{"stopPolicy":null}}`,
	)

	// Step 4: Wait for Running.
	running := waitForPhase3Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	if running.Spec.Suspend == nil || *running.Spec.Suspend {
		t.Fatalf("expected RTJ to be unsuspended after admission, suspend=%v", running.Spec.Suspend)
	}

	// Step 5: Child JobSet must exist, be a plain runtime resource, and have correct shape.
	js := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, js)

	if len(js.Spec.ReplicatedJobs) == 0 {
		t.Fatalf("child JobSet has no replicatedJobs")
	}
	trainerJob := js.Spec.ReplicatedJobs[0]
	if trainerJob.Replicas < 1 {
		t.Fatalf("expected trainer replicas >= 1, got %d", trainerJob.Replicas)
	}
	t.Logf("child JobSet %s has %d replicas for replicatedJob %q", childName, trainerJob.Replicas, trainerJob.Name)

	// Step 6: Verify the admitted-pod-sets annotation is present on the RTJ.
	admitted := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"admitted-pod-sets annotation present",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
			_, ok := v.Metadata.Annotations[rtjjobset.AdmittedPodSetsAnnotation]
			return ok
		},
	)
	annotationValue := admitted.Metadata.Annotations[rtjjobset.AdmittedPodSetsAnnotation]
	t.Logf("admitted-pod-sets annotation: %s", annotationValue)

	// Step 7: Verify status.admission is populated.
	admittedStatus := waitForPhase3RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"status.admission populated",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase3RTJView) bool {
			return v.Status.Admission != nil && v.Status.Admission.AdmittedWorkerCount > 0
		},
	)
	t.Logf("status.admission.admittedWorkerCount=%d preferredWorkerCount=%d",
		admittedStatus.Status.Admission.AdmittedWorkerCount,
		admittedStatus.Status.Admission.PreferredWorkerCount,
	)

	// Step 8: No Workload should be owned by the child JobSet (Phase 2 invariant).
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", childName)
}
