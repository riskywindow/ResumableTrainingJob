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
