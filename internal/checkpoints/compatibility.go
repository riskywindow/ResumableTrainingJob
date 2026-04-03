package checkpoints

import (
	"fmt"
	"strings"
	"time"
)

type ResumeRequest struct {
	StorageRootURI          string
	ClusterIdentity         string
	RTJIdentity             string
	RuntimeMode             string
	WorldSize               int32
	GPUShape                string
	ImageIdentity           string
	CodeVersionIdentity     string
	OptimizerMode           string
	ShardingMode            string
	SupportedFormatVersions []string

	// AllowWorldSizeChange permits resuming from a checkpoint saved at a
	// different world size. When true, the world-size equality check is
	// skipped, but the manifest must declare CrossSizeRestoreSupported=true.
	// All other compatibility dimensions remain strict.
	// Phase 3 addition; default false preserves Phase 2 behavior.
	AllowWorldSizeChange bool

	// CurrentDeviceProfileFingerprint is the SHA256 fingerprint of the
	// current DRA device profile. Empty when DRA is not configured.
	// Phase 8 addition; empty preserves Phase 7 behavior.
	CurrentDeviceProfileFingerprint string
}

func CheckManifestCompatibility(manifest CheckpointManifest, request ResumeRequest) (bool, string) {
	if err := manifest.ValidateComplete(); err != nil {
		return false, fmt.Sprintf("incomplete manifest: %v", err)
	}
	if strings.TrimSpace(manifest.ClusterIdentity) == "" || manifest.ClusterIdentity != request.ClusterIdentity {
		return false, "cluster identity mismatch"
	}
	if strings.TrimSpace(manifest.RTJIdentity) == "" || manifest.RTJIdentity != request.RTJIdentity {
		return false, "RTJ lineage identity mismatch"
	}
	if strings.TrimSpace(manifest.RuntimeMode) == "" || manifest.RuntimeMode != request.RuntimeMode {
		return false, "runtime mode mismatch"
	}

	// Phase 3: world-size check with flexible resume support.
	if manifest.WorldSize != request.WorldSize {
		if !request.AllowWorldSizeChange {
			return false, "world size mismatch"
		}
		// AllowWorldSizeChange is true but the manifest must declare cross-size restore support.
		if manifest.CrossSizeRestoreSupported == nil || !*manifest.CrossSizeRestoreSupported {
			return false, "checkpoint does not support cross-size restore"
		}
	}

	if strings.TrimSpace(manifest.GPUShape) == "" || manifest.GPUShape != request.GPUShape {
		return false, "GPU shape mismatch"
	}
	if strings.TrimSpace(manifest.ImageIdentity) == "" || manifest.ImageIdentity != request.ImageIdentity {
		return false, "image identity mismatch"
	}
	if strings.TrimSpace(manifest.CodeVersionIdentity) == "" || manifest.CodeVersionIdentity != request.CodeVersionIdentity {
		return false, "code version identity mismatch"
	}
	if !isSupportedFormatVersion(manifest.FormatVersion, request.SupportedFormatVersions) {
		return false, "checkpoint format version mismatch"
	}
	if strings.TrimSpace(manifest.OptimizerMode) == "" || manifest.OptimizerMode != request.OptimizerMode {
		return false, "optimizer mode mismatch"
	}
	if strings.TrimSpace(manifest.ShardingMode) == "" || manifest.ShardingMode != request.ShardingMode {
		return false, "sharding mode mismatch"
	}

	// Phase 8: device profile fingerprint check (fail-closed).
	// When the current request has a device profile fingerprint, the
	// checkpoint must have been saved with the same device profile.
	// When neither has a fingerprint (Phase 7 or earlier), the check
	// is skipped (backward compatible).
	// When only one side has a fingerprint:
	//   - Checkpoint has fingerprint, request doesn't: compatible
	//     (the user has downgraded from DRA to non-DRA, checkpoint
	//     is still usable).
	//   - Checkpoint lacks fingerprint, request has one: incompatible
	//     (the checkpoint was saved without DRA; resuming under a
	//     specific device profile is not safe).
	if request.CurrentDeviceProfileFingerprint != "" {
		if manifest.DeviceProfileFingerprint == "" {
			return false, "checkpoint was saved without device profile; cannot resume under DRA device profile (fail-closed)"
		}
		if manifest.DeviceProfileFingerprint != request.CurrentDeviceProfileFingerprint {
			return false, "device profile fingerprint mismatch (fail-closed)"
		}
	}

	if manifest.WorldSize == request.WorldSize {
		matchMsg := "exact match on lineage, mode, cluster, shape, image, code version, world size, optimizer mode, sharding mode, and manifest format"
		if request.CurrentDeviceProfileFingerprint != "" {
			matchMsg += ", device profile"
		}
		return true, matchMsg
	}
	return true, fmt.Sprintf("compatible with world-size change (%d -> %d); all other dimensions match", manifest.WorldSize, request.WorldSize)
}

// IsCheckpointTooOld returns true if the checkpoint's completion time is
// older than maxAge relative to now. Returns false if maxAge is zero (no limit)
// or if the checkpoint has no parseable completion time.
func IsCheckpointTooOld(manifest CheckpointManifest, maxAge time.Duration, now time.Time) (bool, string) {
	if maxAge <= 0 {
		return false, ""
	}
	completionTime, err := manifest.CompletionTime()
	if err != nil {
		return false, fmt.Sprintf("cannot parse checkpoint completion time: %v", err)
	}
	age := now.Sub(completionTime)
	if age > maxAge {
		return true, fmt.Sprintf("checkpoint age %s exceeds maxCheckpointAge %s", age.Round(time.Second), maxAge)
	}
	return false, ""
}

func isSupportedFormatVersion(version string, supported []string) bool {
	if strings.TrimSpace(version) == "" {
		return false
	}
	if len(supported) == 0 {
		supported = []string{SupportedManifestFormatVersion}
	}
	for _, candidate := range supported {
		if version == candidate {
			return true
		}
	}
	return false
}
