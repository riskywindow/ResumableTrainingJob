package dra

import (
	"fmt"
	"sort"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// RTJGroupVersionKind is the GVK for ResumableTrainingJob, used in
// owner references.
var RTJGroupVersionKind = schema.GroupVersionKind{
	Group:   "training.checkpoint.example.io",
	Version: "v1alpha1",
	Kind:    "ResumableTrainingJob",
}

// DesiredTemplate describes a ResourceClaimTemplate that should exist
// for a given RTJ claim. The reconciler uses this to create or compare
// with existing templates.
type DesiredTemplate struct {
	// Name is the Kubernetes object name: "<rtj-name>-<claim-name>".
	Name string

	// ClaimName is the DeviceClaimSpec.Name this template was derived from.
	ClaimName string

	// Template is the fully-constructed ResourceClaimTemplate object.
	Template *resourcev1beta1.ResourceClaimTemplate
}

// BuildDesiredTemplates constructs the set of ResourceClaimTemplate objects
// that should exist for the given RTJ. Returns nil when devices are not
// configured (nil spec or mode Disabled).
//
// Each DeviceClaimSpec produces one ResourceClaimTemplate with:
//   - Name: "<rtj-name>-<claim-name>"
//   - Namespace: same as the RTJ
//   - OwnerReference: RTJ with controller=true, blockOwnerDeletion=true
//   - Spec: a ResourceClaimTemplateSpec containing a DeviceClaim with
//     one DeviceRequest matching the claim's request fragment.
func BuildDesiredTemplates(
	rtj *trainingv1alpha1.ResumableTrainingJob,
) []DesiredTemplate {
	if rtj.Spec.Devices == nil || rtj.Spec.Devices.Mode != trainingv1alpha1.DeviceModeDRA {
		return nil
	}
	if len(rtj.Spec.Devices.Claims) == 0 {
		return nil
	}

	templates := make([]DesiredTemplate, 0, len(rtj.Spec.Devices.Claims))
	for _, claim := range rtj.Spec.Devices.Claims {
		name := TemplateNameForClaim(rtj.Name, claim.Name)
		tmpl := buildTemplate(rtj, name, claim)
		templates = append(templates, DesiredTemplate{
			Name:      name,
			ClaimName: claim.Name,
			Template:  tmpl,
		})
	}

	// Sort for determinism.
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].ClaimName < templates[j].ClaimName
	})
	return templates
}

// buildTemplate constructs a single ResourceClaimTemplate from a
// DeviceClaimSpec.
func buildTemplate(
	rtj *trainingv1alpha1.ResumableTrainingJob,
	name string,
	claim trainingv1alpha1.DeviceClaimSpec,
) *resourcev1beta1.ResourceClaimTemplate {
	// Build DRA selectors from CEL expression strings.
	selectors := make([]resourcev1beta1.DeviceSelector, len(claim.Request.Selectors))
	for i, expr := range claim.Request.Selectors {
		selectors[i] = resourcev1beta1.DeviceSelector{
			CEL: &resourcev1beta1.CELDeviceSelector{
				Expression: expr,
			},
		}
	}

	blockOwnerDeletion := true
	isController := true

	return &resourcev1beta1.ResourceClaimTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "resource.k8s.io/v1beta1",
			Kind:       "ResourceClaimTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rtj.Namespace,
			Labels: map[string]string{
				"training.checkpoint.example.io/rtj-name":    rtj.Name,
				"training.checkpoint.example.io/claim-name":  claim.Name,
				"training.checkpoint.example.io/managed-by":  "rtj-operator",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         RTJGroupVersionKind.GroupVersion().String(),
					Kind:               RTJGroupVersionKind.Kind,
					Name:               rtj.Name,
					UID:                rtj.UID,
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
		Spec: resourcev1beta1.ResourceClaimTemplateSpec{
			Spec: resourcev1beta1.ResourceClaimSpec{
				Devices: resourcev1beta1.DeviceClaim{
					Requests: []resourcev1beta1.DeviceRequest{
						{
							Name:            fmt.Sprintf("%s-req", claim.Name),
							DeviceClassName: claim.Request.DeviceClassName,
							Selectors:       selectors,
							AllocationMode:  resourcev1beta1.DeviceAllocationModeExactCount,
							Count:           int64(claim.Request.Count),
						},
					},
				},
			},
		},
	}
}

// TemplateSpecMatches returns true when an existing ResourceClaimTemplate's
// device request spec matches the desired template's spec. This is used
// to detect spec drift and determine whether a template needs to be
// recreated.
//
// Only the device request fields that the RTJ operator controls are
// compared: DeviceClassName, Count, AllocationMode, and Selectors.
func TemplateSpecMatches(existing, desired *resourcev1beta1.ResourceClaimTemplate) bool {
	existingReqs := existing.Spec.Spec.Devices.Requests
	desiredReqs := desired.Spec.Spec.Devices.Requests

	if len(existingReqs) != len(desiredReqs) {
		return false
	}

	for i := range existingReqs {
		e := &existingReqs[i]
		d := &desiredReqs[i]

		if e.Name != d.Name {
			return false
		}
		if e.DeviceClassName != d.DeviceClassName {
			return false
		}
		if e.Count != d.Count {
			return false
		}
		if e.AllocationMode != d.AllocationMode {
			return false
		}
		if !selectorsEqual(e.Selectors, d.Selectors) {
			return false
		}
	}
	return true
}

func selectorsEqual(a, b []resourcev1beta1.DeviceSelector) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		aCEL := ""
		bCEL := ""
		if a[i].CEL != nil {
			aCEL = a[i].CEL.Expression
		}
		if b[i].CEL != nil {
			bCEL = b[i].CEL.Expression
		}
		if aCEL != bCEL {
			return false
		}
	}
	return true
}

// TemplateKey returns the namespaced name for a desired template.
func TemplateKey(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}
