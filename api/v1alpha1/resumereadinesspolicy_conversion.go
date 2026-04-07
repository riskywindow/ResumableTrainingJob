package v1alpha1

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1beta1 "github.com/example/checkpoint-native-preemption-controller/api/v1beta1"
)

var _ conversion.Convertible = &ResumeReadinessPolicy{}

// ConvertTo converts this v1alpha1 ResumeReadinessPolicy to the hub version (v1beta1).
func (src *ResumeReadinessPolicy) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v1beta1.ResumeReadinessPolicy)
	if !ok {
		return fmt.Errorf("expected *v1beta1.ResumeReadinessPolicy, got %T", dstRaw)
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

// ConvertFrom converts from the hub version (v1beta1) to this v1alpha1 ResumeReadinessPolicy.
func (dst *ResumeReadinessPolicy) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v1beta1.ResumeReadinessPolicy)
	if !ok {
		return fmt.Errorf("expected *v1beta1.ResumeReadinessPolicy, got %T", srcRaw)
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
