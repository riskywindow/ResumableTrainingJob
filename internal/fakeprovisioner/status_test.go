package fakeprovisioner

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetConditions_Empty(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	conds, err := GetConditions(obj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conds != nil {
		t.Fatalf("expected nil conditions, got %v", conds)
	}
}

func TestSetAndGetConditions(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "autoscaling.x-k8s.io/v1beta1",
		"kind":       "ProvisioningRequest",
		"metadata":   map[string]interface{}{"name": "test", "namespace": "default"},
	}}

	now := metav1.Now()
	want := []metav1.Condition{
		{
			Type:               ConditionProvisioned,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "TestReason",
			Message:            "test message",
		},
	}

	if err := SetConditions(obj, want); err != nil {
		t.Fatalf("SetConditions: %v", err)
	}

	got, err := GetConditions(obj)
	if err != nil {
		t.Fatalf("GetConditions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(got))
	}
	if got[0].Type != ConditionProvisioned {
		t.Errorf("condition type: got %q, want %q", got[0].Type, ConditionProvisioned)
	}
	if got[0].Status != metav1.ConditionTrue {
		t.Errorf("condition status: got %q, want %q", got[0].Status, metav1.ConditionTrue)
	}
	if got[0].Reason != "TestReason" {
		t.Errorf("condition reason: got %q, want %q", got[0].Reason, "TestReason")
	}
}

func TestHasConditionTrue(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionProvisioned, Status: metav1.ConditionFalse},
		{Type: ConditionFailed, Status: metav1.ConditionTrue},
	}

	if HasConditionTrue(conditions, ConditionProvisioned) {
		t.Error("expected Provisioned=False to return false")
	}
	if !HasConditionTrue(conditions, ConditionFailed) {
		t.Error("expected Failed=True to return true")
	}
	if HasConditionTrue(conditions, ConditionCapacityRevoked) {
		t.Error("expected missing condition to return false")
	}
	if HasConditionTrue(nil, ConditionProvisioned) {
		t.Error("expected nil conditions to return false")
	}
}

func TestFindCondition(t *testing.T) {
	now := metav1.Now()
	conditions := []metav1.Condition{
		{Type: ConditionProvisioned, Status: metav1.ConditionTrue, LastTransitionTime: now},
		{Type: ConditionFailed, Status: metav1.ConditionFalse},
	}

	c := FindCondition(conditions, ConditionProvisioned)
	if c == nil {
		t.Fatal("expected to find Provisioned condition")
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("status: got %q, want True", c.Status)
	}

	if FindCondition(conditions, ConditionCapacityRevoked) != nil {
		t.Error("expected nil for missing condition")
	}
	if FindCondition(nil, ConditionProvisioned) != nil {
		t.Error("expected nil for nil conditions slice")
	}
}

func TestSetCondition_Append(t *testing.T) {
	var conditions []metav1.Condition

	SetCondition(&conditions, metav1.Condition{
		Type:   ConditionProvisioned,
		Status: metav1.ConditionTrue,
		Reason: "R1",
	})

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if conditions[0].Reason != "R1" {
		t.Errorf("reason: got %q, want %q", conditions[0].Reason, "R1")
	}
}

func TestSetCondition_Update(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionProvisioned, Status: metav1.ConditionFalse, Reason: "Old"},
	}

	SetCondition(&conditions, metav1.Condition{
		Type:   ConditionProvisioned,
		Status: metav1.ConditionTrue,
		Reason: "New",
	})

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if conditions[0].Reason != "New" {
		t.Errorf("reason: got %q, want %q", conditions[0].Reason, "New")
	}
	if conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("status: got %q, want True", conditions[0].Status)
	}
}

func TestGetProvisioningClassName(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"provisioningClassName": "check-capacity.fake.dev",
		},
	}}
	if got := GetProvisioningClassName(obj); got != "check-capacity.fake.dev" {
		t.Errorf("got %q, want %q", got, "check-capacity.fake.dev")
	}

	empty := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if got := GetProvisioningClassName(empty); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestGetParameters(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"parameters": map[string]interface{}{
				"fake.dev/delay": "5s",
				"other":          "val",
			},
		},
	}}
	params := GetParameters(obj)
	if params == nil {
		t.Fatal("expected non-nil params")
	}
	if params["fake.dev/delay"] != "5s" {
		t.Errorf("delay: got %q, want %q", params["fake.dev/delay"], "5s")
	}

	empty := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if GetParameters(empty) != nil {
		t.Error("expected nil for missing parameters")
	}
}

func TestGetParamDuration(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]string
		key        string
		defaultVal time.Duration
		want       time.Duration
	}{
		{"nil params", nil, ParamDelay, 10 * time.Second, 10 * time.Second},
		{"missing key", map[string]string{}, ParamDelay, 10 * time.Second, 10 * time.Second},
		{"empty value", map[string]string{ParamDelay: ""}, ParamDelay, 10 * time.Second, 10 * time.Second},
		{"invalid value", map[string]string{ParamDelay: "abc"}, ParamDelay, 10 * time.Second, 10 * time.Second},
		{"valid 5s", map[string]string{ParamDelay: "5s"}, ParamDelay, 10 * time.Second, 5 * time.Second},
		{"valid 2m", map[string]string{ParamDelay: "2m"}, ParamDelay, 10 * time.Second, 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetParamDuration(tt.params, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetParamString(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]string
		key        string
		defaultVal string
		want       string
	}{
		{"nil params", nil, "k", "def", "def"},
		{"missing key", map[string]string{}, "k", "def", "def"},
		{"empty value", map[string]string{"k": ""}, "k", "def", "def"},
		{"present", map[string]string{"k": "val"}, "k", "def", "val"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetParamString(tt.params, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetConditions_CreatesStatusMap(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "autoscaling.x-k8s.io/v1beta1",
		"kind":       "ProvisioningRequest",
	}}

	conds := []metav1.Condition{{
		Type:   ConditionProvisioned,
		Status: metav1.ConditionTrue,
		Reason: "Test",
	}}

	if err := SetConditions(obj, conds); err != nil {
		t.Fatalf("SetConditions: %v", err)
	}

	got, err := GetConditions(obj)
	if err != nil {
		t.Fatalf("GetConditions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(got))
	}
}
