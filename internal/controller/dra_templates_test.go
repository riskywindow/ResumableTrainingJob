package controller

import (
	"context"
	"testing"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/dra"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = trainingv1alpha1.SchemeBuilder.AddToScheme(scheme)
	_ = resourcev1beta1.AddToScheme(scheme)
	return scheme
}

func makeTestRTJ(name, ns string, devices *trainingv1alpha1.DeviceSpec) *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       "test-uid-1234",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			Devices: devices,
		},
	}
}

func singleGPUDeviceSpec() *trainingv1alpha1.DeviceSpec {
	return &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
					Selectors:       []string{`device.attributes["memory"].compareTo(quantity("80Gi")) >= 0`},
				},
			},
		},
	}
}

func multiClaimDeviceSpec() *trainingv1alpha1.DeviceSpec {
	return &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
			{
				Name:       "rdma",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "rdma.example.com",
					Count:           1,
				},
			},
		},
	}
}

func newTestReconciler(objs ...runtime.Object) *ResumableTrainingJobReconciler {
	scheme := newTestScheme()
	clientObjs := make([]runtime.Object, len(objs))
	copy(clientObjs, objs)

	cb := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range clientObjs {
		cb = cb.WithRuntimeObjects(o)
	}
	cl := cb.Build()

	return &ResumableTrainingJobReconciler{
		Client: cl,
		Scheme: scheme,
		Now:    func() metav1.Time { return metav1.Now() },
	}
}

// --- Test: Devices not configured ---

func TestReconcileDRATemplates_NoDevices(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	r := newTestReconciler(rtj)
	ctx := context.Background()

	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TemplatesReady {
		t.Error("expected TemplatesReady=true when no devices")
	}
	if rtj.Status.Devices != nil {
		t.Error("expected nil device status when no devices")
	}
}

func TestReconcileDRATemplates_DisabledMode(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDisabled,
	})
	r := newTestReconciler(rtj)
	ctx := context.Background()

	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TemplatesReady {
		t.Error("expected TemplatesReady=true for Disabled mode")
	}
}

func TestReconcileDRATemplates_ClearsStatusWhenDisabled(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	rtj.Status.Devices = &trainingv1alpha1.DeviceStatus{
		DeviceMode: trainingv1alpha1.DeviceModeDRA,
	}

	r := newTestReconciler(rtj)
	ctx := context.Background()

	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StatusChanged {
		t.Error("expected StatusChanged=true when clearing device status")
	}
	if rtj.Status.Devices != nil {
		t.Error("expected nil device status after clearing")
	}
}

// --- Test: Single claim template creation ---

func TestReconcileDRATemplates_CreatesSingleTemplate(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TemplatesReady {
		t.Error("expected TemplatesReady=true after creation")
	}
	if !result.StatusChanged {
		t.Error("expected StatusChanged=true after first reconcile")
	}

	// Verify the template was created.
	tmpl := &resourcev1beta1.ResourceClaimTemplate{}
	key := types.NamespacedName{Namespace: "default", Name: "my-rtj-gpu"}
	if err := r.Get(ctx, key, tmpl); err != nil {
		t.Fatalf("expected template to exist: %v", err)
	}

	// Check the template's device request.
	reqs := tmpl.Spec.Spec.Devices.Requests
	if len(reqs) != 1 {
		t.Fatalf("expected 1 device request, got %d", len(reqs))
	}
	if reqs[0].DeviceClassName != "gpu.example.com" {
		t.Errorf("expected DeviceClassName gpu.example.com, got %q", reqs[0].DeviceClassName)
	}
	if reqs[0].Count != 4 {
		t.Errorf("expected Count 4, got %d", reqs[0].Count)
	}

	// Check status was synced.
	if rtj.Status.Devices == nil {
		t.Fatal("expected non-nil device status")
	}
	if rtj.Status.Devices.DeviceMode != trainingv1alpha1.DeviceModeDRA {
		t.Errorf("expected DeviceMode=DRA, got %q", rtj.Status.Devices.DeviceMode)
	}
	if rtj.Status.Devices.CurrentDeviceProfileFingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
	if len(rtj.Status.Devices.ResourceClaimTemplateRefs) != 1 {
		t.Fatalf("expected 1 template ref, got %d", len(rtj.Status.Devices.ResourceClaimTemplateRefs))
	}
	if rtj.Status.Devices.ResourceClaimTemplateRefs[0].Name != "my-rtj-gpu" {
		t.Errorf("expected ref name my-rtj-gpu, got %q", rtj.Status.Devices.ResourceClaimTemplateRefs[0].Name)
	}
}

// --- Test: Multiple claim template creation ---

func TestReconcileDRATemplates_CreatesMultipleTemplates(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", multiClaimDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TemplatesReady {
		t.Error("expected TemplatesReady=true")
	}

	// Verify both templates were created.
	for _, name := range []string{"my-rtj-gpu", "my-rtj-rdma"} {
		tmpl := &resourcev1beta1.ResourceClaimTemplate{}
		key := types.NamespacedName{Namespace: "default", Name: name}
		if err := r.Get(ctx, key, tmpl); err != nil {
			t.Errorf("expected template %s to exist: %v", name, err)
		}
	}

	// Check status has both refs.
	if len(rtj.Status.Devices.ResourceClaimTemplateRefs) != 2 {
		t.Fatalf("expected 2 template refs, got %d", len(rtj.Status.Devices.ResourceClaimTemplateRefs))
	}
}

// --- Test: Idempotent reconciliation ---

func TestReconcileDRATemplates_Idempotent(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	// First reconcile.
	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	fingerprint1 := rtj.Status.Devices.CurrentDeviceProfileFingerprint

	// Second reconcile should be a no-op.
	result2, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if !result2.TemplatesReady {
		t.Error("expected TemplatesReady=true on second reconcile")
	}
	if result2.StatusChanged {
		t.Error("expected StatusChanged=false on idempotent reconcile")
	}

	// Fingerprint should be stable.
	if rtj.Status.Devices.CurrentDeviceProfileFingerprint != fingerprint1 {
		t.Error("fingerprint changed on idempotent reconcile")
	}
}

// --- Test: Spec drift detection and recreation ---

func TestReconcileDRATemplates_SpecDriftRecreatesTemplate(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	// Create initial template.
	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	fingerprint1 := rtj.Status.Devices.CurrentDeviceProfileFingerprint

	// Change the device spec (different count).
	rtj.Spec.Devices.Claims[0].Request.Count = 8

	// Reconcile should detect drift and recreate.
	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("drift reconcile: %v", err)
	}
	if !result.TemplatesReady {
		t.Error("expected TemplatesReady=true after drift reconcile")
	}

	// Verify the template has the new count.
	tmpl := &resourcev1beta1.ResourceClaimTemplate{}
	key := types.NamespacedName{Namespace: "default", Name: "my-rtj-gpu"}
	if err := r.Get(ctx, key, tmpl); err != nil {
		t.Fatalf("expected template to exist: %v", err)
	}
	if tmpl.Spec.Spec.Devices.Requests[0].Count != 8 {
		t.Errorf("expected count 8, got %d", tmpl.Spec.Spec.Devices.Requests[0].Count)
	}

	// Fingerprint should have changed.
	fingerprint2 := rtj.Status.Devices.CurrentDeviceProfileFingerprint
	if fingerprint1 == fingerprint2 {
		t.Error("fingerprint should change when spec changes")
	}
}

func TestReconcileDRATemplates_SpecDriftDifferentClass(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	// Change device class.
	rtj.Spec.Devices.Claims[0].Request.DeviceClassName = "gpu-h100"

	_, err = r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("drift reconcile: %v", err)
	}

	// Verify the new class.
	tmpl := &resourcev1beta1.ResourceClaimTemplate{}
	key := types.NamespacedName{Namespace: "default", Name: "my-rtj-gpu"}
	if err := r.Get(ctx, key, tmpl); err != nil {
		t.Fatalf("expected template to exist: %v", err)
	}
	if tmpl.Spec.Spec.Devices.Requests[0].DeviceClassName != "gpu-h100" {
		t.Errorf("expected DeviceClassName gpu-h100, got %q", tmpl.Spec.Spec.Devices.Requests[0].DeviceClassName)
	}
}

// --- Test: Orphan cleanup ---

func TestReconcileDRATemplates_CleansUpOrphans(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", multiClaimDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	// Create templates for both claims.
	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	// Remove the rdma claim from spec.
	rtj.Spec.Devices.Claims = rtj.Spec.Devices.Claims[:1] // keep only gpu

	// Reconcile should clean up the orphaned rdma template.
	_, err = r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("cleanup reconcile: %v", err)
	}

	// Verify gpu template still exists.
	tmpl := &resourcev1beta1.ResourceClaimTemplate{}
	key := types.NamespacedName{Namespace: "default", Name: "my-rtj-gpu"}
	if err := r.Get(ctx, key, tmpl); err != nil {
		t.Fatalf("expected gpu template to exist: %v", err)
	}

	// Verify rdma template was deleted.
	orphanKey := types.NamespacedName{Namespace: "default", Name: "my-rtj-rdma"}
	err = r.Get(ctx, orphanKey, tmpl)
	if err == nil {
		t.Error("expected rdma template to be deleted")
	}
}

// --- Test: Owner reference verification ---

func TestReconcileDRATemplates_OwnerReference(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl := &resourcev1beta1.ResourceClaimTemplate{}
	key := types.NamespacedName{Namespace: "default", Name: "my-rtj-gpu"}
	if err := r.Get(ctx, key, tmpl); err != nil {
		t.Fatalf("expected template to exist: %v", err)
	}

	if len(tmpl.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(tmpl.OwnerReferences))
	}

	ref := tmpl.OwnerReferences[0]
	if ref.Name != "my-rtj" {
		t.Errorf("expected owner name my-rtj, got %q", ref.Name)
	}
	if ref.Kind != "ResumableTrainingJob" {
		t.Errorf("expected owner kind ResumableTrainingJob, got %q", ref.Kind)
	}
	if ref.UID != rtj.UID {
		t.Errorf("expected owner UID %s, got %s", rtj.UID, ref.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Error("expected controller=true")
	}
	if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		t.Error("expected blockOwnerDeletion=true")
	}
}

// --- Test: Labels on created templates ---

func TestReconcileDRATemplates_Labels(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl := &resourcev1beta1.ResourceClaimTemplate{}
	key := types.NamespacedName{Namespace: "default", Name: "my-rtj-gpu"}
	if err := r.Get(ctx, key, tmpl); err != nil {
		t.Fatalf("expected template to exist: %v", err)
	}

	expected := map[string]string{
		"training.checkpoint.example.io/rtj-name":   "my-rtj",
		"training.checkpoint.example.io/claim-name": "gpu",
		"training.checkpoint.example.io/managed-by": "rtj-operator",
	}
	for k, v := range expected {
		if tmpl.Labels[k] != v {
			t.Errorf("expected label %s=%s, got %s=%s", k, v, k, tmpl.Labels[k])
		}
	}
}

// --- Test: Device status sync ---

func TestReconcileDRATemplates_SyncsDeviceStatus(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ds := rtj.Status.Devices
	if ds == nil {
		t.Fatal("expected non-nil device status")
	}
	if ds.DeviceMode != trainingv1alpha1.DeviceModeDRA {
		t.Errorf("expected DeviceMode=DRA, got %q", ds.DeviceMode)
	}
	if len(ds.RequestedDeviceClasses) != 1 || ds.RequestedDeviceClasses[0] != "gpu.example.com" {
		t.Errorf("expected [gpu.example.com], got %v", ds.RequestedDeviceClasses)
	}
	if ds.CurrentDeviceProfileFingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
	if len(ds.ResourceClaimTemplateRefs) != 1 {
		t.Fatalf("expected 1 template ref, got %d", len(ds.ResourceClaimTemplateRefs))
	}
	if ds.ClaimAllocationState != trainingv1alpha1.ClaimAllocationPending {
		t.Errorf("expected ClaimAllocationPending, got %q", ds.ClaimAllocationState)
	}
}

// --- Test: Fingerprint stability ---

func TestReconcileDRATemplates_FingerprintStableAcrossReconciles(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	fp1 := rtj.Status.Devices.CurrentDeviceProfileFingerprint

	_, err = r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	fp2 := rtj.Status.Devices.CurrentDeviceProfileFingerprint

	if fp1 != fp2 {
		t.Errorf("fingerprint not stable: %q != %q", fp1, fp2)
	}
}

// --- Test: isOwnedByRTJ ---

func TestIsOwnedByRTJ(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)

	tests := []struct {
		name string
		refs []metav1.OwnerReference
		want bool
	}{
		{
			name: "owned",
			refs: []metav1.OwnerReference{
				{
					Kind: dra.RTJGroupVersionKind.Kind,
					Name: "my-rtj",
					UID:  rtj.UID,
				},
			},
			want: true,
		},
		{
			name: "different name",
			refs: []metav1.OwnerReference{
				{
					Kind: dra.RTJGroupVersionKind.Kind,
					Name: "other-rtj",
					UID:  "other-uid",
				},
			},
			want: false,
		},
		{
			name: "different kind",
			refs: []metav1.OwnerReference{
				{
					Kind: "JobSet",
					Name: "my-rtj",
					UID:  rtj.UID,
				},
			},
			want: false,
		},
		{
			name: "no refs",
			refs: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOwnedByRTJ(tt.refs, rtj)
			if got != tt.want {
				t.Errorf("isOwnedByRTJ() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Test: Transition from DRA to Disabled ---

func TestReconcileDRATemplates_TransitionDRAToDisabled(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", singleGPUDeviceSpec())
	r := newTestReconciler(rtj)
	ctx := context.Background()

	// Create with DRA.
	_, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if rtj.Status.Devices == nil {
		t.Fatal("expected non-nil device status")
	}

	// Switch to Disabled.
	rtj.Spec.Devices.Mode = trainingv1alpha1.DeviceModeDisabled
	rtj.Spec.Devices.Claims = nil

	result, err := r.reconcileDRATemplates(ctx, rtj, metav1.Now())
	if err != nil {
		t.Fatalf("disabled reconcile: %v", err)
	}
	if !result.StatusChanged {
		t.Error("expected StatusChanged=true when transitioning to Disabled")
	}
	if rtj.Status.Devices != nil {
		t.Error("expected nil device status after Disabled transition")
	}
}

// --- Test: Status helpers directly ---

func TestSyncDeviceStatus_SetsFields(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	profile := dra.BuildProfile(&trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
		},
	})

	refs := []trainingv1alpha1.ResourceClaimTemplateReference{
		{Name: "my-rtj-gpu", ClaimName: "gpu"},
	}

	changed := syncDeviceStatus(rtj, profile, refs)
	if !changed {
		t.Error("expected changed=true on first sync")
	}
	if rtj.Status.Devices == nil {
		t.Fatal("expected non-nil device status")
	}
	if rtj.Status.Devices.DeviceMode != trainingv1alpha1.DeviceModeDRA {
		t.Errorf("expected DRA mode, got %q", rtj.Status.Devices.DeviceMode)
	}
	if rtj.Status.Devices.CurrentDeviceProfileFingerprint != profile.Fingerprint {
		t.Error("fingerprint mismatch")
	}
}

func TestSyncDeviceStatus_Idempotent(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	profile := dra.BuildProfile(&trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
		},
	})
	refs := []trainingv1alpha1.ResourceClaimTemplateReference{
		{Name: "my-rtj-gpu", ClaimName: "gpu"},
	}

	syncDeviceStatus(rtj, profile, refs)
	changed := syncDeviceStatus(rtj, profile, refs)
	if changed {
		t.Error("expected changed=false on idempotent sync")
	}
}

func TestClearDeviceStatus_AlreadyNil(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	changed := clearDeviceStatus(rtj)
	if changed {
		t.Error("expected changed=false when already nil")
	}
}

func TestClearDeviceStatus_ClearsExisting(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	rtj.Status.Devices = &trainingv1alpha1.DeviceStatus{
		DeviceMode: trainingv1alpha1.DeviceModeDRA,
	}
	changed := clearDeviceStatus(rtj)
	if !changed {
		t.Error("expected changed=true when clearing")
	}
	if rtj.Status.Devices != nil {
		t.Error("expected nil device status")
	}
}

func TestDeviceStatusEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b *trainingv1alpha1.DeviceStatus
		want bool
	}{
		{"both nil", nil, nil, true},
		{"left nil", nil, &trainingv1alpha1.DeviceStatus{DeviceMode: trainingv1alpha1.DeviceModeDRA}, false},
		{"right nil", &trainingv1alpha1.DeviceStatus{DeviceMode: trainingv1alpha1.DeviceModeDRA}, nil, false},
		{
			"equal",
			&trainingv1alpha1.DeviceStatus{
				DeviceMode:                     trainingv1alpha1.DeviceModeDRA,
				CurrentDeviceProfileFingerprint: "abc",
				RequestedDeviceClasses:         []string{"gpu"},
				ResourceClaimTemplateRefs: []trainingv1alpha1.ResourceClaimTemplateReference{
					{Name: "r1", ClaimName: "c1"},
				},
			},
			&trainingv1alpha1.DeviceStatus{
				DeviceMode:                     trainingv1alpha1.DeviceModeDRA,
				CurrentDeviceProfileFingerprint: "abc",
				RequestedDeviceClasses:         []string{"gpu"},
				ResourceClaimTemplateRefs: []trainingv1alpha1.ResourceClaimTemplateReference{
					{Name: "r1", ClaimName: "c1"},
				},
			},
			true,
		},
		{
			"different fingerprint",
			&trainingv1alpha1.DeviceStatus{
				DeviceMode:                     trainingv1alpha1.DeviceModeDRA,
				CurrentDeviceProfileFingerprint: "abc",
			},
			&trainingv1alpha1.DeviceStatus{
				DeviceMode:                     trainingv1alpha1.DeviceModeDRA,
				CurrentDeviceProfileFingerprint: "def",
			},
			false,
		},
		{
			"different classes",
			&trainingv1alpha1.DeviceStatus{
				DeviceMode:             trainingv1alpha1.DeviceModeDRA,
				RequestedDeviceClasses: []string{"gpu"},
			},
			&trainingv1alpha1.DeviceStatus{
				DeviceMode:             trainingv1alpha1.DeviceModeDRA,
				RequestedDeviceClasses: []string{"gpu", "rdma"},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deviceStatusEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("deviceStatusEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSyncDeviceStatus_PreservesAllocationFields(t *testing.T) {
	rtj := makeTestRTJ("my-rtj", "default", nil)
	rtj.Status.Devices = &trainingv1alpha1.DeviceStatus{
		DeviceMode:                     trainingv1alpha1.DeviceModeDRA,
		ClaimAllocationState:           trainingv1alpha1.ClaimAllocationAllocated,
		AllocatedClaimCount:            4,
		LastClaimFailureReason:         "some-reason",
		LastCheckpointDeviceProfileFingerprint: "ckpt-fp",
		LastResumeDeviceProfileFingerprint:     "resume-fp",
		CurrentDeviceProfileFingerprint: "old-fp",
		RequestedDeviceClasses:         []string{"old-class"},
	}

	profile := dra.BuildProfile(&trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
		},
	})
	refs := []trainingv1alpha1.ResourceClaimTemplateReference{
		{Name: "my-rtj-gpu", ClaimName: "gpu"},
	}

	changed := syncDeviceStatus(rtj, profile, refs)
	if !changed {
		t.Error("expected changed=true")
	}

	ds := rtj.Status.Devices
	// Allocation fields should be preserved.
	if ds.ClaimAllocationState != trainingv1alpha1.ClaimAllocationAllocated {
		t.Errorf("expected preserved allocation state, got %q", ds.ClaimAllocationState)
	}
	if ds.AllocatedClaimCount != 4 {
		t.Errorf("expected preserved allocated count 4, got %d", ds.AllocatedClaimCount)
	}
	if ds.LastCheckpointDeviceProfileFingerprint != "ckpt-fp" {
		t.Error("expected preserved checkpoint fingerprint")
	}
	if ds.LastResumeDeviceProfileFingerprint != "resume-fp" {
		t.Error("expected preserved resume fingerprint")
	}

	// Profile fields should be updated.
	if ds.CurrentDeviceProfileFingerprint == "old-fp" {
		t.Error("expected updated fingerprint")
	}
	if len(ds.RequestedDeviceClasses) != 1 || ds.RequestedDeviceClasses[0] != "gpu.example.com" {
		t.Errorf("expected updated device classes, got %v", ds.RequestedDeviceClasses)
	}
}
