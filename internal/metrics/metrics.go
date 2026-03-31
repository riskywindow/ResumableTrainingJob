package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

const (
	namespace = "checkpoint_native"
	subsystem = "operator"
)

type Recorder struct {
	mu        sync.Mutex
	phases    map[string]string
	workloads map[string]workloadObservation
}

type workloadObservation struct {
	created  bool
	admitted bool
}

var (
	registerOnce sync.Once
	recorder     *Recorder

	rtjsByPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rtjs_by_phase",
			Help:      "Current ResumableTrainingJobs tracked by phase.",
		},
		[]string{"phase"},
	)
	pausesRequested = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "pauses_requested_total",
			Help:      "Total manual pause requests accepted by the operator.",
		},
	)
	pauseTimeouts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "pause_timeouts_total",
			Help:      "Total pause drain timeouts observed by the operator.",
		},
	)
	resumesAttempted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "resumes_attempted_total",
			Help:      "Total resume attempts started by the operator.",
		},
	)
	resumesSucceeded = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "resumes_succeeded_total",
			Help:      "Total resume attempts that returned to Running.",
		},
	)
	resumesFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "resumes_failed_total",
			Help:      "Total resume attempts that failed before returning to Running.",
		},
	)
	checkpointsDiscovered = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "checkpoints_discovered_total",
			Help:      "Total committed checkpoint manifests discovered during resume selection.",
		},
	)
	workloadsCreated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "workloads_created_total",
			Help:      "Total RTJ-owned Kueue Workloads observed by the operator.",
		},
	)
	admissionsObserved = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "admissions_observed_total",
			Help:      "Total RTJ-owned Kueue Workloads observed with an admission.",
		},
	)
	kueueSuspensionsObserved = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "kueue_suspensions_observed_total",
			Help:      "Total Kueue-driven RTJ suspensions observed by the operator.",
		},
	)
	preemptionYieldsCompleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "preemption_yields_completed_total",
			Help:      "Total Kueue-driven graceful yields that completed with checkpoint evidence.",
		},
	)
	duplicateChildJobSetPreventions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "duplicate_child_jobset_preventions_total",
			Help:      "Total duplicate child JobSet create attempts avoided by create-if-missing reconciliation.",
		},
	)

	// Phase 4 metrics.
	launchesBlockedByReadinessGate = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "launches_blocked_by_readiness_gate_total",
			Help:      "Total launches blocked by the ResumeReadiness admission check gate.",
		},
	)
	readinessGateOutcomes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "readiness_gate_outcomes_total",
			Help:      "Total readiness gate evaluation outcomes by reason.",
		},
		[]string{"reason"},
	)
	topologyAwareLaunches = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "topology_aware_launches_total",
			Help:      "Total launches that used topology-aware placement.",
		},
	)
	topologyAssignmentWaits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "topology_assignment_waits_total",
			Help:      "Total times the operator waited for a topology assignment on the Workload.",
		},
	)
	phase4ResumesAttempted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "phase4_resumes_attempted_total",
			Help:      "Total resume attempts that went through the Phase 4 gated path.",
		},
	)
	phase4ResumesSucceeded = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "phase4_resumes_succeeded_total",
			Help:      "Total Phase 4 gated resumes that returned to Running.",
		},
	)
	phase4ResumesFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "phase4_resumes_failed_total",
			Help:      "Total Phase 4 gated resumes that failed.",
		},
	)
	unsupportedTopologyShapeFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "unsupported_topology_shape_failures_total",
			Help:      "Total topology assignments that could not be represented in the child JobSet.",
		},
	)

	// Phase 5 metrics.
	priorityEvaluations = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_evaluations_total",
			Help:      "Total checkpoint-aware priority evaluations performed by the priority shaping controller.",
		},
	)
	priorityPenaltiesApplied = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_penalties_applied_total",
			Help:      "Total times a priority penalty was applied (effective priority lowered below base).",
		},
	)
	priorityProtectionWindowActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_protection_window_active",
			Help:      "Number of RTJs currently within their startup protection window.",
		},
	)
	priorityEffectiveValue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_effective_value",
			Help:      "Current effective priority value per RTJ.",
		},
		[]string{"rtj"},
	)
	priorityTelemetryFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_telemetry_failures_total",
			Help:      "Total failures retrieving checkpoint telemetry for priority evaluation.",
		},
	)
	priorityDrivenPreemptions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_driven_preemptions_total",
			Help:      "Total preemptions where checkpoint-aware priority shaping influenced the outcome.",
		},
	)

	// Phase 5 extended metrics.
	rtjsByPreemptionState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rtjs_by_preemption_state",
			Help:      "Current ResumableTrainingJobs tracked by preemption state (Protected, Active, Cooldown, Preemptible).",
		},
		[]string{"state"},
	)
	priorityBasePriority = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_base_value",
			Help:      "Current base priority value per RTJ (from WorkloadPriorityClass).",
		},
		[]string{"rtj"},
	)
	priorityDecisionsByStateReason = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_decisions_total",
			Help:      "Total priority decisions by decision state and reason.",
		},
		[]string{"state", "reason"},
	)
	priorityMaterializationUpdates = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "priority_materialization_updates_total",
			Help:      "Total times effective priority was written to a Kueue Workload.",
		},
	)
	protectedWorkloadsCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "protected_workloads",
			Help:      "Number of RTJs currently in Protected preemption state.",
		},
	)
	preemptibleWorkloadsCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "preemptible_workloads",
			Help:      "Number of RTJs currently in Preemptible preemption state.",
		},
	)
	yieldsBlockedByBudget = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "yields_blocked_by_budget_total",
			Help:      "Total times a yield was prevented by yield budget exhaustion.",
		},
	)
	yieldsBlockedByCooldown = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "yields_blocked_by_cooldown_total",
			Help:      "Total times priority demotion was prevented by the cooldown period.",
		},
	)

	// Phase 3 metrics.
	admissionComparisons = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "admission_comparisons_total",
			Help:      "Total launch/resume observations by admitted-vs-preferred outcome.",
		},
		[]string{"comparison"}, // "equal" or "partial"
	)
	reshardRestoresAttempted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "reshard_restores_attempted_total",
			Help:      "Total reshard (different-size) restore attempts started.",
		},
	)
	reshardRestoresSucceeded = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "reshard_restores_succeeded_total",
			Help:      "Total reshard restores that reached Running.",
		},
	)
	reshardRestoresFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "reshard_restores_failed_total",
			Help:      "Total reshard restores that failed.",
		},
	)
	flavorAssignments = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "flavor_assignments_total",
			Help:      "Total flavor assignments observed during admission, by flavor name.",
		},
		[]string{"flavor"},
	)
	partialAdmissionLaunches = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "partial_admission_launches_total",
			Help:      "Total launches where admitted count was less than preferred count (partial admission).",
		},
	)
	sameSizeResumes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "same_size_resumes_total",
			Help:      "Total resumes where checkpoint and restore world sizes matched.",
		},
	)
	differentSizeResumes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "different_size_resumes_total",
			Help:      "Total resumes where checkpoint and restore world sizes differed.",
		},
	)

	// Phase 6 metrics.
	rtjsByExecutionRole = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "rtjs_by_execution_role",
			Help:      "Current ResumableTrainingJobs by operator execution role (manager or worker).",
		},
		[]string{"role"},
	)
	remoteRTJsByCluster = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "remote_rtjs_by_cluster",
			Help:      "Current MultiKueue-dispatched RTJs by selected worker cluster.",
		},
		[]string{"cluster"},
	)
	managerLocalSuppressions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "manager_local_suppressions_total",
			Help:      "Total times the manager suppressed local child JobSet creation for a MultiKueue-managed RTJ.",
		},
	)
	remoteStatusSyncSuccesses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "remote_status_sync_successes_total",
			Help:      "Total successful remote status syncs from worker to manager.",
		},
	)
	remoteStatusSyncFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "remote_status_sync_failures_total",
			Help:      "Total failed remote status sync attempts.",
		},
	)
	remotePauseEvents = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "remote_pause_events_total",
			Help:      "Total remote pause events completed on the manager.",
		},
	)
	remoteResumeEvents = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "remote_resume_events_total",
			Help:      "Total remote resume events initiated on the manager.",
		},
	)
	remoteCheckpointObservations = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "remote_checkpoint_observations_total",
			Help:      "Total remote checkpoint summaries observed by the manager from worker status.",
		},
	)
	sharedStoreAccessFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "shared_store_access_failures_total",
			Help:      "Total failures accessing the shared checkpoint store.",
		},
	)
)

func NewRecorder() *Recorder {
	registerOnce.Do(func() {
		ctrlmetrics.Registry.MustRegister(
			rtjsByPhase,
			pausesRequested,
			pauseTimeouts,
			resumesAttempted,
			resumesSucceeded,
			resumesFailed,
			checkpointsDiscovered,
			workloadsCreated,
			admissionsObserved,
			kueueSuspensionsObserved,
			preemptionYieldsCompleted,
			duplicateChildJobSetPreventions,
			// Phase 5
			priorityEvaluations,
			priorityPenaltiesApplied,
			priorityProtectionWindowActive,
			priorityEffectiveValue,
			priorityTelemetryFailures,
			priorityDrivenPreemptions,
			rtjsByPreemptionState,
			priorityBasePriority,
			priorityDecisionsByStateReason,
			priorityMaterializationUpdates,
			protectedWorkloadsCount,
			preemptibleWorkloadsCount,
			yieldsBlockedByBudget,
			yieldsBlockedByCooldown,
			// Phase 4
			launchesBlockedByReadinessGate,
			readinessGateOutcomes,
			topologyAwareLaunches,
			topologyAssignmentWaits,
			phase4ResumesAttempted,
			phase4ResumesSucceeded,
			phase4ResumesFailed,
			unsupportedTopologyShapeFailures,
			// Phase 3
			admissionComparisons,
			reshardRestoresAttempted,
			reshardRestoresSucceeded,
			reshardRestoresFailed,
			flavorAssignments,
			partialAdmissionLaunches,
			sameSizeResumes,
			differentSizeResumes,
			// Phase 6
			rtjsByExecutionRole,
			remoteRTJsByCluster,
			managerLocalSuppressions,
			remoteStatusSyncSuccesses,
			remoteStatusSyncFailures,
			remotePauseEvents,
			remoteResumeEvents,
			remoteCheckpointObservations,
			sharedStoreAccessFailures,
		)
		recorder = &Recorder{
			phases:    map[string]string{},
			workloads: map[string]workloadObservation{},
		}
	})
	return recorder
}

func (r *Recorder) ObservePhase(key, phase string) {
	if r == nil || key == "" || phase == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	previous, ok := r.phases[key]
	if ok && previous == phase {
		return
	}
	if ok && previous != "" {
		rtjsByPhase.WithLabelValues(previous).Dec()
	}
	rtjsByPhase.WithLabelValues(phase).Inc()
	r.phases[key] = phase
}

func (r *Recorder) RemoveRTJ(key string) {
	if r == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	previous, ok := r.phases[key]
	if !ok || previous == "" {
		return
	}
	rtjsByPhase.WithLabelValues(previous).Dec()
	delete(r.phases, key)
}

func (r *Recorder) IncPauseRequested() {
	if r != nil {
		pausesRequested.Inc()
	}
}

func (r *Recorder) IncPauseTimeout() {
	if r != nil {
		pauseTimeouts.Inc()
	}
}

func (r *Recorder) IncResumeAttempted() {
	if r != nil {
		resumesAttempted.Inc()
	}
}

func (r *Recorder) IncResumeSucceeded() {
	if r != nil {
		resumesSucceeded.Inc()
	}
}

func (r *Recorder) IncResumeFailed() {
	if r != nil {
		resumesFailed.Inc()
	}
}

func (r *Recorder) AddCheckpointsDiscovered(count int) {
	if r != nil && count > 0 {
		checkpointsDiscovered.Add(float64(count))
	}
}

func (r *Recorder) ObserveWorkload(workload *kueuev1beta2.Workload) {
	if r == nil || workload == nil || workload.UID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := string(workload.UID)
	state := r.workloads[key]
	if !state.created {
		workloadsCreated.Inc()
		state.created = true
	}
	if workload.Status.Admission != nil && !state.admitted {
		admissionsObserved.Inc()
		state.admitted = true
	}
	r.workloads[key] = state
}

func (r *Recorder) IncKueueSuspensionObserved() {
	if r != nil {
		kueueSuspensionsObserved.Inc()
	}
}

func (r *Recorder) IncPreemptionYieldCompleted() {
	if r != nil {
		preemptionYieldsCompleted.Inc()
	}
}

func (r *Recorder) IncDuplicateChildJobSetPrevention() {
	if r != nil {
		duplicateChildJobSetPreventions.Inc()
	}
}

// Phase 3 recorder methods.

// ObserveAdmissionComparison records whether the admitted worker count
// matched the preferred count ("equal") or was below it ("partial").
func (r *Recorder) ObserveAdmissionComparison(admitted, preferred int32) {
	if r == nil {
		return
	}
	if admitted < preferred {
		admissionComparisons.WithLabelValues("partial").Inc()
		partialAdmissionLaunches.Inc()
	} else {
		admissionComparisons.WithLabelValues("equal").Inc()
	}
}

func (r *Recorder) IncReshardRestoreAttempted() {
	if r != nil {
		reshardRestoresAttempted.Inc()
	}
}

func (r *Recorder) IncReshardRestoreSucceeded() {
	if r != nil {
		reshardRestoresSucceeded.Inc()
	}
}

func (r *Recorder) IncReshardRestoreFailed() {
	if r != nil {
		reshardRestoresFailed.Inc()
	}
}

// ObserveFlavorAssignment records a flavor assignment observation.
func (r *Recorder) ObserveFlavorAssignment(flavor string) {
	if r != nil && flavor != "" {
		flavorAssignments.WithLabelValues(flavor).Inc()
	}
}

// ObserveResumeWorldSize records whether a resume was same-size or different-size.
func (r *Recorder) ObserveResumeWorldSize(checkpointWorldSize, restoreWorldSize int32) {
	if r == nil {
		return
	}
	if checkpointWorldSize == restoreWorldSize {
		sameSizeResumes.Inc()
	} else {
		differentSizeResumes.Inc()
		reshardRestoresAttempted.Inc()
	}
}

// Phase 4 recorder methods.

// IncLaunchBlockedByReadinessGate records a launch blocked by the readiness gate.
func (r *Recorder) IncLaunchBlockedByReadinessGate() {
	if r != nil {
		launchesBlockedByReadinessGate.Inc()
	}
}

// ObserveReadinessGateOutcome records a readiness gate evaluation outcome.
func (r *Recorder) ObserveReadinessGateOutcome(reason string) {
	if r != nil && reason != "" {
		readinessGateOutcomes.WithLabelValues(reason).Inc()
	}
}

// IncTopologyAwareLaunch records a topology-aware launch.
func (r *Recorder) IncTopologyAwareLaunch() {
	if r != nil {
		topologyAwareLaunches.Inc()
	}
}

// IncTopologyAssignmentWait records a wait for topology assignment.
func (r *Recorder) IncTopologyAssignmentWait() {
	if r != nil {
		topologyAssignmentWaits.Inc()
	}
}

// IncPhase4ResumeAttempted records a Phase 4 gated resume attempt.
func (r *Recorder) IncPhase4ResumeAttempted() {
	if r != nil {
		phase4ResumesAttempted.Inc()
	}
}

// IncPhase4ResumeSucceeded records a Phase 4 gated resume success.
func (r *Recorder) IncPhase4ResumeSucceeded() {
	if r != nil {
		phase4ResumesSucceeded.Inc()
	}
}

// IncPhase4ResumeFailed records a Phase 4 gated resume failure.
func (r *Recorder) IncPhase4ResumeFailed() {
	if r != nil {
		phase4ResumesFailed.Inc()
	}
}

// IncUnsupportedTopologyShapeFailure records a topology shape that could not
// be represented in the child JobSet.
func (r *Recorder) IncUnsupportedTopologyShapeFailure() {
	if r != nil {
		unsupportedTopologyShapeFailures.Inc()
	}
}

// Phase 5 recorder methods.

// IncPriorityEvaluation records a priority evaluation.
func (r *Recorder) IncPriorityEvaluation() {
	if r != nil {
		priorityEvaluations.Inc()
	}
}

// IncPriorityPenaltyApplied records a priority penalty application.
func (r *Recorder) IncPriorityPenaltyApplied() {
	if r != nil {
		priorityPenaltiesApplied.Inc()
	}
}

// SetPriorityProtectionWindowActive sets the gauge for active protection windows.
func (r *Recorder) SetPriorityProtectionWindowActive(count float64) {
	if r != nil {
		priorityProtectionWindowActive.Set(count)
	}
}

// SetPriorityEffectiveValue records the effective priority for a specific RTJ.
func (r *Recorder) SetPriorityEffectiveValue(rtjKey string, value float64) {
	if r != nil && rtjKey != "" {
		priorityEffectiveValue.WithLabelValues(rtjKey).Set(value)
	}
}

// RemovePriorityEffectiveValue removes the effective priority metric for an RTJ.
func (r *Recorder) RemovePriorityEffectiveValue(rtjKey string) {
	if r != nil && rtjKey != "" {
		priorityEffectiveValue.DeleteLabelValues(rtjKey)
	}
}

// IncPriorityTelemetryFailure records a telemetry retrieval failure.
func (r *Recorder) IncPriorityTelemetryFailure() {
	if r != nil {
		priorityTelemetryFailures.Inc()
	}
}

// IncPriorityDrivenPreemption records a priority-driven preemption.
func (r *Recorder) IncPriorityDrivenPreemption() {
	if r != nil {
		priorityDrivenPreemptions.Inc()
	}
}

// ObservePreemptionState updates the per-RTJ preemption state gauge.
// The previous state is decremented and the new state is incremented.
func (r *Recorder) ObservePreemptionState(rtjKey, previousState, newState string) {
	if r == nil {
		return
	}
	if previousState != "" && previousState != newState {
		rtjsByPreemptionState.WithLabelValues(previousState).Dec()
	}
	if newState != "" {
		if previousState != newState {
			rtjsByPreemptionState.WithLabelValues(newState).Inc()
		}
	}
}

// RemovePreemptionState cleans up the preemption state gauge for a removed RTJ.
func (r *Recorder) RemovePreemptionState(state string) {
	if r != nil && state != "" {
		rtjsByPreemptionState.WithLabelValues(state).Dec()
	}
}

// SetPriorityBasePriority records the base priority for a specific RTJ.
func (r *Recorder) SetPriorityBasePriority(rtjKey string, value float64) {
	if r != nil && rtjKey != "" {
		priorityBasePriority.WithLabelValues(rtjKey).Set(value)
	}
}

// RemovePriorityBasePriority removes the base priority metric for an RTJ.
func (r *Recorder) RemovePriorityBasePriority(rtjKey string) {
	if r != nil && rtjKey != "" {
		priorityBasePriority.DeleteLabelValues(rtjKey)
	}
}

// ObservePriorityDecision records a priority decision by state and reason.
func (r *Recorder) ObservePriorityDecision(state, reason string) {
	if r != nil && state != "" && reason != "" {
		priorityDecisionsByStateReason.WithLabelValues(state, reason).Inc()
	}
}

// IncPriorityMaterializationUpdate records a Workload priority patch.
func (r *Recorder) IncPriorityMaterializationUpdate() {
	if r != nil {
		priorityMaterializationUpdates.Inc()
	}
}

// SetProtectedWorkloadsCount sets the gauge for protected workloads.
func (r *Recorder) SetProtectedWorkloadsCount(count float64) {
	if r != nil {
		protectedWorkloadsCount.Set(count)
	}
}

// SetPreemptibleWorkloadsCount sets the gauge for preemptible workloads.
func (r *Recorder) SetPreemptibleWorkloadsCount(count float64) {
	if r != nil {
		preemptibleWorkloadsCount.Set(count)
	}
}

// IncYieldBlockedByBudget records a yield blocked by budget exhaustion.
func (r *Recorder) IncYieldBlockedByBudget() {
	if r != nil {
		yieldsBlockedByBudget.Inc()
	}
}

// IncYieldBlockedByCooldown records a priority demotion prevented by cooldown.
func (r *Recorder) IncYieldBlockedByCooldown() {
	if r != nil {
		yieldsBlockedByCooldown.Inc()
	}
}

// Phase 6 recorder methods.

// ObserveExecutionRole tracks the current RTJ's execution role (manager or worker).
func (r *Recorder) ObserveExecutionRole(role string) {
	if r != nil && role != "" {
		rtjsByExecutionRole.WithLabelValues(role).Inc()
	}
}

// RemoveExecutionRole decrements the execution role gauge.
func (r *Recorder) RemoveExecutionRole(role string) {
	if r != nil && role != "" {
		rtjsByExecutionRole.WithLabelValues(role).Dec()
	}
}

// ObserveRemoteCluster tracks a remote RTJ assigned to a specific worker cluster.
func (r *Recorder) ObserveRemoteCluster(cluster string) {
	if r != nil && cluster != "" {
		remoteRTJsByCluster.WithLabelValues(cluster).Inc()
	}
}

// RemoveRemoteCluster decrements the remote cluster gauge.
func (r *Recorder) RemoveRemoteCluster(cluster string) {
	if r != nil && cluster != "" {
		remoteRTJsByCluster.WithLabelValues(cluster).Dec()
	}
}

// IncManagerLocalSuppression records a manager-mode local launch suppression.
func (r *Recorder) IncManagerLocalSuppression() {
	if r != nil {
		managerLocalSuppressions.Inc()
	}
}

// IncRemoteStatusSyncSuccess records a successful remote status sync.
func (r *Recorder) IncRemoteStatusSyncSuccess() {
	if r != nil {
		remoteStatusSyncSuccesses.Inc()
	}
}

// IncRemoteStatusSyncFailure records a failed remote status sync.
func (r *Recorder) IncRemoteStatusSyncFailure() {
	if r != nil {
		remoteStatusSyncFailures.Inc()
	}
}

// IncRemotePauseEvent records a remote pause completion.
func (r *Recorder) IncRemotePauseEvent() {
	if r != nil {
		remotePauseEvents.Inc()
	}
}

// IncRemoteResumeEvent records a remote resume initiation.
func (r *Recorder) IncRemoteResumeEvent() {
	if r != nil {
		remoteResumeEvents.Inc()
	}
}

// IncRemoteCheckpointObservation records a remote checkpoint summary observation.
func (r *Recorder) IncRemoteCheckpointObservation() {
	if r != nil {
		remoteCheckpointObservations.Inc()
	}
}

// IncSharedStoreAccessFailure records a shared checkpoint store access failure.
func (r *Recorder) IncSharedStoreAccessFailure() {
	if r != nil {
		sharedStoreAccessFailures.Inc()
	}
}
