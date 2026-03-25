package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

func TestPriorityPreemptionResume(t *testing.T) {
	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run the kind priority-preemption-resume e2e")
	}

	env := setupPhase2Env(t)
	lowName := fmt.Sprintf("phase2-low-%d", time.Now().UnixNano())
	highName := fmt.Sprintf("phase2-high-%d", time.Now().UnixNano())

	lowManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase2/rtj-low-priority.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":      lowName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(lowManifest)

	highManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase2/rtj-high-priority.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":      highName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(highManifest)

	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, lowName, "--ignore-not-found=true")
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, highName, "--ignore-not-found=true")
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, lowName, "--ignore-not-found=true")
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, highName, "--ignore-not-found=true")

	runKubectl(t, env.repoRoot, "apply", "-f", lowManifest)
	waitForWorkloadOwnedBy(t, env.repoRoot, env.namespace, "ResumableTrainingJob", lowName, 2*time.Minute, env.operatorLogs, env.portForward)
	waitForPhase(t, env.repoRoot, env.namespace, lowName, "Running", 4*time.Minute, env.operatorLogs, env.portForward)

	runKubectl(t, env.repoRoot, "apply", "-f", highManifest)
	waitForWorkloadOwnedBy(t, env.repoRoot, env.namespace, "ResumableTrainingJob", highName, 2*time.Minute, env.operatorLogs, env.portForward)

	waitForRTJState(
		t,
		env.repoRoot,
		env.namespace,
		lowName,
		"Kueue-driven stop request on the low-priority RTJ",
		4*time.Minute,
		env.operatorLogs,
		env.portForward,
		func(view rtjView) bool {
			return strings.HasPrefix(view.Status.PauseRequestID, "kueue-suspend-") &&
				view.Status.CurrentSuspension != nil &&
				view.Status.CurrentSuspension.Source == "Kueue"
		},
	)

	lowQueued := waitForRTJState(
		t,
		env.repoRoot,
		env.namespace,
		lowName,
		"low-priority RTJ drained, checkpointed, and re-queued by Kueue",
		6*time.Minute,
		env.operatorLogs,
		env.portForward,
		func(view rtjView) bool {
			return view.Status.Phase == "Queued" &&
				strings.HasPrefix(view.Status.PauseRequestID, "kueue-suspend-") &&
				view.Status.LastCompletedCheckpoint != nil &&
				view.Status.LastCompletedCheckpoint.ManifestURI != "" &&
				view.Status.CurrentSuspension != nil &&
				view.Status.CurrentSuspension.Source == "Kueue"
		},
	)
	waitForJobSetDeleted(
		t,
		env.repoRoot,
		env.namespace,
		rtjjobset.ChildJobSetName(lowName, 1),
		2*time.Minute,
		env.operatorLogs,
		env.portForward,
	)

	preemptedManifest := loadManifestFromObjectStore(
		t,
		env.minioEndpoint,
		env.accessKey,
		env.secretKey,
		env.region,
		lowQueued.Status.LastCompletedCheckpoint.ManifestURI,
	)

	waitForPhase(t, env.repoRoot, env.namespace, highName, "Running", 4*time.Minute, env.operatorLogs, env.portForward)

	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, highName, "--wait=true")
	waitForRTJDeleted(t, env.repoRoot, env.namespace, highName, 2*time.Minute, env.operatorLogs, env.portForward)

	lowResumed := waitForRTJState(
		t,
		env.repoRoot,
		env.namespace,
		lowName,
		"low-priority RTJ resumed from its checkpoint after high-priority quota was released",
		6*time.Minute,
		env.operatorLogs,
		env.portForward,
		func(view rtjView) bool {
			return view.Status.Phase == "Running" &&
				view.Status.CurrentRunAttempt >= 2 &&
				view.Status.SelectedCheckpoint != nil &&
				view.Status.SelectedCheckpoint.ManifestURI == lowQueued.Status.LastCompletedCheckpoint.ManifestURI &&
				view.Status.CurrentSuspension == nil
		},
	)
	if lowResumed.Status.CurrentRunAttempt < 2 {
		t.Fatalf("expected low-priority RTJ to resume as a new run attempt, got %d", lowResumed.Status.CurrentRunAttempt)
	}

	time.Sleep(5 * time.Second)

	runKubectl(
		t,
		env.repoRoot,
		"-n", env.namespace,
		"patch", pauseFlowResource, lowName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	lowPausedAgain := waitForPhase(t, env.repoRoot, env.namespace, lowName, "Paused", 5*time.Minute, env.operatorLogs, env.portForward)
	if lowPausedAgain.Status.LastCompletedCheckpoint == nil || lowPausedAgain.Status.LastCompletedCheckpoint.ManifestURI == "" {
		t.Fatalf("expected resumed low-priority RTJ to publish a later checkpoint after the compatibility pause")
	}

	resumedManifest := loadManifestFromObjectStore(
		t,
		env.minioEndpoint,
		env.accessKey,
		env.secretKey,
		env.region,
		lowPausedAgain.Status.LastCompletedCheckpoint.ManifestURI,
	)
	if resumedManifest.GlobalStep <= preemptedManifest.GlobalStep {
		t.Fatalf(
			"expected resumed low-priority RTJ to advance beyond global step %d, got %d",
			preemptedManifest.GlobalStep,
			resumedManifest.GlobalStep,
		)
	}
}
