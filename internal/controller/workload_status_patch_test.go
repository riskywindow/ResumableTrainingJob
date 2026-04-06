package controller

import (
	"encoding/json"
	"testing"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// --- buildReclaimablePodsSSAPatch tests ---

func TestBuildReclaimablePodsSSAPatch_WithPods(t *testing.T) {
	pods := []kueuev1beta2.ReclaimablePod{
		{
			Name:  kueuev1beta2.NewPodSetReference("worker"),
			Count: 4,
		},
	}

	patch, err := buildReclaimablePodsSSAPatch(pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(patch, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed["apiVersion"] != "kueue.x-k8s.io/v1beta2" {
		t.Errorf("expected apiVersion kueue.x-k8s.io/v1beta2, got %v", parsed["apiVersion"])
	}
	if parsed["kind"] != "Workload" {
		t.Errorf("expected kind Workload, got %v", parsed["kind"])
	}

	status, ok := parsed["status"].(map[string]interface{})
	if !ok {
		t.Fatal("expected status to be a map")
	}

	rp, ok := status["reclaimablePods"].([]interface{})
	if !ok {
		t.Fatal("expected reclaimablePods to be a slice")
	}
	if len(rp) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rp))
	}

	entry, ok := rp[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected entry to be a map")
	}
	if entry["name"] != "worker" {
		t.Errorf("expected name 'worker', got %v", entry["name"])
	}
	// JSON numbers are float64.
	if count, ok := entry["count"].(float64); !ok || count != 4 {
		t.Errorf("expected count 4, got %v", entry["count"])
	}
}

func TestBuildReclaimablePodsSSAPatch_EmptyPods(t *testing.T) {
	patch, err := buildReclaimablePodsSSAPatch(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(patch, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	status, ok := parsed["status"].(map[string]interface{})
	if !ok {
		t.Fatal("expected status to be a map")
	}

	rp, ok := status["reclaimablePods"].([]interface{})
	if !ok {
		t.Fatal("expected reclaimablePods to be a slice (empty)")
	}
	if len(rp) != 0 {
		t.Errorf("expected empty slice for nil input, got %d entries", len(rp))
	}
}

func TestBuildReclaimablePodsSSAPatch_MultiplePodSets(t *testing.T) {
	pods := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 3},
		{Name: kueuev1beta2.NewPodSetReference("ps"), Count: 1},
	}

	patch, err := buildReclaimablePodsSSAPatch(pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(patch, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	status := parsed["status"].(map[string]interface{})
	rp := status["reclaimablePods"].([]interface{})
	if len(rp) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(rp))
	}
}

// --- reclaimPodsToRaw tests ---

func TestReclaimPodsToRaw_NilReturnsEmptySlice(t *testing.T) {
	raw := reclaimPodsToRaw(nil)
	if slice, ok := raw.([]interface{}); !ok || len(slice) != 0 {
		t.Errorf("expected empty slice for nil, got %v", raw)
	}
}

func TestReclaimPodsToRaw_SingleEntry(t *testing.T) {
	pods := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 5},
	}

	raw := reclaimPodsToRaw(pods)
	slice, ok := raw.([]map[string]interface{})
	if !ok {
		t.Fatal("expected []map for non-nil input")
	}
	if len(slice) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(slice))
	}
	if slice[0]["name"] != "worker" {
		t.Errorf("expected name 'worker', got %v", slice[0]["name"])
	}
	if slice[0]["count"] != int32(5) {
		t.Errorf("expected count 5, got %v", slice[0]["count"])
	}
}

// --- SSA patch strategy safety tests ---

func TestSSAPatch_DoesNotContainAdmissionFields(t *testing.T) {
	pods := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 2},
	}

	patch, err := buildReclaimablePodsSSAPatch(pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(patch, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	status := parsed["status"].(map[string]interface{})

	// Verify we do NOT include any Kueue-owned fields.
	kueueOwnedFields := []string{"admission", "conditions", "admissionChecks", "requeueState"}
	for _, field := range kueueOwnedFields {
		if _, exists := status[field]; exists {
			t.Errorf("SSA patch must NOT contain Kueue-owned field %q", field)
		}
	}
}

func TestSSAPatch_OnlyContainsReclaimablePods(t *testing.T) {
	pods := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 3},
	}

	patch, err := buildReclaimablePodsSSAPatch(pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(patch, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	status := parsed["status"].(map[string]interface{})

	if len(status) != 1 {
		t.Errorf("expected status to contain exactly 1 field (reclaimablePods), got %d: %v",
			len(status), status)
	}
	if _, ok := status["reclaimablePods"]; !ok {
		t.Error("expected status to contain reclaimablePods")
	}
}

// --- Field manager isolation tests ---

func TestFieldManagerConstant(t *testing.T) {
	if reclaimFieldManager == "" {
		t.Error("field manager must not be empty")
	}
	if reclaimFieldManager == "kueue-controller" || reclaimFieldManager == "manager" {
		t.Errorf("field manager %q must not conflict with Kueue's field managers", reclaimFieldManager)
	}
}

// --- Idempotency test for SSA patch content ---

func TestSSAPatch_Idempotent(t *testing.T) {
	pods := []kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 4},
	}

	patch1, err := buildReclaimablePodsSSAPatch(pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	patch2, err := buildReclaimablePodsSSAPatch(pods)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(patch1) != string(patch2) {
		t.Error("SSA patches for same input should be identical")
	}
}

func TestSSAPatch_ClearAndSetDiffer(t *testing.T) {
	clearPatch, err := buildReclaimablePodsSSAPatch(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	setPatch, err := buildReclaimablePodsSSAPatch([]kueuev1beta2.ReclaimablePod{
		{Name: kueuev1beta2.NewPodSetReference("worker"), Count: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(clearPatch) == string(setPatch) {
		t.Error("clear and set patches should differ")
	}
}
