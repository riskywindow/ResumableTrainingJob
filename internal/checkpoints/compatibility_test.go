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

// --- Phase 8: device profile compatibility tests ---

func TestCheckManifestCompatibilitySameDeviceProfile(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)
	manifest.DeviceProfileFingerprint = "abc123def456"

	request := baseRequest()
	request.CurrentDeviceProfileFingerprint = "abc123def456"

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected same device profile to be compatible, got reason %q", reason)
	}
	if !strings.Contains(reason, "device profile") {
		t.Fatalf("expected reason to mention device profile, got %q", reason)
	}
}

func TestCheckManifestCompatibilityDifferentDeviceProfileRejected(t *testing.T) {
	manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)
	manifest.DeviceProfileFingerprint = "abc123def456"

	request := baseRequest()
	request.CurrentDeviceProfileFingerprint = "different789xyz"

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if compatible {
		t.Fatal("expected different device profile to be incompatible")
	}
	if !strings.Contains(reason, "device profile fingerprint mismatch") {
		t.Fatalf("expected 'device profile fingerprint mismatch', got %q", reason)
	}
}

func TestCheckManifestCompatibilityCheckpointWithoutProfileRequestWithProfile(t *testing.T) {
	// Checkpoint saved without DRA (Phase 7), request has DRA enabled.
	// Should be rejected (fail-closed).
	manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)
	// DeviceProfileFingerprint is empty (Phase 7 manifest)

	request := baseRequest()
	request.CurrentDeviceProfileFingerprint = "abc123def456"

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if compatible {
		t.Fatal("expected checkpoint without device profile to be rejected when request has DRA")
	}
	if !strings.Contains(reason, "saved without device profile") {
		t.Fatalf("expected 'saved without device profile' message, got %q", reason)
	}
}

func TestCheckManifestCompatibilityCheckpointWithProfileRequestWithout(t *testing.T) {
	// Checkpoint saved with DRA, request has no DRA (downgrade case).
	// Should be compatible (backward-compatible: request without DRA
	// does not enforce device profile check).
	manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)
	manifest.DeviceProfileFingerprint = "abc123def456"

	request := baseRequest()
	// CurrentDeviceProfileFingerprint is empty (no DRA)

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected checkpoint with device profile to be compatible when request has no DRA, got reason %q", reason)
	}
}

func TestCheckManifestCompatibilityBothWithoutDeviceProfile(t *testing.T) {
	// Neither has device profile (Phase 7 behavior preserved).
	manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)

	request := baseRequest()

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected Phase 7 manifest without device profiles to be compatible, got reason %q", reason)
	}
}

func TestCheckManifestCompatibilityDeviceProfileWithWorldSizeChange(t *testing.T) {
	// Same device profile with world size change should be compatible.
	manifest := testManifest("s3://bucket/demo/manifests/ckpt.manifest.json", "ckpt", 1, 10)
	manifest.DeviceProfileFingerprint = "abc123def456"
	manifest.CrossSizeRestoreSupported = ptr.To(true)

	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true
	request.CurrentDeviceProfileFingerprint = "abc123def456"

	compatible, reason := CheckManifestCompatibility(manifest, request)
	if !compatible {
		t.Fatalf("expected same device profile with world-size change to be compatible, got reason %q", reason)
	}
	if !strings.Contains(reason, "world-size change") {
		t.Fatalf("expected reason to mention world-size change, got %q", reason)
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
