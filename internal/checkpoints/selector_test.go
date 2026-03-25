package checkpoints

import (
	"testing"

	"k8s.io/utils/ptr"
)

func TestSelectLatestCompatibleSkipsNewerIncompatibleManifest(t *testing.T) {
	request := baseRequest()

	newer := testManifest("s3://bucket/demo/manifests/ckpt-3.manifest.json", "ckpt-3", 3, 12)
	newer.WorldSize = 4
	older := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 8)
	newer.CompletionTimestamp = "2026-03-21T12:00:20Z"
	older.CompletionTimestamp = "2026-03-21T12:00:10Z"

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{older, newer}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if !ok || selection == nil {
		t.Fatalf("expected a compatible checkpoint to be selected")
	}
	if selection.CheckpointID != "ckpt-2" {
		t.Fatalf("expected older compatible checkpoint ckpt-2, got %s", selection.CheckpointID)
	}
}

func TestSelectLatestCompatibleRejectsIncompleteManifest(t *testing.T) {
	request := baseRequest()

	incomplete := testManifest("s3://bucket/demo/manifests/ckpt-3.manifest.json", "ckpt-3", 3, 12)
	incomplete.CompletionTimestamp = ""

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{incomplete}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if ok || selection != nil {
		t.Fatalf("expected incomplete manifest to be rejected")
	}
}

func TestSelectLatestCompatiblePrefersCompletedTimestampOverInvalidTimestamp(t *testing.T) {
	request := baseRequest()

	invalidTimestamp := testManifest("s3://bucket/demo/manifests/ckpt-bad.manifest.json", "ckpt-bad", 3, 12)
	invalidTimestamp.CompletionTimestamp = "not-a-time"
	validOlder := testManifest("s3://bucket/demo/manifests/ckpt-good.manifest.json", "ckpt-good", 2, 8)
	validOlder.CompletionTimestamp = "2026-03-21T12:00:10Z"

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{invalidTimestamp, validOlder}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if !ok || selection == nil {
		t.Fatalf("expected a compatible checkpoint to be selected")
	}
	if selection.CheckpointID != "ckpt-good" {
		t.Fatalf("expected checkpoint with valid completion timestamp to win, got %s", selection.CheckpointID)
	}
}

func TestSelectLatestCompatibleDifferentSizeWithAllowance(t *testing.T) {
	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	// Checkpoint was saved at world size 2, but request is world size 4.
	// AllowWorldSizeChange + CrossSizeRestoreSupported should make it compatible.
	ckpt := testManifest("s3://bucket/demo/manifests/ckpt-1.manifest.json", "ckpt-1", 1, 100)
	ckpt.WorldSize = 2
	ckpt.CrossSizeRestoreSupported = ptr.To(true)
	ckpt.CompletionTimestamp = "2026-03-21T12:00:10Z"

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{ckpt}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if !ok || selection == nil {
		t.Fatalf("expected checkpoint to be selected for different-size resume")
	}
	if selection.CheckpointID != "ckpt-1" {
		t.Fatalf("expected ckpt-1, got %s", selection.CheckpointID)
	}
	if selection.WorldSize != 2 {
		t.Fatalf("expected selected checkpoint world size 2, got %d", selection.WorldSize)
	}
}

func TestSelectLatestCompatibleDifferentSizeRejectsWithoutCrossSizeSupport(t *testing.T) {
	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	// Checkpoint does not declare cross-size restore support (Phase 2 manifest).
	ckpt := testManifest("s3://bucket/demo/manifests/ckpt-1.manifest.json", "ckpt-1", 1, 100)
	ckpt.WorldSize = 2
	ckpt.CompletionTimestamp = "2026-03-21T12:00:10Z"

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{ckpt}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if ok || selection != nil {
		t.Fatalf("expected checkpoint to be rejected when it does not support cross-size restore")
	}
}

func TestSelectLatestCompatiblePrefersLatestAmongMultipleCrossSizeCompatible(t *testing.T) {
	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	older := testManifest("s3://bucket/demo/manifests/ckpt-1.manifest.json", "ckpt-1", 1, 50)
	older.WorldSize = 8
	older.CrossSizeRestoreSupported = ptr.To(true)
	older.CompletionTimestamp = "2026-03-21T12:00:10Z"

	newer := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 100)
	newer.WorldSize = 2
	newer.CrossSizeRestoreSupported = ptr.To(true)
	newer.CompletionTimestamp = "2026-03-21T12:00:20Z"

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{older, newer}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if !ok || selection == nil {
		t.Fatalf("expected a checkpoint to be selected")
	}
	if selection.CheckpointID != "ckpt-2" {
		t.Fatalf("expected latest compatible ckpt-2, got %s", selection.CheckpointID)
	}
}

func TestSelectLatestCompatibleSkipsCrossSizeIncompatibleAndPicksOlder(t *testing.T) {
	request := baseRequest()
	request.WorldSize = 4
	request.AllowWorldSizeChange = true

	// Newer checkpoint does NOT support cross-size restore.
	newer := testManifest("s3://bucket/demo/manifests/ckpt-2.manifest.json", "ckpt-2", 2, 100)
	newer.WorldSize = 8
	newer.CompletionTimestamp = "2026-03-21T12:00:20Z"
	// CrossSizeRestoreSupported is nil (Phase 2 manifest)

	// Older checkpoint supports cross-size restore.
	older := testManifest("s3://bucket/demo/manifests/ckpt-1.manifest.json", "ckpt-1", 1, 50)
	older.WorldSize = 2
	older.CrossSizeRestoreSupported = ptr.To(true)
	older.CompletionTimestamp = "2026-03-21T12:00:10Z"

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{older, newer}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if !ok || selection == nil {
		t.Fatalf("expected a checkpoint to be selected")
	}
	if selection.CheckpointID != "ckpt-1" {
		t.Fatalf("expected older cross-size-compatible ckpt-1, got %s", selection.CheckpointID)
	}
}

func TestSelectLatestCompatibleSameSizeStillSelectedWithoutCrossSizeField(t *testing.T) {
	request := baseRequest()
	// Request world size matches manifest world size (2 == 2).

	ckpt := testManifest("s3://bucket/demo/manifests/ckpt-1.manifest.json", "ckpt-1", 1, 100)
	ckpt.CompletionTimestamp = "2026-03-21T12:00:10Z"
	// No CrossSizeRestoreSupported field - Phase 2 manifest.

	selection, ok, err := SelectLatestCompatible([]CheckpointManifest{ckpt}, request)
	if err != nil {
		t.Fatalf("select latest compatible: %v", err)
	}
	if !ok || selection == nil {
		t.Fatalf("expected same-size checkpoint to be selected even without CrossSizeRestoreSupported")
	}
}

func TestSelectLatestCompatibleEmptyCandidates(t *testing.T) {
	request := baseRequest()

	selection, ok, err := SelectLatestCompatible(nil, request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || selection != nil {
		t.Fatalf("expected no selection for empty candidates")
	}
}

func testManifest(manifestURI, checkpointID string, runAttempt int32, globalStep int64) CheckpointManifest {
	return CheckpointManifest{
		CheckpointID:        checkpointID,
		ClusterIdentity:     "phase1-kind",
		RTJIdentity:         "demo-rtj",
		RunAttempt:          runAttempt,
		GlobalStep:          globalStep,
		WallClockTimestamp:  "2026-03-21T12:00:00Z",
		ImageIdentity:       "local/fixture:dev",
		CodeVersionIdentity: "git:abc123",
		RuntimeMode:         "DDP",
		WorldSize:           2,
		GPUShape:            "cpu",
		OptimizerMode:       "adamw",
		ShardingMode:        "replicated-optimizer-state",
		FormatVersion:       SupportedManifestFormatVersion,
		ProducerVersion:     "0.1.0",
		StorageRootURI:      "s3://bucket/demo/checkpoints/" + checkpointID,
		ManifestURI:         manifestURI,
		Artifacts: []Artifact{
			{
				Name:            "metadata-runtime.json",
				RelativePath:    "metadata/runtime.json",
				ObjectURI:       "s3://bucket/demo/checkpoints/" + checkpointID + "/metadata/runtime.json",
				SizeBytes:       128,
				DigestAlgorithm: "sha256",
				DigestValue:     "aabbcc",
			},
		},
		CompletionTimestamp: "2026-03-21T12:00:10Z",
	}
}
