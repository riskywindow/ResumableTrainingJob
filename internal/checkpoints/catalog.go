package checkpoints

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	operatormetrics "github.com/example/checkpoint-native-preemption-controller/internal/metrics"
)

type Catalog interface {
	ObservePause(ctx context.Context, storageRoot string, runAttempt int32, requestID string, requestedAt time.Time) (*PauseObservation, bool, error)
	SelectResumeCheckpoint(ctx context.Context, request ResumeRequest) (*v1alpha1.CheckpointReference, bool, error)
}

type PauseObservation struct {
	MarkerURI   string
	Checkpoint  v1alpha1.CheckpointReference
	RequestID   string
	GlobalStep  int64
	CompletedAt metav1.Time
}

type YieldMarker struct {
	CheckpointID        string `json:"checkpointID"`
	ManifestURI         string `json:"manifestURI"`
	RequestID           string `json:"requestID"`
	CompletionTimestamp string `json:"completionTimestamp"`
	GlobalStep          int64  `json:"globalStep"`
}

type ObjectStoreCatalog struct {
	store   ObjectStore
	metrics *operatormetrics.Recorder
}

type NoopCatalog struct{}

func NewCatalogFromEnv(metrics *operatormetrics.Recorder) (Catalog, error) {
	store, err := NewStoreFromEnv()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return &NoopCatalog{}, nil
	}
	return &ObjectStoreCatalog{store: store, metrics: metrics}, nil
}

func NewCatalog(store ObjectStore, metrics *operatormetrics.Recorder) Catalog {
	if store == nil {
		return &NoopCatalog{}
	}
	return &ObjectStoreCatalog{store: store, metrics: metrics}
}

func (c *ObjectStoreCatalog) ObservePause(
	ctx context.Context,
	storageRoot string,
	runAttempt int32,
	requestID string,
	requestedAt time.Time,
) (*PauseObservation, bool, error) {
	markerURI := YieldMarkerURI(storageRoot, runAttempt)
	marker, err := c.readYieldMarker(ctx, markerURI)
	if err != nil {
		if isNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if marker.RequestID != requestID {
		return nil, false, nil
	}

	completedAt, err := time.Parse(time.RFC3339, marker.CompletionTimestamp)
	if err != nil || !completedAt.After(requestedAt) {
		return nil, false, nil
	}

	manifest, err := c.readManifest(ctx, marker.ManifestURI)
	if err != nil {
		if isNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err := c.validateManifestArtifacts(ctx, manifest); err != nil {
		if isNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	manifestCompletedAt, err := manifest.CompletionTime()
	if err != nil || !manifestCompletedAt.After(requestedAt) {
		return nil, false, nil
	}

	ref, err := manifest.CheckpointReference("pause flow accepted a complete checkpoint manifest for the current drain request")
	if err != nil {
		return nil, false, err
	}
	return &PauseObservation{
		MarkerURI:   markerURI,
		Checkpoint:  ref,
		RequestID:   marker.RequestID,
		GlobalStep:  marker.GlobalStep,
		CompletedAt: metav1.Time{Time: manifestCompletedAt},
	}, true, nil
}

func (c *ObjectStoreCatalog) SelectResumeCheckpoint(
	ctx context.Context,
	request ResumeRequest,
) (*v1alpha1.CheckpointReference, bool, error) {
	manifestURIs, err := c.store.ListObjects(ctx, ManifestPrefixURI(request.StorageRootURI))
	if err != nil {
		return nil, false, err
	}
	c.metrics.AddCheckpointsDiscovered(len(manifestURIs))

	candidates := make([]CheckpointManifest, 0, len(manifestURIs))
	for _, manifestURI := range manifestURIs {
		manifest, ok := c.tryLoadCandidate(ctx, manifestURI)
		if ok {
			candidates = append(candidates, manifest)
		}
	}

	selectedManifest, ok, err := SelectLatestCompatible(candidates, request)
	if err != nil || !ok || selectedManifest == nil {
		return nil, ok, err
	}

	ref, err := selectedManifest.CheckpointReference("latest compatible complete checkpoint selected for resume")
	if err != nil {
		return nil, false, err
	}
	return &ref, true, nil
}

func (c *ObjectStoreCatalog) tryLoadCandidate(ctx context.Context, manifestURI string) (CheckpointManifest, bool) {
	manifest, err := c.readManifest(ctx, manifestURI)
	if err != nil {
		return CheckpointManifest{}, false
	}
	if err := manifest.ValidateComplete(); err != nil {
		return CheckpointManifest{}, false
	}
	if err := c.validateManifestArtifacts(ctx, manifest); err != nil {
		return CheckpointManifest{}, false
	}
	return manifest, true
}

func (c *ObjectStoreCatalog) readYieldMarker(ctx context.Context, markerURI string) (*YieldMarker, error) {
	rawBytes, err := c.store.ReadObject(ctx, markerURI)
	if err != nil {
		return nil, err
	}

	var marker YieldMarker
	if err := json.Unmarshal(rawBytes, &marker); err != nil {
		return nil, fmt.Errorf("decode yield marker %s: %w", markerURI, err)
	}
	if marker.CheckpointID == "" || marker.ManifestURI == "" || marker.RequestID == "" || marker.CompletionTimestamp == "" {
		return nil, fmt.Errorf("yield marker %s is incomplete", markerURI)
	}
	return &marker, nil
}

func (c *ObjectStoreCatalog) readManifest(ctx context.Context, manifestURI string) (CheckpointManifest, error) {
	rawBytes, err := c.store.ReadObject(ctx, manifestURI)
	if err != nil {
		return CheckpointManifest{}, err
	}
	return DecodeManifest(rawBytes, manifestURI)
}

func (c *ObjectStoreCatalog) validateManifestArtifacts(ctx context.Context, manifest CheckpointManifest) error {
	if err := manifest.ValidateComplete(); err != nil {
		return err
	}
	for _, artifact := range manifest.Artifacts {
		if err := c.store.StatObject(ctx, artifact.ObjectURI); err != nil {
			return err
		}
	}
	return nil
}

func (c *NoopCatalog) ObservePause(context.Context, string, int32, string, time.Time) (*PauseObservation, bool, error) {
	return nil, false, nil
}

func (c *NoopCatalog) SelectResumeCheckpoint(context.Context, ResumeRequest) (*v1alpha1.CheckpointReference, bool, error) {
	return nil, false, nil
}
