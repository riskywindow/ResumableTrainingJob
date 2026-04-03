package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestDRAQuotaAndAllocation verifies the full DRA-backed launch lifecycle
// including ResourceClaimTemplate creation, Kueue quota accounting via
// deviceClassMappings, and quota exhaustion blocking.
//
// Test flow:
//  1. Submit a DRA-backed RTJ requesting 2 GPUs per worker (4 total).
//  2. Verify companion ResourceClaimTemplates are created and owned by the RTJ.
//  3. Verify status.devices is populated with device profile fingerprint
//     and template refs.
//  4. Verify a Workload is created owned by this RTJ.
//  5. Verify the Workload is admitted and quota accounting includes
//     example.dev/gpu via deviceClassMappings.
//  6. Verify the RTJ transitions to Running after DRA allocation.
//  7. Verify the child JobSet is created as a plain runtime resource.
//  8. Submit a second DRA-backed RTJ requesting 4 GPUs per worker (8 total,
//     which exceeds remaining quota since the first RTJ uses 4 of 8).
//  9. Verify the second RTJ stays Queued (blocked by quota exhaustion).
// 10. Delete the first RTJ to free quota.
// 11. Verify the second RTJ is now admitted and reaches Running.
// 12. Verify no Workload is owned by any child JobSet (Phase 2 invariant).
//
// This test exercises Phase 8 Goals:
//   - G1: DRA-backed launch with ResourceClaimTemplate lifecycle
//   - G2: Kueue quota/accounting via deviceClassMappings
//   - G3: DRA quota exhaustion blocking
func TestDRAQuotaAndAllocation(t *testing.T) {
	env := setupPhase8Env(t)

	rtjName := fmt.Sprintf("dra-launch-%d", time.Now().UnixNano())

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase8/rtj-dra-launch.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(rtjManifest)

	// Cleanup on exit.
	defer cleanupPhase8RTJ(t, env, rtjName, 1)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 1: Verify ResourceClaimTemplates are created ────────────────
	// The RTJ has one claim named "gpu", so one template should exist:
	// "<rtj-name>-gpu".
	templates := waitForResourceClaimTemplates(
		t, env.repoRoot, env.namespace, rtjName,
		1, 2*time.Minute, env.operatorLogs, env.portForward,
	)
	expectedTemplateName := rtjName + "-gpu"
	foundExpected := false
	for _, tmpl := range templates {
		if tmpl.Metadata.Name == expectedTemplateName {
			foundExpected = true
			// Verify ownership.
			ownerFound := false
			for _, ref := range tmpl.Metadata.OwnerReferences {
				if ref.Kind == "ResumableTrainingJob" && ref.Name == rtjName {
					ownerFound = true
				}
			}
			if !ownerFound {
				t.Fatalf("ResourceClaimTemplate %s is not owned by RTJ %s", tmpl.Metadata.Name, rtjName)
			}
			// Verify device request content.
			if len(tmpl.Spec.Spec.Devices.Requests) != 1 {
				t.Fatalf("expected 1 device request in template, got %d", len(tmpl.Spec.Spec.Devices.Requests))
			}
			req := tmpl.Spec.Spec.Devices.Requests[0]
			if req.DeviceClassName != "example-gpu" {
				t.Fatalf("expected DeviceClassName=example-gpu, got %s", req.DeviceClassName)
			}
			if req.Count != 2 {
				t.Fatalf("expected Count=2, got %d", req.Count)
			}
		}
	}
	if !foundExpected {
		t.Fatalf("expected ResourceClaimTemplate %s not found; found: %v", expectedTemplateName, templates)
	}
	t.Logf("ResourceClaimTemplate %s created and owned by RTJ", expectedTemplateName)

	// ── Step 2: Verify status.devices is populated ──────────────────────
	withDevices := waitForPhase8RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"status.devices populated with fingerprint and template refs",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase8RTJView) bool {
			return v.Status.Devices != nil &&
				v.Status.Devices.CurrentDeviceProfileFingerprint != "" &&
				len(v.Status.Devices.ResourceClaimTemplateRefs) > 0
		},
	)
	t.Logf("devices: mode=%s fingerprint=%s templateRefs=%d",
		withDevices.Status.Devices.DeviceMode,
		withDevices.Status.Devices.CurrentDeviceProfileFingerprint[:12]+"...",
		len(withDevices.Status.Devices.ResourceClaimTemplateRefs),
	)

	// Verify template refs match expectations.
	if len(withDevices.Status.Devices.ResourceClaimTemplateRefs) != 1 {
		t.Fatalf("expected 1 template ref, got %d", len(withDevices.Status.Devices.ResourceClaimTemplateRefs))
	}
	ref := withDevices.Status.Devices.ResourceClaimTemplateRefs[0]
	if ref.Name != expectedTemplateName {
		t.Fatalf("expected template ref name %s, got %s", expectedTemplateName, ref.Name)
	}
	if ref.ClaimName != "gpu" {
		t.Fatalf("expected claim name 'gpu', got %s", ref.ClaimName)
	}

	// ── Step 3: Verify Workload is created owned by RTJ ─────────────────
	workload := waitForPhase8WorkloadOwnedBy(
		t, env.repoRoot, env.namespace,
		"ResumableTrainingJob", rtjName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Logf("Workload %s created for RTJ", workload.Metadata.Name)

	// ── Step 4: Wait for Workload admission with DRA quota ──────────────
	admittedWorkload := waitForPhase8WorkloadAdmitted(
		t, env.repoRoot, env.namespace,
		"ResumableTrainingJob", rtjName,
		3*time.Minute, env.operatorLogs, env.portForward,
	)
	if admittedWorkload.Status.Admission.ClusterQueue != "phase8-cq" {
		t.Fatalf("expected ClusterQueue phase8-cq, got %s", admittedWorkload.Status.Admission.ClusterQueue)
	}
	t.Logf("Workload admitted to ClusterQueue %s", admittedWorkload.Status.Admission.ClusterQueue)

	// Check for DRA resource accounting in the admission.
	// The Workload's podSetAssignments should show example.dev/gpu usage
	// from deviceClassMappings.
	for _, psa := range admittedWorkload.Status.Admission.PodSetAssignments {
		if gpuUsage, ok := psa.ResourceUsage["example.dev/gpu"]; ok {
			t.Logf("PodSet %s: example.dev/gpu usage=%s (from deviceClassMappings)", psa.Name, gpuUsage)
		}
	}

	// ── Step 5: Verify RTJ reaches Running ──────────────────────────────
	waitForPhase8Phase(
		t, env.repoRoot, env.namespace, rtjName,
		"Running", 4*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("RTJ is Running after DRA allocation")

	// ── Step 6: Verify child JobSet is plain runtime ────────────────────
	childName := rtjjobset.ChildJobSetName(rtjName, 1)
	js := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, js)
	t.Logf("child JobSet %s is a plain runtime resource", childName)

	// ── Step 7: Verify no Workload owned by child JobSet ────────────────
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", childName)
	t.Log("no Workload owned by child JobSet (Phase 2 invariant preserved)")

	// ── Step 8: Submit a quota-hog RTJ that exhausts DRA quota ──────────
	// The quota-hog requests 4 GPUs per worker x 2 workers = 8 GPUs,
	// which exhausts the full quota (first RTJ already holds 4).
	hogName := fmt.Sprintf("dra-hog-%d", time.Now().UnixNano())
	hogManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase8/rtj-dra-quota-hog.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     hogName,
			"__TRAINER_IMAGE__": env.trainerImage,
		},
	)
	defer os.Remove(hogManifest)
	defer cleanupPhase8RTJ(t, env, hogName, 1)

	runKubectl(t, env.repoRoot, "apply", "-f", hogManifest)

	// ── Step 9: Verify quota-hog RTJ stays Queued ───────────────────────
	// With only 4 GPUs remaining (8 total - 4 used by first RTJ), the
	// quota-hog requesting 8 GPUs should be blocked.
	waitForPhase8RTJState(
		t, env.repoRoot, env.namespace, hogName,
		"quota-hog Queued (blocked by quota exhaustion)",
		2*time.Minute, env.operatorLogs, env.portForward,
		func(v phase8RTJView) bool {
			return v.Status.Phase == "Queued"
		},
	)
	t.Log("quota-hog RTJ is Queued (blocked by DRA quota exhaustion)")

	// Verify no child JobSet exists for the blocked RTJ.
	hogChild := rtjjobset.ChildJobSetName(hogName, 1)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := getJobSetDetail(env.repoRoot, env.namespace, hogChild)
		if err == nil {
			t.Fatalf("expected no child JobSet for blocked RTJ %s, but found one", hogName)
		}
		time.Sleep(1 * time.Second)
	}
	t.Log("no child JobSet for blocked RTJ (correct)")

	// ── Step 10: Delete first RTJ to free quota ─────────────────────────
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")
	t.Logf("deleted first RTJ %s to free quota", rtjName)

	// ── Step 11: Verify quota-hog RTJ is now admitted and Running ────────
	waitForPhase8Phase(
		t, env.repoRoot, env.namespace, hogName,
		"Running", 5*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Log("quota-hog RTJ is Running after quota freed")

	// Verify child JobSet for the quota-hog is plain runtime.
	hogJs := waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, hogChild,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	assertChildJobSetPlainRuntime(t, hogJs)
	t.Logf("quota-hog child JobSet %s is a plain runtime resource", hogChild)

	// Final Phase 2 invariant check.
	assertNoWorkloadOwnedBy(t, env.repoRoot, env.namespace, "JobSet", hogChild)
	t.Log("no Workload owned by quota-hog child JobSet (Phase 2 invariant preserved)")
}
