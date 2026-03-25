package jobset

import (
	"encoding/json"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type Spec struct {
	ReplicatedJobs []ReplicatedJob `json:"replicatedJobs"`
}

type ReplicatedJob struct {
	Name     string                  `json:"name"`
	Replicas *int32                  `json:"replicas,omitempty"`
	Template batchv1.JobTemplateSpec `json:"template"`
}

type Object struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   metav1.ObjectMeta `json:"metadata"`
	Spec       Spec              `json:"spec"`
}

func ParseTemplate(raw runtime.RawExtension) (Spec, error) {
	var spec Spec
	if len(raw.Raw) == 0 {
		return spec, fmt.Errorf("embedded JobSet spec is empty")
	}
	if err := json.Unmarshal(raw.Raw, &spec); err != nil {
		return spec, fmt.Errorf("decode embedded JobSet spec: %w", err)
	}
	if len(spec.ReplicatedJobs) == 0 {
		return spec, fmt.Errorf("embedded JobSet spec must contain at least one replicated job")
	}
	return spec, nil
}

func ToUnstructured(obj Object) (*unstructured.Unstructured, error) {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
	if err != nil {
		return nil, fmt.Errorf("convert rendered JobSet to unstructured: %w", err)
	}
	rendered := &unstructured.Unstructured{Object: raw}
	rendered.SetAPIVersion(obj.APIVersion)
	rendered.SetKind(obj.Kind)
	return rendered, nil
}

func FromUnstructured(obj *unstructured.Unstructured) (Object, error) {
	var decoded Object
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &decoded); err != nil {
		return decoded, fmt.Errorf("decode rendered JobSet: %w", err)
	}
	return decoded, nil
}

func NewEmptyChildJobSet(apiVersion, kind string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	return obj
}

func podSpec(repJob *ReplicatedJob) *corev1.PodSpec {
	return &repJob.Template.Spec.Template.Spec
}
