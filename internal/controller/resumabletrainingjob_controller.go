package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
	operatormetrics "github.com/example/checkpoint-native-preemption-controller/internal/metrics"
	"github.com/example/checkpoint-native-preemption-controller/internal/remote"
)

const (
	resumableTrainingJobFinalizer = "training.checkpoint.example.io/finalizer"
	launchGateRequeueInterval     = 5 * time.Second
)

// ResumableTrainingJobReconciler reconciles a ResumableTrainingJob object.
type ResumableTrainingJobReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Catalog checkpoints.Catalog
	Metrics *operatormetrics.Recorder
	Now     func() metav1.Time

	// Mode determines how this operator instance handles RTJ reconciliation.
	// ModeWorker (default) runs the full Phase 5 runtime path.
	// ModeManager suppresses local runtime for MultiKueue-managed RTJs.
	Mode OperatorMode

	// ClusterResolver resolves the execution cluster for MultiKueue-
	// dispatched RTJs. Only used when Mode is ModeManager. Nil is safe
	// (execution cluster will be reported as unknown).
	ClusterResolver remote.ClusterResolver

	// ProvisioningACNames identifies which Workload AdmissionCheck names are
	// ProvisioningRequest checks. When empty, provisioning is considered
	// not configured and Phase 6 backward-compatible behavior is preserved.
	// Phase 7: set from ClusterQueue AdmissionCheck configuration or operator flags.
	ProvisioningACNames map[string]bool
}

// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloadpriorityclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=checkpointprioritypolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaimtemplates,verbs=get;list;watch;create;delete

func (r *ResumableTrainingJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("resumableTrainingJob", req.NamespacedName)

	var job trainingv1alpha1.ResumableTrainingJob
	if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
		if apierrors.IsNotFound(err) && r.Metrics != nil {
			r.Metrics.RemoveRTJ(req.NamespacedName.String())
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !job.DeletionTimestamp.IsZero() {
		if r.Metrics != nil {
			r.Metrics.RemoveRTJ(req.NamespacedName.String())
		}
		if controllerutil.ContainsFinalizer(&job, resumableTrainingJobFinalizer) {
			controllerutil.RemoveFinalizer(&job, resumableTrainingJobFinalizer)
			if err := r.Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	defer r.observePhase(&job)

	if controllerutil.AddFinalizer(&job, resumableTrainingJobFinalizer) {
		if err := r.Update(ctx, &job); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if job.InitializePhase1Status(r.now()) {
		if err := r.Status().Update(ctx, &job); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Phase 6: In manager mode, suppress the entire runtime path for RTJs
	// managed by MultiKueue. The manager reconciles intent and status only;
	// runtime execution is delegated to a remote worker cluster.
	//
	// Phase 7 multi-cluster compatibility:
	//   - Manager mode: local runtime is suppressed. Phase 7 worker status
	//     (launchGate, provisioning, startupRecovery, capacity) is surfaced
	//     transparently via the adapter's full-status mirror. The manager
	//     does NOT evaluate launch gates or create ProvisioningRequests.
	//   - Worker mode: the full Phase 7 path runs unchanged. Launch gating,
	//     provisioning-aware gates, topology second-pass, waitForPodsReady
	//     semantics, and podSetUpdates all apply identically to single-cluster
	//     mode. The worker-side RTJ (created by the adapter) goes through
	//     the same Reconcile path below.
	if ShouldSuppressRuntime(r.Mode, &job) {
		return r.reconcileManagerIntent(ctx, &job)
	}

	// Phase 8: reconcile DRA ResourceClaimTemplate companions. This runs
	// early (before launch gates) to ensure templates exist before Kueue
	// evaluates the Workload for admission. When DRA is not configured,
	// this is a no-op that returns TemplatesReady=true.
	draResult, err := r.reconcileDRATemplates(ctx, &job, r.now())
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile DRA templates: %w", err)
	}
	if draResult.StatusChanged {
		if err := r.Status().Update(ctx, &job); err != nil {
			return ctrl.Result{}, err
		}
	}

	activeJobSet, activeExists, err := r.getActiveJobSet(ctx, &job)
	if err != nil {
		return r.failLaunch(ctx, &job, fmt.Sprintf("lookup child JobSet: %v", err))
	}
	now := r.now()
	if stopSource, shouldStop := r.requestedStopSource(&job, activeExists); shouldStop {
		// Phase 7: detect and classify Kueue eviction for startup/recovery tracking.
		// This must happen before the stop flow so the eviction classification is
		// recorded before the phase transitions to YieldRequested/Draining.
		if stopSource == stopSourceKueue && job.Status.WorkloadReference != nil {
			if evictionChanged := r.detectAndRecordEviction(ctx, &job, now); evictionChanged {
				if err := r.Status().Update(ctx, &job); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
		return r.reconcileStopFlow(ctx, &job, activeJobSet, stopSource, now)
	}
	if job.Spec.Control == nil || job.Spec.Control.DesiredState != trainingv1alpha1.DesiredStateRunning {
		return r.reconcileManualHold(ctx, &job, now)
	}
	if !activeExists && job.IsSuspendedForKueue() {
		if markQueuedForAdmission(&job, now) {
			if err := r.Status().Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("deferring child JobSet creation until RTJ admission is granted")
		return ctrl.Result{}, nil
	}
	if activeExists {
		wasRestoring := job.Status.Phase == trainingv1alpha1.PhaseRestoring
		statusChanged := markRunning(&job, now)
		// Phase 7: sync startup recovery state to Running.
		statusChanged = syncStartupRecoveryOnRunning(&job, now) || statusChanged
		// Phase 7: clear timeout conditions on successful transition to Running.
		statusChanged = clearStartupRecoveryTimeoutConditions(&job) || statusChanged
		if wasRestoring && r.Metrics != nil {
			r.Metrics.IncResumeSucceeded()
		}

		// Phase 5: evaluate priority shaping when the job is in an active phase.
		var priorityRequeue time.Duration
		if isActivePriorityPhase(job.Status.Phase) && job.IsPriorityShapingEnabled() {
			psResult := r.reconcilePriorityState(ctx, &job, now)
			statusChanged = psResult.StatusChanged || statusChanged
			if psResult.AnnotationsChanged {
				if err := r.Update(ctx, &job); err != nil {
					return ctrl.Result{}, err
				}
			}
			priorityRequeue = psResult.RequeueAfter
		}

		if statusChanged {
			if err := r.Status().Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("observed active child JobSet", "jobSet", activeJobSet.GetName(), "phase", job.Status.Phase)
		if priorityRequeue > 0 {
			return ctrl.Result{RequeueAfter: priorityRequeue}, nil
		}
		return ctrl.Result{}, nil
	}

	// Phase 8: gate launch on DRA template readiness. If templates were
	// just created or are in a transient race state, defer launch until
	// the next reconcile.
	if !draResult.TemplatesReady {
		logger.Info("DRA templates not ready, deferring launch")
		return ctrl.Result{RequeueAfter: launchGateRequeueInterval}, nil
	}

	// Phase 4/7: evaluate pre-launch gates (readiness check, topology, provisioning).
	// Only evaluate when features are potentially active (topology enabled,
	// workload reference present indicating Kueue integration, or provisioning configured).
	if job.IsTopologyEnabled() || job.Status.WorkloadReference != nil || len(r.ProvisioningACNames) > 0 {
		gateResult, err := r.evaluateLaunchGates(ctx, &job)
		if err != nil {
			return r.failLaunch(ctx, &job, fmt.Sprintf("evaluate launch gates: %v", err))
		}
		if gateResult != nil && !gateResult.Ready {
			changed := syncLaunchReadinessStatus(&job, gateResult)
			// Phase 7: sync launch gate, provisioning, capacity status and conditions.
			changed = syncLaunchGateStatus(&job, gateResult, now) || changed
			if gateResult.LaunchView != nil {
				changed = syncProvisioningStatus(&job, gateResult.LaunchView, now) || changed
				changed = syncCapacityStatus(&job, gateResult.LaunchView) || changed
			}
			changed = syncPhase7Conditions(&job, gateResult, now) || changed
			if changed {
				if err := r.Status().Update(ctx, &job); err != nil {
					return ctrl.Result{}, err
				}
			}
			// Phase 4 metrics: record gate block reason.
			if r.Metrics != nil {
				r.Metrics.ObserveReadinessGateOutcome(gateResult.Reason)
				switch gateResult.Reason {
				case reasonWaitingForReadinessGate, reasonReadinessGateRejected:
					r.Metrics.IncLaunchBlockedByReadinessGate()
				case reasonWaitingForTopology:
					r.Metrics.IncTopologyAssignmentWait()
				case reasonTopologyNotRepresentable:
					r.Metrics.IncUnsupportedTopologyShapeFailure()
				}
			}
			logger.Info("launch gated", "reason", gateResult.Reason)
			return ctrl.Result{RequeueAfter: launchGateRequeueInterval}, nil
		}
		// Gates passed — populate status and continue to launch.
		if gateResult != nil {
			changed := syncLaunchReadinessStatus(&job, gateResult)
			workerName := resolveWorkerPodSetNameForJob(&job)
			changed = syncTopologyStatus(&job, gateResult.TopologyResult, workerName) || changed
			// Phase 7: sync launch gate, provisioning, capacity status and conditions.
			changed = syncLaunchGateStatus(&job, gateResult, now) || changed
			if gateResult.LaunchView != nil {
				changed = syncProvisioningStatus(&job, gateResult.LaunchView, now) || changed
				changed = syncCapacityStatus(&job, gateResult.LaunchView) || changed
			}
			changed = syncPhase7Conditions(&job, gateResult, now) || changed
			if changed {
				if err := r.Status().Update(ctx, &job); err != nil {
					return ctrl.Result{}, err
				}
			}
			// Phase 4 metrics: record gate pass and topology-aware launch.
			if r.Metrics != nil {
				r.Metrics.ObserveReadinessGateOutcome("Ready")
				if gateResult.TopologyResult != nil {
					r.Metrics.IncTopologyAwareLaunch()
				}
			}
			// Phase 7: check for podSetUpdate conflicts before launching.
			if gateResult.LaunchView != nil {
				plan := buildLaunchPlan(&job, gateResult)
				if len(plan.PodSetUpdates) > 0 {
					// Dry-run apply to check for conflicts.
					spec, parseErr := rtjjobset.ParseTemplate(job.Spec.Runtime.Template.Spec)
					if parseErr == nil {
						testResult := rtjjobset.ApplyPodSetUpdates(&spec, plan.PodSetUpdates)
						if !testResult.Applied {
							msg := testResult.ConflictMessage()
							changed := setLaunchBlockedByConflictingUpdate(&job, msg, now)
							changed = markFailed(&job, reasonLaunchBlockedByConflictingUpdate, msg, now) || changed
							if changed {
								if err := r.Status().Update(ctx, &job); err != nil {
									return ctrl.Result{}, err
								}
							}
							logger.Info("launch blocked by conflicting podSetUpdate", "conflicts", msg)
							return ctrl.Result{}, fmt.Errorf("launch blocked: %s", msg)
						}
					}
				}
			}
			// Store the gate result for the launch/resume paths to use.
			if r.shouldResume(&job) {
				return r.reconcileResumeWithGate(ctx, &job, now, gateResult)
			}
			return r.reconcileLaunchWithGate(ctx, &job, now, gateResult)
		}
	}

	if r.shouldResume(&job) {
		return r.reconcileResume(ctx, &job, now)
	}
	return r.reconcileLaunch(ctx, &job, now)
}

func (r *ResumableTrainingJobReconciler) observePhase(job *trainingv1alpha1.ResumableTrainingJob) {
	if r.Metrics == nil || job == nil || job.Status.Phase == "" {
		return
	}
	r.Metrics.ObservePhase(client.ObjectKeyFromObject(job).String(), string(job.Status.Phase))
}

func createIfMissing(ctx context.Context, c client.Client, obj client.Object) (bool, error) {
	key := client.ObjectKeyFromObject(obj)
	current := obj.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, key, current); err == nil {
		return false, nil
	} else if !apierrors.IsNotFound(err) {
		return false, err
	}
	if err := c.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func buildControlConfigMap(job *trainingv1alpha1.ResumableTrainingJob, name string, runAttempt int32) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: job.Namespace,
			Labels: map[string]string{
				rtjjobset.ManagedByLabelKey:  rtjjobset.ManagedByLabelValue,
				rtjjobset.RTJNameLabelKey:    job.Name,
				rtjjobset.RunAttemptLabelKey: fmt.Sprintf("%d", runAttempt),
			},
		},
		Data: map[string]string{
			rtjjobset.ControlConfigKey: controlPayload(trainingv1alpha1.DesiredStateRunning, "", time.Time{}),
		},
	}
}

func (r *ResumableTrainingJobReconciler) getActiveJobSet(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (*unstructured.Unstructured, bool, error) {
	if job.Status.ActiveJobSetName == "" {
		return nil, false, nil
	}

	child := rtjjobset.NewEmptyChildJobSet(job.Spec.Runtime.Template.APIVersion, job.Spec.Runtime.Template.Kind)
	key := types.NamespacedName{Name: job.Status.ActiveJobSetName, Namespace: job.Namespace}
	if err := r.Get(ctx, key, child); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !child.GetDeletionTimestamp().IsZero() {
		return nil, false, nil
	}
	return child, true, nil
}

func (r *ResumableTrainingJobReconciler) failLaunch(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	message string,
) (ctrl.Result, error) {
	if markFailed(job, reasonLaunchFailed, message, r.now()) {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, errors.New(message)
}

// reconcileManagerIntent handles RTJs managed by MultiKueue when the operator
// is running in manager mode. It sets the MultiCluster status to indicate that
// local runtime execution is suppressed and reconciles the manager-side intent
// (desired state). It does NOT create child JobSets, control ConfigMaps, or
// perform any checkpoint I/O.
//
// Phase 6 Session 5: the manager now detects when the Kueue generic adapter
// has mirrored remote status and populates MultiClusterStatus with the
// execution cluster, remote phase, and remote checkpoint summary.
//
// Phase 6 Session 8: the manager now handles remote pause/resume propagation.
// When the user patches spec.control.desiredState to Paused, the adapter's
// delete-recreate cycle tears down the active remote RTJ. The manager
// preserves the last known checkpoint summary and marks the RTJ as Paused
// once the remote is no longer active. For resume, the adapter creates a
// new remote RTJ with desiredState=Running, and the worker resumes from the
// shared checkpoint store.
func (r *ResumableTrainingJobReconciler) reconcileManagerIntent(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	now := r.now()

	// Resolve the execution cluster from the Workload admission check.
	executionCluster := ""
	if r.ClusterResolver != nil {
		var err error
		executionCluster, err = r.ClusterResolver.ResolveExecutionCluster(ctx, job)
		if err != nil {
			logger.Error(err, "failed to resolve execution cluster; continuing with unknown")
		}
	}

	pauseRequested := isRemotePauseRequested(job)

	// Preserve the remote checkpoint summary before syncRemoteStatus
	// potentially clears it. When the adapter tears down the active remote
	// and creates a new one (with Paused spec), the fresh remote's empty
	// status is mirrored. Without preservation, the checkpoint evidence
	// from the previous run is lost.
	var preserved *trainingv1alpha1.RemoteCheckpointSummary
	if pauseRequested {
		preserved = preserveRemoteCheckpoint(job)
	}

	// Sync remote status: detect mirrored status from the adapter and
	// populate MultiClusterStatus fields.
	changed := syncRemoteStatus(job, executionCluster, now)

	// Restore the preserved checkpoint if syncRemoteStatus cleared it.
	if pauseRequested {
		changed = restoreRemoteCheckpoint(job, preserved) || changed
	}

	// Handle pause/resume for remote RTJs.
	if pauseRequested {
		// Mark as Paused when the remote is no longer active (the adapter
		// has torn it down) or when the remote itself reports a paused
		// state. While the remote is still active, requeue to wait for
		// teardown.
		if !hasRemoteStatusSignal(job) {
			changed = markRemotePaused(job, now) || changed
		} else {
			// Remote is still active; the adapter will tear it down soon.
			// Requeue to poll.
			logger.Info("manager mode: pause requested, waiting for remote teardown",
				"remotePhase", job.Status.MultiCluster.RemotePhase)
		}
	} else if !hasRemoteStatusSignal(job) {
		// Normal: no pause requested and no remote signal. Either waiting
		// for initial dispatch or transitioning after a resume.
		changed = markManagerSuppressed(job, now) || changed
	}

	if job.Status.ObservedGeneration != job.Generation {
		job.Status.ObservedGeneration = job.Generation
		changed = true
	}

	if changed {
		if err := r.Status().Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
	}

	mc := job.Status.MultiCluster
	logger.Info("manager mode: reconciled remote status",
		"dispatchPhase", mc.DispatchPhase,
		"executionCluster", mc.ExecutionCluster,
		"remotePhase", mc.RemotePhase,
		"localExecutionSuppressed", mc.LocalExecutionSuppressed,
		"pauseRequested", pauseRequested,
	)

	// Phase 7: log worker-side launch/provisioning status when the adapter
	// has mirrored Phase 7 fields. This surfaces worker-side launch gating,
	// provisioning progress, and capacity guarantees on the manager cluster.
	if hasRemoteStatusSignal(job) && hasPhase7RemoteStatus(job) {
		summary := buildRemoteLaunchSummary(job)
		logger.Info("manager mode: remote Phase 7 launch status (from worker)",
			"remoteLaunchGateState", summary.LaunchGateState,
			"remoteProvisioningState", summary.ProvisioningState,
			"remoteCapacityGuarantee", summary.CapacityGuaranteeActive,
			"remoteStartupState", summary.StartupState,
		)
	}

	// Requeue during transitional states to ensure timely convergence.
	if pauseRequested && hasRemoteStatusSignal(job) {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ResumableTrainingJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trainingv1alpha1.ResumableTrainingJob{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
