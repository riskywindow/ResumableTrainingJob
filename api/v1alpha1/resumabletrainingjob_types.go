package v1alpha1

import (
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/kueue/pkg/controller/constants"
)

const (
	DefaultJobSetAPIVersion         = "jobset.x-k8s.io/v1alpha2"
	DefaultJobSetKind               = "JobSet"
	DefaultSafePointMode            = SafePointModeStepBoundary
	DefaultResumeSourcePolicy       = ResumeSourcePolicyLatestCompatibleComplete
	DefaultDesiredState             = DesiredStateRunning
	DefaultMaxResumeRetries   int32 = 3
	DefaultAllowWorldSizeChange     = false
	DefaultEnablePartialAdmission   = false
	DefaultTopologyMode             = TopologyModeDisabled

	// MultiKueueControllerName is the well-known managedBy value for Kueue MultiKueue.
	// When spec.managedBy is set to this value, the RTJ is eligible for MultiKueue
	// dispatch to a remote worker cluster.
	MultiKueueControllerName = "kueue.x-k8s.io/multikueue"

	// MaxManagedByLength is the maximum allowed length for the managedBy field.
	MaxManagedByLength = 256
)

const (
	ReasonControllerInitialized  = "ControllerInitialized"
	MessageControllerInitialized = "Phase 2 scaffold initialized the ResumableTrainingJob status."
)

// ResumableTrainingJobPhase is the high-level lifecycle phase published by the controller.
// +kubebuilder:validation:Enum=Pending;Queued;Admitted;Starting;Running;YieldRequested;Draining;Paused;Restoring;Succeeded;Failed
type ResumableTrainingJobPhase string

const (
	PhasePending        ResumableTrainingJobPhase = "Pending"
	PhaseQueued         ResumableTrainingJobPhase = "Queued"
	PhaseAdmitted       ResumableTrainingJobPhase = "Admitted"
	PhaseStarting       ResumableTrainingJobPhase = "Starting"
	PhaseRunning        ResumableTrainingJobPhase = "Running"
	PhaseYieldRequested ResumableTrainingJobPhase = "YieldRequested"
	PhaseDraining       ResumableTrainingJobPhase = "Draining"
	PhasePaused         ResumableTrainingJobPhase = "Paused"
	PhaseRestoring      ResumableTrainingJobPhase = "Restoring"
	PhaseSucceeded      ResumableTrainingJobPhase = "Succeeded"
	PhaseFailed         ResumableTrainingJobPhase = "Failed"
)

// RuntimeMode is the supported distributed runtime mode.
// +kubebuilder:validation:Enum=DDP;FSDP
type RuntimeMode string

const (
	RuntimeModeDDP  RuntimeMode = "DDP"
	RuntimeModeFSDP RuntimeMode = "FSDP"
)

// SafePointMode fixes the pause boundary contract.
// +kubebuilder:validation:Enum=StepBoundary
type SafePointMode string

const (
	SafePointModeStepBoundary SafePointMode = "StepBoundary"
)

// ResumeSourcePolicy fixes the checkpoint selection rule.
// +kubebuilder:validation:Enum=LatestCompatibleComplete
type ResumeSourcePolicy string

const (
	ResumeSourcePolicyLatestCompatibleComplete ResumeSourcePolicy = "LatestCompatibleComplete"
)

// DesiredState is the Phase 0 manual control field reused in Phase 1.
// +kubebuilder:validation:Enum=Running;Paused
type DesiredState string

const (
	DesiredStateRunning DesiredState = "Running"
	DesiredStatePaused  DesiredState = "Paused"
)

// CompatibilityState describes compatibility evaluation for a checkpoint.
// +kubebuilder:validation:Enum=Compatible;Incompatible;Unknown
type CompatibilityState string

const (
	CompatibilityStateCompatible   CompatibilityState = "Compatible"
	CompatibilityStateIncompatible CompatibilityState = "Incompatible"
	CompatibilityStateUnknown      CompatibilityState = "Unknown"
)

// SuspensionSource describes the dominant source for the current suspended state.
// +kubebuilder:validation:Enum=Kueue;Manual;Unknown
type SuspensionSource string

const (
	SuspensionSourceKueue   SuspensionSource = "Kueue"
	SuspensionSourceManual  SuspensionSource = "Manual"
	SuspensionSourceUnknown SuspensionSource = "Unknown"
)

// RestoreMode describes whether the most recent restore was same-size or required resharding.
// +kubebuilder:validation:Enum=SameSize;Reshard
type RestoreMode string

const (
	RestoreModeSameSize RestoreMode = "SameSize"
	RestoreModeReshard  RestoreMode = "Reshard"
)

// TopologyMode indicates the topology placement mode for the worker pod set.
// +kubebuilder:validation:Enum=Disabled;Required;Preferred;Unconstrained
type TopologyMode string

const (
	// TopologyModeDisabled disables topology-aware scheduling. Phase 3 behavior.
	TopologyModeDisabled TopologyMode = "Disabled"
	// TopologyModeRequired requires topology placement at the specified level.
	// Admission fails if placement cannot be satisfied.
	TopologyModeRequired TopologyMode = "Required"
	// TopologyModePreferred requests best-effort topology placement.
	// Kueue tries the specified level but may spread across domains.
	TopologyModePreferred TopologyMode = "Preferred"
	// TopologyModeUnconstrained enables topology-aware scheduling but lets
	// Kueue place pods freely across all available domains.
	TopologyModeUnconstrained TopologyMode = "Unconstrained"
)

// ReadinessGateState describes the state of the launch-readiness gate.
// +kubebuilder:validation:Enum=Pending;Ready;Rejected
type ReadinessGateState string

const (
	ReadinessGatePending  ReadinessGateState = "Pending"
	ReadinessGateReady    ReadinessGateState = "Ready"
	ReadinessGateRejected ReadinessGateState = "Rejected"
)

// LaunchGateState describes the aggregate launch gate evaluation state.
// +kubebuilder:validation:Enum=Open;Blocked;Unknown
type LaunchGateState string

const (
	// LaunchGateOpen means all launch prerequisites are satisfied and the
	// controller may render the child JobSet.
	LaunchGateOpen LaunchGateState = "Open"

	// LaunchGateBlocked means one or more launch prerequisites are not yet
	// satisfied. The controller must not render the child JobSet.
	LaunchGateBlocked LaunchGateState = "Blocked"

	// LaunchGateUnknown means the gate state could not be determined.
	LaunchGateUnknown LaunchGateState = "Unknown"
)

// ProvisioningState describes the state of the ProvisioningRequest
// AdmissionCheck gate for this RTJ.
// +kubebuilder:validation:Enum=NotConfigured;Pending;Provisioned;Failed
type ProvisioningState string

const (
	// ProvisioningNotConfigured means no ProvisioningRequest AdmissionCheck
	// is configured on the ClusterQueue. Phase 6 behavior preserved.
	ProvisioningNotConfigured ProvisioningState = "NotConfigured"

	// ProvisioningPending means a ProvisioningRequest has been created by
	// Kueue but the backend has not yet satisfied it.
	ProvisioningPending ProvisioningState = "Pending"

	// ProvisioningProvisioned means the ProvisioningRequest backend has
	// confirmed that physical capacity is available.
	ProvisioningProvisioned ProvisioningState = "Provisioned"

	// ProvisioningFailed means the ProvisioningRequest backend has rejected
	// the provisioning request.
	ProvisioningFailed ProvisioningState = "Failed"
)

// StartupState describes the startup/recovery lifecycle of the child runtime.
// +kubebuilder:validation:Enum=NotStarted;Starting;Running;StartupTimedOut;RecoveryTimedOut;Evicted
type StartupState string

const (
	// StartupNotStarted means no child runtime has been launched yet.
	StartupNotStarted StartupState = "NotStarted"

	// StartupStarting means the child runtime has been launched but pods
	// have not yet reached Ready.
	StartupStarting StartupState = "Starting"

	// StartupRunning means the child runtime pods are Ready.
	StartupRunning StartupState = "Running"

	// StartupTimedOut means the child runtime pods did not reach Ready
	// within the waitForPodsReady startup timeout.
	StartupTimedOut StartupState = "StartupTimedOut"

	// StartupRecoveryTimedOut means the child runtime pods lost Ready state
	// and did not recover within the waitForPodsReady recovery window.
	StartupRecoveryTimedOut StartupState = "RecoveryTimedOut"

	// StartupEvicted means the workload was evicted by Kueue
	// (startup or recovery timeout).
	StartupEvicted StartupState = "Evicted"
)

// PodsReadyState describes the pod readiness state derivable from the child
// runtime. This is a controller-derived summary, not a direct Kueue field.
// +kubebuilder:validation:Enum=Unknown;PodsReady;PodsNotReady;NoRuntime
type PodsReadyState string

const (
	// PodsReadyUnknown means the pod readiness state has not been determined.
	PodsReadyUnknown PodsReadyState = "Unknown"

	// PodsReady means all expected worker pods are in Ready condition.
	PodsReady PodsReadyState = "PodsReady"

	// PodsNotReady means one or more expected worker pods are not Ready.
	PodsNotReady PodsReadyState = "PodsNotReady"

	// PodsReadyNoRuntime means there is no active child runtime to evaluate.
	PodsReadyNoRuntime PodsReadyState = "NoRuntime"
)

// AdmissionCheckState describes the state of a single admission check.
// +kubebuilder:validation:Enum=Pending;Ready;Retry;Rejected
type AdmissionCheckState string

const (
	AdmissionCheckPending  AdmissionCheckState = "Pending"
	AdmissionCheckReady    AdmissionCheckState = "Ready"
	AdmissionCheckRetry    AdmissionCheckState = "Retry"
	AdmissionCheckRejected AdmissionCheckState = "Rejected"
)

// TopologyGateState describes whether topology assignment is satisfied.
// +kubebuilder:validation:Enum=NotConfigured;Pending;Assigned
type TopologyGateState string

const (
	// TopologyGateNotConfigured means topology-aware scheduling is not
	// enabled (Phase 3 behavior).
	TopologyGateNotConfigured TopologyGateState = "NotConfigured"

	// TopologyGatePending means topology is configured but assignment is
	// not yet present on the Workload.
	TopologyGatePending TopologyGateState = "Pending"

	// TopologyGateAssigned means topology assignment is present on the
	// Workload and ready for rendering.
	TopologyGateAssigned TopologyGateState = "Assigned"
)

// MultiClusterDispatchPhase describes the high-level multi-cluster dispatch lifecycle.
// +kubebuilder:validation:Enum=Pending;Dispatched;Active
type MultiClusterDispatchPhase string

const (
	// DispatchPhasePending means the RTJ is waiting for MultiKueue to dispatch
	// it to a worker cluster.
	DispatchPhasePending MultiClusterDispatchPhase = "Pending"

	// DispatchPhaseDispatched means MultiKueue has created a remote copy on a
	// worker cluster. The worker may not have started processing yet.
	DispatchPhaseDispatched MultiClusterDispatchPhase = "Dispatched"

	// DispatchPhaseActive means the worker cluster has acknowledged the RTJ and
	// is actively managing it (the remote phase is populated).
	DispatchPhaseActive MultiClusterDispatchPhase = "Active"
)

// TopologySpec declares topology placement requirements for the training job.
// When set with a mode other than Disabled, the operator includes TopologyRequest
// on Workload PodSets, enabling Kueue's TopologyAwareScheduling.
type TopologySpec struct {
	// Mode sets the topology placement mode for the worker pod set.
	// Disabled: no topology request (Phase 3 behavior).
	// Required: topology level must be satisfied; admission fails without placement.
	// Preferred: topology level should be satisfied on best-effort basis.
	// Unconstrained: topology-aware scheduling is active but Kueue may place freely.
	// +kubebuilder:validation:Enum=Disabled;Required;Preferred;Unconstrained
	Mode TopologyMode `json:"mode"`

	// TopologyLevel is the node label key used as the topology domain
	// (e.g., "topology.kubernetes.io/zone", "kubernetes.io/hostname", or a custom rack label).
	// Required when mode is Required or Preferred. Ignored when mode is Disabled.
	// +optional
	TopologyLevel string `json:"topologyLevel,omitempty"`

	// LeaderWorkerColocation requests that the leader pod (if present in the
	// template as a separate replicatedJob) is co-located in the same topology
	// domain as workers. Only meaningful when mode is Required or Preferred
	// and the template has a separate leader replicatedJob.
	// Default: false.
	// +optional
	LeaderWorkerColocation bool `json:"leaderWorkerColocation,omitempty"`
}

// ResumableTrainingJobSpec defines the desired state of ResumableTrainingJob.
type ResumableTrainingJobSpec struct {
	// Suspend is the Kueue-facing admission gate used by the external jobframework integration.
	// When true, the RTJ must not start or continue a runtime attempt until Kueue clears suspension.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// QueueName is the Kueue queue the workload targets.
	// +kubebuilder:validation:MinLength=1
	QueueName string `json:"queueName"`

	// WorkloadPriorityClassName is the workload priority class used by Kueue.
	// +kubebuilder:validation:MinLength=1
	WorkloadPriorityClassName string `json:"workloadPriorityClassName"`

	// Identity carries strict resume-compatibility identity fields from Phase 0.
	Identity ResumableTrainingJobIdentity `json:"identity"`

	// Runtime carries the in-scope runtime settings and embedded JobSet template.
	Runtime ResumableTrainingJobRuntime `json:"runtime"`

	// Checkpoint defines the checkpoint policy for the training lineage.
	Checkpoint CheckpointPolicy `json:"checkpoint"`

	// Resume defines restore selection and retry behavior.
	Resume ResumePolicy `json:"resume"`

	// Parallelism configures the scalable worker group, minimum counts for partial
	// admission, and per-job partial-admission opt-in. When nil, the controller derives
	// worker count from spec.identity.worldSize with no partial admission (Phase 2 behavior).
	// +optional
	Parallelism *ParallelismSpec `json:"parallelism,omitempty"`

	// Topology declares topology placement requirements for the training job.
	// When nil, topology-aware scheduling is disabled (Phase 3 behavior preserved).
	// +optional
	Topology *TopologySpec `json:"topology,omitempty"`

	// PriorityPolicyRef is an optional reference to a cluster-scoped
	// CheckpointPriorityPolicy that configures checkpoint-aware priority
	// shaping for this RTJ. When nil, no priority shaping is applied and
	// the RTJ retains its base WorkloadPriorityClass priority throughout
	// its lifetime (Phase 4 behavior preserved).
	// +optional
	PriorityPolicyRef *PriorityPolicyReference `json:"priorityPolicyRef,omitempty"`

	// ManagedBy identifies the external controller responsible for this RTJ's
	// Workload lifecycle. When set to "kueue.x-k8s.io/multikueue", the RTJ
	// is eligible for MultiKueue dispatch to a remote worker cluster.
	// When empty or absent, the RTJ follows the single-cluster Phase 5 path.
	// This field is user-authored and immutable once set.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ManagedBy string `json:"managedBy,omitempty"`

	// Control carries the declarative manual pause or resume intent.
	Control *ControlSpec `json:"control,omitempty"`
}

// ResumableTrainingJobIdentity captures fields that must match on resume.
type ResumableTrainingJobIdentity struct {
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`
	// +kubebuilder:validation:MinLength=1
	CodeVersion string `json:"codeVersion"`
	// +kubebuilder:validation:Minimum=1
	WorldSize int32 `json:"worldSize"`
	// +kubebuilder:validation:MinLength=1
	GPUShape string `json:"gpuShape"`
}

// ResumableTrainingJobRuntime describes the training runtime.
type ResumableTrainingJobRuntime struct {
	Mode RuntimeMode `json:"mode"`
	// +kubebuilder:validation:MinLength=1
	OptimizerMode string `json:"optimizerMode"`
	// +kubebuilder:validation:MinLength=1
	ShardingMode string         `json:"shardingMode"`
	Template     JobSetTemplate `json:"template"`
}

// JobSetTemplate is the smallest practical embedded template form for Phase 1.
type JobSetTemplate struct {
	// APIVersion defaults to the supported JobSet apiVersion.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind defaults to JobSet.
	// +optional
	Kind string `json:"kind,omitempty"`
	// Metadata carries labels and annotations propagated to the child JobSet.
	// +optional
	Metadata *EmbeddedObjectMetadata `json:"metadata,omitempty"`
	// Spec is an embedded JobSet spec payload.
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Spec runtime.RawExtension `json:"spec"`
}

// EmbeddedObjectMetadata is a narrow embedded metadata helper.
type EmbeddedObjectMetadata struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CheckpointPolicy defines checkpoint cadence and drain behavior.
type CheckpointPolicy struct {
	// +kubebuilder:validation:MinLength=1
	StorageURI string          `json:"storageURI"`
	Interval   metav1.Duration `json:"interval"`
	// FreshnessBudget is the maximum age for the latest completed checkpoint while healthy.
	FreshnessBudget metav1.Duration `json:"freshnessBudget"`
	// MaxDrainTime is the maximum bounded graceful drain window.
	MaxDrainTime metav1.Duration `json:"maxDrainTime"`
	// SafePointMode is fixed to StepBoundary in Phase 1.
	// +optional
	SafePointMode SafePointMode `json:"safePointMode,omitempty"`
}

// ResumePolicy defines restore selection and bounded retries.
type ResumePolicy struct {
	// SourcePolicy is fixed to LatestCompatibleComplete in Phase 1.
	// +optional
	SourcePolicy ResumeSourcePolicy `json:"sourcePolicy,omitempty"`
	// +kubebuilder:validation:Minimum=1
	MaxResumeRetries int32 `json:"maxResumeRetries"`

	// AllowWorldSizeChange permits resuming from a checkpoint that was saved at a
	// different world size than the current admitted world size. When true, the
	// trainer must use PyTorch DCP resharding to adapt the checkpoint to the new
	// world size. All other compatibility dimensions remain strict.
	// Default: false (Phase 2 exact-match behavior preserved).
	// +optional
	AllowWorldSizeChange bool `json:"allowWorldSizeChange,omitempty"`
}

// ParallelismSpec configures the scalable worker group and partial admission.
// The leader role (if any) is always fixed-size; only the worker pod set is scalable.
type ParallelismSpec struct {
	// PreferredCount is the desired number of worker pods for Kueue admission.
	// Maps to PodSet.Count for the worker pod set. When zero or unset, defaults
	// to spec.identity.worldSize (backward compatibility with Phase 2).
	// +optional
	// +kubebuilder:validation:Minimum=1
	PreferredCount int32 `json:"preferredCount,omitempty"`

	// MinCount is the minimum acceptable worker pods for partial admission.
	// Only effective when EnablePartialAdmission is true.
	// Maps to PodSet.MinCount for the worker pod set.
	// Must be >= 1 and <= PreferredCount (or <= spec.identity.worldSize when
	// PreferredCount is unset).
	// +optional
	// +kubebuilder:validation:Minimum=1
	MinCount *int32 `json:"minCount,omitempty"`

	// PodSetName identifies the scalable worker replicatedJob in the embedded
	// JobSet template. Defaults to the first replicatedJob name if not set.
	// Any other replicatedJobs are treated as fixed-size leaders.
	// +optional
	PodSetName string `json:"podSetName,omitempty"`

	// EnablePartialAdmission enables Kueue partial admission for this RTJ.
	// When true, Kueue may admit fewer workers than PreferredCount (but >= MinCount).
	// Requires spec.resume.allowWorldSizeChange=true because partial admission
	// changes the effective world size.
	// This is an EXPERIMENTAL field and is off by default.
	// +optional
	EnablePartialAdmission bool `json:"enablePartialAdmission,omitempty"`
}

// PriorityPolicyReference is a reference to a cluster-scoped CheckpointPriorityPolicy.
type PriorityPolicyReference struct {
	// Name is the name of the CheckpointPriorityPolicy object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ControlSpec carries manual desired state.
type ControlSpec struct {
	// +optional
	DesiredState DesiredState `json:"desiredState,omitempty"`
}

// ResumableTrainingJobStatus defines the observed state of ResumableTrainingJob.
type ResumableTrainingJobStatus struct {
	// +optional
	Phase ResumableTrainingJobPhase `json:"phase,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	WorkloadReference *WorkloadReference `json:"workloadReference,omitempty"`
	// +optional
	AdmittedClusterQueue string `json:"admittedClusterQueue,omitempty"`
	// +optional
	CurrentSuspension *SuspensionStatus `json:"currentSuspension,omitempty"`
	// +optional
	CurrentRunAttempt int32 `json:"currentRunAttempt,omitempty"`
	// +optional
	PauseRequestID string `json:"pauseRequestID,omitempty"`
	// +optional
	ActiveJobSetName string `json:"activeJobSetName,omitempty"`
	// +optional
	ActiveControlConfigMapName string `json:"activeControlConfigMapName,omitempty"`
	// +optional
	SelectedCheckpoint *CheckpointReference `json:"selectedCheckpoint,omitempty"`
	// +optional
	LastCompletedCheckpoint *CheckpointReference `json:"lastCompletedCheckpoint,omitempty"`
	// +optional
	TransitionTimestamps TransitionTimestamps `json:"transitionTimestamps,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Admission captures the admitted shape from Kueue for the current or most recent admission.
	// +optional
	Admission *AdmissionStatus `json:"admission,omitempty"`

	// Restore captures details of the most recent checkpoint restore.
	// +optional
	Restore *RestoreStatus `json:"restore,omitempty"`

	// LaunchReadiness captures the current pre-launch readiness state.
	// Set by the ResumeReadiness AdmissionCheck controller when configured.
	// Nil when the readiness gate is not active or no admission is in progress.
	// +optional
	LaunchReadiness *LaunchReadinessStatus `json:"launchReadiness,omitempty"`

	// Topology records the admitted topology assignment from Kueue TAS.
	// Nil when topology is not enabled or not yet admitted with topology.
	// +optional
	Topology *TopologyStatus `json:"topology,omitempty"`

	// EffectiveLaunchShape captures the computed launch shape derived from
	// admission decisions. Nil before first admission.
	// +optional
	EffectiveLaunchShape *EffectiveLaunchShape `json:"effectiveLaunchShape,omitempty"`

	// PriorityShaping captures the checkpoint-aware priority shaping state.
	// Nil when no CheckpointPriorityPolicy is referenced or the priority
	// shaping controller has not yet evaluated this RTJ.
	// +optional
	PriorityShaping *PriorityShapingStatus `json:"priorityShaping,omitempty"`

	// LaunchGate captures the aggregate launch gate evaluation state.
	// The launch gate determines when the controller may render the child
	// JobSet. Nil before the first admission attempt.
	// All fields are controller-owned.
	// +optional
	LaunchGate *LaunchGateStatus `json:"launchGate,omitempty"`

	// Provisioning captures the state of the ProvisioningRequest
	// AdmissionCheck gate. Nil when no ProvisioningRequest AC is configured
	// or when the controller has not yet evaluated the gate.
	// All fields are controller-owned.
	// +optional
	Provisioning *ProvisioningStatus `json:"provisioning,omitempty"`

	// StartupRecovery captures the startup and recovery lifecycle state
	// of the child runtime, including eviction reasons from
	// waitForPodsReady. Nil before the first launch attempt.
	// All fields are controller-owned.
	// +optional
	StartupRecovery *StartupRecoveryStatus `json:"startupRecovery,omitempty"`

	// Capacity captures whether a physical capacity guarantee is active
	// for this RTJ. This is a derived indicator computed from admission
	// and provisioning state. Nil before first evaluation.
	// All fields are controller-owned.
	// +optional
	Capacity *CapacityStatus `json:"capacity,omitempty"`

	// MultiCluster captures the manager-side view of multi-cluster dispatch.
	// Populated only when the RTJ is managed by MultiKueue (spec.managedBy is
	// set to the MultiKueue controller value). Nil in single-cluster mode.
	// All fields are controller-owned; users must not write to this section.
	// +optional
	MultiCluster *MultiClusterStatus `json:"multiCluster,omitempty"`
}

// AdmissionStatus captures the admitted shape from Kueue.
type AdmissionStatus struct {
	// AdmittedWorkerCount is the number of worker pods admitted by Kueue for
	// the scalable worker pod set. Zero when not yet admitted.
	// +optional
	AdmittedWorkerCount int32 `json:"admittedWorkerCount,omitempty"`

	// PreferredWorkerCount mirrors the effective preferred count at admission time
	// (from spec.parallelism.preferredCount or spec.identity.worldSize).
	// +optional
	PreferredWorkerCount int32 `json:"preferredWorkerCount,omitempty"`

	// ActiveWorkerCount is the observed number of running worker pods.
	// Zero when no runtime is active.
	// +optional
	ActiveWorkerCount int32 `json:"activeWorkerCount,omitempty"`

	// AdmittedFlavors maps pod set name to the ResourceFlavor name assigned by Kueue.
	// +optional
	AdmittedFlavors map[string]string `json:"admittedFlavors,omitempty"`
}

// RestoreStatus captures details of the most recent checkpoint restore.
type RestoreStatus struct {
	// LastCheckpointWorldSize is the world size recorded in the checkpoint
	// manifest that was used for the most recent restore.
	// +optional
	LastCheckpointWorldSize int32 `json:"lastCheckpointWorldSize,omitempty"`

	// LastRestoreWorldSize is the effective world size at which the most
	// recent restore was launched (from the admitted worker count).
	// +optional
	LastRestoreWorldSize int32 `json:"lastRestoreWorldSize,omitempty"`

	// RestoreMode indicates whether the most recent restore was same-size
	// or required DCP resharding due to a world-size change.
	// +optional
	RestoreMode RestoreMode `json:"restoreMode,omitempty"`
}

// LaunchReadinessStatus summarizes the pre-launch readiness state.
// Populated by the ResumeReadiness AdmissionCheck controller when it is
// configured on the ClusterQueue. Nil when the readiness gate is not active.
type LaunchReadinessStatus struct {
	// Ready indicates whether all pre-launch gates have passed and the RTJ
	// is ready to launch a child JobSet.
	Ready bool `json:"ready"`

	// GateState describes the current state of the readiness gate.
	// +optional
	GateState ReadinessGateState `json:"gateState,omitempty"`

	// Reason is a machine-readable reason for the current gate state.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is a human-readable explanation of the current gate state.
	// +optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime is when the readiness state last changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// TopologyStatus records the topology assignment from Kueue admission.
// Nil when topology is not enabled or the workload is not yet admitted
// with a topology assignment.
type TopologyStatus struct {
	// Levels records the topology level keys from the assignment
	// (e.g., ["topology.kubernetes.io/zone"]).
	// +optional
	Levels []string `json:"levels,omitempty"`

	// Domains records the assigned topology domains with pod counts.
	// +optional
	Domains []TopologyDomainStatus `json:"domains,omitempty"`
}

// TopologyDomainStatus records a single assigned topology domain.
type TopologyDomainStatus struct {
	// Values are the topology domain values for each level in the Levels list.
	Values []string `json:"values,omitempty"`

	// Count is the number of pods assigned to this domain.
	Count int32 `json:"count,omitempty"`
}

// EffectiveLaunchShape captures the computed shape that will be or was used
// for the current/next launch attempt. Derived from admission and spec.
type EffectiveLaunchShape struct {
	// WorkerCount is the effective number of worker pods for this launch.
	// +optional
	WorkerCount int32 `json:"workerCount,omitempty"`

	// WorldSize is the effective world size for this launch
	// (may differ from spec.identity.worldSize under partial admission).
	// +optional
	WorldSize int32 `json:"worldSize,omitempty"`

	// ResumeMode indicates whether this launch is a fresh start or checkpoint restore.
	// Empty on first launch.
	// +optional
	ResumeMode RestoreMode `json:"resumeMode,omitempty"`

	// SelectedCheckpointID is the ID of the checkpoint selected for this launch.
	// Empty on first launch (no checkpoint to restore).
	// +optional
	SelectedCheckpointID string `json:"selectedCheckpointID,omitempty"`
}

// LaunchGateStatus captures the aggregate launch gate evaluation state.
// The launch gate is the decision point where the RTJ operator transitions
// from Admitted to Starting and renders the child JobSet.
type LaunchGateStatus struct {
	// State is the aggregate launch gate state.
	// Open: all prerequisites satisfied, child JobSet may be rendered.
	// Blocked: one or more prerequisites not satisfied.
	// Unknown: gate state could not be determined.
	// +optional
	State LaunchGateState `json:"launchGateState,omitempty"`

	// Reason is a machine-readable reason for the current gate state.
	// Examples: "AllChecksPassed", "AdmissionCheckPending",
	// "TopologyNotAssigned", "Suspended".
	// +optional
	Reason string `json:"launchGateReason,omitempty"`

	// Message is a human-readable explanation of the current gate state.
	// +optional
	Message string `json:"message,omitempty"`

	// AdmissionCheckSummary summarizes the state of all AdmissionChecks
	// on the Workload. Each entry maps the check name to its state.
	// Empty when no AdmissionChecks are configured (Phase 6 fail-open).
	// +optional
	AdmissionCheckSummary map[string]AdmissionCheckState `json:"admissionCheckSummary,omitempty"`

	// TopologyGateState describes whether the topology assignment
	// prerequisite is satisfied. NotConfigured when topology is disabled.
	// +optional
	TopologyGateState TopologyGateState `json:"topologyGateState,omitempty"`

	// LastTransitionTime is when the launch gate state last changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// ProvisioningStatus captures the state of the ProvisioningRequest
// AdmissionCheck gate for this RTJ. Derived from the Workload's
// AdmissionCheck state, not from ProvisioningRequest resources directly.
type ProvisioningStatus struct {
	// State is the current provisioning gate state.
	// NotConfigured: no ProvisioningRequest AC on the ClusterQueue.
	// Pending: ProvisioningRequest created, backend processing.
	// Provisioned: backend confirmed physical capacity available.
	// Failed: backend rejected the provisioning request.
	// +optional
	State ProvisioningState `json:"provisioningState,omitempty"`

	// ProvisioningRequestRef is a reference to the ProvisioningRequest
	// resource created by Kueue for this workload. Nil when provisioning
	// is not configured or the request has not been created yet.
	// +optional
	ProvisioningRequestRef *ProvisioningRequestReference `json:"provisioningRequestRef,omitempty"`

	// Attempt is the number of provisioning attempts observed.
	// Incremented each time the workload is re-admitted and a new
	// ProvisioningRequest cycle begins.
	// +optional
	Attempt int32 `json:"provisioningAttempt,omitempty"`

	// Reason is a machine-readable reason for the current state.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is a human-readable explanation of the current state.
	// +optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime is when the provisioning state last changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"provisioningLastTransitionTime,omitempty"`
}

// ProvisioningRequestReference identifies the ProvisioningRequest resource
// created by Kueue for this workload's provisioning gate.
type ProvisioningRequestReference struct {
	// Name is the name of the ProvisioningRequest.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the namespace of the ProvisioningRequest.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// StartupRecoveryStatus captures the startup and recovery lifecycle of the
// child runtime. Integrates with Kueue's waitForPodsReady eviction signals.
type StartupRecoveryStatus struct {
	// StartupState is the current startup/recovery state.
	// +optional
	StartupState StartupState `json:"startupState,omitempty"`

	// PodsReadyState is the derived pod readiness indicator.
	// +optional
	PodsReadyState PodsReadyState `json:"podsReadyState,omitempty"`

	// LastLaunchFailureReason is the machine-readable reason for the most
	// recent launch failure (e.g., "ImagePullBackOff", "Unschedulable").
	// Empty when no launch failure has occurred.
	// +optional
	LastLaunchFailureReason string `json:"lastLaunchFailureReason,omitempty"`

	// LastEvictionReason is the machine-readable reason for the most recent
	// Kueue eviction (e.g., "PodsReadyTimeout", "Preempted",
	// "InactiveWorkload"). Empty when no eviction has occurred.
	// +optional
	LastEvictionReason string `json:"lastEvictionReason,omitempty"`

	// LastRequeueReason is the machine-readable reason for the most recent
	// requeue after eviction. Empty when no requeue has occurred.
	// +optional
	LastRequeueReason string `json:"lastRequeueReason,omitempty"`

	// LastTransitionTime is when the startup state last changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// CapacityStatus is a derived indicator summarizing whether a physical
// capacity guarantee is active for this RTJ. Computed from admission
// and provisioning state.
type CapacityStatus struct {
	// GuaranteeActive is true when the RTJ has been admitted with a
	// ProvisioningRequest that has been satisfied, meaning physical
	// capacity (not just quota) has been confirmed for this workload.
	// False when provisioning is not configured or not yet satisfied.
	GuaranteeActive bool `json:"guaranteeActive"`

	// Reason is a machine-readable reason for the current state.
	// Examples: "ProvisioningSatisfied", "QuotaOnlyAdmission",
	// "ProvisioningPending", "NotAdmitted".
	// +optional
	Reason string `json:"reason,omitempty"`
}

// MultiClusterStatus captures the manager-side view of multi-cluster dispatch.
// All fields are controller-owned and populated by the manager-mode reconciler
// based on information mirrored from the remote worker-side RTJ via MultiKueue.
type MultiClusterStatus struct {
	// DispatchPhase is the high-level multi-cluster dispatch state.
	// +optional
	DispatchPhase MultiClusterDispatchPhase `json:"dispatchPhase,omitempty"`

	// NominatedClusters lists the worker clusters that MultiKueue considered
	// or is considering for dispatching this RTJ.
	// +optional
	NominatedClusters []string `json:"nominatedClusters,omitempty"`

	// ExecutionCluster is the worker cluster where the RTJ is currently
	// dispatched and executing. Empty when not yet dispatched.
	// +optional
	ExecutionCluster string `json:"executionCluster,omitempty"`

	// RemoteObjectRef points to the remote RTJ copy on the worker cluster.
	// Nil before dispatch.
	// +optional
	RemoteObjectRef *RemoteObjectReference `json:"remoteObjectRef,omitempty"`

	// RemotePhase is the phase observed on the remote worker-side RTJ.
	// Mirrors the worker's .status.phase. Empty before the worker has
	// initialized status.
	// +optional
	RemotePhase ResumableTrainingJobPhase `json:"remotePhase,omitempty"`

	// RemoteCheckpoint summarizes the latest completed checkpoint reported
	// by the remote worker. Nil before any checkpoint completes on the worker.
	// +optional
	RemoteCheckpoint *RemoteCheckpointSummary `json:"remoteCheckpoint,omitempty"`

	// RemoteObservedGeneration is the metadata.generation of the remote
	// worker-side RTJ as of the last status mirror. Used as a sync marker
	// to detect when the remote has observed a spec change.
	// +optional
	RemoteObservedGeneration int64 `json:"remoteObservedGeneration,omitempty"`

	// LocalExecutionSuppressed indicates that manager-mode controller has
	// suppressed local child JobSet creation because this RTJ is managed
	// by MultiKueue. Always true when spec.managedBy is set to the
	// MultiKueue value and the operator is running in manager mode.
	// +optional
	LocalExecutionSuppressed bool `json:"localExecutionSuppressed,omitempty"`
}

// RemoteObjectReference identifies the remote RTJ copy on a worker cluster.
type RemoteObjectReference struct {
	// Cluster is the name of the worker cluster hosting the remote copy.
	// +kubebuilder:validation:MinLength=1
	Cluster string `json:"cluster"`

	// Namespace is the namespace of the remote RTJ on the worker cluster.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the remote RTJ on the worker cluster.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// UID is the UID of the remote RTJ on the worker cluster.
	// +optional
	UID string `json:"uid,omitempty"`
}

// RemoteCheckpointSummary summarizes the latest checkpoint from a remote worker.
// This is a lightweight mirror of the worker's .status.lastCompletedCheckpoint
// to avoid embedding the full CheckpointReference on the manager.
type RemoteCheckpointSummary struct {
	// LastCompletedCheckpointID is the checkpoint ID from the worker's
	// .status.lastCompletedCheckpoint.id.
	// +optional
	LastCompletedCheckpointID string `json:"lastCompletedCheckpointID,omitempty"`

	// LastCompletedCheckpointTime is the completion timestamp from the worker's
	// .status.lastCompletedCheckpoint.completionTime.
	// +optional
	LastCompletedCheckpointTime *metav1.Time `json:"lastCompletedCheckpointTime,omitempty"`

	// StorageURI is the storage URI of the checkpoint from the worker's
	// .status.lastCompletedCheckpoint.storageURI.
	// +optional
	StorageURI string `json:"storageURI,omitempty"`
}

// WorkloadReference points at the Kueue Workload owned for the RTJ.
type WorkloadReference struct {
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// +optional
	Kind string `json:"kind,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// SuspensionStatus captures the current suspension source and reason.
type SuspensionStatus struct {
	Suspended bool `json:"suspended"`
	// +optional
	Source SuspensionSource `json:"source,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	ObservedAt *metav1.Time `json:"observedAt,omitempty"`
}

// CheckpointReference is the controller-published checkpoint reference shape.
type CheckpointReference struct {
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`
	// +kubebuilder:validation:MinLength=1
	StorageURI string `json:"storageURI"`
	// +optional
	ManifestURI string `json:"manifestURI,omitempty"`
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// +optional
	SourceRunAttempt int32 `json:"sourceRunAttempt,omitempty"`
	// +optional
	CompatibilityState CompatibilityState `json:"compatibilityState,omitempty"`
	// +optional
	CompatibilityReason string `json:"compatibilityReason,omitempty"`
	// WorldSize is the world size recorded in the checkpoint manifest.
	// Added in Phase 3 for world-size-flexible resume observability.
	// +optional
	WorldSize int32 `json:"worldSize,omitempty"`
}

// TransitionTimestamps captures major lifecycle timestamps.
type TransitionTimestamps struct {
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
	// +optional
	QueuedAt *metav1.Time `json:"queuedAt,omitempty"`
	// +optional
	AdmittedAt *metav1.Time `json:"admittedAt,omitempty"`
	// +optional
	StartingAt *metav1.Time `json:"startingAt,omitempty"`
	// +optional
	RunningAt *metav1.Time `json:"runningAt,omitempty"`
	// +optional
	YieldRequestedAt *metav1.Time `json:"yieldRequestedAt,omitempty"`
	// +optional
	DrainingAt *metav1.Time `json:"drainingAt,omitempty"`
	// +optional
	LastCheckpointCompletedAt *metav1.Time `json:"lastCheckpointCompletedAt,omitempty"`
	// +optional
	PausedAt *metav1.Time `json:"pausedAt,omitempty"`
	// +optional
	RestoringAt *metav1.Time `json:"restoringAt,omitempty"`
	// +optional
	RestoreCompletedAt *metav1.Time `json:"restoreCompletedAt,omitempty"`
	// +optional
	SucceededAt *metav1.Time `json:"succeededAt,omitempty"`
	// +optional
	FailedAt *metav1.Time `json:"failedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=rtj
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Desired",type=string,JSONPath=`.spec.control.desiredState`
// +kubebuilder:printcolumn:name="Suspended",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Queue",type=string,JSONPath=`.spec.queueName`
// +kubebuilder:printcolumn:name="Attempt",type=integer,JSONPath=`.status.currentRunAttempt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ResumableTrainingJob is the Schema for the resumabletrainingjobs API.
type ResumableTrainingJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResumableTrainingJobSpec   `json:"spec,omitempty"`
	Status ResumableTrainingJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResumableTrainingJobList contains a list of ResumableTrainingJob.
type ResumableTrainingJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResumableTrainingJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResumableTrainingJob{}, &ResumableTrainingJobList{})
}

// Default applies default values that do not need an admission webhook.
func (r *ResumableTrainingJob) Default() {
	if r.Spec.Control == nil {
		r.Spec.Control = &ControlSpec{}
	}
	if r.Spec.Control.DesiredState == "" {
		r.Spec.Control.DesiredState = DefaultDesiredState
	}
	if r.Spec.Checkpoint.SafePointMode == "" {
		r.Spec.Checkpoint.SafePointMode = DefaultSafePointMode
	}
	if r.Spec.Resume.SourcePolicy == "" {
		r.Spec.Resume.SourcePolicy = DefaultResumeSourcePolicy
	}
	if r.Spec.Resume.MaxResumeRetries == 0 {
		r.Spec.Resume.MaxResumeRetries = DefaultMaxResumeRetries
	}
	if r.Spec.Runtime.Template.APIVersion == "" {
		r.Spec.Runtime.Template.APIVersion = DefaultJobSetAPIVersion
	}
	if r.Spec.Runtime.Template.Kind == "" {
		r.Spec.Runtime.Template.Kind = DefaultJobSetKind
	}
	// Phase 4: default topology mode when topology is set but mode is empty.
	if r.Spec.Topology != nil && r.Spec.Topology.Mode == "" {
		r.Spec.Topology.Mode = DefaultTopologyMode
	}
	r.projectKueueLabels()
}

// EffectivePreferredCount returns the effective preferred worker count, falling back
// to spec.identity.worldSize when spec.parallelism is nil or preferredCount is zero.
func (r *ResumableTrainingJob) EffectivePreferredCount() int32 {
	if r.Spec.Parallelism != nil && r.Spec.Parallelism.PreferredCount > 0 {
		return r.Spec.Parallelism.PreferredCount
	}
	return r.Spec.Identity.WorldSize
}

// EffectiveMinCount returns the effective minimum worker count for partial admission.
// Returns nil when partial admission is not configured.
func (r *ResumableTrainingJob) EffectiveMinCount() *int32 {
	if r.Spec.Parallelism != nil && r.Spec.Parallelism.EnablePartialAdmission && r.Spec.Parallelism.MinCount != nil {
		return r.Spec.Parallelism.MinCount
	}
	return nil
}

// EffectivePodSetName returns the scalable worker pod set name. Defaults to empty
// which the controller interprets as the first replicatedJob.
func (r *ResumableTrainingJob) EffectivePodSetName() string {
	if r.Spec.Parallelism != nil && r.Spec.Parallelism.PodSetName != "" {
		return r.Spec.Parallelism.PodSetName
	}
	return ""
}

// ValidateCreate validates the Phase 1 subset API contract.
func (r *ResumableTrainingJob) ValidateCreate() error {
	return r.validate()
}

// ValidateUpdate validates updates against the same Phase 1 subset rules.
func (r *ResumableTrainingJob) ValidateUpdate(_ runtime.Object) error {
	return r.validate()
}

// ValidateDelete allows deletes without extra validation.
func (r *ResumableTrainingJob) ValidateDelete() error {
	return nil
}

func (r *ResumableTrainingJob) validate() error {
	allErrs := r.validationErrors()
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("ResumableTrainingJob").GroupKind(), r.Name, allErrs)
}

func (r *ResumableTrainingJob) validationErrors() field.ErrorList {
	var allErrs field.ErrorList

	if strings.TrimSpace(r.Spec.QueueName) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "queueName"), "queueName is required"))
	}
	if strings.TrimSpace(r.Spec.WorkloadPriorityClassName) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "workloadPriorityClassName"), "workloadPriorityClassName is required"))
	}
	if strings.TrimSpace(r.Spec.Identity.Image) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "identity", "image"), "image is required"))
	}
	if strings.TrimSpace(r.Spec.Identity.CodeVersion) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "identity", "codeVersion"), "codeVersion is required"))
	}
	if r.Spec.Identity.WorldSize < 1 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "identity", "worldSize"), r.Spec.Identity.WorldSize, "worldSize must be greater than zero"))
	}
	if strings.TrimSpace(r.Spec.Identity.GPUShape) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "identity", "gpuShape"), "gpuShape is required"))
	}
	if r.Spec.Runtime.Mode != RuntimeModeDDP && r.Spec.Runtime.Mode != RuntimeModeFSDP {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "runtime", "mode"), r.Spec.Runtime.Mode, []string{string(RuntimeModeDDP), string(RuntimeModeFSDP)}))
	}
	if strings.TrimSpace(r.Spec.Runtime.OptimizerMode) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "runtime", "optimizerMode"), "optimizerMode is required"))
	}
	if strings.TrimSpace(r.Spec.Runtime.ShardingMode) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "runtime", "shardingMode"), "shardingMode is required"))
	}
	if strings.TrimSpace(r.Spec.Runtime.Template.APIVersion) == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "runtime", "template", "apiVersion"), "apiVersion is required"))
	}
	if r.Spec.Runtime.Template.Kind != DefaultJobSetKind {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "runtime", "template", "kind"), r.Spec.Runtime.Template.Kind, []string{DefaultJobSetKind}))
	}
	if len(trimRawExtension(r.Spec.Runtime.Template.Spec)) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "runtime", "template", "spec"), "embedded JobSet spec is required"))
	} else if !json.Valid(trimRawExtension(r.Spec.Runtime.Template.Spec)) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "runtime", "template", "spec"), string(trimRawExtension(r.Spec.Runtime.Template.Spec)), "embedded JobSet spec must be valid JSON"))
	}

	if !strings.HasPrefix(r.Spec.Checkpoint.StorageURI, "s3://") {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "checkpoint", "storageURI"), r.Spec.Checkpoint.StorageURI, "storageURI must use an s3:// prefix"))
	}
	if r.Spec.Checkpoint.Interval.Duration <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "checkpoint", "interval"), r.Spec.Checkpoint.Interval.Duration.String(), "interval must be greater than zero"))
	}
	if r.Spec.Checkpoint.FreshnessBudget.Duration <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "checkpoint", "freshnessBudget"), r.Spec.Checkpoint.FreshnessBudget.Duration.String(), "freshnessBudget must be greater than zero"))
	}
	if r.Spec.Checkpoint.MaxDrainTime.Duration <= 0 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "checkpoint", "maxDrainTime"), r.Spec.Checkpoint.MaxDrainTime.Duration.String(), "maxDrainTime must be greater than zero"))
	}
	if r.Spec.Checkpoint.FreshnessBudget.Duration > 0 && r.Spec.Checkpoint.Interval.Duration > 0 &&
		r.Spec.Checkpoint.FreshnessBudget.Duration < r.Spec.Checkpoint.Interval.Duration {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "checkpoint", "freshnessBudget"), r.Spec.Checkpoint.FreshnessBudget.Duration.String(), "freshnessBudget must be greater than or equal to interval"))
	}
	if r.Spec.Checkpoint.SafePointMode != SafePointModeStepBoundary {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "checkpoint", "safePointMode"), r.Spec.Checkpoint.SafePointMode, []string{string(SafePointModeStepBoundary)}))
	}

	if r.Spec.Resume.SourcePolicy != ResumeSourcePolicyLatestCompatibleComplete {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "resume", "sourcePolicy"), r.Spec.Resume.SourcePolicy, []string{string(ResumeSourcePolicyLatestCompatibleComplete)}))
	}
	if r.Spec.Resume.MaxResumeRetries < 1 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "resume", "maxResumeRetries"), r.Spec.Resume.MaxResumeRetries, "maxResumeRetries must be greater than zero"))
	}

	if r.Spec.Suspend != nil && *r.Spec.Suspend && r.Spec.Control != nil && r.Spec.Control.DesiredState == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "control", "desiredState"), "desiredState must be explicit when suspend is set"))
	}
	if r.Spec.Control != nil && r.Spec.Control.DesiredState != DesiredStateRunning && r.Spec.Control.DesiredState != DesiredStatePaused {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "control", "desiredState"), r.Spec.Control.DesiredState, []string{string(DesiredStateRunning), string(DesiredStatePaused)}))
	}

	// Phase 3: parallelism validation
	allErrs = append(allErrs, r.validateParallelism()...)

	// Phase 4: topology validation
	allErrs = append(allErrs, r.validateTopology()...)

	// Phase 5: priority policy ref validation
	allErrs = append(allErrs, r.validatePriorityPolicyRef()...)

	// Phase 6: managedBy validation
	allErrs = append(allErrs, r.validateManagedBy()...)

	return allErrs
}

func (r *ResumableTrainingJob) validateParallelism() field.ErrorList {
	var allErrs field.ErrorList
	p := r.Spec.Parallelism
	if p == nil {
		return allErrs
	}
	fldPath := field.NewPath("spec", "parallelism")

	effectivePreferred := r.EffectivePreferredCount()

	if p.PreferredCount < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("preferredCount"), p.PreferredCount, "preferredCount must be >= 0 (0 means use identity.worldSize)"))
	}

	if p.MinCount != nil {
		mc := *p.MinCount
		if mc < 1 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("minCount"), mc, "minCount must be >= 1"))
		}
		if mc > effectivePreferred {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("minCount"), mc,
				fmt.Sprintf("minCount must be <= preferredCount (effective: %d)", effectivePreferred)))
		}
	}

	if p.EnablePartialAdmission {
		if !r.Spec.Resume.AllowWorldSizeChange {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("enablePartialAdmission"),
				"enablePartialAdmission requires spec.resume.allowWorldSizeChange=true"))
		}
		if p.MinCount == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("minCount"),
				"minCount is required when enablePartialAdmission is true"))
		}
	}

	return allErrs
}

func (r *ResumableTrainingJob) validateTopology() field.ErrorList {
	var allErrs field.ErrorList
	t := r.Spec.Topology
	if t == nil {
		return allErrs
	}
	fldPath := field.NewPath("spec", "topology")

	switch t.Mode {
	case TopologyModeDisabled, TopologyModeUnconstrained:
		// TopologyLevel is optional for Disabled and Unconstrained.
	case TopologyModeRequired, TopologyModePreferred:
		if strings.TrimSpace(t.TopologyLevel) == "" {
			allErrs = append(allErrs, field.Required(fldPath.Child("topologyLevel"),
				"topologyLevel is required when mode is Required or Preferred"))
		}
	default:
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("mode"), t.Mode,
			[]string{string(TopologyModeDisabled), string(TopologyModeRequired),
				string(TopologyModePreferred), string(TopologyModeUnconstrained)}))
	}

	// LeaderWorkerColocation is only meaningful when topology is active.
	if t.LeaderWorkerColocation && (t.Mode == TopologyModeDisabled) {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("leaderWorkerColocation"),
			"leaderWorkerColocation requires topology mode to be Required, Preferred, or Unconstrained"))
	}

	return allErrs
}

func (r *ResumableTrainingJob) validatePriorityPolicyRef() field.ErrorList {
	var allErrs field.ErrorList
	ref := r.Spec.PriorityPolicyRef
	if ref == nil {
		return allErrs
	}
	fldPath := field.NewPath("spec", "priorityPolicyRef")

	if strings.TrimSpace(ref.Name) == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"),
			"name is required when priorityPolicyRef is set"))
	}

	return allErrs
}

func (r *ResumableTrainingJob) validateManagedBy() field.ErrorList {
	var allErrs field.ErrorList
	mb := r.Spec.ManagedBy
	if mb == "" {
		return allErrs
	}
	fldPath := field.NewPath("spec", "managedBy")

	if len(mb) > MaxManagedByLength {
		allErrs = append(allErrs, field.TooLong(fldPath, mb, MaxManagedByLength))
	}
	if !strings.Contains(mb, "/") {
		allErrs = append(allErrs, field.Invalid(fldPath, mb,
			"managedBy must be a domain-prefixed value containing '/' (e.g., kueue.x-k8s.io/multikueue)"))
	}

	return allErrs
}

// IsManagedByMultiKueue returns true when the RTJ is managed by MultiKueue.
func (r *ResumableTrainingJob) IsManagedByMultiKueue() bool {
	return r.Spec.ManagedBy == MultiKueueControllerName
}

// IsPriorityShapingEnabled returns true when a CheckpointPriorityPolicy is referenced.
func (r *ResumableTrainingJob) IsPriorityShapingEnabled() bool {
	return r.Spec.PriorityPolicyRef != nil && strings.TrimSpace(r.Spec.PriorityPolicyRef.Name) != ""
}

// IsTopologyEnabled returns true when topology-aware scheduling is active.
func (r *ResumableTrainingJob) IsTopologyEnabled() bool {
	return r.Spec.Topology != nil && r.Spec.Topology.Mode != TopologyModeDisabled
}

func trimRawExtension(raw runtime.RawExtension) []byte {
	return []byte(strings.TrimSpace(string(raw.Raw)))
}

func (r *ResumableTrainingJob) projectKueueLabels() {
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	if strings.TrimSpace(r.Spec.QueueName) != "" {
		r.Labels[constants.QueueLabel] = r.Spec.QueueName
	} else {
		delete(r.Labels, constants.QueueLabel)
	}
	if strings.TrimSpace(r.Spec.WorkloadPriorityClassName) != "" {
		r.Labels[constants.WorkloadPriorityClassLabel] = r.Spec.WorkloadPriorityClassName
	} else {
		delete(r.Labels, constants.WorkloadPriorityClassLabel)
	}
}

func (r *ResumableTrainingJob) SyncKueueLabels() {
	r.projectKueueLabels()
}

func (r *ResumableTrainingJob) IsSuspendedForKueue() bool {
	return ptr.Deref(r.Spec.Suspend, false)
}

// InitializePhase1Status applies the minimal status initialization used by the scaffolded controller.
func (r *ResumableTrainingJob) InitializePhase1Status(now metav1.Time) bool {
	changed := false
	if r.Status.Phase == "" {
		if r.Status.SetPhase(PhasePending, ReasonControllerInitialized, MessageControllerInitialized, now) {
			changed = true
		}
	}
	if r.Status.ObservedGeneration != r.Generation {
		r.Status.ObservedGeneration = r.Generation
		changed = true
	}
	return changed
}

// SetPhase updates the dominant phase, reason, message, and transition timestamps.
func (s *ResumableTrainingJobStatus) SetPhase(phase ResumableTrainingJobPhase, reason, message string, now metav1.Time) bool {
	changed := false
	if s.Phase != phase {
		s.Phase = phase
		changed = true
		if s.TransitionTimestamps.markPhase(phase, now) {
			changed = true
		}
	} else if s.TransitionTimestamps.LastTransitionTime == nil {
		if s.TransitionTimestamps.setLastTransitionTime(now) {
			changed = true
		}
	}
	if s.Reason != reason {
		s.Reason = reason
		changed = true
	}
	if s.Message != message {
		s.Message = message
		changed = true
	}
	return changed
}

func (t *TransitionTimestamps) markPhase(phase ResumableTrainingJobPhase, now metav1.Time) bool {
	changed := t.setLastTransitionTime(now)
	switch phase {
	case PhaseQueued:
		changed = t.setTime(&t.QueuedAt, now) || changed
	case PhaseAdmitted:
		changed = t.setTime(&t.AdmittedAt, now) || changed
	case PhaseStarting:
		changed = t.setTime(&t.StartingAt, now) || changed
	case PhaseRunning:
		changed = t.setTime(&t.RunningAt, now) || changed
	case PhaseYieldRequested:
		changed = t.setTime(&t.YieldRequestedAt, now) || changed
	case PhaseDraining:
		changed = t.setTime(&t.DrainingAt, now) || changed
	case PhasePaused:
		changed = t.setTime(&t.PausedAt, now) || changed
	case PhaseRestoring:
		changed = t.setTime(&t.RestoringAt, now) || changed
	case PhaseSucceeded:
		changed = t.setTime(&t.SucceededAt, now) || changed
	case PhaseFailed:
		changed = t.setTime(&t.FailedAt, now) || changed
	}
	return changed
}

func (t *TransitionTimestamps) setLastTransitionTime(now metav1.Time) bool {
	return t.setTime(&t.LastTransitionTime, now)
}

func (t *TransitionTimestamps) setTime(dst **metav1.Time, now metav1.Time) bool {
	if *dst != nil && (*dst).Time.Equal(now.Time) {
		return false
	}
	copy := now.DeepCopy()
	*dst = copy
	return true
}

func (p ResumableTrainingJobPhase) String() string {
	return string(p)
}

func (d DesiredState) String() string {
	return string(d)
}

func (m RuntimeMode) String() string {
	return string(m)
}

func (s SafePointMode) String() string {
	return string(s)
}

func (p ResumeSourcePolicy) String() string {
	return string(p)
}

func (c CompatibilityState) String() string {
	return string(c)
}

func (t TopologyMode) String() string {
	return string(t)
}

func (g ReadinessGateState) String() string {
	return string(g)
}

func (d MultiClusterDispatchPhase) String() string {
	return string(d)
}

func (s LaunchGateState) String() string {
	return string(s)
}

func (s ProvisioningState) String() string {
	return string(s)
}

func (s StartupState) String() string {
	return string(s)
}

func (s PodsReadyState) String() string {
	return string(s)
}

func (s TopologyGateState) String() string {
	return string(s)
}

func (s AdmissionCheckState) String() string {
	return string(s)
}

func (r ResumableTrainingJob) String() string {
	return fmt.Sprintf("%s/%s", r.Namespace, r.Name)
}
