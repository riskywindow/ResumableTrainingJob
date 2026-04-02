package dra

import (
	"testing"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

func makeRTJ(name, ns string, devices *trainingv1alpha1.DeviceSpec) *trainingv1alpha1.ResumableTrainingJob {
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

func TestBuildDesiredTemplates_NilDevices(t *testing.T) {
	rtj := makeRTJ("my-rtj", "default", nil)
	templates := BuildDesiredTemplates(rtj)
	if templates != nil {
		t.Errorf("expected nil templates, got %d", len(templates))
	}
}

func TestBuildDesiredTemplates_DisabledMode(t *testing.T) {
	rtj := makeRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDisabled,
	})
	templates := BuildDesiredTemplates(rtj)
	if templates != nil {
		t.Errorf("expected nil templates for Disabled mode, got %d", len(templates))
	}
}

func TestBuildDesiredTemplates_SingleClaim(t *testing.T) {
	rtj := makeRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
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
	})

	templates := BuildDesiredTemplates(rtj)
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}

	tmpl := templates[0]
	if tmpl.Name != "my-rtj-gpu" {
		t.Errorf("expected name my-rtj-gpu, got %q", tmpl.Name)
	}
	if tmpl.ClaimName != "gpu" {
		t.Errorf("expected claim name gpu, got %q", tmpl.ClaimName)
	}

	// Check template object.
	obj := tmpl.Template
	if obj.Name != "my-rtj-gpu" {
		t.Errorf("expected template name my-rtj-gpu, got %q", obj.Name)
	}
	if obj.Namespace != "default" {
		t.Errorf("expected namespace default, got %q", obj.Namespace)
	}

	// Check owner reference.
	if len(obj.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(obj.OwnerReferences))
	}
	ownerRef := obj.OwnerReferences[0]
	if ownerRef.Name != "my-rtj" {
		t.Errorf("expected owner name my-rtj, got %q", ownerRef.Name)
	}
	if ownerRef.Kind != "ResumableTrainingJob" {
		t.Errorf("expected owner kind ResumableTrainingJob, got %q", ownerRef.Kind)
	}
	if ownerRef.Controller == nil || !*ownerRef.Controller {
		t.Error("expected controller=true")
	}
	if ownerRef.BlockOwnerDeletion == nil || !*ownerRef.BlockOwnerDeletion {
		t.Error("expected blockOwnerDeletion=true")
	}

	// Check device requests.
	reqs := obj.Spec.Spec.Devices.Requests
	if len(reqs) != 1 {
		t.Fatalf("expected 1 device request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.DeviceClassName != "gpu.example.com" {
		t.Errorf("expected DeviceClassName gpu.example.com, got %q", req.DeviceClassName)
	}
	if req.Count != 4 {
		t.Errorf("expected Count 4, got %d", req.Count)
	}
	if req.AllocationMode != resourcev1beta1.DeviceAllocationModeExactCount {
		t.Errorf("expected ExactCount allocation mode, got %q", req.AllocationMode)
	}
	if len(req.Selectors) != 1 {
		t.Fatalf("expected 1 selector, got %d", len(req.Selectors))
	}
	if req.Selectors[0].CEL == nil || req.Selectors[0].CEL.Expression != `device.attributes["memory"].compareTo(quantity("80Gi")) >= 0` {
		t.Errorf("unexpected selector: %+v", req.Selectors[0])
	}
}

func TestBuildDesiredTemplates_MultipleClaims(t *testing.T) {
	rtj := makeRTJ("training-1", "ml-team", &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "rdma",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "rdma.example.com",
					Count:           1,
				},
			},
			{
				Name:       "gpu",
				Containers: []string{"worker", "sidecar"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           8,
				},
			},
		},
	})

	templates := BuildDesiredTemplates(rtj)
	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}

	// Should be sorted by claim name.
	if templates[0].ClaimName != "gpu" {
		t.Errorf("expected first claim to be gpu, got %q", templates[0].ClaimName)
	}
	if templates[1].ClaimName != "rdma" {
		t.Errorf("expected second claim to be rdma, got %q", templates[1].ClaimName)
	}

	// Check names.
	if templates[0].Name != "training-1-gpu" {
		t.Errorf("expected training-1-gpu, got %q", templates[0].Name)
	}
	if templates[1].Name != "training-1-rdma" {
		t.Errorf("expected training-1-rdma, got %q", templates[1].Name)
	}

	// Check both are in the same namespace as the RTJ.
	for _, tmpl := range templates {
		if tmpl.Template.Namespace != "ml-team" {
			t.Errorf("expected namespace ml-team for %s, got %q", tmpl.Name, tmpl.Template.Namespace)
		}
	}
}

func TestBuildDesiredTemplates_Labels(t *testing.T) {
	rtj := makeRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           1,
				},
			},
		},
	})

	templates := BuildDesiredTemplates(rtj)
	obj := templates[0].Template

	expectedLabels := map[string]string{
		"training.checkpoint.example.io/rtj-name":   "my-rtj",
		"training.checkpoint.example.io/claim-name": "gpu",
		"training.checkpoint.example.io/managed-by": "rtj-operator",
	}

	for k, v := range expectedLabels {
		if obj.Labels[k] != v {
			t.Errorf("expected label %s=%s, got %s=%s", k, v, k, obj.Labels[k])
		}
	}
}

func TestBuildDesiredTemplates_NoSelectors(t *testing.T) {
	rtj := makeRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           1,
				},
			},
		},
	})

	templates := BuildDesiredTemplates(rtj)
	reqs := templates[0].Template.Spec.Spec.Devices.Requests
	if len(reqs[0].Selectors) != 0 {
		t.Errorf("expected no selectors, got %d", len(reqs[0].Selectors))
	}
}

func TestBuildDesiredTemplates_Deterministic(t *testing.T) {
	rtj := makeRTJ("my-rtj", "default", &trainingv1alpha1.DeviceSpec{
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

	t1 := BuildDesiredTemplates(rtj)
	t2 := BuildDesiredTemplates(rtj)

	if t1[0].Name != t2[0].Name {
		t.Error("template names not deterministic")
	}
	if !TemplateSpecMatches(t1[0].Template, t2[0].Template) {
		t.Error("template specs not deterministic")
	}
}

func TestTemplateSpecMatches_Identical(t *testing.T) {
	tmpl := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{
							Name:            "gpu-req",
							DeviceClassName: "gpu.example.com",
							Count:           4,
							AllocationMode:  resourcev1beta1.DeviceAllocationModeExactCount,
							Selectors: []resourcev1beta1.DeviceSelector{
								{CEL: &resourcev1beta1.CELDeviceSelector{Expression: "sel-a"}},
							},
						},
					},
				},
			},
		},
	}

	if !TemplateSpecMatches(tmpl, tmpl) {
		t.Error("identical templates should match")
	}
}

func TestTemplateSpecMatches_DifferentClass(t *testing.T) {
	tmpl1 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{Name: "req", DeviceClassName: "gpu-a100", Count: 1, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
					},
				},
			},
		},
	}
	tmpl2 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{Name: "req", DeviceClassName: "gpu-h100", Count: 1, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
					},
				},
			},
		},
	}

	if TemplateSpecMatches(tmpl1, tmpl2) {
		t.Error("different device classes should not match")
	}
}

func TestTemplateSpecMatches_DifferentCount(t *testing.T) {
	tmpl1 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{Name: "req", DeviceClassName: "gpu", Count: 4, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
					},
				},
			},
		},
	}
	tmpl2 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{Name: "req", DeviceClassName: "gpu", Count: 8, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
					},
				},
			},
		},
	}

	if TemplateSpecMatches(tmpl1, tmpl2) {
		t.Error("different counts should not match")
	}
}

func TestTemplateSpecMatches_DifferentSelectorCount(t *testing.T) {
	tmpl1 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{
							Name:           "req",
							DeviceClassName: "gpu",
							Count:          1,
							AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount,
							Selectors: []resourcev1beta1.DeviceSelector{
								{CEL: &resourcev1beta1.CELDeviceSelector{Expression: "a"}},
							},
						},
					},
				},
			},
		},
	}
	tmpl2 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{
							Name:           "req",
							DeviceClassName: "gpu",
							Count:          1,
							AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount,
							Selectors: []resourcev1beta1.DeviceSelector{
								{CEL: &resourcev1beta1.CELDeviceSelector{Expression: "a"}},
								{CEL: &resourcev1beta1.CELDeviceSelector{Expression: "b"}},
							},
						},
					},
				},
			},
		},
	}

	if TemplateSpecMatches(tmpl1, tmpl2) {
		t.Error("different selector counts should not match")
	}
}

func TestTemplateSpecMatches_DifferentRequestCount(t *testing.T) {
	tmpl1 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{Name: "req", DeviceClassName: "gpu", Count: 1, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
					},
				},
			},
		},
	}
	tmpl2 := &resourcev1beta1.ResourceClaimTemplate{
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{Name: "req1", DeviceClassName: "gpu", Count: 1, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
						{Name: "req2", DeviceClassName: "rdma", Count: 1, AllocationMode: resourcev1beta1.DeviceAllocationModeExactCount},
					},
				},
			},
		},
	}

	if TemplateSpecMatches(tmpl1, tmpl2) {
		t.Error("different request counts should not match")
	}
}

func TestTemplateKey(t *testing.T) {
	key := TemplateKey("default", "my-rtj-gpu")
	if key.Namespace != "default" || key.Name != "my-rtj-gpu" {
		t.Errorf("unexpected key: %v", key)
	}
}
