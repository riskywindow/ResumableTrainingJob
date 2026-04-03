package controller

import (
	"context"
	"testing"
	"time"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/dra"
)

// --- observeDRAClaimStatus tests ---

func TestObserveDRAClaimStatus_NoDevicesIsNoOp(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	r := newTestReconciler(rtj)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusChanged {
		t.Error("expected no status change when devices not configured")
	}
}

func TestObserveDRAClaimStatus_DisabledIsNoOp(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDisabled,
	})
	r := newTestReconciler(rtj)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusChanged {
		t.Error("expected no status change when devices disabled")
	}
}

func TestObserveDRAClaimStatus_NoClaimsYet(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// State should remain or be set to Pending when no claims exist but templates do.
	if rtj.Status.Devices.ClaimAllocationState != trainingv1alpha1.ClaimAllocationPending {
		t.Errorf("expected Pending state when no claims found, got %s", rtj.Status.Devices.ClaimAllocationState)
	}
	_ = result
}

func TestObserveDRAClaimStatus_AllAllocated(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())

	// Create an allocated ResourceClaim with the RTJ label.
	claim := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rtj-gpu-pod1",
			Namespace: "default",
			Labels: map[string]string{
				"training.checkpoint.example.io/rtj-name": "my-rtj",
			},
		},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
		},
	}

	r := newTestReconciler(rtj, claim)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StatusChanged {
		t.Error("expected status change")
	}
	if rtj.Status.Devices.ClaimAllocationState != trainingv1alpha1.ClaimAllocationAllocated {
		t.Errorf("expected Allocated state, got %s", rtj.Status.Devices.ClaimAllocationState)
	}
	if rtj.Status.Devices.AllocatedClaimCount != 1 {
		t.Errorf("expected 1 allocated, got %d", rtj.Status.Devices.AllocatedClaimCount)
	}
}

func TestObserveDRAClaimStatus_FailedClaim(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	failTime := metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))

	claim := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rtj-gpu-pod1",
			Namespace: "default",
			Labels: map[string]string{
				"training.checkpoint.example.io/rtj-name": "my-rtj",
			},
		},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
			Devices: []resourcev1beta1.AllocatedDeviceStatus{
				{
					Driver: "gpu-driver",
					Pool:   "pool-1",
					Device: "dev-0",
					Conditions: []metav1.Condition{
						{
							Type:               "AllocationFailed",
							Status:             metav1.ConditionTrue,
							Reason:             "DeviceAllocationFailed",
							Message:            "no matching devices",
							LastTransitionTime: failTime,
						},
					},
				},
			},
		},
	}

	r := newTestReconciler(rtj, claim)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StatusChanged {
		t.Error("expected status change")
	}
	if rtj.Status.Devices.ClaimAllocationState != trainingv1alpha1.ClaimAllocationFailed {
		t.Errorf("expected Failed state, got %s", rtj.Status.Devices.ClaimAllocationState)
	}
	if rtj.Status.Devices.LastClaimFailureReason == "" {
		t.Error("expected failure reason to be set")
	}
	if rtj.Status.Devices.LastClaimFailureTime == nil {
		t.Error("expected failure time to be set")
	}
}

func TestObserveDRAClaimStatus_MixedAllocatedAndPending(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", multiClaimDeviceSpec())

	allocatedClaim := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rtj-gpu-pod1",
			Namespace: "default",
			Labels: map[string]string{
				"training.checkpoint.example.io/rtj-name": "my-rtj",
			},
		},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
		},
	}
	pendingClaim := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rtj-rdma-pod1",
			Namespace: "default",
			Labels: map[string]string{
				"training.checkpoint.example.io/rtj-name": "my-rtj",
			},
		},
	}

	r := newTestReconciler(rtj, allocatedClaim, pendingClaim)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StatusChanged {
		t.Error("expected status change")
	}
	if rtj.Status.Devices.ClaimAllocationState != trainingv1alpha1.ClaimAllocationPending {
		t.Errorf("expected Pending state for mixed, got %s", rtj.Status.Devices.ClaimAllocationState)
	}
	if rtj.Status.Devices.AllocatedClaimCount != 1 {
		t.Errorf("expected 1 allocated, got %d", rtj.Status.Devices.AllocatedClaimCount)
	}
}

func TestObserveDRAClaimStatus_FiltersUnrelatedClaims(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())

	// Unrelated claim without RTJ labels.
	unrelatedClaim := &resourcev1beta1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-claim",
			Namespace: "default",
		},
		Status: resourcev1beta1.ResourceClaimStatus{
			Allocation: &resourcev1beta1.AllocationResult{},
		},
	}

	r := newTestReconciler(rtj, unrelatedClaim)

	result, err := r.observeDRAClaimStatus(context.Background(), rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No RTJ claims found, so state stays as is (Pending for unknown initial state).
	_ = result
}

// --- syncClaimAllocationFields tests ---

func TestSyncClaimAllocationFields_SetsAllocated(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	summary := dra.ClaimAllocationSummary{
		State:          trainingv1alpha1.ClaimAllocationAllocated,
		TotalClaims:    2,
		AllocatedCount: 2,
	}

	changed := syncClaimAllocationFields(rtj, summary, metav1.Now())
	if !changed {
		t.Error("expected change")
	}
	if rtj.Status.Devices.ClaimAllocationState != trainingv1alpha1.ClaimAllocationAllocated {
		t.Errorf("expected Allocated, got %s", rtj.Status.Devices.ClaimAllocationState)
	}
	if rtj.Status.Devices.AllocatedClaimCount != 2 {
		t.Errorf("expected 2, got %d", rtj.Status.Devices.AllocatedClaimCount)
	}
}

func TestSyncClaimAllocationFields_IdempotentForSameState(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	rtj.Status.Devices.ClaimAllocationState = trainingv1alpha1.ClaimAllocationAllocated
	rtj.Status.Devices.AllocatedClaimCount = 2

	summary := dra.ClaimAllocationSummary{
		State:          trainingv1alpha1.ClaimAllocationAllocated,
		TotalClaims:    2,
		AllocatedCount: 2,
	}

	changed := syncClaimAllocationFields(rtj, summary, metav1.Now())
	if changed {
		t.Error("expected no change for idempotent update")
	}
}

func TestSyncClaimAllocationFields_NilDeviceStatusIsNoOp(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	summary := dra.ClaimAllocationSummary{
		State: trainingv1alpha1.ClaimAllocationAllocated,
	}

	changed := syncClaimAllocationFields(rtj, summary, metav1.Now())
	if changed {
		t.Error("expected no change when device status is nil")
	}
}

func TestSyncClaimAllocationFields_TracksFailure(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	failTime := metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))

	summary := dra.ClaimAllocationSummary{
		State:             trainingv1alpha1.ClaimAllocationFailed,
		TotalClaims:       2,
		FailedCount:       1,
		AllocatedCount:    1,
		LastFailureReason: "DeviceAllocationFailed: no matching devices",
		LastFailureTime:   &failTime,
	}

	changed := syncClaimAllocationFields(rtj, summary, metav1.Now())
	if !changed {
		t.Error("expected change")
	}
	if rtj.Status.Devices.LastClaimFailureReason != "DeviceAllocationFailed: no matching devices" {
		t.Errorf("unexpected failure reason: %s", rtj.Status.Devices.LastClaimFailureReason)
	}
	if rtj.Status.Devices.LastClaimFailureTime == nil {
		t.Error("expected failure time to be set")
	}
}

// --- syncDRAClaimConditions tests ---

func TestSyncDRAClaimConditions_SetsFailureCondition(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	summary := dra.ClaimAllocationSummary{
		State:             trainingv1alpha1.ClaimAllocationFailed,
		TotalClaims:       2,
		FailedCount:       1,
		LastFailureReason: "device not found",
	}

	changed := syncDRAClaimConditions(rtj, summary, metav1.Now())
	if !changed {
		t.Error("expected condition to be set")
	}
	found := false
	for _, c := range rtj.Status.Conditions {
		if c.Type == conditionTypeDRAClaimFailure {
			found = true
			if c.Status != metav1.ConditionTrue {
				t.Errorf("expected ConditionTrue, got %s", c.Status)
			}
		}
	}
	if !found {
		t.Error("expected DRAClaimAllocationFailed condition")
	}
}

func TestSyncDRAClaimConditions_ClearsOnSuccess(t *testing.T) {
	rtj := makeTestRTJWithDeviceStatus("my-rtj", "default", singleGPUDeviceSpec())
	// Pre-set the failure condition.
	rtj.Status.Conditions = append(rtj.Status.Conditions, metav1.Condition{
		Type:   conditionTypeDRAClaimFailure,
		Status: metav1.ConditionTrue,
	})

	summary := dra.ClaimAllocationSummary{
		State:          trainingv1alpha1.ClaimAllocationAllocated,
		TotalClaims:    2,
		AllocatedCount: 2,
	}

	changed := syncDRAClaimConditions(rtj, summary, metav1.Now())
	if !changed {
		t.Error("expected condition to be cleared")
	}
	for _, c := range rtj.Status.Conditions {
		if c.Type == conditionTypeDRAClaimFailure {
			t.Error("expected DRAClaimAllocationFailed condition to be removed")
		}
	}
}

// --- test helpers ---

func makeTestRTJWithDeviceStatus(name, ns string, devices *trainingv1alpha1.DeviceSpec) *trainingv1alpha1.ResumableTrainingJob {
	rtj := makeTestRTJ(name, ns, devices)

	if devices != nil && devices.Mode == trainingv1alpha1.DeviceModeDRA {
		profile := dra.BuildProfile(devices)
		refs := dra.TemplateRefs(name, devices.Claims)
		rtj.Status.Devices = &trainingv1alpha1.DeviceStatus{
			DeviceMode:                      trainingv1alpha1.DeviceModeDRA,
			RequestedDeviceClasses:          profile.DeviceClasses,
			CurrentDeviceProfileFingerprint: profile.Fingerprint,
			ResourceClaimTemplateRefs:       refs,
			ClaimAllocationState:            trainingv1alpha1.ClaimAllocationPending,
		}
	}

	return rtj
}

// newTestReconciler is already defined in dra_templates_test.go,
// so we reuse it. For claims, we need to ensure ResourceClaim types
// are registered in the scheme.
func newTestReconcilerWithClaims(objs ...runtime.Object) *ResumableTrainingJobReconciler {
	return newTestReconciler(objs...)
}
