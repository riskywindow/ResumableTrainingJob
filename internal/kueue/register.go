package kueue

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

const (
	ExternalFrameworkName = "ResumableTrainingJob.v1alpha1.training.checkpoint.example.io"
)

var GroupVersionKind = trainingv1alpha1.GroupVersion.WithKind("ResumableTrainingJob")

func RegisterExternalFramework() error {
	return jobframework.RegisterExternalJobType(ExternalFrameworkName)
}

func WorkloadNameForJob(jobName string, jobUID types.UID) string {
	return jobframework.GetWorkloadNameForOwnerWithGVK(jobName, jobUID, GroupVersionKind)
}

func WorkloadNameForObject(job *trainingv1alpha1.ResumableTrainingJob) string {
	if job == nil {
		return ""
	}
	return WorkloadNameForJob(job.Name, job.UID)
}

func ExternalFrameworks() []string {
	return []string{ExternalFrameworkName}
}

func KindArgForGVK(gvk schema.GroupVersionKind) string {
	return fmt.Sprintf("%s.%s.%s", gvk.Kind, gvk.Version, gvk.Group)
}
