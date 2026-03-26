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
