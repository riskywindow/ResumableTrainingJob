package checkpoints

import (
	"strings"
	"testing"

	"k8s.io/utils/ptr"
)

func TestCheckManifestCompatibilityAcceptsExactMatch(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	request := ResumeRequest{
		StorageRootURI:          "s3://bucket/demo/",
		ClusterIdentity:         "phase1-kind",
		RTJIdentity:             "demo-rtj",
		RuntimeMode:             "DDP",
		WorldSize:               2,
		GPUShape:                "cpu",
		ImageIdentity:           "local/fixture:dev",
		CodeVersionIdentity:     "git:abc123",
		OptimizerMode:           "adamw",
		ShardingMode:            "replicated-optimizer-state",
		SupportedFormatVersions: []string{SupportedManifestFormatVersion},
	}

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected manifest to be compatible, got reason %q", reason)
	}
}

func TestCheckManifestCompatibilityRejectsWorldSizeMismatch(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	request := ResumeRequest{
		StorageRootURI:          "s3://bucket/demo/",
		ClusterIdentity:         "phase1-kind",
		RTJIdentity:             "demo-rtj",
		RuntimeMode:             "DDP",
		WorldSize:               4,
		GPUShape:                "cpu",
		ImageIdentity:           "local/fixture:dev",
		CodeVersionIdentity:     "git:abc123",
		OptimizerMode:           "adamw",
		ShardingMode:            "replicated-optimizer-state",
		SupportedFormatVersions: []string{SupportedManifestFormatVersion},
	}

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if compatible {
		t.Fatalf("expected manifest to be incompatible")
	}
	if reason != "world size mismatch" {
		t.Fatalf("expected world size mismatch, got %q", reason)
	}
}

func TestCheckManifestCompatibilityAllowsWorldSizeChangeWithCrossSizeSupport(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	manifest.CrossSizeRestoreSupported = ptr.To(true)

	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected manifest to be compatible with world-size change, got reason %q", reason)
	}
	if !strings.Contains(reason, "world-size change") {
		t.Fatalf("expected reason to mention world-size change, got %q", reason)
	}
}

func TestCheckManifestCompatibilityRejectsCrossSizeWhenManifestDoesNotSupport(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	// CrossSizeRestoreSupported is nil (Phase 2 manifest)

	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if compatible {
		t.Fatalf("expected manifest without cross-size restore support to be rejected")
	}
	if reason != "checkpoint does not support cross-size restore" {
		t.Fatalf("expected 'checkpoint does not support cross-size restore', got %q", reason)
	}
}

func TestCheckManifestCompatibilityRejectsCrossSizeWhenExplicitlyFalse(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	manifest.CrossSizeRestoreSupported = ptr.To(false)

	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if compatible {
		t.Fatalf("expected manifest with CrossSizeRestoreSupported=false to be rejected")
	}
	if reason != "checkpoint does not support cross-size restore" {
		t.Fatalf("expected 'checkpoint does not support cross-size restore', got %q", reason)
	}
}

func TestCheckManifestCompatibilitySameSizeWithAllowChangeStillWorks(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	// No CrossSizeRestoreSupported needed when world sizes match.

	request := baseRequest()
	request.AllowWorldSizeChange = true

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected same-size manifest to be compatible even with AllowWorldSizeChange, got %q", reason)
	}
}

func TestCheckManifestCompatibilityWorldSizeMismatchWithoutAllowChange(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	manifest.CrossSizeRestoreSupported = ptr.To(true)

	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = false

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if compatible {
		t.Fatalf("expected manifest to be rejected when AllowWorldSizeChange is false")
	}
	if reason != "world size mismatch" {
		t.Fatalf("expected 'world size mismatch', got %q", reason)
	}
}

func TestCheckManifestCompatibilityPreservesStrictDimensionChecks(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*CheckpointManifest)
		wantMsg string
	}{
		{
			name:    "cluster identity mismatch",
			mutate:  func(m *CheckpointManifest) { m.ClusterIdentity = "other-cluster" },
			wantMsg: "cluster identity mismatch",
		},
		{
			name:    "RTJ identity mismatch",
			mutate:  func(m *CheckpointManifest) { m.RTJIdentity = "other-rtj" },
			wantMsg: "RTJ lineage identity mismatch",
		},
		{
			name:    "GPU shape mismatch",
			mutate:  func(m *CheckpointManifest) { m.GPUShape = "nvidia-h100" },
			wantMsg: "GPU shape mismatch",
		},
		{
			name:    "image identity mismatch",
			mutate:  func(m *CheckpointManifest) { m.ImageIdentity = "other:image" },
			wantMsg: "image identity mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)
			manifest.CrossSizeRestoreSupported = ptr.To(true)
			tt.mutate(&manifest)

			request := baseRequest()
			request.AllowWorldSizeChange = true

			compatible, reason := CheckManifestCompatibility(manifest, request)
			if compatible {
				t.Fatalf("expected rejection for %s", tt.name)
			}
			if reason != tt.wantMsg {
				t.Fatalf("expected reason %q, got %q", tt.wantMsg, reason)
			}
		})
	}
}

func baseRequest() ResumeRequest {
	return ResumeRequest{
		StorageRootURI:          "s3://bucket/demo/",
		ClusterIdentity:         "phase1-kind",
		RTJIdentity:             "demo-rtj",
		RuntimeMode:             "DDP",
		WorldSize:               2,
		GPUShape:                "cpu",
		ImageIdentity:           "local/fixture:dev",
		CodeVersionIdentity:     "git:abc123",
		OptimizerMode:           "adamw",
		ShardingMode:            "replicated-optimizer-state",
		SupportedFormatVersions: []string{SupportedManifestFormatVersion},
	}
}
