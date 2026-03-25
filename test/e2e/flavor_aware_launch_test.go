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

// TestFlavorAwareLaunch verifies that when an RTJ is admitted through the
// Phase 3 multi-flavor queue:
//
//  1. The child JobSet pod template has nodeSelector derived from the
//     assigned ResourceFlavor (checkpoint-native.dev/pool).
//  2. If the assigned flavor is "spot", the pod template also has the
//     spot toleration.
//  3. Pods land on nodes whose pool label matches the assigned flavor.
//
// This test exercises Phase 3 Goal G2 (flavor-aware child JobSet rendering).
//
// The test uses the direct Phase 3 queue (phase3-training) and lets Kueue
// assign whichever flavor has available quota. Since the on-demand flavor is
// listed first in the ClusterQueue, Kueue will prefer it when quota allows.
func TestFlavorAwareLaunch(t *testing.T) {
	env := setupPhase3Env(t, false)

	rtjName := fmt.Sprintf("flavor-%d", time.Now().UnixNano())

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase3/rtj-phase3.yaml"),
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

	// Wait for Running.
	waitForPhase3Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)

	// Get the child JobSet.
	childName := rtjjobset.ChildJobSetName(rtjName, 1)
	js := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)

	// Verify the child JobSet is a plain runtime resource.
	assertChildJobSetPlainRuntime(t, js)

	if len(js.Spec.ReplicatedJobs) == 0 {
		t.Fatalf("child JobSet has no replicatedJobs")
	}

	// Check that at least one replicatedJob has the pool nodeSelector.
	// Kueue's podset.Merge injects the ResourceFlavor's nodeLabels into
	// the pod template nodeSelector via RunWithPodSetsInfo.
	trainerJob := js.Spec.ReplicatedJobs[0]
	podSpec := trainerJob.Template.Spec.Template.Spec
	poolLabel := podSpec.NodeSelector["checkpoint-native.dev/pool"]
	if poolLabel == "" {
		t.Fatalf("child JobSet pod template missing nodeSelector checkpoint-native.dev/pool; nodeSelector=%v", podSpec.NodeSelector)
	}
	t.Logf("child JobSet pod template has nodeSelector checkpoint-native.dev/pool=%s", poolLabel)

	// If assigned to spot flavor, verify toleration is present.
	if poolLabel == "spot" {
		found := false
		for _, tol := range podSpec.Tolerations {
			if tol.Key == "checkpoint-native.dev/spot" && tol.Effect == "NoSchedule" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("spot flavor assigned but pod template missing spot toleration; tolerations=%v", podSpec.Tolerations)
		}
		t.Logf("child JobSet pod template has spot toleration")
	}

	// Wait for pods to be running and verify node placement.
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		pods, err := getPods(env.repoRoot, env.namespace, "app.kubernetes.io/name=rtj-phase3-e2e")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == "" {
				continue
			}
			runningPods++

			// Verify the pod's node has the expected pool label.
			labels, err := getNodeLabels(env.repoRoot, pod.Spec.NodeName)
			if err != nil {
				t.Logf("could not get labels for node %s: %v", pod.Spec.NodeName, err)
				continue
			}
			nodePool := labels["checkpoint-native.dev/pool"]
			if nodePool == "" {
				t.Fatalf("pod %s landed on node %s which has no pool label", pod.Metadata.Name, pod.Spec.NodeName)
			}
			if nodePool != poolLabel {
				t.Fatalf("pod %s landed on node %s with pool=%s, expected pool=%s",
					pod.Metadata.Name, pod.Spec.NodeName, nodePool, poolLabel)
			}
			t.Logf("pod %s running on node %s (pool=%s)", pod.Metadata.Name, pod.Spec.NodeName, nodePool)
		}

		if runningPods > 0 {
			t.Logf("all %d pods landed on %s pool nodes", runningPods, poolLabel)
			return
		}
		time.Sleep(2 * time.Second)
	}

	// If we reach here, pods never got placed. Check for any kueue labels
	// that might have leaked through.
	pods, _ := getPods(env.repoRoot, env.namespace, "app.kubernetes.io/name=rtj-phase3-e2e")
	for _, pod := range pods.Items {
		for key := range pod.Spec.NodeSelector {
			if strings.HasPrefix(key, "kueue.x-k8s.io/") {
				t.Errorf("pod %s has leaked Kueue nodeSelector key %q", pod.Metadata.Name, key)
			}
		}
	}
	t.Fatalf("timed out waiting for pods to be placed on flavor-labeled nodes\noperator logs:\n%s",
		env.operatorLogs.String())
}
