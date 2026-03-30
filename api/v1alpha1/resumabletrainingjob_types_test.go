package v1alpha1

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestResumableTrainingJobDefault(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Control = nil
	job.Spec.Checkpoint.SafePointMode = ""
	job.Spec.Resume.SourcePolicy = ""
	job.Spec.Resume.MaxResumeRetries = 0
	job.Spec.Runtime.Template.APIVersion = ""
	job.Spec.Runtime.Template.Kind = ""

	job.Default()

	if job.Spec.Control == nil {
		t.Fatalf("expected control defaults to allocate control spec")
	}
	if job.Spec.Control.DesiredState != DefaultDesiredState {
		t.Fatalf("expected desired state %q, got %q", DefaultDesiredState, job.Spec.Control.DesiredState)
	}
	if job.Spec.Checkpoint.SafePointMode != DefaultSafePointMode {
		t.Fatalf("expected safePointMode %q, got %q", DefaultSafePointMode, job.Spec.Checkpoint.SafePointMode)
	}
	if job.Spec.Resume.SourcePolicy != DefaultResumeSourcePolicy {
		t.Fatalf("expected sourcePolicy %q, got %q", DefaultResumeSourcePolicy, job.Spec.Resume.SourcePolicy)
	}
	if job.Spec.Resume.MaxResumeRetries != DefaultMaxResumeRetries {
		t.Fatalf("expected maxResumeRetries %d, got %d", DefaultMaxResumeRetries, job.Spec.Resume.MaxResumeRetries)
	}
	if job.Spec.Runtime.Template.APIVersion != DefaultJobSetAPIVersion {
		t.Fatalf("expected template apiVersion %q, got %q", DefaultJobSetAPIVersion, job.Spec.Runtime.Template.APIVersion)
	}
	if job.Spec.Runtime.Template.Kind != DefaultJobSetKind {
		t.Fatalf("expected template kind %q, got %q", DefaultJobSetKind, job.Spec.Runtime.Template.Kind)
	}
}

func TestResumableTrainingJobValidateCreateSuccess(t *testing.T) {
	job := minimalValidRTJ()
	job.Default()

	if err := job.ValidateCreate(); err != nil {
		t.Fatalf("expected create validation to succeed, got %v", err)
	}
}

func TestResumableTrainingJobValidateCreateRejectsInvalidFields(t *testing.T) {
	testCases := []struct {
		name        string
		mutate      func(*ResumableTrainingJob)
		wantMessage string
	}{
		{
			name: "freshness budget less than interval",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Checkpoint.FreshnessBudget = metav1.Duration{Duration: 1 * time.Minute}
			},
			wantMessage: "freshnessBudget",
		},
		{
			name: "invalid storage uri",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Checkpoint.StorageURI = "file:///tmp/checkpoints"
			},
			wantMessage: "storageURI",
		},
		{
			name: "non jobset kind",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Runtime.Template.Kind = "Deployment"
			},
			wantMessage: "template.kind",
		},
		{
			name: "missing embedded spec",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Runtime.Template.Spec = runtime.RawExtension{}
			},
			wantMessage: "template.spec",
		},
		{
			name: "invalid desired state",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Control.DesiredState = DesiredState("Stopped")
			},
			wantMessage: "desiredState",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Default()
			tc.mutate(job)

			err := job.ValidateCreate()
			if err == nil {
				t.Fatalf("expected create validation to fail")
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("expected error to mention %q, got %v", tc.wantMessage, err)
			}
		})
	}
}

func TestInitializePhase1StatusAndSetPhase(t *testing.T) {
	job := minimalValidRTJ()
	job.Generation = 7
	now := metav1.NewTime(time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC))

	if changed := job.InitializePhase1Status(now); !changed {
		t.Fatalf("expected initialization to report changes")
	}
	if job.Status.Phase != PhasePending {
		t.Fatalf("expected phase %q, got %q", PhasePending, job.Status.Phase)
	}
	if job.Status.ObservedGeneration != 7 {
		t.Fatalf("expected observedGeneration 7, got %d", job.Status.ObservedGeneration)
	}
	if job.Status.TransitionTimestamps.LastTransitionTime == nil {
		t.Fatalf("expected lastTransitionTime to be initialized")
	}
	if job.Status.Reason != ReasonControllerInitialized {
		t.Fatalf("expected reason %q, got %q", ReasonControllerInitialized, job.Status.Reason)
	}

	runTime := metav1.NewTime(now.Add(5 * time.Minute))
	if changed := job.Status.SetPhase(PhaseRunning, "Running", "Job is running.", runTime); !changed {
		t.Fatalf("expected running phase transition to change status")
	}
	if job.Status.TransitionTimestamps.RunningAt == nil {
		t.Fatalf("expected runningAt to be recorded")
	}
	if !job.Status.TransitionTimestamps.LastTransitionTime.Time.Equal(runTime.Time) {
		t.Fatalf("expected lastTransitionTime to match running transition")
	}

	pauseTime := metav1.NewTime(now.Add(10 * time.Minute))
	if changed := job.Status.SetPhase(PhasePaused, "Paused", "Job is paused.", pauseTime); !changed {
		t.Fatalf("expected paused phase transition to change status")
	}
	if job.Status.TransitionTimestamps.PausedAt == nil {
		t.Fatalf("expected pausedAt to be recorded")
	}
	if !job.Status.TransitionTimestamps.LastTransitionTime.Time.Equal(pauseTime.Time) {
		t.Fatalf("expected lastTransitionTime to match paused transition")
	}
}

// --- Phase 3 backward-compatibility tests ---

func TestPhase2SpecDecodesWithoutParallelism(t *testing.T) {
	job := minimalValidRTJ()
	job.Default()

	// Phase 2 spec has no parallelism section
	if job.Spec.Parallelism != nil {
		t.Fatalf("expected nil parallelism for Phase 2 spec")
	}
	if err := job.ValidateCreate(); err != nil {
		t.Fatalf("expected Phase 2 spec to pass validation, got %v", err)
	}
}

func TestEffectivePreferredCountFallsBackToWorldSize(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Parallelism = nil

	if got := job.EffectivePreferredCount(); got != 8 {
		t.Fatalf("expected effectivePreferredCount 8, got %d", got)
	}
}

func TestEffectivePreferredCountUsesParallelism(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Identity.WorldSize = 8
	job.Spec.Parallelism = &ParallelismSpec{PreferredCount: 4}

	if got := job.EffectivePreferredCount(); got != 4 {
		t.Fatalf("expected effectivePreferredCount 4, got %d", got)
	}
}

func TestEffectiveMinCountNilWhenPartialAdmissionDisabled(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount: 8,
		MinCount:       ptr.To[int32](4),
		// EnablePartialAdmission is false by default
	}

	if got := job.EffectiveMinCount(); got != nil {
		t.Fatalf("expected nil effectiveMinCount when partial admission disabled, got %d", *got)
	}
}

func TestEffectiveMinCountReturnsValueWhenEnabled(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		EnablePartialAdmission: true,
	}

	got := job.EffectiveMinCount()
	if got == nil {
		t.Fatalf("expected non-nil effectiveMinCount")
	}
	if *got != 4 {
		t.Fatalf("expected effectiveMinCount 4, got %d", *got)
	}
}

func TestDefaultPreservesPhase2ResumePolicy(t *testing.T) {
	job := minimalValidRTJ()
	job.Default()

	if job.Spec.Resume.AllowWorldSizeChange != DefaultAllowWorldSizeChange {
		t.Fatalf("expected allowWorldSizeChange=%v, got %v", DefaultAllowWorldSizeChange, job.Spec.Resume.AllowWorldSizeChange)
	}
}

// --- Phase 3 validation tests ---

func TestValidateParallelismSuccess(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*ResumableTrainingJob)
	}{
		{
			name: "parallelism nil (Phase 2 compat)",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Parallelism = nil
			},
		},
		{
			name: "preferred count equals world size",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Identity.WorldSize = 8
				job.Spec.Parallelism = &ParallelismSpec{PreferredCount: 8}
			},
		},
		{
			name: "preferred count zero falls back to world size",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Parallelism = &ParallelismSpec{PreferredCount: 0}
			},
		},
		{
			name: "valid partial admission",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Identity.WorldSize = 8
				job.Spec.Resume.AllowWorldSizeChange = true
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount:         8,
					MinCount:               ptr.To[int32](4),
					EnablePartialAdmission: true,
				}
			},
		},
		{
			name: "allow world size change without partial admission",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Resume.AllowWorldSizeChange = true
			},
		},
		{
			name: "min count set but partial admission disabled",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount: 8,
					MinCount:       ptr.To[int32](4),
					// EnablePartialAdmission = false: minCount is inert but not rejected
				}
				job.Spec.Identity.WorldSize = 8
			},
		},
		{
			name: "pod set name specified",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount: 8,
					PodSetName:     "workers",
				}
				job.Spec.Identity.WorldSize = 8
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Default()
			tc.mutate(job)

			if err := job.ValidateCreate(); err != nil {
				t.Fatalf("expected validation to succeed, got %v", err)
			}
		})
	}
}

func TestValidateParallelismRejectsInvalidFields(t *testing.T) {
	testCases := []struct {
		name        string
		mutate      func(*ResumableTrainingJob)
		wantMessage string
	}{
		{
			name: "min count greater than preferred count",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Identity.WorldSize = 8
				job.Spec.Resume.AllowWorldSizeChange = true
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount:         4,
					MinCount:               ptr.To[int32](8),
					EnablePartialAdmission: true,
				}
			},
			wantMessage: "minCount",
		},
		{
			name: "min count greater than world size when preferred unset",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Identity.WorldSize = 4
				job.Spec.Resume.AllowWorldSizeChange = true
				job.Spec.Parallelism = &ParallelismSpec{
					MinCount:               ptr.To[int32](8),
					EnablePartialAdmission: true,
				}
			},
			wantMessage: "minCount",
		},
		{
			name: "min count zero",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Resume.AllowWorldSizeChange = true
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount:         8,
					MinCount:               ptr.To[int32](0),
					EnablePartialAdmission: true,
				}
				job.Spec.Identity.WorldSize = 8
			},
			wantMessage: "minCount",
		},
		{
			name: "partial admission without allow world size change",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Resume.AllowWorldSizeChange = false
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount:         8,
					MinCount:               ptr.To[int32](4),
					EnablePartialAdmission: true,
				}
				job.Spec.Identity.WorldSize = 8
			},
			wantMessage: "enablePartialAdmission",
		},
		{
			name: "partial admission without min count",
			mutate: func(job *ResumableTrainingJob) {
				job.Spec.Resume.AllowWorldSizeChange = true
				job.Spec.Parallelism = &ParallelismSpec{
					PreferredCount:         8,
					EnablePartialAdmission: true,
				}
				job.Spec.Identity.WorldSize = 8
			},
			wantMessage: "minCount",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Default()
			tc.mutate(job)

			err := job.ValidateCreate()
			if err == nil {
				t.Fatalf("expected create validation to fail")
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("expected error to mention %q, got %v", tc.wantMessage, err)
			}
		})
	}
}

// --- Phase 3 deep copy tests ---

func TestDeepCopyParallelismSpec(t *testing.T) {
	orig := &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "workers",
		EnablePartialAdmission: true,
	}
	copy := orig.DeepCopy()

	if copy.PreferredCount != 8 {
		t.Fatalf("expected preferredCount 8, got %d", copy.PreferredCount)
	}
	if copy.MinCount == nil || *copy.MinCount != 4 {
		t.Fatalf("expected minCount 4")
	}
	// Mutate copy to verify independence
	*copy.MinCount = 2
	if *orig.MinCount != 4 {
		t.Fatalf("mutating copy affected original")
	}
}

func TestDeepCopyAdmissionStatus(t *testing.T) {
	orig := &AdmissionStatus{
		AdmittedWorkerCount:  4,
		PreferredWorkerCount: 8,
		ActiveWorkerCount:    4,
		AdmittedFlavors:      map[string]string{"workers": "a100-80gb"},
	}
	copy := orig.DeepCopy()

	if copy.AdmittedWorkerCount != 4 {
		t.Fatalf("expected admittedWorkerCount 4, got %d", copy.AdmittedWorkerCount)
	}
	// Mutate copy to verify independence
	copy.AdmittedFlavors["workers"] = "h100"
	if orig.AdmittedFlavors["workers"] != "a100-80gb" {
		t.Fatalf("mutating copy affected original")
	}
}

func TestDeepCopyRestoreStatus(t *testing.T) {
	orig := &RestoreStatus{
		LastCheckpointWorldSize: 8,
		LastRestoreWorldSize:    4,
		RestoreMode:             RestoreModeReshard,
	}
	copy := orig.DeepCopy()

	if copy.RestoreMode != RestoreModeReshard {
		t.Fatalf("expected restoreMode %q, got %q", RestoreModeReshard, copy.RestoreMode)
	}
	if copy.LastCheckpointWorldSize != 8 {
		t.Fatalf("expected lastCheckpointWorldSize 8, got %d", copy.LastCheckpointWorldSize)
	}
}

func TestDeepCopyRTJWithPhase3Fields(t *testing.T) {
	job := minimalValidRTJ()
	job.Spec.Resume.AllowWorldSizeChange = true
	job.Spec.Parallelism = &ParallelismSpec{
		PreferredCount:         8,
		MinCount:               ptr.To[int32](4),
		PodSetName:             "workers",
		EnablePartialAdmission: true,
	}
	job.Status.Admission = &AdmissionStatus{
		AdmittedWorkerCount: 4,
		AdmittedFlavors:     map[string]string{"workers": "a100"},
	}
	job.Status.Restore = &RestoreStatus{
		LastCheckpointWorldSize: 8,
		LastRestoreWorldSize:    4,
		RestoreMode:             RestoreModeReshard,
	}

	copy := job.DeepCopy()

	if copy.Spec.Parallelism == nil {
		t.Fatalf("expected parallelism to be preserved in deep copy")
	}
	if copy.Spec.Parallelism.PreferredCount != 8 {
		t.Fatalf("expected preferredCount 8")
	}
	if copy.Status.Admission == nil {
		t.Fatalf("expected admission status to be preserved in deep copy")
	}
	if copy.Status.Restore == nil {
		t.Fatalf("expected restore status to be preserved in deep copy")
	}

	// Verify independence
	*copy.Spec.Parallelism.MinCount = 2
	if *job.Spec.Parallelism.MinCount != 4 {
		t.Fatalf("deep copy not independent")
	}
}

// --- Phase 5 backward-compatibility and deep copy tests ---

func TestPhase4SpecDecodesWithoutPriorityPolicyRef(t *testing.T) {
	job := minimalValidRTJ()
	job.Default()

	// Phase 4 spec has no priorityPolicyRef.
	if job.Spec.PriorityPolicyRef != nil {
		t.Fatalf("expected nil priorityPolicyRef for Phase 4 spec")
	}
	if err := job.ValidateCreate(); err != nil {
		t.Fatalf("expected Phase 4 spec to pass validation, got %v", err)
	}
}

func TestDeepCopyPriorityPolicyReference(t *testing.T) {
	orig := &PriorityPolicyReference{Name: "default-shaping"}
	cp := orig.DeepCopy()

	if cp.Name != "default-shaping" {
		t.Fatalf("expected name 'default-shaping', got %q", cp.Name)
	}
	cp.Name = "other"
	if orig.Name != "default-shaping" {
		t.Fatalf("mutating copy affected original")
	}
}

func TestDeepCopyRTJWithPhase5Fields(t *testing.T) {
	now := metav1.Now()
	job := minimalValidRTJ()
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{Name: "default-shaping"}
	job.Status.PriorityShaping = &PriorityShapingStatus{
		BasePriority:                100,
		EffectivePriority:           80,
		PreemptionState:             PreemptionStateProtected,
		PreemptionStateReason:       "WithinProtectionWindow",
		ProtectedUntil:              &now,
		LastCompletedCheckpointTime: &now,
		CheckpointAge:               "5m0s",
		LastYieldTime:               &now,
		LastResumeTime:              &now,
		RecentYieldCount:            1,
		AppliedPolicyRef:            "default-shaping",
	}

	cp := job.DeepCopy()

	if cp.Spec.PriorityPolicyRef == nil || cp.Spec.PriorityPolicyRef.Name != "default-shaping" {
		t.Fatalf("expected priorityPolicyRef to be preserved in deep copy")
	}
	if cp.Status.PriorityShaping == nil {
		t.Fatalf("expected priorityShaping status to be preserved in deep copy")
	}
	if cp.Status.PriorityShaping.EffectivePriority != 80 {
		t.Fatalf("expected effectivePriority 80, got %d", cp.Status.PriorityShaping.EffectivePriority)
	}
	if cp.Status.PriorityShaping.PreemptionState != PreemptionStateProtected {
		t.Fatalf("expected preemptionState Protected, got %s", cp.Status.PriorityShaping.PreemptionState)
	}

	// Verify independence.
	cp.Spec.PriorityPolicyRef.Name = "other"
	if job.Spec.PriorityPolicyRef.Name != "default-shaping" {
		t.Fatalf("deep copy not independent for policyRef")
	}
}

// --- Phase 6 backward-compatibility and deep copy tests ---

func TestPhase5SpecDecodesWithoutManagedBy(t *testing.T) {
	job := minimalValidRTJ()
	job.Default()

	// Phase 5 spec has no managedBy.
	if job.Spec.ManagedBy != "" {
		t.Fatalf("expected empty managedBy for Phase 5 spec")
	}
	if err := job.ValidateCreate(); err != nil {
		t.Fatalf("expected Phase 5 spec to pass validation, got %v", err)
	}
}

func TestPhase5StatusDecodesWithoutMultiCluster(t *testing.T) {
	job := minimalValidRTJ()
	job.Default()
	now := metav1.Now()
	job.InitializePhase1Status(metav1.NewTime(now.Time))

	// Phase 5 status has no multiCluster section.
	if job.Status.MultiCluster != nil {
		t.Fatalf("expected nil multiCluster for Phase 5 status")
	}
}

func TestDeepCopyMultiClusterStatus(t *testing.T) {
	now := metav1.Now()
	orig := &MultiClusterStatus{
		DispatchPhase:     DispatchPhaseActive,
		NominatedClusters: []string{"worker-1", "worker-2"},
		ExecutionCluster:  "worker-1",
		RemoteObjectRef: &RemoteObjectReference{
			Cluster:   "worker-1",
			Namespace: "default",
			Name:      "example",
			UID:       "abc-123",
		},
		RemotePhase: PhaseRunning,
		RemoteCheckpoint: &RemoteCheckpointSummary{
			LastCompletedCheckpointID:   "ckpt-7",
			LastCompletedCheckpointTime: &now,
			StorageURI:                  "s3://checkpoints/example/ckpt-7",
		},
		RemoteObservedGeneration: 5,
		LocalExecutionSuppressed: true,
	}

	cp := orig.DeepCopy()

	if cp.DispatchPhase != DispatchPhaseActive {
		t.Fatalf("expected dispatchPhase Active, got %s", cp.DispatchPhase)
	}
	if len(cp.NominatedClusters) != 2 || cp.NominatedClusters[0] != "worker-1" {
		t.Fatalf("expected nominatedClusters [worker-1, worker-2], got %v", cp.NominatedClusters)
	}
	if cp.ExecutionCluster != "worker-1" {
		t.Fatalf("expected executionCluster worker-1, got %s", cp.ExecutionCluster)
	}
	if cp.RemoteObjectRef == nil || cp.RemoteObjectRef.Cluster != "worker-1" {
		t.Fatalf("expected remoteObjectRef with cluster worker-1")
	}
	if cp.RemotePhase != PhaseRunning {
		t.Fatalf("expected remotePhase Running, got %s", cp.RemotePhase)
	}
	if cp.RemoteCheckpoint == nil || cp.RemoteCheckpoint.LastCompletedCheckpointID != "ckpt-7" {
		t.Fatalf("expected remoteCheckpoint with ID ckpt-7")
	}
	if cp.RemoteObservedGeneration != 5 {
		t.Fatalf("expected remoteObservedGeneration 5, got %d", cp.RemoteObservedGeneration)
	}
	if !cp.LocalExecutionSuppressed {
		t.Fatalf("expected localExecutionSuppressed true")
	}

	// Verify independence: mutate copy.
	cp.NominatedClusters[0] = "worker-3"
	if orig.NominatedClusters[0] != "worker-1" {
		t.Fatalf("mutating copy affected original nominatedClusters")
	}
	cp.RemoteObjectRef.Cluster = "worker-3"
	if orig.RemoteObjectRef.Cluster != "worker-1" {
		t.Fatalf("mutating copy affected original remoteObjectRef")
	}
	cp.RemoteCheckpoint.LastCompletedCheckpointID = "ckpt-99"
	if orig.RemoteCheckpoint.LastCompletedCheckpointID != "ckpt-7" {
		t.Fatalf("mutating copy affected original remoteCheckpoint")
	}
}

func TestDeepCopyRTJWithPhase6Fields(t *testing.T) {
	now := metav1.Now()
	job := minimalValidRTJ()
	job.Spec.ManagedBy = MultiKueueControllerName
	job.Spec.PriorityPolicyRef = &PriorityPolicyReference{Name: "default-shaping"}
	job.Status.MultiCluster = &MultiClusterStatus{
		DispatchPhase:    DispatchPhaseActive,
		ExecutionCluster: "worker-1",
		RemotePhase:      PhaseRunning,
		RemoteCheckpoint: &RemoteCheckpointSummary{
			LastCompletedCheckpointID:   "ckpt-5",
			LastCompletedCheckpointTime: &now,
			StorageURI:                  "s3://checkpoints/example/ckpt-5",
		},
		LocalExecutionSuppressed: true,
	}

	cp := job.DeepCopy()

	if cp.Spec.ManagedBy != MultiKueueControllerName {
		t.Fatalf("expected managedBy to be preserved in deep copy")
	}
	if cp.Status.MultiCluster == nil {
		t.Fatalf("expected multiCluster status to be preserved in deep copy")
	}
	if cp.Status.MultiCluster.ExecutionCluster != "worker-1" {
		t.Fatalf("expected executionCluster worker-1, got %s", cp.Status.MultiCluster.ExecutionCluster)
	}
	if cp.Status.MultiCluster.RemoteCheckpoint == nil || cp.Status.MultiCluster.RemoteCheckpoint.LastCompletedCheckpointID != "ckpt-5" {
		t.Fatalf("expected remoteCheckpoint preserved in deep copy")
	}

	// Verify independence.
	cp.Spec.ManagedBy = "other.io/controller"
	if job.Spec.ManagedBy != MultiKueueControllerName {
		t.Fatalf("deep copy not independent for managedBy")
	}
	cp.Status.MultiCluster.ExecutionCluster = "worker-2"
	if job.Status.MultiCluster.ExecutionCluster != "worker-1" {
		t.Fatalf("deep copy not independent for multiCluster")
	}
}

func TestValidateManagedByRejectsInvalidFormat(t *testing.T) {
	testCases := []struct {
		name        string
		managedBy   string
		wantMessage string
	}{
		{
			name:        "no slash",
			managedBy:   "invalid-no-slash",
			wantMessage: "managedBy",
		},
		{
			name:        "too long",
			managedBy:   strings.Repeat("a", 200) + "/" + strings.Repeat("b", 100),
			wantMessage: "managedBy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Default()
			job.Spec.ManagedBy = tc.managedBy

			err := job.ValidateCreate()
			if err == nil {
				t.Fatalf("expected create validation to fail")
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("expected error to mention %q, got %v", tc.wantMessage, err)
			}
		})
	}
}

func TestValidateManagedByAcceptsValidValues(t *testing.T) {
	testCases := []struct {
		name      string
		managedBy string
	}{
		{name: "empty (Phase 5 compat)", managedBy: ""},
		{name: "multikueue", managedBy: MultiKueueControllerName},
		{name: "custom controller", managedBy: "custom.example.com/my-controller"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := minimalValidRTJ()
			job.Default()
			job.Spec.ManagedBy = tc.managedBy

			if err := job.ValidateCreate(); err != nil {
				t.Fatalf("expected validation to succeed, got %v", err)
			}
		})
	}
}

func minimalValidRTJ() *ResumableTrainingJob {
	return &ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
		},
		Spec: ResumableTrainingJobSpec{
			QueueName:                 "research-a",
			WorkloadPriorityClassName: "batch-medium",
			Identity: ResumableTrainingJobIdentity{
				Image:       "registry.example.com/training/counter:sha256-1234",
				CodeVersion: "git:1234",
				WorldSize:   2,
				GPUShape:    "cpu",
			},
			Runtime: ResumableTrainingJobRuntime{
				Mode:          RuntimeModeDDP,
				OptimizerMode: "adamw",
				ShardingMode:  "none",
				Template: JobSetTemplate{
					APIVersion: DefaultJobSetAPIVersion,
					Kind:       DefaultJobSetKind,
					Spec: runtime.RawExtension{
						Raw: []byte(`{"replicatedJobs":[{"name":"trainer","replicas":2}]}`),
					},
				},
			},
			Checkpoint: CheckpointPolicy{
				StorageURI:      "s3://checkpoints/example/",
				Interval:        metav1.Duration{Duration: 5 * time.Minute},
				FreshnessBudget: metav1.Duration{Duration: 10 * time.Minute},
				MaxDrainTime:    metav1.Duration{Duration: 15 * time.Minute},
				SafePointMode:   SafePointModeStepBoundary,
			},
			Resume: ResumePolicy{
				SourcePolicy:     ResumeSourcePolicyLatestCompatibleComplete,
				MaxResumeRetries: 3,
			},
			Control: &ControlSpec{
				DesiredState: DesiredStateRunning,
			},
		},
	}
}
