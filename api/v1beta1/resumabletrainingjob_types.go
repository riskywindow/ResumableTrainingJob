package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ---------------------------------------------------------------------------
// Constants — defaults and well-known values
// ---------------------------------------------------------------------------

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
	DefaultDeviceMode               = DeviceModeDisabled
	DefaultDeviceRequestCount int32 = 1

	// Phase 9: Elasticity defaults.
	DefaultElasticityMode      = ElasticityModeDisabled
	DefaultInPlaceShrinkPolicy = InPlaceShrinkPolicyIfSupported
	DefaultReclaimMode         = ReclaimModeReclaimablePods
	DefaultResizeState         = ResizeStateIdle
	DefaultExecutionMode       = ExecutionModeFixed

	// MultiKueueControllerName is the well-known managedBy value for Kueue MultiKueue.
	MultiKueueControllerName = "kueue.x-k8s.io/multikueue"

	MaxManagedByLength = 256
	MaxClaimNameLength = 63
)

// ---------------------------------------------------------------------------
// Enums — lifecycle phases, runtime modes, etc.
// ---------------------------------------------------------------------------

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

// DesiredState is the manual control field.
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
	TopologyModeDisabled      TopologyMode = "Disabled"
	TopologyModeRequired      TopologyMode = "Required"
	TopologyModePreferred     TopologyMode = "Preferred"
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
	LaunchGateOpen    LaunchGateState = "Open"
	LaunchGateBlocked LaunchGateState = "Blocked"
	LaunchGateUnknown LaunchGateState = "Unknown"
)

// ProvisioningState describes the state of the ProvisioningRequest AdmissionCheck gate.
// +kubebuilder:validation:Enum=NotConfigured;Pending;Provisioned;Failed
type ProvisioningState string

const (
	ProvisioningNotConfigured ProvisioningState = "NotConfigured"
	ProvisioningPending       ProvisioningState = "Pending"
	ProvisioningProvisioned   ProvisioningState = "Provisioned"
	ProvisioningFailed        ProvisioningState = "Failed"
)

// StartupState describes the startup/recovery lifecycle of the child runtime.
// +kubebuilder:validation:Enum=NotStarted;Starting;Running;StartupTimedOut;RecoveryTimedOut;Evicted
type StartupState string

const (
	StartupNotStarted       StartupState = "NotStarted"
	StartupStarting         StartupState = "Starting"
	StartupRunning          StartupState = "Running"
	StartupTimedOut         StartupState = "StartupTimedOut"
	StartupRecoveryTimedOut StartupState = "RecoveryTimedOut"
	StartupEvicted          StartupState = "Evicted"
)

// PodsReadyState describes the pod readiness state.
// +kubebuilder:validation:Enum=Unknown;PodsReady;PodsNotReady;NoRuntime
type PodsReadyState string

const (
	PodsReadyUnknown   PodsReadyState = "Unknown"
	PodsReady          PodsReadyState = "PodsReady"
	PodsNotReady       PodsReadyState = "PodsNotReady"
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
	TopologyGateNotConfigured TopologyGateState = "NotConfigured"
	TopologyGatePending       TopologyGateState = "Pending"
	TopologyGateAssigned      TopologyGateState = "Assigned"
)

// MultiClusterDispatchPhase describes the high-level multi-cluster dispatch lifecycle.
// +kubebuilder:validation:Enum=Pending;Dispatched;Active
type MultiClusterDispatchPhase string

const (
	DispatchPhasePending    MultiClusterDispatchPhase = "Pending"
	DispatchPhaseDispatched MultiClusterDispatchPhase = "Dispatched"
	DispatchPhaseActive     MultiClusterDispatchPhase = "Active"
)

// DeviceMode indicates whether DRA device requests are enabled for this RTJ.
// +kubebuilder:validation:Enum=Disabled;DRA
type DeviceMode string

const (
	DeviceModeDisabled DeviceMode = "Disabled"
	DeviceModeDRA      DeviceMode = "DRA"
)

// ClaimAllocationState describes the aggregate allocation state.
// +kubebuilder:validation:Enum=Pending;Allocated;Failed;Unknown
type ClaimAllocationState string

const (
	ClaimAllocationPending   ClaimAllocationState = "Pending"
	ClaimAllocationAllocated ClaimAllocationState = "Allocated"
	ClaimAllocationFailed    ClaimAllocationState = "Failed"
	ClaimAllocationUnknown   ClaimAllocationState = "Unknown"
)

// ---------------------------------------------------------------------------
// Phase 9 — Elasticity enums
// ---------------------------------------------------------------------------

// ElasticityMode controls whether elasticity is enabled.
// +kubebuilder:validation:Enum=Disabled;Manual
type ElasticityMode string

const (
	ElasticityModeDisabled ElasticityMode = "Disabled"
	ElasticityModeManual   ElasticityMode = "Manual"
)

// InPlaceShrinkPolicy controls the in-place shrink behavior.
// +kubebuilder:validation:Enum=IfSupported;Never
type InPlaceShrinkPolicy string

const (
	InPlaceShrinkPolicyIfSupported InPlaceShrinkPolicy = "IfSupported"
	InPlaceShrinkPolicyNever       InPlaceShrinkPolicy = "Never"
)

// ReclaimMode controls how freed quota is released during shrink.
// +kubebuilder:validation:Enum=ReclaimablePods
type ReclaimMode string

const (
	ReclaimModeReclaimablePods ReclaimMode = "ReclaimablePods"
)

// ResizeState describes the current state of a resize operation.
// +kubebuilder:validation:Enum=Idle;Pending;InProgress;Blocked;Completed;Failed
type ResizeState string

const (
	ResizeStateIdle       ResizeState = "Idle"
	ResizeStatePending    ResizeState = "Pending"
	ResizeStateInProgress ResizeState = "InProgress"
	ResizeStateBlocked    ResizeState = "Blocked"
	ResizeStateCompleted  ResizeState = "Completed"
	ResizeStateFailed     ResizeState = "Failed"
)

// ResizePath describes the resize execution path.
// +kubebuilder:validation:Enum=InPlace;CheckpointAndRelaunch
type ResizePath string

const (
	ResizePathInPlace              ResizePath = "InPlace"
	ResizePathCheckpointAndRelaunch ResizePath = "CheckpointAndRelaunch"
)

// ExecutionMode describes the current execution mode.
// +kubebuilder:validation:Enum=Fixed;Elastic
type ExecutionMode string

const (
	ExecutionModeFixed   ExecutionMode = "Fixed"
	ExecutionModeElastic ExecutionMode = "Elastic"
)

// ---------------------------------------------------------------------------
// Spec sub-types
// ---------------------------------------------------------------------------

// ElasticitySpec configures manual target-based worker-count resize.
type ElasticitySpec struct {
	// +kubebuilder:validation:Enum=Disabled;Manual
	Mode ElasticityMode `json:"mode"`

	// +optional
	// +kubebuilder:validation:Minimum=1
	TargetWorkerCount *int32 `json:"targetWorkerCount,omitempty"`

	// +optional
	// +kubebuilder:validation:Enum=IfSupported;Never
	InPlaceShrinkPolicy InPlaceShrinkPolicy `json:"inPlaceShrinkPolicy,omitempty"`

	// +optional
	// +kubebuilder:validation:Enum=ReclaimablePods
	ReclaimMode ReclaimMode `json:"reclaimMode,omitempty"`
}

// DeviceSpec declares DRA device requirements for worker pods.
type DeviceSpec struct {
	// +kubebuilder:validation:Enum=Disabled;DRA
	Mode DeviceMode `json:"mode"`

	// +optional
	Claims []DeviceClaimSpec `json:"claims,omitempty"`
}

// DeviceClaimSpec describes a single ResourceClaimTemplate.
type DeviceClaimSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`
	Name string `json:"name"`

	// +kubebuilder:validation:MinItems=1
	Containers []string `json:"containers"`

	Request DeviceRequestSpec `json:"request"`
}

// DeviceRequestSpec is a constrained subset of a DRA DeviceRequest.
type DeviceRequestSpec struct {
	// +kubebuilder:validation:MinLength=1
	DeviceClassName string `json:"deviceClassName"`

	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Count int32 `json:"count,omitempty"`

	// +optional
	Selectors []string `json:"selectors,omitempty"`
}

// TopologySpec declares topology placement requirements.
type TopologySpec struct {
	// +kubebuilder:validation:Enum=Disabled;Required;Preferred;Unconstrained
	Mode TopologyMode `json:"mode"`

	// +optional
	TopologyLevel string `json:"topologyLevel,omitempty"`

	// +optional
	LeaderWorkerColocation bool `json:"leaderWorkerColocation,omitempty"`
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

// JobSetTemplate is the embedded template form for the child JobSet.
type JobSetTemplate struct {
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// +optional
	Kind string `json:"kind,omitempty"`
	// +optional
	Metadata *EmbeddedObjectMetadata `json:"metadata,omitempty"`
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
	StorageURI      string          `json:"storageURI"`
	Interval        metav1.Duration `json:"interval"`
	FreshnessBudget metav1.Duration `json:"freshnessBudget"`
	MaxDrainTime    metav1.Duration `json:"maxDrainTime"`
	// +optional
	SafePointMode SafePointMode `json:"safePointMode,omitempty"`
}

// ResumePolicy defines restore selection and bounded retries.
type ResumePolicy struct {
	// +optional
	SourcePolicy ResumeSourcePolicy `json:"sourcePolicy,omitempty"`
	// +kubebuilder:validation:Minimum=1
	MaxResumeRetries int32 `json:"maxResumeRetries"`
	// +optional
	AllowWorldSizeChange bool `json:"allowWorldSizeChange,omitempty"`
}

// ParallelismSpec configures the scalable worker group and partial admission.
type ParallelismSpec struct {
	// +optional
	// +kubebuilder:validation:Minimum=1
	PreferredCount int32 `json:"preferredCount,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=1
	MinCount *int32 `json:"minCount,omitempty"`

	// +optional
	PodSetName string `json:"podSetName,omitempty"`

	// EnablePartialAdmission is EXPERIMENTAL and off by default.
	// +optional
	EnablePartialAdmission bool `json:"enablePartialAdmission,omitempty"`
}

// PriorityPolicyReference is a reference to a cluster-scoped CheckpointPriorityPolicy.
type PriorityPolicyReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ControlSpec carries manual desired state.
type ControlSpec struct {
	// +optional
	DesiredState DesiredState `json:"desiredState,omitempty"`
}

// ---------------------------------------------------------------------------
// ResumableTrainingJobSpec — user-authored
// ---------------------------------------------------------------------------

// ResumableTrainingJobSpec defines the desired state of ResumableTrainingJob.
type ResumableTrainingJobSpec struct {
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// +kubebuilder:validation:MinLength=1
	QueueName string `json:"queueName"`

	// +kubebuilder:validation:MinLength=1
	WorkloadPriorityClassName string `json:"workloadPriorityClassName"`

	Identity   ResumableTrainingJobIdentity `json:"identity"`
	Runtime    ResumableTrainingJobRuntime  `json:"runtime"`
	Checkpoint CheckpointPolicy            `json:"checkpoint"`
	Resume     ResumePolicy                `json:"resume"`

	// +optional
	Parallelism *ParallelismSpec `json:"parallelism,omitempty"`

	// +optional
	Topology *TopologySpec `json:"topology,omitempty"`

	// +optional
	PriorityPolicyRef *PriorityPolicyReference `json:"priorityPolicyRef,omitempty"`

	// +optional
	// +kubebuilder:validation:MaxLength=256
	ManagedBy string `json:"managedBy,omitempty"`

	// Devices is EXPERIMENTAL. When present with mode=DRA, the operator creates
	// companion ResourceClaimTemplate objects.
	// +optional
	Devices *DeviceSpec `json:"devices,omitempty"`

	// Elasticity is EXPERIMENTAL. When present with mode=Manual, the RTJ supports
	// manual target-based worker-count resize.
	// +optional
	Elasticity *ElasticitySpec `json:"elasticity,omitempty"`

	// +optional
	Control *ControlSpec `json:"control,omitempty"`
}

// ---------------------------------------------------------------------------
// Status sub-types
// ---------------------------------------------------------------------------

// AdmissionStatus captures the admitted shape from Kueue.
type AdmissionStatus struct {
	// +optional
	AdmittedWorkerCount int32 `json:"admittedWorkerCount,omitempty"`
	// +optional
	PreferredWorkerCount int32 `json:"preferredWorkerCount,omitempty"`
	// +optional
	ActiveWorkerCount int32 `json:"activeWorkerCount,omitempty"`
	// +optional
	AdmittedFlavors map[string]string `json:"admittedFlavors,omitempty"`
}

// RestoreStatus captures details of the most recent checkpoint restore.
type RestoreStatus struct {
	// +optional
	LastCheckpointWorldSize int32 `json:"lastCheckpointWorldSize,omitempty"`
	// +optional
	LastRestoreWorldSize int32 `json:"lastRestoreWorldSize,omitempty"`
	// +optional
	RestoreMode RestoreMode `json:"restoreMode,omitempty"`
}

// LaunchReadinessStatus summarizes the pre-launch readiness state.
type LaunchReadinessStatus struct {
	Ready bool `json:"ready"`
	// +optional
	GateState ReadinessGateState `json:"gateState,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// TopologyStatus records the topology assignment from Kueue admission.
type TopologyStatus struct {
	// +optional
	Levels []string `json:"levels,omitempty"`
	// +optional
	Domains []TopologyDomainStatus `json:"domains,omitempty"`
}

// TopologyDomainStatus records a single assigned topology domain.
type TopologyDomainStatus struct {
	Values []string `json:"values,omitempty"`
	Count  int32    `json:"count,omitempty"`
}

// EffectiveLaunchShape captures the computed launch shape.
type EffectiveLaunchShape struct {
	// +optional
	WorkerCount int32 `json:"workerCount,omitempty"`
	// +optional
	WorldSize int32 `json:"worldSize,omitempty"`
	// +optional
	ResumeMode RestoreMode `json:"resumeMode,omitempty"`
	// +optional
	SelectedCheckpointID string `json:"selectedCheckpointID,omitempty"`
}

// LaunchGateStatus captures the aggregate launch gate evaluation state.
type LaunchGateStatus struct {
	// +optional
	State LaunchGateState `json:"launchGateState,omitempty"`
	// +optional
	Reason string `json:"launchGateReason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	AdmissionCheckSummary map[string]AdmissionCheckState `json:"admissionCheckSummary,omitempty"`
	// +optional
	TopologyGateState TopologyGateState `json:"topologyGateState,omitempty"`
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// ProvisioningStatus captures the ProvisioningRequest AdmissionCheck gate state.
type ProvisioningStatus struct {
	// +optional
	State ProvisioningState `json:"provisioningState,omitempty"`
	// +optional
	ProvisioningRequestRef *ProvisioningRequestReference `json:"provisioningRequestRef,omitempty"`
	// +optional
	Attempt int32 `json:"provisioningAttempt,omitempty"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	LastTransitionTime *metav1.Time `json:"provisioningLastTransitionTime,omitempty"`
}

// ProvisioningRequestReference identifies a ProvisioningRequest resource.
type ProvisioningRequestReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// StartupRecoveryStatus captures the startup and recovery lifecycle.
type StartupRecoveryStatus struct {
	// +optional
	StartupState StartupState `json:"startupState,omitempty"`
	// +optional
	PodsReadyState PodsReadyState `json:"podsReadyState,omitempty"`
	// +optional
	LastLaunchFailureReason string `json:"lastLaunchFailureReason,omitempty"`
	// +optional
	LastEvictionReason string `json:"lastEvictionReason,omitempty"`
	// +optional
	LastRequeueReason string `json:"lastRequeueReason,omitempty"`
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// CapacityStatus is a derived indicator of physical capacity guarantee.
type CapacityStatus struct {
	GuaranteeActive bool `json:"guaranteeActive"`
	// +optional
	Reason string `json:"reason,omitempty"`
}

// MultiClusterStatus captures the manager-side view of multi-cluster dispatch.
type MultiClusterStatus struct {
	// +optional
	DispatchPhase MultiClusterDispatchPhase `json:"dispatchPhase,omitempty"`
	// +optional
	NominatedClusters []string `json:"nominatedClusters,omitempty"`
	// +optional
	ExecutionCluster string `json:"executionCluster,omitempty"`
	// +optional
	RemoteObjectRef *RemoteObjectReference `json:"remoteObjectRef,omitempty"`
	// +optional
	RemotePhase ResumableTrainingJobPhase `json:"remotePhase,omitempty"`
	// +optional
	RemoteCheckpoint *RemoteCheckpointSummary `json:"remoteCheckpoint,omitempty"`
	// +optional
	RemoteObservedGeneration int64 `json:"remoteObservedGeneration,omitempty"`
	// +optional
	LocalExecutionSuppressed bool `json:"localExecutionSuppressed,omitempty"`
}

// RemoteObjectReference identifies the remote RTJ copy on a worker cluster.
type RemoteObjectReference struct {
	// +kubebuilder:validation:MinLength=1
	Cluster string `json:"cluster"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +optional
	UID string `json:"uid,omitempty"`
}

// RemoteCheckpointSummary summarizes the latest checkpoint from a remote worker.
type RemoteCheckpointSummary struct {
	// +optional
	LastCompletedCheckpointID string `json:"lastCompletedCheckpointID,omitempty"`
	// +optional
	LastCompletedCheckpointTime *metav1.Time `json:"lastCompletedCheckpointTime,omitempty"`
	// +optional
	StorageURI string `json:"storageURI,omitempty"`
}

// DeviceStatus captures the DRA device allocation state.
type DeviceStatus struct {
	// +optional
	DeviceMode DeviceMode `json:"deviceMode,omitempty"`
	// +optional
	RequestedDeviceClasses []string `json:"requestedDeviceClasses,omitempty"`
	// +optional
	CurrentDeviceProfileFingerprint string `json:"currentDeviceProfileFingerprint,omitempty"`
	// +optional
	ResourceClaimTemplateRefs []ResourceClaimTemplateReference `json:"resourceClaimTemplateRefs,omitempty"`
	// +optional
	ClaimAllocationState ClaimAllocationState `json:"claimAllocationState,omitempty"`
	// +optional
	AllocatedClaimCount int32 `json:"allocatedClaimCount,omitempty"`
	// +optional
	LastClaimFailureReason string `json:"lastClaimFailureReason,omitempty"`
	// +optional
	LastClaimFailureTime *metav1.Time `json:"lastClaimFailureTime,omitempty"`
	// +optional
	LastCheckpointDeviceProfileFingerprint string `json:"lastCheckpointDeviceProfileFingerprint,omitempty"`
	// +optional
	LastResumeDeviceProfileFingerprint string `json:"lastResumeDeviceProfileFingerprint,omitempty"`
}

// ResourceClaimTemplateReference maps a DeviceClaimSpec name to the generated object.
type ResourceClaimTemplateReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	ClaimName string `json:"claimName"`
}

// ElasticityStatus captures the controller-owned elasticity state.
type ElasticityStatus struct {
	// +optional
	DesiredWorkerCount int32 `json:"desiredWorkerCount,omitempty"`
	// +optional
	TargetWorkerCount int32 `json:"targetWorkerCount,omitempty"`
	// +optional
	ActiveWorkerCount int32 `json:"activeWorkerCount,omitempty"`
	// +optional
	AdmittedWorkerCount int32 `json:"admittedWorkerCount,omitempty"`
	// +optional
	ResizeState ResizeState `json:"resizeState,omitempty"`
	// +optional
	ResizeReason string `json:"resizeReason,omitempty"`
	// +optional
	CurrentExecutionMode ExecutionMode `json:"currentExecutionMode,omitempty"`
	// +optional
	ResizePath ResizePath `json:"resizePath,omitempty"`
	// +optional
	ReclaimableWorkerCount int32 `json:"reclaimableWorkerCount,omitempty"`
	// +optional
	ReclaimablePodsPublished bool `json:"reclaimablePodsPublished,omitempty"`
	// +optional
	InPlaceShrinkSupported bool `json:"inPlaceShrinkSupported,omitempty"`
	// +optional
	LastResizeEvent string `json:"lastResizeEvent,omitempty"`
	// +optional
	LastResizeCheckpoint *CheckpointReference `json:"lastResizeCheckpoint,omitempty"`
	// +optional
	LastResizeFailureReason string `json:"lastResizeFailureReason,omitempty"`
	// +optional
	LastElasticTransitionTime *metav1.Time `json:"lastElasticTransitionTime,omitempty"`
	// +optional
	LastResizeCompletedTime *metav1.Time `json:"lastResizeCompletedTime,omitempty"`
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

// ---------------------------------------------------------------------------
// ResumableTrainingJobStatus — controller-owned
// ---------------------------------------------------------------------------

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
	// +optional
	Admission *AdmissionStatus `json:"admission,omitempty"`
	// +optional
	Restore *RestoreStatus `json:"restore,omitempty"`
	// +optional
	LaunchReadiness *LaunchReadinessStatus `json:"launchReadiness,omitempty"`
	// +optional
	Topology *TopologyStatus `json:"topology,omitempty"`
	// +optional
	EffectiveLaunchShape *EffectiveLaunchShape `json:"effectiveLaunchShape,omitempty"`
	// +optional
	PriorityShaping *PriorityShapingStatus `json:"priorityShaping,omitempty"`
	// +optional
	LaunchGate *LaunchGateStatus `json:"launchGate,omitempty"`
	// +optional
	Provisioning *ProvisioningStatus `json:"provisioning,omitempty"`
	// +optional
	StartupRecovery *StartupRecoveryStatus `json:"startupRecovery,omitempty"`
	// +optional
	Capacity *CapacityStatus `json:"capacity,omitempty"`
	// +optional
	MultiCluster *MultiClusterStatus `json:"multiCluster,omitempty"`
	// +optional
	Devices *DeviceStatus `json:"devices,omitempty"`
	// +optional
	Elasticity *ElasticityStatus `json:"elasticity,omitempty"`
}

// ---------------------------------------------------------------------------
// Root types
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// Default applies default values to the ResumableTrainingJob.
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
	if r.Spec.Topology != nil && r.Spec.Topology.Mode == "" {
		r.Spec.Topology.Mode = DefaultTopologyMode
	}
	if r.Spec.Devices != nil && r.Spec.Devices.Mode == "" {
		r.Spec.Devices.Mode = DefaultDeviceMode
	}
	if r.Spec.Devices != nil {
		for i := range r.Spec.Devices.Claims {
			if r.Spec.Devices.Claims[i].Request.Count == 0 {
				r.Spec.Devices.Claims[i].Request.Count = DefaultDeviceRequestCount
			}
		}
	}
	if r.Spec.Elasticity != nil && r.Spec.Elasticity.Mode == "" {
		r.Spec.Elasticity.Mode = DefaultElasticityMode
	}
	if r.Spec.Elasticity != nil && r.Spec.Elasticity.Mode == ElasticityModeManual {
		if r.Spec.Elasticity.InPlaceShrinkPolicy == "" {
			r.Spec.Elasticity.InPlaceShrinkPolicy = DefaultInPlaceShrinkPolicy
		}
		if r.Spec.Elasticity.ReclaimMode == "" {
			r.Spec.Elasticity.ReclaimMode = DefaultReclaimMode
		}
	}
}
