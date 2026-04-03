package checkpoints

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

const SupportedManifestFormatVersion = "yield-sdk.manifest/v1alpha1"

type Artifact struct {
	Name            string `json:"name"`
	RelativePath    string `json:"relativePath"`
	ObjectURI       string `json:"objectURI"`
	SizeBytes       int64  `json:"sizeBytes"`
	DigestAlgorithm string `json:"digestAlgorithm"`
	DigestValue     string `json:"digestValue"`
}

type CheckpointManifest struct {
	CheckpointID        string     `json:"checkpointID"`
	ClusterIdentity     string     `json:"clusterIdentity"`
	RTJIdentity         string     `json:"rtjIdentity"`
	RunAttempt          int32      `json:"runAttempt"`
	GlobalStep          int64      `json:"globalStep"`
	WallClockTimestamp  string     `json:"wallClockTimestamp"`
	ImageIdentity       string     `json:"imageIdentity"`
	CodeVersionIdentity string     `json:"codeVersionIdentity"`
	RuntimeMode         string     `json:"runtimeMode"`
	WorldSize           int32      `json:"worldSize"`
	GPUShape            string     `json:"gpuShape"`
	OptimizerMode       string     `json:"optimizerMode"`
	ShardingMode        string     `json:"shardingMode"`
	FormatVersion       string     `json:"formatVersion"`
	ProducerVersion     string     `json:"producerVersion"`
	// Phase 3 fields: optional, nil for Phase 2 manifests.
	// CrossSizeRestoreSupported indicates whether this checkpoint can be
	// restored at a different world size via DCP resharding. nil is treated
	// as false (Phase 2 backward compatibility).
	CrossSizeRestoreSupported *bool `json:"crossSizeRestoreSupported,omitempty"`

	// Phase 8 fields: optional, empty for Phase 7 and earlier manifests.
	// DeviceProfileFingerprint is the SHA256 fingerprint of the DRA device
	// profile that was active when this checkpoint was saved. Empty when
	// the checkpoint was saved without DRA device spec (Phase 7 behavior).
	// Used for fail-closed checkpoint compatibility: a checkpoint saved
	// with one device profile cannot be restored under a different profile.
	DeviceProfileFingerprint string `json:"deviceProfileFingerprint,omitempty"`
	StorageRootURI      string     `json:"storageRootURI"`
	ManifestURI         string     `json:"manifestURI,omitempty"`
	Artifacts           []Artifact `json:"artifacts"`
	CompletionTimestamp string     `json:"completionTimestamp"`
}

func ResumeManifestURI(ref *v1alpha1.CheckpointReference) string {
	if ref == nil {
		return ""
	}
	return ref.ManifestURI
}

func YieldMarkerURI(storageRoot string, runAttempt int32) string {
	root := strings.TrimRight(storageRoot, "/")
	return fmt.Sprintf("%s/yield-markers/run-%d.json", root, runAttempt)
}

func ManifestPrefixURI(storageRoot string) string {
	root := strings.TrimRight(storageRoot, "/")
	return root + "/manifests/"
}

func DecodeManifest(rawBytes []byte, manifestURI string) (CheckpointManifest, error) {
	var manifest CheckpointManifest
	if err := json.Unmarshal(rawBytes, &manifest); err != nil {
		return CheckpointManifest{}, fmt.Errorf("decode checkpoint manifest %s: %w", manifestURI, err)
	}
	if manifest.ManifestURI == "" {
		manifest.ManifestURI = manifestURI
	}
	return manifest, nil
}

func (m CheckpointManifest) ValidateComplete() error {
	if strings.TrimSpace(m.CheckpointID) == "" {
		return fmt.Errorf("missing checkpointID")
	}
	if strings.TrimSpace(m.StorageRootURI) == "" {
		return fmt.Errorf("missing storageRootURI")
	}
	if strings.TrimSpace(m.CompletionTimestamp) == "" {
		return fmt.Errorf("missing completionTimestamp")
	}
	if _, err := m.CompletionTime(); err != nil {
		return fmt.Errorf("invalid completionTimestamp: %w", err)
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("missing artifacts")
	}
	for _, artifact := range m.Artifacts {
		if strings.TrimSpace(artifact.Name) == "" {
			return fmt.Errorf("artifact is missing name")
		}
		if strings.TrimSpace(artifact.RelativePath) == "" {
			return fmt.Errorf("artifact %s is missing relativePath", artifact.Name)
		}
		if strings.TrimSpace(artifact.ObjectURI) == "" {
			return fmt.Errorf("artifact %s is missing objectURI", artifact.Name)
		}
		if strings.TrimSpace(artifact.DigestAlgorithm) == "" {
			return fmt.Errorf("artifact %s is missing digestAlgorithm", artifact.Name)
		}
		if strings.TrimSpace(artifact.DigestValue) == "" {
			return fmt.Errorf("artifact %s is missing digestValue", artifact.Name)
		}
		if artifact.SizeBytes < 0 {
			return fmt.Errorf("artifact %s has negative size", artifact.Name)
		}
	}
	return nil
}

func (m CheckpointManifest) CompletionTime() (time.Time, error) {
	return time.Parse(time.RFC3339, m.CompletionTimestamp)
}

// CheckpointInfo is a lightweight summary of the latest checkpoint for
// telemetry purposes. It avoids the full compatibility check and artifact
// validation that SelectResumeCheckpoint performs.
type CheckpointInfo struct {
	CheckpointID        string
	GlobalStep          int64
	CompletionTimestamp metav1.Time
	ManifestURI         string
}

func (m CheckpointManifest) CheckpointReference(reason string) (v1alpha1.CheckpointReference, error) {
	completedAt, err := m.CompletionTime()
	if err != nil {
		return v1alpha1.CheckpointReference{}, err
	}
	return v1alpha1.CheckpointReference{
		ID:                  m.CheckpointID,
		StorageURI:          m.StorageRootURI,
		ManifestURI:         m.ManifestURI,
		CompletionTime:      &metav1.Time{Time: completedAt},
		SourceRunAttempt:    m.RunAttempt,
		CompatibilityState:  v1alpha1.CompatibilityStateCompatible,
		CompatibilityReason: reason,
		WorldSize:           m.WorldSize,
	}, nil
}
