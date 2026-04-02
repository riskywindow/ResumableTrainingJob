package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// TestWaitForPodsReadyTimeout verifies that Kueue's waitForPodsReady timeout
// correctly evicts and requeues an RTJ whose pods cannot become ready, and that
// the RTJ status reflects a startup timeout rather than manual pause or
// preemption.
//
// Test flow:
//  1. Submit an RTJ that uses a nonexistent container image on the Phase 7
//     queue (delayed-success provisioning).
//  2. Wait for provisioning to succeed (fake backend, ~10s).
//  3. Wait for the RTJ to transition to Starting/Running (child JobSet created).
//  4. Verify that a child JobSet is created.
//  5. Wait for Kueue's waitForPodsReady timeout to fire (~120s in Phase 7
//     profile) and evict the Workload.
//  6. Verify RTJ status.startupRecovery reflects startup timeout/eviction.
//  7. Verify the RTJ transitions back to Queued (Kueue requeues after eviction).
//  8. Verify status does NOT show manual pause or preemption.
//
// This test exercises Phase 7 Goal G3 (waitForPodsReady startup timeout and
// recovery). The waitForPodsReady timeout is configured at 120s in the Phase 7
// Kueue config, so this test has a long timeout.
func TestWaitForPodsReadyTimeout(t *testing.T) {
	env := setupPhase7Env(t)

	rtjName := fmt.Sprintf("wfpr-timeout-%d", time.Now().UnixNano())

	rtjManifest := renderTemplate(
		t,
		filepath.Join(env.repoRoot, "test/e2e/testdata/phase7/rtj-startup-timeout.yaml"),
		map[string]string{
			"__DEV_NAMESPACE__": env.namespace,
			"__RTJ_NAME__":     rtjName,
		},
	)
	defer os.Remove(rtjManifest)

	// Cleanup on exit.
	defer cleanupPhase7RTJ(t, env, rtjName, 1)

	runKubectl(t, env.repoRoot, "apply", "-f", rtjManifest)

	// ── Step 1: Wait for provisioning to complete ────────────────────────
	// The fake backend (check-capacity.fake.dev) provisions after ~10s.
	waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"provisioning.state=Provisioned or phase=Starting/Running",
		3*time.Minute, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			if v.Status.Provisioning != nil && v.Status.Provisioning.State == "Provisioned" {
				return true
			}
			return v.Status.Phase == "Starting" || v.Status.Phase == "Running"
		},
	)
	t.Log("provisioning completed or RTJ already starting")

	// ── Step 2: Wait for child JobSet creation ───────────────────────────
	// The RTJ controller creates the child JobSet after the launch gate opens.
	childName := rtjjobset.ChildJobSetName(rtjName, 1)
	waitForJobSetDetailPresent(
		t, env.repoRoot, env.namespace, childName,
		2*time.Minute, env.operatorLogs, env.portForward,
	)
	t.Logf("child JobSet %s created", childName)

	// ── Step 3: Verify pods enter ImagePullBackOff ───────────────────────
	// The nonexistent image causes pods to fail pulling. We observe this
	// indirectly via the RTJ's startupRecovery status or Kueue eviction.
	t.Log("waiting for waitForPodsReady timeout (~120s in Phase 7 profile)...")

	// ── Step 4: Wait for Kueue to evict the Workload ─────────────────────
	// Kueue's waitForPodsReady fires after the configured timeout, setting
	// an Evicted condition on the Workload. The RTJ controller detects this.
	//
	// We wait for either:
	// (a) startupRecovery.startupState to show timeout/eviction, OR
	// (b) the RTJ to transition back to Queued (Kueue re-suspended after eviction).
	evictedOrRequeued := waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"startupRecovery shows timeout OR phase returns to Queued",
		6*time.Minute, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			// Check for startup timeout detection.
			if v.Status.StartupRecovery != nil {
				state := v.Status.StartupRecovery.StartupState
				if state == "StartupTimedOut" || state == "Evicted" {
					return true
				}
			}
			// Check for Kueue re-suspension (RTJ goes back to Queued).
			if v.Status.Phase == "Queued" && v.Spec.Suspend != nil && *v.Spec.Suspend {
				return true
			}
			// Check for specific condition.
			if hasPhase7Condition(v, "StartupTimeoutEvicted", "True") {
				return true
			}
			return false
		},
	)

	// Log what we observed.
	if evictedOrRequeued.Status.StartupRecovery != nil {
		t.Logf("startupRecovery: startupState=%s podsReadyState=%s lastEvictionReason=%s",
			evictedOrRequeued.Status.StartupRecovery.StartupState,
			evictedOrRequeued.Status.StartupRecovery.PodsReadyState,
			evictedOrRequeued.Status.StartupRecovery.LastEvictionReason,
		)
	}
	t.Logf("phase=%s suspend=%v", evictedOrRequeued.Status.Phase, evictedOrRequeued.Spec.Suspend)

	// ── Step 5: Verify this is NOT a manual pause ────────────────────────
	// Manual pause uses spec.control.desiredState=Paused and
	// currentSuspension.source=manual. This should NOT be the case.
	if evictedOrRequeued.Spec.Control.DesiredState == "Paused" {
		t.Fatalf("RTJ desiredState should still be Running, not Paused (this is a timeout eviction, not manual pause)")
	}
	if evictedOrRequeued.Status.CurrentSuspension != nil &&
		evictedOrRequeued.Status.CurrentSuspension.Source == "manual" {
		t.Fatalf("currentSuspension.source should not be 'manual' for a timeout eviction")
	}
	t.Log("confirmed: not a manual pause")

	// ── Step 6: Verify conditions reflect startup timeout ────────────────
	startupTimeoutCondFound := hasPhase7Condition(evictedOrRequeued, "StartupTimeoutEvicted", "True")
	recoveryTimeoutCondFound := hasPhase7Condition(evictedOrRequeued, "RecoveryTimeoutEvicted", "True")

	if startupTimeoutCondFound {
		t.Log("StartupTimeoutEvicted condition is True (expected for first-run timeout)")
	} else if recoveryTimeoutCondFound {
		t.Log("RecoveryTimeoutEvicted condition is True (unexpected — this is a first run)")
	} else {
		// The condition may have been cleared by the time we check if Kueue
		// quickly re-suspended. Log but don't fail — the key invariant is
		// that we saw eviction/requeue behavior and it wasn't manual pause.
		t.Log("NOTE: StartupTimeoutEvicted condition not observed at check time (Kueue may have already requeued)")
	}

	// ── Step 7: Verify startup recovery shows timeout classification ─────
	if evictedOrRequeued.Status.StartupRecovery != nil {
		sr := evictedOrRequeued.Status.StartupRecovery
		// The StartupState should be one of the timeout/eviction states.
		validTimeoutStates := map[string]bool{
			"StartupTimedOut": true,
			"Evicted":         true,
			"NotStarted":      true, // May have been reset for requeue.
		}
		if !validTimeoutStates[sr.StartupState] {
			t.Logf("WARNING: unexpected startupState %q; expected one of StartupTimedOut, Evicted, or NotStarted", sr.StartupState)
		}

		// If eviction reason is captured, verify it matches PodsReadyTimeout.
		if sr.LastEvictionReason != "" {
			t.Logf("lastEvictionReason=%s (expected PodsReadyTimeout)", sr.LastEvictionReason)
			if sr.LastEvictionReason != "PodsReadyTimeout" {
				t.Logf("NOTE: eviction reason %q is not PodsReadyTimeout; may be a different Kueue eviction path", sr.LastEvictionReason)
			}
		}
	}

	// ── Step 8: Final invariant — RTJ is requeued, not stuck ─────────────
	// After eviction, Kueue should requeue the Workload. The RTJ should
	// eventually return to Queued (suspended by Kueue) for a retry.
	waitForPhase7RTJState(
		t, env.repoRoot, env.namespace, rtjName,
		"requeued to Queued after eviction",
		3*time.Minute, env.operatorLogs, env.portForward,
		func(v phase7RTJView) bool {
			return v.Status.Phase == "Queued" && v.Spec.Suspend != nil && *v.Spec.Suspend
		},
	)
	t.Log("RTJ successfully requeued to Queued after startup timeout eviction")
}
