package v1alpha1

import (
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1beta1 "github.com/example/checkpoint-native-preemption-controller/api/v1beta1"
)

var _ conversion.Convertible = &ResumableTrainingJob{}

// ConvertTo converts this v1alpha1 ResumableTrainingJob to the hub version (v1beta1).
//
// The v1alpha1 and v1beta1 schemas are structurally identical at this point
// in the graduation (Phase 10). We use JSON marshaling for a reliable,
// maintenance-free field copy that is immune to new fields being added to
// both versions simultaneously. The conversion webhook is not in the
// request hot-path, so the marshaling overhead is acceptable.
func (src *ResumableTrainingJob) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v1beta1.ResumableTrainingJob)
	if !ok {
		return fmt.Errorf("expected *v1beta1.ResumableTrainingJob, got %T", dstRaw)
	}

	// ObjectMeta is version-independent; copy directly.
	dst.ObjectMeta = src.ObjectMeta

	// Spec: JSON roundtrip between identically-tagged structs.
	if err := jsonRoundTrip(&src.Spec, &dst.Spec); err != nil {
		return fmt.Errorf("converting spec: %w", err)
	}

	// Status: JSON roundtrip.
	if err := jsonRoundTrip(&src.Status, &dst.Status); err != nil {
		return fmt.Errorf("converting status: %w", err)
	}

	return nil
}

// ConvertFrom converts from the hub version (v1beta1) to this v1alpha1 ResumableTrainingJob.
func (dst *ResumableTrainingJob) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v1beta1.ResumableTrainingJob)
	if !ok {
		return fmt.Errorf("expected *v1beta1.ResumableTrainingJob, got %T", srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	if err := jsonRoundTrip(&src.Spec, &dst.Spec); err != nil {
		return fmt.Errorf("converting spec: %w", err)
	}

	if err := jsonRoundTrip(&src.Status, &dst.Status); err != nil {
		return fmt.Errorf("converting status: %w", err)
	}

	return nil
}

// jsonRoundTrip marshals src to JSON and unmarshals into dst.
// Both types must have identical JSON tags for lossless conversion.
func jsonRoundTrip(src, dst interface{}) error {
	data, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}
