package v1alpha1

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"sigs.k8s.io/kueue/pkg/controller/constants"
)

func TestResumableTrainingJobWebhookDefaultSetsKueueSuspendAndProjectedLabels(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Suspend = nil
	job.Spec.Control = nil
	job.Labels = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Control == nil {
		t.Fatalf("expected manual control to be defaulted")
	}
	if job.Spec.Control.DesiredState != DefaultDesiredState {
		t.Fatalf("expected desiredState %q, got %q", DefaultDesiredState, job.Spec.Control.DesiredState)
	}
	if !ptr.Deref(job.Spec.Suspend, false) {
		t.Fatalf("expected Kueue-facing suspend to default to true")
	}
	if got := job.Labels[constants.QueueLabel]; got != job.Spec.QueueName {
		t.Fatalf("expected queue label %q, got %q", job.Spec.QueueName, got)
	}
	if got := job.Labels[constants.WorkloadPriorityClassLabel]; got != job.Spec.WorkloadPriorityClassName {
		t.Fatalf("expected workload priority label %q, got %q", job.Spec.WorkloadPriorityClassName, got)
	}
}

func TestResumableTrainingJobWebhookDefaultPreservesManualPausedIntent(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Suspend = nil
	job.Spec.Control.DesiredState = DesiredStatePaused

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Control.DesiredState != DesiredStatePaused {
		t.Fatalf("expected desiredState to stay %q, got %q", DesiredStatePaused, job.Spec.Control.DesiredState)
	}
	if !ptr.Deref(job.Spec.Suspend, false) {
		t.Fatalf("expected Kueue-facing suspend to default to true even for manual pause")
	}
}

func TestResumableTrainingJobWebhookValidateUpdatePreservesKueueInvariants(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	t.Run("queue name immutable while unsuspended", func(t *testing.T) {
		oldJob := minimalValidRTJ()
		oldJob.Spec.Suspend = ptr.To(false)
		oldJob.Default()

		newJob := oldJob.DeepCopy()
		newJob.Spec.QueueName = "research-b"

		_, err := wh.ValidateUpdate(context.Background(), oldJob, newJob)
		if err == nil {
			t.Fatalf("expected queue-name update to fail while unsuspended")
		}
		if !strings.Contains(err.Error(), constants.QueueLabel) {
			t.Fatalf("expected queue-name validation error, got %v", err)
		}
	})

	t.Run("workload priority class remains mutable under pinned helper semantics", func(t *testing.T) {
		oldJob := minimalValidRTJ()
		oldJob.Spec.Suspend = ptr.To(false)
		oldJob.Default()

		newJob := oldJob.DeepCopy()
		newJob.Spec.WorkloadPriorityClassName = "batch-high"

		if _, err := wh.ValidateUpdate(context.Background(), oldJob, newJob); err != nil {
			t.Fatalf("expected workload priority class update to follow pinned helper behavior, got %v", err)
		}
	})

	t.Run("queue name mutable while suspended", func(t *testing.T) {
		oldJob := minimalValidRTJ()
		oldJob.Spec.Suspend = ptr.To(true)
		oldJob.Default()

		newJob := oldJob.DeepCopy()
		newJob.Spec.QueueName = "research-b"
		newJob.Spec.Suspend = ptr.To(true)

		if _, err := wh.ValidateUpdate(context.Background(), oldJob, newJob); err != nil {
			t.Fatalf("expected suspended queue-name update to succeed, got %v", err)
		}
	})
}

func TestResumableTrainingJobWebhookValidateCreatePreservesManualAPICompatibility(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Control.DesiredState = DesiredStatePaused
	job.Spec.Suspend = ptr.To(true)

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected create validation to preserve manual desiredState compatibility, got %v", err)
	}
}

// --- Phase 3 webhook tests ---

func TestWebhookDefaultPreservesPhase2SpecWithoutParallelism(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Parallelism = nil
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Phase 2 spec should pass without parallelism
	if job.Spec.Parallelism != nil {
		t.Fatalf("expected parallelism to remain nil for Phase 2 backward compat")
	}
	if job.Spec.Resume.AllowWorldSizeChange {
		t.Fatalf("expected allowWorldSizeChange to default to false")
	}
}

func TestWebhookValidateCreateAcceptsPhase3Parallelism(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected Phase 3 spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsPartialAdmissionWithoutAllowWorldSizeChange(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = false
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		EnablePartialAdmission: true,
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject partial admission without allowWorldSizeChange")
	}
	if !strings.Contains(err.Error(), "enablePartialAdmission") {
		t.Fatalf("expected error about enablePartialAdmission, got %v", err)
	}
}

func webhookTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add RTJ scheme: %v", err)
	}
	return scheme
}
