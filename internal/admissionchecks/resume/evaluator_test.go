package resume

import (
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

var (
	testNow = time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	defaultPolicy = ResolvedPolicy{
		RequireCompleteCheckpoint:           true,
		MaxCheckpointAge:                    nil,
		FailurePolicy:                       trainingv1alpha1.FailurePolicyFailClosed,
		AllowInitialLaunchWithoutCheckpoint: true,
	}
)

func testRTJ() *trainingv1alpha1.ResumableTrainingJob {
	return &trainingv1alpha1.ResumableTrainingJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rtj",
			Namespace: "default",
		},
		Spec: trainingv1alpha1.ResumableTrainingJobSpec{
			Identity: trainingv1alpha1.ResumableTrainingJobIdentity{
				Image:       "local/fixture:dev",
				CodeVersion: "git:abc123",
				WorldSize:   2,
				GPUShape:    "cpu",
			},
		},
		Status: trainingv1alpha1.ResumableTrainingJobStatus{
			CurrentRunAttempt: 0,
		},
	}
}

func testCheckpoint(id string, age time.Duration) *checkpoints.CheckpointManifest {
	completionTime := testNow.Add(-age)
	return &checkpoints.CheckpointManifest{
		CheckpointID:        id,
		ClusterIdentity:     "phase1-kind",
		RTJIdentity:         "default/test-rtj",
		RuntimeMode:         "DDP",
		WorldSize:           2,
		GPUShape:            "cpu",
		ImageIdentity:       "local/fixture:dev",
		CodeVersionIdentity: "git:abc123",
		FormatVersion:       checkpoints.SupportedManifestFormatVersion,
		ProducerVersion:     "0.1.0",
		StorageRootURI:      "s3://bucket/demo/checkpoints/" + id,
		ManifestURI:         "s3://bucket/demo/manifests/" + id + ".manifest.json",
		CompletionTimestamp: completionTime.Format(time.RFC3339),
		OptimizerMode:       "adamw",
		ShardingMode:        "replicated-optimizer-state",
		Artifacts: []checkpoints.Artifact{
			{
				Name:            "metadata-runtime.json",
				RelativePath:    "metadata/runtime.json",
				ObjectURI:       "s3://bucket/demo/checkpoints/" + id + "/metadata/runtime.json",
				SizeBytes:       128,
				DigestAlgorithm: "sha256",
				DigestValue:     "aabbcc",
			},
		},
	}
}

// --- Test: first launch allowed without checkpoint (default policy) ---

func TestEvaluateInitialLaunchReadyWithDefaultPolicy(t *testing.T) {
	decision := Evaluate(EvaluatorInput{
		RTJ:            testRTJ(),
		Policy:         defaultPolicy,
		CatalogQueried: true,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonInitialLaunchReady)
}

// --- Test: first launch blocked when policy disallows ---

func TestEvaluateInitialLaunchBlockedByPolicy(t *testing.T) {
	policy := defaultPolicy
	policy.AllowInitialLaunchWithoutCheckpoint = false

	decision := Evaluate(EvaluatorInput{
		RTJ:            testRTJ(),
		Policy:         policy,
		CatalogQueried: true,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateRejected, ReasonInitialLaunchBlocked)
}

// --- Test: resume requires complete checkpoint and one exists ---

func TestEvaluateCheckpointReady(t *testing.T) {
	ckpt := testCheckpoint("ckpt-1", 10*time.Minute)

	decision := Evaluate(EvaluatorInput{
		RTJ:                testRTJ(),
		Policy:             defaultPolicy,
		SelectedCheckpoint: ckpt,
		CatalogQueried:     true,
		Now:                testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonCheckpointReady)
	if !strings.Contains(decision.Message, "ckpt-1") {
		t.Errorf("expected message to reference checkpoint ID, got %q", decision.Message)
	}
}

// --- Test: selected checkpoint too old under policy ---

func TestEvaluateCheckpointTooOld(t *testing.T) {
	maxAge := 30 * time.Minute
	policy := defaultPolicy
	policy.MaxCheckpointAge = &maxAge

	// Checkpoint is 2 hours old, limit is 30 minutes.
	ckpt := testCheckpoint("ckpt-old", 2*time.Hour)

	decision := Evaluate(EvaluatorInput{
		RTJ:                testRTJ(),
		Policy:             policy,
		SelectedCheckpoint: ckpt,
		CatalogQueried:     true,
		Now:                testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateRejected, ReasonCheckpointTooOld)
}

// --- Test: checkpoint within age limit passes ---

func TestEvaluateCheckpointWithinAgeLimit(t *testing.T) {
	maxAge := 1 * time.Hour
	policy := defaultPolicy
	policy.MaxCheckpointAge = &maxAge

	// Checkpoint is 10 minutes old, limit is 1 hour.
	ckpt := testCheckpoint("ckpt-fresh", 10*time.Minute)

	decision := Evaluate(EvaluatorInput{
		RTJ:                testRTJ(),
		Policy:             policy,
		SelectedCheckpoint: ckpt,
		CatalogQueried:     true,
		Now:                testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonCheckpointReady)
}

// --- Test: no compatible checkpoint (after prior run attempts) ---

func TestEvaluateNoCheckpointAfterPriorRun(t *testing.T) {
	policy := defaultPolicy
	policy.AllowInitialLaunchWithoutCheckpoint = false

	rtj := testRTJ()
	rtj.Status.CurrentRunAttempt = 2
	completedAt := metav1.NewTime(testNow.Add(-1 * time.Hour))
	rtj.Status.LastCompletedCheckpoint = &trainingv1alpha1.CheckpointReference{
		ID:             "ckpt-old",
		CompletionTime: &completedAt,
	}

	decision := Evaluate(EvaluatorInput{
		RTJ:            rtj,
		Policy:         policy,
		CatalogQueried: true,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateRejected, ReasonNoCheckpointAvailable)
}

// --- Test: transient storage failure with FailClosed ---

func TestEvaluateStorageErrorFailClosed(t *testing.T) {
	policy := defaultPolicy
	policy.FailurePolicy = trainingv1alpha1.FailurePolicyFailClosed

	decision := Evaluate(EvaluatorInput{
		RTJ:          testRTJ(),
		Policy:       policy,
		CatalogError: fmt.Errorf("connection refused"),
		Now:          testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateRetry, ReasonStorageUnavailable)
	if !strings.Contains(decision.Message, "connection refused") {
		t.Errorf("expected message to include error, got %q", decision.Message)
	}
}

// --- Test: transient storage failure with FailOpen ---

func TestEvaluateStorageErrorFailOpen(t *testing.T) {
	policy := defaultPolicy
	policy.FailurePolicy = trainingv1alpha1.FailurePolicyFailOpen

	decision := Evaluate(EvaluatorInput{
		RTJ:          testRTJ(),
		Policy:       policy,
		CatalogError: fmt.Errorf("timeout"),
		Now:          testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonStorageUnavailable)
}

// --- Test: no catalog configured, allow initial launch ---

func TestEvaluateNoCatalogAllowInitial(t *testing.T) {
	decision := Evaluate(EvaluatorInput{
		RTJ:            testRTJ(),
		Policy:         defaultPolicy,
		CatalogQueried: false,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonInitialLaunchReady)
}

// --- Test: no catalog configured, initial blocked, FailClosed ---

func TestEvaluateNoCatalogBlockedFailClosed(t *testing.T) {
	policy := defaultPolicy
	policy.AllowInitialLaunchWithoutCheckpoint = false
	policy.FailurePolicy = trainingv1alpha1.FailurePolicyFailClosed

	decision := Evaluate(EvaluatorInput{
		RTJ:            testRTJ(),
		Policy:         policy,
		CatalogQueried: false,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateRetry, ReasonStorageUnavailable)
}

// --- Test: no catalog configured, initial blocked, FailOpen ---

func TestEvaluateNoCatalogBlockedFailOpen(t *testing.T) {
	policy := defaultPolicy
	policy.AllowInitialLaunchWithoutCheckpoint = false
	policy.FailurePolicy = trainingv1alpha1.FailurePolicyFailOpen

	decision := Evaluate(EvaluatorInput{
		RTJ:            testRTJ(),
		Policy:         policy,
		CatalogQueried: false,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonStorageUnavailable)
}

// --- Test: checkpoint found, no age limit configured ---

func TestEvaluateCheckpointReadyNoAgeLimit(t *testing.T) {
	policy := defaultPolicy // MaxCheckpointAge is nil

	// Very old checkpoint, but no age limit.
	ckpt := testCheckpoint("ckpt-ancient", 1000*time.Hour)

	decision := Evaluate(EvaluatorInput{
		RTJ:                testRTJ(),
		Policy:             policy,
		SelectedCheckpoint: ckpt,
		CatalogQueried:     true,
		Now:                testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonCheckpointReady)
}

// --- Test: re-admission after prior run, checkpoint found ---

func TestEvaluateResumeAfterPreemptionCheckpointAvailable(t *testing.T) {
	rtj := testRTJ()
	rtj.Status.CurrentRunAttempt = 3

	ckpt := testCheckpoint("ckpt-preempted", 5*time.Minute)

	decision := Evaluate(EvaluatorInput{
		RTJ:                rtj,
		Policy:             defaultPolicy,
		SelectedCheckpoint: ckpt,
		CatalogQueried:     true,
		Now:                testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonCheckpointReady)
}

// --- Test: re-admission after prior run, no checkpoint, allow initial true ---

func TestEvaluateResumeNoCheckpointAllowInitialTrue(t *testing.T) {
	rtj := testRTJ()
	rtj.Status.CurrentRunAttempt = 1

	decision := Evaluate(EvaluatorInput{
		RTJ:            rtj,
		Policy:         defaultPolicy,
		CatalogQueried: true,
		Now:            testNow,
	})

	assertDecision(t, decision, kueuev1beta2.CheckStateReady, ReasonInitialLaunchReady)
}

// --- Test: catalog error takes precedence over nil checkpoint ---

func TestEvaluateCatalogErrorPrecedesNoCheckpoint(t *testing.T) {
	policy := defaultPolicy
	policy.AllowInitialLaunchWithoutCheckpoint = false

	decision := Evaluate(EvaluatorInput{
		RTJ:          testRTJ(),
		Policy:       policy,
		CatalogError: fmt.Errorf("DNS resolution failed"),
		Now:          testNow,
	})

	// Should retry (FailClosed), not reject.
	assertDecision(t, decision, kueuev1beta2.CheckStateRetry, ReasonStorageUnavailable)
}

// --- helpers ---

func assertDecision(t *testing.T, got ReadinessDecision, wantState kueuev1beta2.CheckState, wantReason string) {
	t.Helper()
	if got.State != wantState {
		t.Errorf("expected state %v, got %v (reason=%s, message=%s)", wantState, got.State, got.Reason, got.Message)
	}
	if got.Reason != wantReason {
		t.Errorf("expected reason %q, got %q (state=%v, message=%s)", wantReason, got.Reason, got.State, got.Message)
	}
	if got.Message == "" {
		t.Error("expected non-empty message")
	}
}
