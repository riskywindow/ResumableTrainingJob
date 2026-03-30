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
