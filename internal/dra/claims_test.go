package dra

import (
	"testing"
	"time"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

func TestSummarizeClaimAllocations_NoClaims(t *testing.T) {
	summary := SummarizeClaimAllocations(nil)
	if summary.State != trainingv1alpha1.ClaimAllocationUnknown {
		t.Fatalf("expected Unknown state for no claims, got %s", summary.State)
	}
	if summary.TotalClaims != 0 {
		t.Fatalf("expected 0 total claims, got %d", summary.TotalClaims)
	}
}

func TestSummarizeClaimAllocations_EmptyClaims(t *testing.T) {
	summary := SummarizeClaimAllocations([]resourcev1beta1.ResourceClaim{})
	if summary.State != trainingv1alpha1.ClaimAllocationUnknown {
		t.Fatalf("expected Unknown state for empty claims, got %s", summary.State)
	}
}

func TestSummarizeClaimAllocations_AllAllocated(t *testing.T) {
	claims := []resourcev1beta1.ResourceClaim{
		allocatedClaim("claim-1"),
		allocatedClaim("claim-2"),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationAllocated {
		t.Fatalf("expected Allocated state, got %s", summary.State)
	}
	if summary.AllocatedCount != 2 {
		t.Fatalf("expected 2 allocated, got %d", summary.AllocatedCount)
	}
	if summary.TotalClaims != 2 {
		t.Fatalf("expected 2 total, got %d", summary.TotalClaims)
	}
}

func TestSummarizeClaimAllocations_AllPending(t *testing.T) {
	claims := []resourcev1beta1.ResourceClaim{
		pendingClaim("claim-1"),
		pendingClaim("claim-2"),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationPending {
		t.Fatalf("expected Pending state, got %s", summary.State)
	}
	if summary.PendingCount != 2 {
		t.Fatalf("expected 2 pending, got %d", summary.PendingCount)
	}
}

func TestSummarizeClaimAllocations_MixedAllocatedAndPending(t *testing.T) {
	claims := []resourcev1beta1.ResourceClaim{
		allocatedClaim("claim-1"),
		pendingClaim("claim-2"),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationPending {
		t.Fatalf("expected Pending state for mixed, got %s", summary.State)
	}
	if summary.AllocatedCount != 1 {
		t.Fatalf("expected 1 allocated, got %d", summary.AllocatedCount)
	}
	if summary.PendingCount != 1 {
		t.Fatalf("expected 1 pending, got %d", summary.PendingCount)
	}
}

func TestSummarizeClaimAllocations_FailedDeviceCondition(t *testing.T) {
	failTime := metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	claims := []resourcev1beta1.ResourceClaim{
		allocatedClaim("claim-1"),
		failedDeviceClaim("claim-2", "gpu-driver", "pool-a", "dev-0",
			"DeviceAllocationFailed", "no matching devices", failTime),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationFailed {
		t.Fatalf("expected Failed state, got %s", summary.State)
	}
	if summary.FailedCount != 1 {
		t.Fatalf("expected 1 failed, got %d", summary.FailedCount)
	}
	if summary.AllocatedCount != 1 {
		t.Fatalf("expected 1 allocated, got %d", summary.AllocatedCount)
	}
	if summary.LastFailureReason == "" {
		t.Fatal("expected failure reason to be set")
	}
	if summary.LastFailureTime == nil {
		t.Fatal("expected failure time to be set")
	}
}

func TestSummarizeClaimAllocations_MultipleFailed_PicksLatest(t *testing.T) {
	earlier := metav1.NewTime(time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC))
	later := metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	claims := []resourcev1beta1.ResourceClaim{
		failedDeviceClaim("claim-1", "driver-a", "pool", "dev-0",
			"Unschedulable", "earlier failure", earlier),
		failedDeviceClaim("claim-2", "driver-b", "pool", "dev-1",
			"DriverError", "later failure", later),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationFailed {
		t.Fatalf("expected Failed state, got %s", summary.State)
	}
	if summary.FailedCount != 2 {
		t.Fatalf("expected 2 failed, got %d", summary.FailedCount)
	}
	if summary.LastFailureTime == nil || !summary.LastFailureTime.Equal(&later) {
		t.Fatalf("expected latest failure time, got %v", summary.LastFailureTime)
	}
}

func TestSummarizeClaimAllocations_SingleAllocated(t *testing.T) {
	claims := []resourcev1beta1.ResourceClaim{
		allocatedClaim("claim-1"),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationAllocated {
		t.Fatalf("expected Allocated state, got %s", summary.State)
	}
}

func TestSummarizeClaimAllocations_FailedTakesPrecedenceOverPending(t *testing.T) {
	failTime := metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	claims := []resourcev1beta1.ResourceClaim{
		pendingClaim("claim-1"),
		failedDeviceClaim("claim-2", "driver", "pool", "dev-0",
			"AllocationFailed", "device not found", failTime),
	}
	summary := SummarizeClaimAllocations(claims)
	if summary.State != trainingv1alpha1.ClaimAllocationFailed {
		t.Fatalf("expected Failed state (takes precedence over Pending), got %s", summary.State)
	}
}

func TestSummarizeClaimAllocations_ReadyFalseIsFailure(t *testing.T) {
	failTime := metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	claim := resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-1"},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
			Devices: []resourcev1beta1.AllocatedDeviceStatus{
				{
					Driver: "gpu-driver",
					Pool:   "pool-1",
					Device: "dev-0",
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "DeviceNotReady",
							Message:            "GPU device is in error state",
							LastTransitionTime: failTime,
						},
					},
				},
			},
		},
	}
	summary := SummarizeClaimAllocations([]resourcev1beta1.ResourceClaim{claim})
	if summary.State != trainingv1alpha1.ClaimAllocationFailed {
		t.Fatalf("expected Failed state for Ready=False, got %s", summary.State)
	}
}

func TestFilterClaimsForRTJ_ByLabel(t *testing.T) {
	claims := []resourcev1beta1.ResourceClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-rtj-gpu-abc",
				Labels: map[string]string{
					"training.checkpoint.example.io/rtj-name": "my-rtj",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "unrelated-claim",
				Labels: map[string]string{},
			},
		},
	}

	filtered := FilterClaimsForRTJ(claims, "my-rtj", "uid-1", nil)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered claim, got %d", len(filtered))
	}
	if filtered[0].Name != "my-rtj-gpu-abc" {
		t.Fatalf("expected my-rtj-gpu-abc, got %s", filtered[0].Name)
	}
}

func TestFilterClaimsForRTJ_ByTemplateAnnotation(t *testing.T) {
	templates := map[string]bool{
		"my-rtj-gpu": true,
	}
	claims := []resourcev1beta1.ResourceClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "generated-claim-xyz",
				Annotations: map[string]string{
					"resource.kubernetes.io/claim-template-name": "my-rtj-gpu",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-claim",
				Annotations: map[string]string{
					"resource.kubernetes.io/claim-template-name": "other-template",
				},
			},
		},
	}

	filtered := FilterClaimsForRTJ(claims, "my-rtj", "uid-1", templates)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered claim, got %d", len(filtered))
	}
	if filtered[0].Name != "generated-claim-xyz" {
		t.Fatalf("expected generated-claim-xyz, got %s", filtered[0].Name)
	}
}

func TestFilterClaimsForRTJ_Empty(t *testing.T) {
	filtered := FilterClaimsForRTJ(nil, "my-rtj", "uid-1", nil)
	if filtered != nil {
		t.Fatalf("expected nil for empty input, got %v", filtered)
	}
}

func TestFilterClaimsForRTJ_SortedByName(t *testing.T) {
	claims := []resourcev1beta1.ResourceClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "z-claim",
				Labels: map[string]string{"training.checkpoint.example.io/rtj-name": "my-rtj"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "a-claim",
				Labels: map[string]string{"training.checkpoint.example.io/rtj-name": "my-rtj"},
			},
		},
	}
	filtered := FilterClaimsForRTJ(claims, "my-rtj", "uid-1", nil)
	if len(filtered) != 2 {
		t.Fatalf("expected 2, got %d", len(filtered))
	}
	if filtered[0].Name != "a-claim" {
		t.Fatalf("expected a-claim first, got %s", filtered[0].Name)
	}
}

func TestIsDeviceFailureCondition_AllocationFailed(t *testing.T) {
	cond := metav1.Condition{
		Type:   "AllocationFailed",
		Status: metav1.ConditionTrue,
	}
	if !isDeviceFailureCondition(cond) {
		t.Fatal("expected AllocationFailed to be recognized as failure")
	}
}

func TestIsDeviceFailureCondition_DriverError(t *testing.T) {
	cond := metav1.Condition{
		Type:   "SomeCondition",
		Status: metav1.ConditionTrue,
		Reason: "DriverError",
	}
	if !isDeviceFailureCondition(cond) {
		t.Fatal("expected DriverError reason to be recognized as failure")
	}
}

func TestIsDeviceFailureCondition_ReadyFalse(t *testing.T) {
	cond := metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionFalse,
		Reason: "DeviceNotReady",
	}
	if !isDeviceFailureCondition(cond) {
		t.Fatal("expected Ready=False to be recognized as failure")
	}
}

func TestIsDeviceFailureCondition_ReadyTrue(t *testing.T) {
	cond := metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionTrue,
		Reason: "DeviceReady",
	}
	if isDeviceFailureCondition(cond) {
		t.Fatal("expected Ready=True not to be a failure")
	}
}

func TestIsDeviceFailureCondition_NotFailure(t *testing.T) {
	cond := metav1.Condition{
		Type:   "Healthy",
		Status: metav1.ConditionTrue,
		Reason: "AllGood",
	}
	if isDeviceFailureCondition(cond) {
		t.Fatal("expected Healthy/AllGood not to be a failure")
	}
}

// --- test helpers ---

func allocatedClaim(name string) resourcev1beta1.ResourceClaim {
	return resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
		},
	}
}

func pendingClaim(name string) resourcev1beta1.ResourceClaim {
	return resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func failedDeviceClaim(name, driver, pool, device, reason, message string, failTime metav1.Time) resourcev1beta1.ResourceClaim {
	return resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
			Devices: []resourcev1beta1.AllocatedDeviceStatus{
				{
					Driver: driver,
					Pool:   pool,
					Device: device,
					Conditions: []metav1.Condition{
						{
							Type:               "AllocationFailed",
							Status:             metav1.ConditionTrue,
							Reason:             reason,
							Message:            message,
							LastTransitionTime: failTime,
						},
					},
				},
			},
		},
	}
}
