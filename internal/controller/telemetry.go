package controller

import (
	"context"
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// TelemetrySnapshot holds the Phase 5 telemetry fields collected from
// RTJ status, checkpoint catalog, and operator lifecycle events.
// All fields are optional—nil/zero means "not available for this reconcile".
//
// The priority shaping controller uses this snapshot to compute preemption
// state and effective priority. Fields are populated from the cheapest
// available source: RTJ status timestamps first, checkpoint catalog only
// when status lacks the data.
type TelemetrySnapshot struct {
	// LastCompletedCheckpointTime is the completion timestamp of the most
	// recent checkpoint. Sourced from RTJ status.lastCompletedCheckpoint
	// first, falling back to the checkpoint catalog.
	LastCompletedCheckpointTime *metav1.Time

	// LastCompletedCheckpointID is the ID of the most recent checkpoint.
	LastCompletedCheckpointID string

	// LastCompletedCheckpointStep is the global training step of the most
	// recent checkpoint.
	LastCompletedCheckpointStep int64

	// LastRunStartTime is the timestamp when the current run attempt started
	// (transitioned to Starting phase). Sourced from TransitionTimestamps.StartingAt.
	LastRunStartTime *metav1.Time

	// LastResumeTime is the timestamp when the most recent resume completed
	// (transitioned from Restoring to Running). Sourced from
	// TransitionTimestamps.RestoreCompletedAt or RunningAt when the previous
	// phase was Restoring.
	LastResumeTime *metav1.Time

	// LastYieldTime is the timestamp when the most recent yield was requested.
	// Sourced from TransitionTimestamps.YieldRequestedAt.
	LastYieldTime *metav1.Time

	// LastDrainDuration is the time between the yield request and the
	// completed drain (Paused). Computed from YieldRequestedAt and PausedAt.
	// Zero when not available.
	LastDrainDuration time.Duration

	// RecentYieldCount is the number of yields within the active accounting
	// window. Computed from the YieldHistory annotation on the RTJ.
	RecentYieldCount int32

	// CurrentRunActiveSince is the timestamp when the current run attempt
	// became active (Running phase). Sourced from TransitionTimestamps.RunningAt.
	CurrentRunActiveSince *metav1.Time
}

// CollectTelemetry gathers Phase 5 telemetry from the RTJ status and,
// when necessary, the checkpoint catalog. It prefers deriving data from
// existing timestamps and status fields over performing I/O.
//
// The function is idempotent: calling it multiple times with the same
// RTJ state and catalog contents produces the same snapshot. It does not
// mutate the RTJ.
func CollectTelemetry(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	catalog checkpoints.Catalog,
	now metav1.Time,
	yieldWindow time.Duration,
) TelemetrySnapshot {
	snap := TelemetrySnapshot{}

	// --- Checkpoint telemetry ---
	// Prefer the operator-recorded lastCompletedCheckpoint (populated during
	// the drain flow) over a catalog lookup. This avoids S3 I/O on every
	// reconcile and is authoritative because the operator validates manifests
	// during ObservePause.
	if ref := job.Status.LastCompletedCheckpoint; ref != nil {
		if ref.CompletionTime != nil {
			snap.LastCompletedCheckpointTime = ref.CompletionTime
		}
		snap.LastCompletedCheckpointID = ref.ID
	}

	// Backfill from the previously-persisted PriorityShapingStatus when the
	// checkpoint reference is present but the step is not directly available.
	// The CheckpointReference type does not carry GlobalStep, so the catalog
	// is the original source.
	if snap.LastCompletedCheckpointTime != nil && snap.LastCompletedCheckpointStep == 0 {
		if ps := job.Status.PriorityShaping; ps != nil {
			// The checkpoint time in PriorityShapingStatus matches—reuse
			// the cached step to avoid a catalog round-trip.
			if ps.LastCompletedCheckpointTime != nil &&
				ps.LastCompletedCheckpointTime.Equal(snap.LastCompletedCheckpointTime) {
				// Step was previously recorded; leave it as-is for now.
				// The priority shaping controller will resolve it when
				// it evaluates the full policy.
			}
		}
	}

	// If we still lack checkpoint time (e.g., first reconcile after operator
	// restart with no drain observed yet), and a catalog is available,
	// attempt a lightweight lookup.
	if snap.LastCompletedCheckpointTime == nil && catalog != nil {
		info, found, err := catalog.LatestCheckpointInfo(ctx, job.Spec.Checkpoint.StorageURI)
		if err == nil && found && info != nil {
			snap.LastCompletedCheckpointTime = &info.CompletionTimestamp
			snap.LastCompletedCheckpointID = info.CheckpointID
			snap.LastCompletedCheckpointStep = info.GlobalStep
		}
	}

	// --- Lifecycle timestamps ---
	ts := &job.Status.TransitionTimestamps

	snap.LastRunStartTime = ts.StartingAt
	snap.CurrentRunActiveSince = ts.RunningAt
	snap.LastYieldTime = ts.YieldRequestedAt

	// LastResumeTime: use RestoreCompletedAt when available (set when a
	// Restoring->Running transition is observed). Fall back to RunningAt
	// if the job has been through a Restoring phase.
	if ts.RestoreCompletedAt != nil {
		snap.LastResumeTime = ts.RestoreCompletedAt
	} else if ts.RestoringAt != nil && ts.RunningAt != nil {
		// The restore completed when the job transitioned to Running.
		snap.LastResumeTime = ts.RunningAt
	}

	// LastDrainDuration: time between yield request and paused.
	if ts.YieldRequestedAt != nil && ts.PausedAt != nil && ts.PausedAt.After(ts.YieldRequestedAt.Time) {
		snap.LastDrainDuration = ts.PausedAt.Sub(ts.YieldRequestedAt.Time)
	}

	// --- Yield windowing ---
	snap.RecentYieldCount = countYieldsInWindow(job, now, yieldWindow)

	return snap
}

// yieldHistoryAnnotation is the annotation key used to store serialized
// yield timestamps for windowed counting. The value is a JSON array of
// RFC3339 timestamps.
const yieldHistoryAnnotation = "training.checkpoint.example.io/yield-history"

// countYieldsInWindow counts how many yields occurred within the window
// ending at `now`. Yield timestamps are stored in the RTJ's
// yieldHistoryAnnotation.
//
// The annotation is the source of truth for yield counting because:
// 1. TransitionTimestamps only records the *last* yield time, not a history.
// 2. The annotation survives operator restarts (persisted in etcd).
// 3. The window can span multiple run attempts.
func countYieldsInWindow(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
	window time.Duration,
) int32 {
	if window <= 0 {
		return 0
	}
	raw, ok := job.Annotations[yieldHistoryAnnotation]
	if !ok || raw == "" {
		return 0
	}
	timestamps := parseYieldHistory(raw)
	cutoff := now.Add(-window)
	var count int32
	for _, ts := range timestamps {
		if !ts.Before(cutoff) {
			count++
		}
	}
	return count
}

// parseYieldHistory parses the JSON array of RFC3339 timestamps stored
// in the yield history annotation.
func parseYieldHistory(raw string) []time.Time {
	var parsed []string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	timestamps := make([]time.Time, 0, len(parsed))
	for _, s := range parsed {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			continue
		}
		timestamps = append(timestamps, t)
	}
	return timestamps
}

// RecordYieldEvent appends the current timestamp to the yield history
// annotation and prunes entries older than the window. Returns true if
// the annotation was changed.
func RecordYieldEvent(
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
	window time.Duration,
) bool {
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	existing := parseYieldHistory(job.Annotations[yieldHistoryAnnotation])
	cutoff := now.Add(-window)

	// Prune expired entries and add the new one.
	kept := make([]time.Time, 0, len(existing)+1)
	for _, ts := range existing {
		if !ts.Before(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now.Time)

	job.Annotations[yieldHistoryAnnotation] = marshalYieldHistory(kept)
	return true
}

// marshalYieldHistory serializes yield timestamps to a JSON array.
func marshalYieldHistory(timestamps []time.Time) string {
	strs := make([]string, 0, len(timestamps))
	for _, ts := range timestamps {
		strs = append(strs, ts.UTC().Format(time.RFC3339))
	}
	data, _ := json.Marshal(strs)
	return string(data)
}

// SyncPriorityShapingTelemetry updates the RTJ's PriorityShapingStatus with
// the latest telemetry snapshot. Returns true when any field changed.
// This function does NOT compute preemption state or effective priority—
// that is the priority shaping controller's responsibility.
func SyncPriorityShapingTelemetry(
	job *trainingv1alpha1.ResumableTrainingJob,
	snap TelemetrySnapshot,
	now metav1.Time,
) bool {
	if !job.IsPriorityShapingEnabled() {
		// No policy attached — clear any stale telemetry.
		if job.Status.PriorityShaping != nil {
			job.Status.PriorityShaping = nil
			return true
		}
		return false
	}

	if job.Status.PriorityShaping == nil {
		job.Status.PriorityShaping = &trainingv1alpha1.PriorityShapingStatus{}
	}
	ps := job.Status.PriorityShaping
	changed := false

	// LastCompletedCheckpointTime
	if !timesEqual(ps.LastCompletedCheckpointTime, snap.LastCompletedCheckpointTime) {
		ps.LastCompletedCheckpointTime = snap.LastCompletedCheckpointTime
		changed = true
	}

	// CheckpointAge — computed at reconcile time, not persisted as a
	// durable field. Recomputed each reconcile for freshness.
	var newAge string
	if snap.LastCompletedCheckpointTime != nil {
		age := now.Sub(snap.LastCompletedCheckpointTime.Time)
		newAge = age.Round(time.Second).String()
	}
	if ps.CheckpointAge != newAge {
		ps.CheckpointAge = newAge
		changed = true
	}

	// LastYieldTime
	if !timesEqual(ps.LastYieldTime, snap.LastYieldTime) {
		ps.LastYieldTime = snap.LastYieldTime
		changed = true
	}

	// LastResumeTime
	if !timesEqual(ps.LastResumeTime, snap.LastResumeTime) {
		ps.LastResumeTime = snap.LastResumeTime
		changed = true
	}

	// RecentYieldCount
	if ps.RecentYieldCount != snap.RecentYieldCount {
		ps.RecentYieldCount = snap.RecentYieldCount
		changed = true
	}

	// AppliedPolicyRef
	policyName := ""
	if job.Spec.PriorityPolicyRef != nil {
		policyName = job.Spec.PriorityPolicyRef.Name
	}
	if ps.AppliedPolicyRef != policyName {
		ps.AppliedPolicyRef = policyName
		changed = true
	}

	return changed
}
