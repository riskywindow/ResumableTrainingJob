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

// --- Phase 4 webhook tests: topology ---

func TestWebhookDefaultPreservesPhase3SpecWithoutTopology(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = nil
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Phase 3 spec should pass without topology
	if job.Spec.Topology != nil {
		t.Fatalf("expected topology to remain nil for Phase 3 backward compat")
	}
}

func TestWebhookDefaultSetsTopologyModeWhenEmpty(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{} // mode empty
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Topology.Mode != DefaultTopologyMode {
		t.Fatalf("expected topology mode to default to %q, got %q", DefaultTopologyMode, job.Spec.Topology.Mode)
	}
}

func TestWebhookDefaultPreservesExplicitTopologyMode(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Topology.Mode != TopologyModeRequired {
		t.Fatalf("expected topology mode to stay %q, got %q", TopologyModeRequired, job.Spec.Topology.Mode)
	}
}

func TestWebhookValidateCreateAcceptsTopologyRequired(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected topology Required spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsTopologyPreferred(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModePreferred,
		TopologyLevel: "kubernetes.io/hostname",
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected topology Preferred spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsTopologyUnconstrained(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode: TopologyModeUnconstrained,
		// TopologyLevel is optional for Unconstrained
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected topology Unconstrained spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsTopologyDisabled(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode: TopologyModeDisabled,
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected topology Disabled spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsRequiredWithoutTopologyLevel(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode: TopologyModeRequired,
		// TopologyLevel missing
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject Required mode without topologyLevel")
	}
	if !strings.Contains(err.Error(), "topologyLevel") {
		t.Fatalf("expected error about topologyLevel, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsPreferredWithoutTopologyLevel(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode: TopologyModePreferred,
		// TopologyLevel missing
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject Preferred mode without topologyLevel")
	}
	if !strings.Contains(err.Error(), "topologyLevel") {
		t.Fatalf("expected error about topologyLevel, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsColocationWithDisabledTopology(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode:                   TopologyModeDisabled,
		LeaderWorkerColocation: true,
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject leaderWorkerColocation with Disabled mode")
	}
	if !strings.Contains(err.Error(), "leaderWorkerColocation") {
		t.Fatalf("expected error about leaderWorkerColocation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsColocationWithRequiredTopology(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode:                   TopologyModeRequired,
		TopologyLevel:          "topology.kubernetes.io/zone",
		LeaderWorkerColocation: true,
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected colocation with Required mode to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsPhase3ManifestUnchanged(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// A Phase 3 manifest with no topology at all should pass unchanged.
	job := minimalValidRTJ()
	job.Spec.Topology = nil
	job.Spec.Parallelism = nil
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected Phase 3 manifest without topology to pass, got %v", err)
	}

	// Topology must remain nil after defaulting.
	if job.Spec.Topology != nil {
		t.Fatalf("expected topology to stay nil for Phase 3 backward compatibility")
	}
}

func TestWebhookValidateCreateAcceptsTopologyWithParallelism(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// Topology + parallelism (Phase 4 full-feature spec).
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected Phase 4 full spec to pass validation, got %v", err)
	}
}

func TestIsTopologyEnabled(t *testing.T) {
	tests := []struct {
		name     string
		topology *TopologySpec
		want     bool
	}{
		{name: "nil topology", topology: nil, want: false},
		{name: "disabled", topology: &TopologySpec{Mode: TopologyModeDisabled}, want: false},
		{name: "required", topology: &TopologySpec{Mode: TopologyModeRequired, TopologyLevel: "zone"}, want: true},
		{name: "preferred", topology: &TopologySpec{Mode: TopologyModePreferred, TopologyLevel: "zone"}, want: true},
		{name: "unconstrained", topology: &TopologySpec{Mode: TopologyModeUnconstrained}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Spec.Topology = tt.topology
			if got := job.IsTopologyEnabled(); got != tt.want {
				t.Fatalf("IsTopologyEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Phase 5 webhook tests: priority policy ref ---

func TestWebhookDefaultPreservesPhase4SpecWithoutPriorityPolicyRef(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.PriorityPolicyRef = nil
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Phase 4 spec should pass without priority policy ref.
	if job.Spec.PriorityPolicyRef != nil {
		t.Fatalf("expected priorityPolicyRef to remain nil for Phase 4 backward compat")
	}
}

func TestWebhookValidateCreateAcceptsPriorityPolicyRef(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "default-shaping",
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected spec with priorityPolicyRef to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsEmptyPriorityPolicyRefName(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "",
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject empty priorityPolicyRef name")
	}
	if !strings.Contains(err.Error(), "priorityPolicyRef") {
		t.Fatalf("expected error about priorityPolicyRef, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsPhase4ManifestUnchanged(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// A Phase 4 manifest with topology but no priority policy ref should pass.
	job := minimalValidRTJ()
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.PriorityPolicyRef = nil
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected Phase 4 manifest without priorityPolicyRef to pass, got %v", err)
	}

	// PriorityPolicyRef must remain nil.
	if job.Spec.PriorityPolicyRef != nil {
		t.Fatalf("expected priorityPolicyRef to stay nil for Phase 4 backward compatibility")
	}
}

func TestWebhookValidateCreateAcceptsPriorityPolicyRefWithTopologyAndParallelism(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// Full Phase 5 spec with all optional features enabled.
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "aggressive-shaping",
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected full Phase 5 spec to pass validation, got %v", err)
	}
}

func TestIsPriorityShapingEnabled(t *testing.T) {
	tests := []struct {
		name      string
		policyRef *PriorityPolicyReference
		want      bool
	}{
		{name: "nil ref", policyRef: nil, want: false},
		{name: "empty name", policyRef: &PriorityPolicyReference{Name: ""}, want: false},
		{name: "whitespace name", policyRef: &PriorityPolicyReference{Name: "  "}, want: false},
		{name: "valid ref", policyRef: &PriorityPolicyReference{Name: "default-shaping"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Spec.PriorityPolicyRef = tt.policyRef
			if got := job.IsPriorityShapingEnabled(); got != tt.want {
				t.Fatalf("IsPriorityShapingEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Phase 6 webhook tests: managedBy and multi-cluster ---

func TestWebhookDefaultPreservesPhase5SpecWithoutManagedBy(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.ManagedBy = ""
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Phase 5 spec should pass without managedBy.
	if job.Spec.ManagedBy != "" {
		t.Fatalf("expected managedBy to remain empty for Phase 5 backward compat")
	}
}

func TestWebhookValidateCreateAcceptsManagedByMultiKueue(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected spec with managedBy=multikueue to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsEmptyManagedBy(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.ManagedBy = ""
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected spec with empty managedBy to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsManagedByWithoutDomainPrefix(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.ManagedBy = "invalid-no-slash"
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject managedBy without domain prefix")
	}
	if !strings.Contains(err.Error(), "managedBy") {
		t.Fatalf("expected error about managedBy, got %v", err)
	}
}

func TestWebhookValidateUpdateRejectsManagedByChange(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	oldJob := minimalValidRTJ()
	oldJob.Spec.ManagedBy = MultiKueueControllerName
	oldJob.Spec.Suspend = ptr.To(true)
	oldJob.Default()

	newJob := oldJob.DeepCopy()
	newJob.Spec.ManagedBy = "other.io/controller"

	_, err := wh.ValidateUpdate(context.Background(), oldJob, newJob)
	if err == nil {
		t.Fatalf("expected managedBy update to fail (immutable)")
	}
	if !strings.Contains(err.Error(), "managedBy") {
		t.Fatalf("expected error about managedBy immutability, got %v", err)
	}
}

func TestWebhookValidateUpdateRejectsManagedByRemoval(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	oldJob := minimalValidRTJ()
	oldJob.Spec.ManagedBy = MultiKueueControllerName
	oldJob.Spec.Suspend = ptr.To(true)
	oldJob.Default()

	newJob := oldJob.DeepCopy()
	newJob.Spec.ManagedBy = ""

	_, err := wh.ValidateUpdate(context.Background(), oldJob, newJob)
	if err == nil {
		t.Fatalf("expected managedBy removal to fail (immutable)")
	}
	if !strings.Contains(err.Error(), "managedBy") {
		t.Fatalf("expected error about managedBy immutability, got %v", err)
	}
}

func TestWebhookValidateUpdatePreservesManagedBy(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	oldJob := minimalValidRTJ()
	oldJob.Spec.ManagedBy = MultiKueueControllerName
	oldJob.Spec.Suspend = ptr.To(true)
	oldJob.Default()

	newJob := oldJob.DeepCopy()
	// Same managedBy, update desiredState only.
	newJob.Spec.Control.DesiredState = DesiredStatePaused

	if _, err := wh.ValidateUpdate(context.Background(), oldJob, newJob); err != nil {
		t.Fatalf("expected update preserving managedBy to succeed, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsPhase5ManifestUnchangedForPhase6(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// A Phase 5 manifest with all features but no managedBy should pass.
	job := minimalValidRTJ()
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{Name: "default-shaping"}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.ManagedBy = ""
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected Phase 5 manifest without managedBy to pass, got %v", err)
	}

	// ManagedBy must remain empty.
	if job.Spec.ManagedBy != "" {
		t.Fatalf("expected managedBy to stay empty for Phase 5 backward compatibility")
	}
}

func TestWebhookValidateCreateAcceptsManagedByWithAllPhaseFeatures(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// Full Phase 6 spec with all optional features enabled.
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "aggressive-shaping",
	}
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected full Phase 6 spec to pass validation, got %v", err)
	}
}

func TestIsManagedByMultiKueue(t *testing.T) {
	tests := []struct {
		name      string
		managedBy string
		want      bool
	}{
		{name: "empty", managedBy: "", want: false},
		{name: "multikueue", managedBy: MultiKueueControllerName, want: true},
		{name: "other controller", managedBy: "other.io/controller", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Spec.ManagedBy = tt.managedBy
			if got := job.IsManagedByMultiKueue(); got != tt.want {
				t.Fatalf("IsManagedByMultiKueue() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Phase 7 webhook tests: backward compatibility ---

func TestWebhookDefaultPreservesPhase6SpecForPhase7(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// A Phase 6 spec with all features should pass defaulting unchanged.
	job := minimalValidRTJ()
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{Name: "default-shaping"}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// No Phase 7 spec additions should appear.
	if job.Spec.ManagedBy != MultiKueueControllerName {
		t.Fatalf("expected managedBy to be preserved")
	}
}

func TestWebhookValidateCreateAcceptsPhase6ManifestForPhase7(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// Full Phase 6 spec with all optional features enabled.
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "aggressive-shaping",
	}
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected full Phase 6 spec to pass Phase 7 validation, got %v", err)
	}
}

func TestWebhookValidateUpdatePreservesPhase7StatusFields(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	oldJob := minimalValidRTJ()
	oldJob.Spec.Suspend = ptr.To(true)
	oldJob.Default()

	// Simulate controller setting Phase 7 status.
	oldJob.Status.LaunchGate = &LaunchGateStatus{
		State:  LaunchGateBlocked,
		Reason: "AdmissionCheckPending",
	}
	oldJob.Status.Provisioning = &ProvisioningStatus{
		State:   ProvisioningPending,
		Attempt: 1,
	}

	newJob := oldJob.DeepCopy()
	// User-only change: update desiredState.
	newJob.Spec.Control.DesiredState = DesiredStatePaused

	if _, err := wh.ValidateUpdate(context.Background(), oldJob, newJob); err != nil {
		t.Fatalf("expected update with Phase 7 status to succeed, got %v", err)
	}

	// Status fields are not cleared by webhook validation.
	if newJob.Status.LaunchGate == nil || newJob.Status.LaunchGate.State != LaunchGateBlocked {
		t.Fatalf("expected launchGate status to be preserved through webhook")
	}
	if newJob.Status.Provisioning == nil || newJob.Status.Provisioning.State != ProvisioningPending {
		t.Fatalf("expected provisioning status to be preserved through webhook")
	}
}

func TestWebhookDefaultDoesNotInjectPhase7Status(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Defaulting must NOT inject Phase 7 status fields.
	if job.Status.LaunchGate != nil {
		t.Fatalf("webhook defaulting must not inject launchGate status")
	}
	if job.Status.Provisioning != nil {
		t.Fatalf("webhook defaulting must not inject provisioning status")
	}
	if job.Status.StartupRecovery != nil {
		t.Fatalf("webhook defaulting must not inject startupRecovery status")
	}
	if job.Status.Capacity != nil {
		t.Fatalf("webhook defaulting must not inject capacity status")
	}
}

// --- Phase 8 webhook tests: DRA device requests ---

func TestWebhookDefaultPreservesPhase7SpecWithoutDevices(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = nil
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Phase 7 spec should pass without devices.
	if job.Spec.Devices != nil {
		t.Fatalf("expected devices to remain nil for Phase 7 backward compat")
	}
}

func TestWebhookDefaultSetsDeviceModeWhenEmpty(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{} // mode empty
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Devices.Mode != DefaultDeviceMode {
		t.Fatalf("expected device mode to default to %q, got %q", DefaultDeviceMode, job.Spec.Devices.Mode)
	}
}

func TestWebhookDefaultPreservesExplicitDeviceMode(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
			},
		}},
	}
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Devices.Mode != DeviceModeDRA {
		t.Fatalf("expected device mode to stay %q, got %q", DeviceModeDRA, job.Spec.Devices.Mode)
	}
}

func TestWebhookDefaultSetsDeviceRequestCount(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				// Count not set -> should default to 1.
			},
		}},
	}
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	if job.Spec.Devices.Claims[0].Request.Count != DefaultDeviceRequestCount {
		t.Fatalf("expected device request count to default to %d, got %d",
			DefaultDeviceRequestCount, job.Spec.Devices.Claims[0].Request.Count)
	}
}

func TestWebhookValidateCreateAcceptsDRADeviceSpec(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           8,
				Selectors: []string{
					`device.attributes["memory"].compareTo(quantity("80Gi")) >= 0`,
				},
			},
		}},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected DRA device spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsMultipleClaims(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           4,
				},
			},
			{
				Name:       "rdma",
				Containers: []string{"worker"},
				Request: DeviceRequestSpec{
					DeviceClassName: "rdma.example.com",
					Count:           1,
				},
			},
		},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected multiple claims to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateAcceptsDeviceDisabled(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDisabled,
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected Disabled device spec to pass validation, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsDRAWithoutClaims(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode:   DeviceModeDRA,
		Claims: nil, // empty claims
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject DRA mode without claims")
	}
	if !strings.Contains(err.Error(), "claims") {
		t.Fatalf("expected error about claims, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsDisabledWithClaims(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDisabled,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           1,
			},
		}},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject Disabled mode with claims")
	}
	if !strings.Contains(err.Error(), "claims") {
		t.Fatalf("expected error about claims, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsDuplicateClaimNames(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           1,
				},
			},
			{
				Name:       "gpu", // duplicate
				Containers: []string{"sidecar"},
				Request: DeviceRequestSpec{
					DeviceClassName: "gpu-b.example.com",
					Count:           1,
				},
			},
		},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject duplicate claim names")
	}
	if !strings.Contains(err.Error(), "Duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsEmptyContainers(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: nil, // empty
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           1,
			},
		}},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject empty containers")
	}
	if !strings.Contains(err.Error(), "containers") {
		t.Fatalf("expected error about containers, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsMissingDeviceClassName(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "", // missing
				Count:           1,
			},
		}},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject missing deviceClassName")
	}
	if !strings.Contains(err.Error(), "deviceClassName") {
		t.Fatalf("expected error about deviceClassName, got %v", err)
	}
}

func TestWebhookValidateCreateRejectsEmptySelector(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           1,
				Selectors:       []string{"  "}, // whitespace-only
			},
		}},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject empty selector")
	}
	if !strings.Contains(err.Error(), "selector") {
		t.Fatalf("expected error about selector, got %v", err)
	}
}

func TestWebhookDefaultDoesNotInjectPhase8Status(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           1,
			},
		}},
	}
	job.Spec.Suspend = nil

	if err := wh.Default(context.Background(), job); err != nil {
		t.Fatalf("default webhook returned error: %v", err)
	}

	// Defaulting must NOT inject Phase 8 device status.
	if job.Status.Devices != nil {
		t.Fatalf("webhook defaulting must not inject devices status")
	}
}

func TestWebhookValidateUpdatePreservesPhase8StatusFields(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	oldJob := minimalValidRTJ()
	oldJob.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           8,
			},
		}},
	}
	oldJob.Spec.Suspend = ptr.To(true)
	oldJob.Default()

	// Simulate controller setting Phase 8 status.
	oldJob.Status.Devices = &DeviceStatus{
		DeviceMode:                      DeviceModeDRA,
		RequestedDeviceClasses:          []string{"gpu.example.com"},
		CurrentDeviceProfileFingerprint: "sha256:abc123",
		ClaimAllocationState:            ClaimAllocationPending,
		ResourceClaimTemplateRefs: []ResourceClaimTemplateReference{
			{Name: "example-gpu", ClaimName: "gpu"},
		},
	}

	newJob := oldJob.DeepCopy()
	// User-only change: update desiredState.
	newJob.Spec.Control.DesiredState = DesiredStatePaused

	if _, err := wh.ValidateUpdate(context.Background(), oldJob, newJob); err != nil {
		t.Fatalf("expected update with Phase 8 status to succeed, got %v", err)
	}

	// Status fields are not cleared by webhook validation.
	if newJob.Status.Devices == nil {
		t.Fatalf("expected devices status to be preserved through webhook")
	}
	if newJob.Status.Devices.DeviceMode != DeviceModeDRA {
		t.Fatalf("expected devices status mode to be preserved")
	}
	if newJob.Status.Devices.CurrentDeviceProfileFingerprint != "sha256:abc123" {
		t.Fatalf("expected device fingerprint to be preserved")
	}
}

func TestWebhookValidateCreateAcceptsPhase7ManifestForPhase8(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// Full Phase 7 spec with all features enabled, no devices.
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "aggressive-shaping",
	}
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.Devices = nil // Phase 7: no devices
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected full Phase 7 spec to pass Phase 8 validation, got %v", err)
	}

	// Devices must remain nil.
	if job.Spec.Devices != nil {
		t.Fatalf("expected devices to stay nil for Phase 7 backward compatibility")
	}
}

func TestWebhookValidateCreateAcceptsFullPhase8Spec(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	// Full Phase 8 spec with all optional features enabled.
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "trainer",
		EnablePartialAdmission: true,
	}
	job.Spec.Topology = &TopologySpec{
		Mode:          TopologyModeRequired,
		TopologyLevel: "topology.kubernetes.io/zone",
	}
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{
		Name: "aggressive-shaping",
	}
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{
			{
				Name:       "gpu",
				Containers: []string{"worker"},
				Request: DeviceRequestSpec{
					DeviceClassName: "gpu.example.com",
					Count:           8,
					Selectors: []string{
						`device.attributes["memory"].compareTo(quantity("80Gi")) >= 0`,
					},
				},
			},
		},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	if _, err := wh.ValidateCreate(context.Background(), job); err != nil {
		t.Fatalf("expected full Phase 8 spec to pass validation, got %v", err)
	}
}

func TestIsDevicesEnabled(t *testing.T) {
	tests := []struct {
		name    string
		devices *DeviceSpec
		want    bool
	}{
		{name: "nil devices", devices: nil, want: false},
		{name: "disabled", devices: &DeviceSpec{Mode: DeviceModeDisabled}, want: false},
		{name: "dra", devices: &DeviceSpec{Mode: DeviceModeDRA}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Spec.Devices = tt.devices
			if got := job.IsDevicesEnabled(); got != tt.want {
				t.Fatalf("IsDevicesEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookValidateCreateRejectsDuplicateContainerNames(t *testing.T) {
	scheme := webhookTestScheme(t)
	wh := &ResumableTrainingJobWebhook{
		Client:            fake.NewClientBuilder().WithScheme(scheme).Build(),
		DefaultQueueExist: func(string) bool { return false },
	}

	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker", "worker"}, // duplicate
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           1,
			},
		}},
	}
	job.Spec.Suspend = ptr.To(true)
	job.Default()

	_, err := wh.ValidateCreate(context.Background(), job)
	if err == nil {
		t.Fatalf("expected validation to reject duplicate container names")
	}
	if !strings.Contains(err.Error(), "Duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestWebhookDeepCopyIndependenceForDeviceSpec(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Devices = &DeviceSpec{
		Mode: DeviceModeDRA,
		Claims: []DeviceClaimSpec{{
			Name:       "gpu",
			Containers: []string{"worker"},
			Request: DeviceRequestSpec{
				DeviceClassName: "gpu.example.com",
				Count:           8,
				Selectors: []string{
					"expr1",
					"expr2",
				},
			},
		}},
	}

	copied := job.DeepCopy()

	// Mutate the copy.
	copied.Spec.Devices.Mode = DeviceModeDisabled
	copied.Spec.Devices.Claims[0].Name = "modified"
	copied.Spec.Devices.Claims[0].Containers[0] = "modified"
	copied.Spec.Devices.Claims[0].Request.DeviceClassName = "modified"
	copied.Spec.Devices.Claims[0].Request.Selectors[0] = "modified"

	// Original should be unchanged.
	if job.Spec.Devices.Mode != DeviceModeDRA {
		t.Fatalf("deep copy mutation leaked to original: mode")
	}
	if job.Spec.Devices.Claims[0].Name != "gpu" {
		t.Fatalf("deep copy mutation leaked to original: claim name")
	}
	if job.Spec.Devices.Claims[0].Containers[0] != "worker" {
		t.Fatalf("deep copy mutation leaked to original: container name")
	}
	if job.Spec.Devices.Claims[0].Request.DeviceClassName != "gpu.example.com" {
		t.Fatalf("deep copy mutation leaked to original: deviceClassName")
	}
	if job.Spec.Devices.Claims[0].Request.Selectors[0] != "expr1" {
		t.Fatalf("deep copy mutation leaked to original: selectors")
	}
}

func TestWebhookDeepCopyIndependenceForDeviceStatus(t *testing.T) {
	job := minimalValidRTJ()
	job.Status.Devices = &DeviceStatus{
		DeviceMode:                      DeviceModeDRA,
		RequestedDeviceClasses:          []string{"gpu.example.com"},
		CurrentDeviceProfileFingerprint: "sha256:abc",
		ResourceClaimTemplateRefs: []ResourceClaimTemplateReference{
			{Name: "example-gpu", ClaimName: "gpu"},
		},
		ClaimAllocationState: ClaimAllocationAllocated,
		AllocatedClaimCount:  1,
	}

	copied := job.DeepCopy()

	// Mutate the copy.
	copied.Status.Devices.DeviceMode = DeviceModeDisabled
	copied.Status.Devices.RequestedDeviceClasses[0] = "modified"
	copied.Status.Devices.ResourceClaimTemplateRefs[0].Name = "modified"

	// Original should be unchanged.
	if job.Status.Devices.DeviceMode != DeviceModeDRA {
		t.Fatalf("deep copy mutation leaked to original: deviceMode")
	}
	if job.Status.Devices.RequestedDeviceClasses[0] != "gpu.example.com" {
		t.Fatalf("deep copy mutation leaked to original: requestedDeviceClasses")
	}
	if job.Status.Devices.ResourceClaimTemplateRefs[0].Name != "example-gpu" {
		t.Fatalf("deep copy mutation leaked to original: resourceClaimTemplateRefs")
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
