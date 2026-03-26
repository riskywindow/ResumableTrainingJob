package checkpoints

import (
	"sort"

	v1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

func SelectLatestCompatible(candidates []CheckpointManifest, request ResumeRequest) (*CheckpointManifest, bool, error) {
	if len(candidates) == 0 {
		return nil, false, nil
	}

	sorted := append([]CheckpointManifest(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, leftErr := sorted[i].CompletionTime()
		right, rightErr := sorted[j].CompletionTime()
		switch {
		case leftErr == nil && rightErr != nil:
			return true
		case leftErr != nil && rightErr == nil:
			return false
		case leftErr != nil && rightErr != nil:
			return sorted[i].ManifestURI > sorted[j].ManifestURI
		}
		if left.Equal(right) {
			return sorted[i].ManifestURI > sorted[j].ManifestURI
		}
		return left.After(right)
	})

	for _, candidate := range sorted {
		compatible, _ := CheckManifestCompatibility(candidate, request)
		if compatible {
			return &candidate, true, nil
		}
	}
	return nil, false, nil
}

// ResumeRequestFromRTJ builds a ResumeRequest from an RTJ's spec fields.
// The world size is taken from spec.identity.worldSize (the canonical identity
// dimension). Pre-admission, this is the best available approximation; with
// partial admission the actual admitted world size may differ, and the operator
// performs final validation at launch time.
func ResumeRequestFromRTJ(rtj *v1alpha1.ResumableTrainingJob, clusterIdentity string) ResumeRequest {
	return ResumeRequest{
		StorageRootURI:          rtj.Spec.Checkpoint.StorageURI,
		ClusterIdentity:         clusterIdentity,
		RTJIdentity:             rtj.Namespace + "/" + rtj.Name,
		RuntimeMode:             string(rtj.Spec.Runtime.Mode),
		WorldSize:               rtj.Spec.Identity.WorldSize,
		GPUShape:                rtj.Spec.Identity.GPUShape,
		ImageIdentity:           rtj.Spec.Identity.Image,
		CodeVersionIdentity:     rtj.Spec.Identity.CodeVersion,
		OptimizerMode:           rtj.Spec.Runtime.OptimizerMode,
		ShardingMode:            rtj.Spec.Runtime.ShardingMode,
		SupportedFormatVersions: []string{SupportedManifestFormatVersion},
		AllowWorldSizeChange:    rtj.Spec.Resume.AllowWorldSizeChange,
	}
}
