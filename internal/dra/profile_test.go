package dra

import (
	"testing"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

func TestBuildProfile_NilDeviceSpec(t *testing.T) {
	p := BuildProfile(nil)
	if !p.IsEmpty() {
		t.Errorf("expected empty profile for nil spec, got fingerprint=%q", p.Fingerprint)
	}
	if len(p.DeviceClasses) != 0 {
		t.Errorf("expected no device classes, got %v", p.DeviceClasses)
	}
}

func TestBuildProfile_DisabledMode(t *testing.T) {
	spec := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDisabled,
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
	}
	p := BuildProfile(spec)
	if !p.IsEmpty() {
		t.Errorf("expected empty profile for Disabled mode, got fingerprint=%q", p.Fingerprint)
	}
}

func TestBuildProfile_EmptyClaims(t *testing.T) {
	spec := &trainingv1alpha1.DeviceSpec{
		Mode:   trainingv1alpha1.DeviceModeDRA,
		Claims: nil,
	}
	p := BuildProfile(spec)
	if !p.IsEmpty() {
		t.Errorf("expected empty profile for empty claims, got fingerprint=%q", p.Fingerprint)
	}
}

func TestBuildProfile_SingleClaim(t *testing.T) {
	spec := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           8,
					Selectors:       []string{`device.attributes["memory"].compareTo(quantity("80Gi")) >= 0`},
				},
			},
		},
	}
	p := BuildProfile(spec)
	if p.IsEmpty() {
		t.Fatal("expected non-empty profile")
	}
	if len(p.Fingerprint) != 64 {
		t.Errorf("expected 64-char hex fingerprint, got len=%d %q", len(p.Fingerprint), p.Fingerprint)
	}
	if len(p.DeviceClasses) != 1 || p.DeviceClasses[0] != "gpu.example.com" {
		t.Errorf("expected [gpu.example.com], got %v", p.DeviceClasses)
	}
}

func TestBuildProfile_Deterministic(t *testing.T) {
	spec := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
					Selectors:       []string{"sel-b", "sel-a"},
				},
			},
		},
	}

	p1 := BuildProfile(spec)
	p2 := BuildProfile(spec)

	if p1.Fingerprint != p2.Fingerprint {
		t.Errorf("fingerprint not deterministic: %q != %q", p1.Fingerprint, p2.Fingerprint)
	}
}

func TestBuildProfile_SelectorOrderIndependent(t *testing.T) {
	spec1 := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           2,
					Selectors:       []string{"sel-b", "sel-a", "sel-c"},
				},
			},
		},
	}
	spec2 := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           2,
					Selectors:       []string{"sel-c", "sel-a", "sel-b"},
				},
			},
		},
	}

	p1 := BuildProfile(spec1)
	p2 := BuildProfile(spec2)

	if p1.Fingerprint != p2.Fingerprint {
		t.Errorf("selector order should not affect fingerprint: %q != %q", p1.Fingerprint, p2.Fingerprint)
	}
}

func TestBuildProfile_ClaimOrderIndependent(t *testing.T) {
	spec1 := &trainingv1alpha1.DeviceSpec{
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
	spec2 := &trainingv1alpha1.DeviceSpec{
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
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
		},
	}

	p1 := BuildProfile(spec1)
	p2 := BuildProfile(spec2)

	if p1.Fingerprint != p2.Fingerprint {
		t.Errorf("claim order should not affect fingerprint: %q != %q", p1.Fingerprint, p2.Fingerprint)
	}
}

func TestBuildProfile_DifferentCountProducesDifferentFingerprint(t *testing.T) {
	spec1 := &trainingv1alpha1.DeviceSpec{
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
	}
	spec2 := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           8,
				},
			},
		},
	}

	p1 := BuildProfile(spec1)
	p2 := BuildProfile(spec2)

	if p1.Fingerprint == p2.Fingerprint {
		t.Error("different counts should produce different fingerprints")
	}
}

func TestBuildProfile_DifferentClassProducesDifferentFingerprint(t *testing.T) {
	spec1 := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu-a100",
					Count:           4,
				},
			},
		},
	}
	spec2 := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu-h100",
					Count:           4,
				},
			},
		},
	}

	p1 := BuildProfile(spec1)
	p2 := BuildProfile(spec2)

	if p1.Fingerprint == p2.Fingerprint {
		t.Error("different device classes should produce different fingerprints")
	}
}

func TestBuildProfile_MultipleClaims_DeviceClassesSorted(t *testing.T) {
	spec := &trainingv1alpha1.DeviceSpec{
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
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
		},
	}

	p := BuildProfile(spec)
	if len(p.DeviceClasses) != 2 {
		t.Fatalf("expected 2 device classes, got %d", len(p.DeviceClasses))
	}
	if p.DeviceClasses[0] != "gpu.example.com" || p.DeviceClasses[1] != "rdma.example.com" {
		t.Errorf("expected sorted [gpu.example.com, rdma.example.com], got %v", p.DeviceClasses)
	}
}

func TestBuildProfile_DuplicateDeviceClassDeduped(t *testing.T) {
	spec := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu-main",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
			{
				Name:       "gpu-extra",
				Containers: []string{"worker"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           2,
				},
			},
		},
	}

	p := BuildProfile(spec)
	if len(p.DeviceClasses) != 1 {
		t.Errorf("expected 1 deduplicated device class, got %d: %v", len(p.DeviceClasses), p.DeviceClasses)
	}
}

func TestBuildProfile_ContainersDoNotAffectFingerprint(t *testing.T) {
	spec1 := &trainingv1alpha1.DeviceSpec{
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
	}
	spec2 := &trainingv1alpha1.DeviceSpec{
		Mode: trainingv1alpha1.DeviceModeDRA,
		Claims: []trainingv1alpha1.DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker", "sidecar"},
				Request: trainingv1alpha1.DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
		},
	}

	p1 := BuildProfile(spec1)
	p2 := BuildProfile(spec2)

	if p1.Fingerprint != p2.Fingerprint {
		t.Error("container targets should not affect the device profile fingerprint")
	}
}

func TestTemplateNameForClaim(t *testing.T) {
	tests := []struct {
		rtjName   string
		claimName string
		want      string
	}{
		{"my-rtj", "gpu", "my-rtj-gpu"},
		{"training-job-1", "rdma", "training-job-1-rdma"},
		{"rtj", "accelerator", "rtj-accelerator"},
	}
	for _, tt := range tests {
		got := TemplateNameForClaim(tt.rtjName, tt.claimName)
		if got != tt.want {
			t.Errorf("TemplateNameForClaim(%q, %q) = %q, want %q", tt.rtjName, tt.claimName, got, tt.want)
		}
	}
}

func TestTemplateNameForClaim_Deterministic(t *testing.T) {
	n1 := TemplateNameForClaim("my-rtj", "gpu")
	n2 := TemplateNameForClaim("my-rtj", "gpu")
	if n1 != n2 {
		t.Errorf("template name not deterministic: %q != %q", n1, n2)
	}
}

func TestTemplateRefs_NilClaims(t *testing.T) {
	refs := TemplateRefs("my-rtj", nil)
	if refs != nil {
		t.Errorf("expected nil refs, got %v", refs)
	}
}

func TestTemplateRefs_SingleClaim(t *testing.T) {
	claims := []trainingv1alpha1.DeviceClaimSpec{
		{Name: "gpu"},
	}
	refs := TemplateRefs("my-rtj", claims)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].Name != "my-rtj-gpu" || refs[0].ClaimName != "gpu" {
		t.Errorf("unexpected ref: %+v", refs[0])
	}
}

func TestTemplateRefs_SortedByClaimName(t *testing.T) {
	claims := []trainingv1alpha1.DeviceClaimSpec{
		{Name: "rdma"},
		{Name: "gpu"},
		{Name: "nic"},
	}
	refs := TemplateRefs("my-rtj", claims)
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	for i := 1; i < len(refs); i++ {
		if refs[i].ClaimName < refs[i-1].ClaimName {
			t.Errorf("refs not sorted: %v before %v", refs[i-1].ClaimName, refs[i].ClaimName)
		}
	}
}
