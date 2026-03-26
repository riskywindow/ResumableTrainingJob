package checkpointpriority

import "time"

// ProtectionWindowResult holds the result of a startup protection window check.
type ProtectionWindowResult struct {
	// Protected is true when the job is within the protection window.
	Protected bool

	// ProtectedUntil is the expiry time of the protection window.
	// Set regardless of whether Protected is true, so callers can observe
	// when the window expired or will expire.
	ProtectedUntil time.Time

	// Anchor is the time from which the protection window is measured
	// (the later of run start time and resume time).
	Anchor time.Time
}

// CheckProtectionWindow determines if the job is within its startup
// protection window. The anchor time is the later of runStartTime and
// resumeTime because the protection window resets on every resume.
//
// Returns a result with Protected=false if:
//   - both anchor times are nil (job hasn't started),
//   - protectionDuration is zero or negative, or
//   - the protection window has expired.
func CheckProtectionWindow(
	now time.Time,
	runStartTime, resumeTime *time.Time,
	protectionDuration time.Duration,
) ProtectionWindowResult {
	if protectionDuration <= 0 {
		return ProtectionWindowResult{}
	}

	anchor := protectionAnchor(runStartTime, resumeTime)
	if anchor == nil {
		return ProtectionWindowResult{}
	}

	expiresAt := anchor.Add(protectionDuration)
	return ProtectionWindowResult{
		Protected:      now.Before(expiresAt),
		ProtectedUntil: expiresAt,
		Anchor:         *anchor,
	}
}

// protectionAnchor returns the later of runStartTime and resumeTime.
// Returns nil if both are nil.
func protectionAnchor(runStartTime, resumeTime *time.Time) *time.Time {
	switch {
	case runStartTime == nil && resumeTime == nil:
		return nil
	case runStartTime == nil:
		return resumeTime
	case resumeTime == nil:
		return runStartTime
	default:
		if resumeTime.After(*runStartTime) {
			return resumeTime
		}
		return runStartTime
	}
}

// CheckCooldown determines if the job is within the cooldown period after
// a resume. The cooldown prevents immediate re-preemption (anti-thrashing).
//
// Returns false if:
//   - resumeTime is nil (job has never been yielded and resumed),
//   - minRuntimeBetweenYields is zero or negative.
func CheckCooldown(
	now time.Time,
	resumeTime *time.Time,
	minRuntimeBetweenYields time.Duration,
) bool {
	if resumeTime == nil || minRuntimeBetweenYields <= 0 {
		return false
	}
	return now.Before(resumeTime.Add(minRuntimeBetweenYields))
}

// IsYieldBudgetExhausted determines if the yield count has reached or
// exceeded the configured maximum. Returns false when maxYieldsPerWindow
// is zero or negative (yield counting disabled).
func IsYieldBudgetExhausted(recentYieldCount int32, maxYieldsPerWindow int32) bool {
	if maxYieldsPerWindow <= 0 {
		return false
	}
	return recentYieldCount >= maxYieldsPerWindow
}

// CheckCheckpointFreshness computes the checkpoint age and whether it
// exceeds the freshness target.
//
// Returns (0, false) when checkpointTime is nil. Clamps negative ages
// (future checkpoint times due to clock skew) to zero.
func CheckCheckpointFreshness(
	now time.Time,
	checkpointTime *time.Time,
	freshnessTarget time.Duration,
) (age time.Duration, stale bool) {
	if checkpointTime == nil {
		return 0, false
	}
	age = now.Sub(*checkpointTime)
	if age < 0 {
		age = 0
	}
	return age, age > freshnessTarget
}
