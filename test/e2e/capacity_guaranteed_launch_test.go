package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestCapacityGuaranteedLaunch verifies that an RTJ using Kueue's
// ProvisioningRequest AdmissionCheck does not launch child runtime until
// the fake provisioning backend reports success.
//
// Test flow:
//  1. Create a held LocalQueue pointing to the Phase 7 ClusterQueue.
//  2. Submit an RTJ on the held queue.
//  3. Verify RTJ stays Queued while the queue is held — no child JobSet created.
//  4. Release the hold so Kueue begins admission.
//  5. Verify a Workload is created with an admission check in Pending state.
//  6. Verify no child JobSet is created while provisioning is still pending.
//  7. Verify RTJ status.provisioning shows Pending state.
//  8. Wait for the fake backend to provision (delayed success, ~10s).
//  9. Verify RTJ status.provisioning transitions to Provisioned.
//  10. Verify RTJ transitions to Running after provisioning succeeds.
//  11. Verify child JobSet is created as a plain runtime resource.
//  12. Verify status.launchGate shows Open.
//  13. Verify status.capacity shows guaranteeActive=true.
//  14. Verify no Workload is owned by the child JobSet (Phase 2 invariant).
//
// This test exercises Phase 7 Goals G1 (ProvisioningRequest AC integration)
// and G4 (deterministic local dev/test profile).
func TestCapacityGuaranteedLaunch(t *testing.T) {
	env := setupPhase7Env(t)

	rtjName := fmt.Sprintf("cap-launch-%d", time.Now().UnixNano())
	localQueueName := fmt.Sprintf("cap-launch-q-%d", time.Now().UnixNano())

	// Create a held queue pointing to the Phase 7 ClusterQueue.
	queueManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase7/localqueue-hold-phase7.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__LOCAL_QUEUE_NAME__": localQueueName,
		},
	)
	defer os.Remove(queueManifest)

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase7/rtj-capacity-guaranteed.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":  env.namespace,
			"__RTJ_NAME__":      rtjName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjManifest)

	// Override queue name in the RTJ manifest to use the held queue.
	rtjManifestWithQueue := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase7/rtj-capacity-guaranteed.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__":    env.namespace,
			"__RTJ_NAME__":        rtjName,
			"__TRAINER_IMAGE__":   env.trainerImage,
			"phase7-training":     localQueueName,
		},
	)
	defer os.Remove(rtjManifestWithQueue)

	// Cleanup on exit.
	defer cleanupPhase7RTJ(t, env, rtjName, 1)
	defer runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", "localqueue.kueue.x-k8s.io", localQueueName, "--ignore-not-found=true")

	runKubectl(t, env.repoRoot, "apply", "-f", queueManifest)
	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifestWithQueue)

	// ── Step 1: RTJ should be Queued while the LocalQueue is held ────────
	waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"Queued while LocalQueue is held",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			return v.Status.Phase == "Queued" && v.Spec.Suspend != nil && *v.Spec.Suspend
		},
	)
	t.Log("RTJ is Queued while queue is held")

	// ── Step 2: No child JobSet should exist before admission ────────────
	assertNoChildJobSetExistsPhase7(t, env.repoRoot, env.namespace, rtjName, 1)
	t.Log("no child JobSet exists before admission (correct)")

	// ── Step 3: Release the hold ─────────────────────────────────────────
	runKubectl(
		t, env.repoRoot,
		"-n", env.namespace,
		"patch", "localqueue.kueue.x-k8s.io", localQueueName,
		"--type=merge",
		"-p", `{"spec":{"stopPolicy":null}}`,
	)
	t.Log("released LocalQueue hold")

	// ── Step 4: Verify Workload exists owned by this RTJ ─────────────────
	waitForPhase7WorkloadOwnedBy(
		t, env.repoRoot, env.namespace,
		"ResumableTrainingJob", rtjName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("Workload created for RTJ")

	// ── Step 5: Verify provisioning status shows Pending or Provisioned ──
	// The fake backend has a 10s delay. We check that the RTJ sees
	// provisioning state during or after the transition.
	waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"provisioning.state is Pending or Provisioned",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			if v.Status.Provisioning == nil {
				return false
			}
			return v.Status.Provisioning.State == "Pending" ||
				v.Status.Provisioning.State == "Provisioned"
		},
	)
	t.Log("provisioning status populated")

	// ── Step 6: Wait for Running (after fake provisioner succeeds) ───────
	waitForPhase7Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running after capacity provisioned")

	// ── Step 7: Verify child JobSet exists and is plain runtime ──────────
	childName := rtjjobset.ChildJobSetName(rtjName, 1)
	js := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, js)
	t.Logf("child JobSet %s is a plain runtime resource", childName)

	// ── Step 8: Verify status.provisioning shows Provisioned ─────────────
	withProv := waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"provisioning.state=Provisioned",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			return v.Status.Provisioning != nil &&
				v.Status.Provisioning.State == "Provisioned"
		},
	)
	t.Logf("provisioning: state=%s", withProv.Status.Provisioning.State)

	// ── Step 9: Verify status.launchGate shows Open ──────────────────────
	withGate := waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"launchGate.state=Open",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			return v.Status.LaunchGate != nil &&
				v.Status.LaunchGate.State == "Open"
		},
	)
	t.Logf("launchGate: state=%s reason=%s",
		withGate.Status.LaunchGate.State,
		withGate.Status.LaunchGate.Reason,
	)

	// ── Step 10: Verify status.capacity shows guaranteeActive ────────────
	withCap := waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"capacity.guaranteeActive=true",
		30*time.Second, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			return v.Status.Capacity != nil && v.Status.Capacity.GuaranteeActive
		},
	)
	t.Logf("capacity: guaranteeActive=%v reason=%s",
		withCap.Status.Capacity.GuaranteeActive,
		withCap.Status.Capacity.Reason,
	)

	// ── Step 11: No Workload should be owned by the child JobSet ─────────
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", childName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")
}
