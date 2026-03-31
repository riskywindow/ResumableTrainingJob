package provisioning

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// ---------- Phase 6 fallback (no provisioning configured) ----------

func TestBuildViewNilWorkload(t *testing.T) {
	view := BuildView(nil, ViewOptions{})
	if view == nil {
		t.Fatal("expected non-nil view")
	}
	if view.QuotaReserved {
		t.Fatal("expected QuotaReserved=false for nil workload")
	}
	if !view.AllChecksReady {
		t.Fatal("expected AllChecksReady=true for nil workload")
	}
	if view.Provisioning != ProvisioningNotConfigured {
		t.Fatalf("expected NotConfigured, got %s", view.Provisioning)
	}
	if view.ProvisioningRequestPresent {
		t.Fatal("expected ProvisioningRequestPresent=false")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready (no quota)")
	}
}

func TestBuildViewNoACsPhase6Fallback(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
		},
	}

	view := BuildView(wl, ViewOptions{})
	if !view.QuotaReserved {
		t.Fatal("expected QuotaReserved=true")
	}
	if !view.AllChecksReady {
		t.Fatal("expected AllChecksReady=true (no ACs)")
	}
	if view.Provisioning != ProvisioningNotConfigured {
		t.Fatalf("expected NotConfigured, got %s", view.Provisioning)
	}
	if view.ProvisioningRequestPresent {
		t.Fatal("expected ProvisioningRequestPresent=false")
	}
	if !view.IsLaunchReady() {
		t.Fatal("expected launch ready (quota + no ACs)")
	}
}

// ---------- ProvisioningRequest readiness detection ----------

func TestBuildViewProvisioningPending(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "provision-ac", State: kueuev1beta2.CheckStatePending},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if !view.ProvisioningRequestPresent {
		t.Fatal("expected ProvisioningRequestPresent=true")
	}
	if view.Provisioning != ProvisioningPending {
		t.Fatalf("expected Pending, got %s", view.Provisioning)
	}
	if view.AllChecksReady {
		t.Fatal("expected AllChecksReady=false")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready (pending check)")
	}
	if view.ProvisioningRequestRef == nil {
		t.Fatal("expected non-nil PR ref")
	}
	if view.ProvisioningRequestRef.Name != "wl-1-provision-ac-1" {
		t.Fatalf("expected PR name 'wl-1-provision-ac-1', got %q", view.ProvisioningRequestRef.Name)
	}
}

func TestBuildViewProvisioningReady(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "provision-ac", State: kueuev1beta2.CheckStateReady},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if view.Provisioning != ProvisioningProvisioned {
		t.Fatalf("expected Provisioned, got %s", view.Provisioning)
	}
	if !view.AllChecksReady {
		t.Fatal("expected AllChecksReady=true")
	}
	if !view.IsLaunchReady() {
		t.Fatal("expected launch ready (quota + all checks ready + no topology)")
	}
}

// ---------- Failure/retry classification ----------

func TestBuildViewProvisioningFailed(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "provision-ac", State: kueuev1beta2.CheckStateRejected, Message: "capacity unavailable"},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if view.Provisioning != ProvisioningFailed {
		t.Fatalf("expected Failed, got %s", view.Provisioning)
	}
	if view.AllChecksReady {
		t.Fatal("expected AllChecksReady=false")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready")
	}
	// Verify the AC message is captured.
	if len(view.AdmissionChecks) != 1 {
		t.Fatalf("expected 1 AC, got %d", len(view.AdmissionChecks))
	}
	if view.AdmissionChecks[0].Message != "capacity unavailable" {
		t.Fatalf("expected message 'capacity unavailable', got %q", view.AdmissionChecks[0].Message)
	}
}

func TestBuildViewProvisioningRetry(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "provision-ac", State: kueuev1beta2.CheckStateRetry, Message: "retrying after backoff"},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if view.Provisioning != ProvisioningRetry {
		t.Fatalf("expected Retry, got %s", view.Provisioning)
	}
	if view.AllChecksReady {
		t.Fatal("expected AllChecksReady=false")
	}
	if view.AdmissionChecks[0].State != CheckRetry {
		t.Fatalf("expected CheckRetry state, got %s", view.AdmissionChecks[0].State)
	}
}

// ---------- Multiple admission checks ----------

func TestBuildViewMultipleACsAllReady(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "resume-readiness", State: kueuev1beta2.CheckStateReady},
				{Name: "provision-ac", State: kueuev1beta2.CheckStateReady},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if !view.AllChecksReady {
		t.Fatal("expected AllChecksReady=true")
	}
	if view.Provisioning != ProvisioningProvisioned {
		t.Fatalf("expected Provisioned, got %s", view.Provisioning)
	}
	if !view.IsLaunchReady() {
		t.Fatal("expected launch ready")
	}

	// Verify both ACs are captured.
	if len(view.AdmissionChecks) != 2 {
		t.Fatalf("expected 2 ACs, got %d", len(view.AdmissionChecks))
	}
	resumeAC := view.AdmissionChecks[0]
	if resumeAC.IsProvisioningCheck {
		t.Fatal("resume-readiness should not be a provisioning check")
	}
	provAC := view.AdmissionChecks[1]
	if !provAC.IsProvisioningCheck {
		t.Fatal("provision-ac should be a provisioning check")
	}
}

func TestBuildViewMultipleACsOneNotReady(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "resume-readiness", State: kueuev1beta2.CheckStatePending},
				{Name: "provision-ac", State: kueuev1beta2.CheckStateReady},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if view.AllChecksReady {
		t.Fatal("expected AllChecksReady=false (resume-readiness is Pending)")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready")
	}
}

// ---------- podSetUpdates parsing ----------

func TestBuildViewPodSetUpdatesFromACs(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{
					Name:  "provision-ac",
					State: kueuev1beta2.CheckStateReady,
					PodSetUpdates: []kueuev1beta2.PodSetUpdate{
						{
							Name:         "workers",
							NodeSelector: map[string]string{"cloud.google.com/gke-nodepool": "gpu-pool-1"},
							Tolerations: []corev1.Toleration{
								{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists},
							},
						},
					},
				},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)
	if len(view.PodSetUpdates) != 1 {
		t.Fatalf("expected 1 PodSetUpdateSet, got %d", len(view.PodSetUpdates))
	}
	if view.PodSetUpdates[0].AdmissionCheckName != "provision-ac" {
		t.Fatalf("expected AC name 'provision-ac', got %q", view.PodSetUpdates[0].AdmissionCheckName)
	}
	if len(view.PodSetUpdates[0].Updates) != 1 {
		t.Fatalf("expected 1 update entry, got %d", len(view.PodSetUpdates[0].Updates))
	}

	entry := view.PodSetUpdates[0].Updates[0]
	if entry.NodeSelector["cloud.google.com/gke-nodepool"] != "gpu-pool-1" {
		t.Fatalf("expected nodeSelector value, got %v", entry.NodeSelector)
	}
	if len(entry.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(entry.Tolerations))
	}

	// MergedPodSetUpdates should work.
	merged := view.MergedPodSetUpdates()
	if merged == nil {
		t.Fatal("expected non-nil merged updates")
	}
	if merged["workers"].NodeSelector["cloud.google.com/gke-nodepool"] != "gpu-pool-1" {
		t.Fatalf("expected merged nodeSelector, got %v", merged["workers"].NodeSelector)
	}
}

func TestBuildViewNoPodSetUpdates(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "provision-ac", State: kueuev1beta2.CheckStateReady},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
	}

	view := BuildView(wl, opts)
	if len(view.PodSetUpdates) != 0 {
		t.Fatalf("expected no PodSetUpdates, got %d", len(view.PodSetUpdates))
	}
}

// ---------- Topology detection ----------

func TestBuildViewTopologyAssigned(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{
						Name:  "workers",
						Count: ptr.To[int32](4),
						TopologyAssignment: &kueuev1beta2.TopologyAssignment{
							Levels: []string{"topology.kubernetes.io/zone"},
						},
					},
				},
			},
		},
	}
	opts := ViewOptions{TopologyEnabled: true}

	view := BuildView(wl, opts)
	if !view.TopologyState.Configured {
		t.Fatal("expected topology configured")
	}
	if !view.TopologyState.Assigned {
		t.Fatal("expected topology assigned")
	}
	if view.TopologyState.SecondPassPending {
		t.Fatal("expected SecondPassPending=false")
	}
	if !view.IsLaunchReady() {
		t.Fatal("expected launch ready")
	}
}

func TestBuildViewTopologyPendingSecondPass(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
		},
	}
	opts := ViewOptions{TopologyEnabled: true}

	view := BuildView(wl, opts)
	if !view.TopologyState.Configured {
		t.Fatal("expected topology configured")
	}
	if view.TopologyState.Assigned {
		t.Fatal("expected topology not assigned")
	}
	if !view.TopologyState.SecondPassPending {
		t.Fatal("expected SecondPassPending=true")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready (topology pending)")
	}
}

func TestBuildViewDelayedTopologyPending(t *testing.T) {
	pending := kueuev1beta2.DelayedTopologyRequestStatePending
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{
						Name:                   "workers",
						Count:                  ptr.To[int32](4),
						DelayedTopologyRequest: &pending,
					},
				},
			},
		},
	}
	opts := ViewOptions{TopologyEnabled: true}

	view := BuildView(wl, opts)
	if view.TopologyState.DelayedTopologyState != DelayedTopologyPending {
		t.Fatalf("expected delayed pending, got %s", view.TopologyState.DelayedTopologyState)
	}
	if !view.TopologyState.SecondPassPending {
		t.Fatal("expected SecondPassPending=true")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready")
	}
}

func TestBuildViewTopologyNotConfigured(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{Name: "workers", Count: ptr.To[int32](4)},
				},
			},
		},
	}
	opts := ViewOptions{TopologyEnabled: false}

	view := BuildView(wl, opts)
	if view.TopologyState.Configured {
		t.Fatal("expected topology not configured")
	}
	if view.TopologyState.SecondPassPending {
		t.Fatal("expected SecondPassPending=false")
	}
	if !view.IsLaunchReady() {
		t.Fatal("expected launch ready (no topology + quota + no ACs)")
	}
}

// ---------- No quota reserved ----------

func TestBuildViewNoAdmission(t *testing.T) {
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "provision-ac", State: kueuev1beta2.CheckStatePending},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		TopologyEnabled:     true,
	}

	view := BuildView(wl, opts)
	if view.QuotaReserved {
		t.Fatal("expected QuotaReserved=false")
	}
	if view.IsLaunchReady() {
		t.Fatal("expected not launch ready (no quota)")
	}
	// Topology should show pending second pass since no admission.
	if !view.TopologyState.SecondPassPending {
		t.Fatal("expected SecondPassPending=true when no admission")
	}
}

// ---------- IsLaunchReady nil receiver ----------

func TestIsLaunchReadyNilView(t *testing.T) {
	var v *LaunchReadinessView
	if v.IsLaunchReady() {
		t.Fatal("expected false for nil view")
	}
}

// ---------- MergedPodSetUpdates nil receiver ----------

func TestMergedPodSetUpdatesNilView(t *testing.T) {
	var v *LaunchReadinessView
	if v.MergedPodSetUpdates() != nil {
		t.Fatal("expected nil for nil view")
	}
}

// ---------- Full integration: provisioning + topology + multiple ACs ----------

func TestBuildViewFullIntegration(t *testing.T) {
	ready := kueuev1beta2.DelayedTopologyRequestStateReady
	wl := &kueuev1beta2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl-1", Namespace: "default"},
		Status: kueuev1beta2.WorkloadStatus{
			Admission: &kueuev1beta2.Admission{
				ClusterQueue: "gpu-queue",
				PodSetAssignments: []kueuev1beta2.PodSetAssignment{
					{
						Name:                   "workers",
						Count:                  ptr.To[int32](4),
						DelayedTopologyRequest: &ready,
						TopologyAssignment: &kueuev1beta2.TopologyAssignment{
							Levels: []string{"topology.kubernetes.io/zone"},
						},
					},
				},
			},
			AdmissionChecks: []kueuev1beta2.AdmissionCheckState{
				{Name: "resume-readiness", State: kueuev1beta2.CheckStateReady},
				{
					Name:  "provision-ac",
					State: kueuev1beta2.CheckStateReady,
					PodSetUpdates: []kueuev1beta2.PodSetUpdate{
						{
							Name:         "workers",
							NodeSelector: map[string]string{"pool": "provisioned-gpu"},
						},
					},
				},
			},
		},
	}
	opts := ViewOptions{
		ProvisioningACNames: map[string]bool{"provision-ac": true},
		TopologyEnabled:     true,
		WorkloadName:        "wl-1",
		WorkloadNamespace:   "default",
	}

	view := BuildView(wl, opts)

	// All conditions met.
	if !view.QuotaReserved {
		t.Fatal("expected QuotaReserved=true")
	}
	if !view.AllChecksReady {
		t.Fatal("expected AllChecksReady=true")
	}
	if view.Provisioning != ProvisioningProvisioned {
		t.Fatalf("expected Provisioned, got %s", view.Provisioning)
	}
	if !view.TopologyState.Assigned {
		t.Fatal("expected topology assigned")
	}
	if view.TopologyState.SecondPassPending {
		t.Fatal("expected SecondPassPending=false")
	}
	if !view.IsLaunchReady() {
		t.Fatal("expected launch ready")
	}

	// PodSetUpdates should be present from provision-ac.
	merged := view.MergedPodSetUpdates()
	if merged["workers"].NodeSelector["pool"] != "provisioned-gpu" {
		t.Fatalf("expected provisioned nodeSelector, got %v", merged["workers"].NodeSelector)
	}

	// PR ref should be resolved.
	if view.ProvisioningRequestRef == nil {
		t.Fatal("expected non-nil PR ref")
	}
	if view.ProvisioningRequestRef.Name != "wl-1-provision-ac-1" {
		t.Fatalf("expected PR name, got %q", view.ProvisioningRequestRef.Name)
	}
}

// ---------- AC check state normalization ----------

func TestNormalizeCheckState(t *testing.T) {
	tests := []struct {
		input    kueuev1beta2.CheckState
		expected CheckStateClassification
	}{
		{kueuev1beta2.CheckStatePending, CheckPending},
		{kueuev1beta2.CheckStateReady, CheckReady},
		{kueuev1beta2.CheckStateRetry, CheckRetry},
		{kueuev1beta2.CheckStateRejected, CheckRejected},
		{kueuev1beta2.CheckState("Unknown"), CheckPending},
	}

	for _, tt := range tests {
		got := normalizeCheckState(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeCheckState(%q) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}
