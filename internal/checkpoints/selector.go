package checkpoints

import "sort"

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
