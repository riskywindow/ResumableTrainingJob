package fakeprovisioner

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ── DelayedSuccess tests ────────────────────────────────────────────

func TestComputeAction_DelayedSuccess_WaitsForDelay(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := createdAt.Add(5 * time.Second) // 5s after creation, default delay is 10s

	action := ComputeAction(ClassDelayedSuccess, nil, createdAt, nil, now)

	if action.Done {
		t.Fatal("expected action not done (waiting for delay)")
	}
	if len(action.Conditions) != 0 {
		t.Fatal("expected no conditions to set")
	}
	if action.RequeueAfter <= 0 {
		t.Fatal("expected positive requeue delay")
	}
	// Should requeue in approximately 5s.
	if action.RequeueAfter < 4*time.Second || action.RequeueAfter > 6*time.Second {
		t.Errorf("requeue delay: got %v, want ~5s", action.RequeueAfter)
	}
}

func TestComputeAction_DelayedSuccess_SetsProvisioned(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := createdAt.Add(15 * time.Second) // past default 10s delay

	action := ComputeAction(ClassDelayedSuccess, nil, createdAt, nil, now)

	if action.Done {
		t.Fatal("expected action not done (should set condition)")
	}
	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Type != ConditionProvisioned {
		t.Errorf("condition type: got %q, want %q", action.Conditions[0].Type, ConditionProvisioned)
	}
	if action.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("condition status: got %q, want True", action.Conditions[0].Status)
	}
}

func TestComputeAction_DelayedSuccess_AlreadyProvisioned(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionProvisioned, Status: metav1.ConditionTrue},
	}
	action := ComputeAction(ClassDelayedSuccess, conditions, time.Now(), nil, time.Now())

	if !action.Done {
		t.Fatal("expected done when already provisioned")
	}
}

func TestComputeAction_DelayedSuccess_CustomDelay(t *testing.T) {
	params := map[string]string{ParamDelay: "3s"}
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := createdAt.Add(4 * time.Second)

	action := ComputeAction(ClassDelayedSuccess, nil, createdAt, params, now)

	if action.Done {
		t.Fatal("expected action not done")
	}
	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Type != ConditionProvisioned {
		t.Errorf("condition type: got %q, want %q", action.Conditions[0].Type, ConditionProvisioned)
	}
}

func TestComputeAction_DelayedSuccess_CustomDelay_StillWaiting(t *testing.T) {
	params := map[string]string{ParamDelay: "30s"}
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := createdAt.Add(10 * time.Second)

	action := ComputeAction(ClassDelayedSuccess, nil, createdAt, params, now)

	if action.Done {
		t.Fatal("expected not done")
	}
	if len(action.Conditions) != 0 {
		t.Fatal("expected no conditions")
	}
	if action.RequeueAfter < 19*time.Second || action.RequeueAfter > 21*time.Second {
		t.Errorf("requeue delay: got %v, want ~20s", action.RequeueAfter)
	}
}

// ── PermanentFailure tests ──────────────────────────────────────────

func TestComputeAction_PermanentFailure_SetsFailed(t *testing.T) {
	action := ComputeAction(ClassPermanentFailure, nil, time.Now(), nil, time.Now())

	if action.Done {
		t.Fatal("expected action not done (should set condition)")
	}
	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Type != ConditionFailed {
		t.Errorf("condition type: got %q, want %q", action.Conditions[0].Type, ConditionFailed)
	}
	if action.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("condition status: got %q, want True", action.Conditions[0].Status)
	}
}

func TestComputeAction_PermanentFailure_AlreadyFailed(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionFailed, Status: metav1.ConditionTrue},
	}
	action := ComputeAction(ClassPermanentFailure, conditions, time.Now(), nil, time.Now())

	if !action.Done {
		t.Fatal("expected done when already failed")
	}
}

func TestComputeAction_PermanentFailure_CustomMessage(t *testing.T) {
	params := map[string]string{ParamFailureMessage: "GPU pool exhausted"}
	action := ComputeAction(ClassPermanentFailure, nil, time.Now(), params, time.Now())

	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Message != "GPU pool exhausted" {
		t.Errorf("message: got %q, want %q", action.Conditions[0].Message, "GPU pool exhausted")
	}
}

// ── BookingExpiry tests ─────────────────────────────────────────────

func TestComputeAction_BookingExpiry_WaitsForDelay(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := createdAt.Add(5 * time.Second)

	action := ComputeAction(ClassBookingExpiry, nil, createdAt, nil, now)

	if action.Done {
		t.Fatal("expected not done (waiting for delay)")
	}
	if len(action.Conditions) != 0 {
		t.Fatal("expected no conditions")
	}
	if action.RequeueAfter <= 0 {
		t.Fatal("expected positive requeue delay")
	}
}

func TestComputeAction_BookingExpiry_SetsProvisionedThenRequeues(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := createdAt.Add(15 * time.Second)

	action := ComputeAction(ClassBookingExpiry, nil, createdAt, nil, now)

	if action.Done {
		t.Fatal("expected not done")
	}
	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Type != ConditionProvisioned {
		t.Errorf("condition type: got %q, want %q", action.Conditions[0].Type, ConditionProvisioned)
	}
	// Should requeue for expiry check.
	if action.RequeueAfter <= 0 {
		t.Fatal("expected requeue for expiry check")
	}
}

func TestComputeAction_BookingExpiry_ProvisionedWaitingForExpiry(t *testing.T) {
	provisionedAt := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)
	conditions := []metav1.Condition{
		{
			Type:               ConditionProvisioned,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(provisionedAt),
		},
	}
	now := provisionedAt.Add(30 * time.Second) // 30s after provisioned, default expiry is 60s

	action := ComputeAction(ClassBookingExpiry, conditions, time.Time{}, nil, now)

	if action.Done {
		t.Fatal("expected not done (waiting for expiry)")
	}
	if len(action.Conditions) != 0 {
		t.Fatal("expected no conditions")
	}
	if action.RequeueAfter < 29*time.Second || action.RequeueAfter > 31*time.Second {
		t.Errorf("requeue delay: got %v, want ~30s", action.RequeueAfter)
	}
}

func TestComputeAction_BookingExpiry_Revokes(t *testing.T) {
	provisionedAt := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)
	conditions := []metav1.Condition{
		{
			Type:               ConditionProvisioned,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(provisionedAt),
		},
	}
	now := provisionedAt.Add(90 * time.Second) // past default 60s expiry

	action := ComputeAction(ClassBookingExpiry, conditions, time.Time{}, nil, now)

	if action.Done {
		t.Fatal("expected not done (should set revoked)")
	}
	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Type != ConditionCapacityRevoked {
		t.Errorf("condition type: got %q, want %q", action.Conditions[0].Type, ConditionCapacityRevoked)
	}
}

func TestComputeAction_BookingExpiry_AlreadyRevoked(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionProvisioned, Status: metav1.ConditionTrue},
		{Type: ConditionCapacityRevoked, Status: metav1.ConditionTrue},
	}
	action := ComputeAction(ClassBookingExpiry, conditions, time.Time{}, nil, time.Now())

	if !action.Done {
		t.Fatal("expected done when already revoked")
	}
}

func TestComputeAction_BookingExpiry_CustomExpiry(t *testing.T) {
	params := map[string]string{
		ParamDelay:  "2s",
		ParamExpiry: "5s",
	}
	provisionedAt := time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC)
	conditions := []metav1.Condition{
		{
			Type:               ConditionProvisioned,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(provisionedAt),
		},
	}
	now := provisionedAt.Add(6 * time.Second) // past 5s custom expiry

	action := ComputeAction(ClassBookingExpiry, conditions, time.Time{}, params, now)

	if len(action.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(action.Conditions))
	}
	if action.Conditions[0].Type != ConditionCapacityRevoked {
		t.Errorf("condition type: got %q, want %q", action.Conditions[0].Type, ConditionCapacityRevoked)
	}
}

// ── Unknown class tests ─────────────────────────────────────────────

func TestComputeAction_UnknownClass(t *testing.T) {
	action := ComputeAction("unknown-class", nil, time.Now(), nil, time.Now())
	if !action.Done {
		t.Fatal("expected done for unknown class")
	}
}

func TestComputeAction_EmptyClass(t *testing.T) {
	action := ComputeAction("", nil, time.Now(), nil, time.Now())
	if !action.Done {
		t.Fatal("expected done for empty class")
	}
}
