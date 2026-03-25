package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

func TestNativeKueueAdmission(t *testing.T) {
	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run the kind native Kueue admission e2e")
	}

	env := setupPhase2Env(t)
	rtjName := fmt.Sprintf("native-admission-%d", time.Now().UnixNano())
	localQueueName := fmt.Sprintf("native-adm-%d", time.Now().UnixNano())

	queueManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase2/localqueue-hold.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__LOCAL_QUEUE_NAME__": localQueueName,
		},
	)
	defer os.Remove(queueManifest)

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase2/rtj-native-admission.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__RTJ_NAME__":         rtjName,
			"__TRAINER_IMAGE__":    env.trainerImage,
			"__LOCAL_QUEUE_NAME__": localQueueName,
		},
	)
	defer os.Remove(rtjManifest)

	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", "localqueue.kueue.x-k8s.io", localQueueName, "--ignore-not-found=true")
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", "localqueue.kueue.x-k8s.io", localQueueName, "--ignore-not-found=true")

	runKubectl(t, env.repoRoot, "apply", "-f", queueManifest)
	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	queued := waitForRTJState(
		t,
		env.repoRoot,
		env.namespace,
		rtjName,
		"phase Queued while the dedicated LocalQueue is stopped",
		2*time.Minute,
		env.operatorLogs,
		env.portForward,
		func(view rtjView) bool {
			return view.Status.Phase == "Queued" && view.Spec.Suspend != nil && *view.Spec.Suspend
		},
	)
	if queued.Status.CurrentRunAttempt != 0 {
		t.Fatalf("expected no run attempt before admission, got %d", queued.Status.CurrentRunAttempt)
	}

	waitForWorkloadOwnedBy(t, env.repoRoot, env.namespace, "ResumableTrainingJob", rtjName, 2*time.Minute, env.operatorLogs, env.portForward)

	initialChildName := rtjjobset.ChildJobSetName(rtjName, 1)
	if _, err := getJobSet(env.repoRoot, env.namespace, initialChildName); err == nil {
		t.Fatalf("expected no child JobSet before admission, but %s already exists", initialChildName)
	} else if !isNotFoundError(err) {
		t.Fatalf("get child JobSet before admission: %v", err)
	}

	runKubectl(
		t,
		env.repoRoot,
		"-n", env.namespace,
		"patch", "localqueue.kueue.x-k8s.io", localQueueName,
		"--type=merge",
		"-p", `{"spec":{"stopPolicy":null}}`,
	)

	running := waitForPhase(t, env.repoRoot, env.namespace, rtjName, "Running", 4*time.Minute, env.operatorLogs, env.portForward)
	if running.Spec.Suspend == nil || *running.Spec.Suspend {
		t.Fatalf("expected RTJ to be unsuspended after admission, got %#v", running.Spec.Suspend)
	}

	childJobSet := waitForJobSetPresent(t, env.repoRoot, env.namespace, initialChildName, 2*time.Minute, env.operatorLogs, env.portForward)
	assertChildJobSetNotKueueManaged(t, childJobSet)

	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", initialChildName)
}
