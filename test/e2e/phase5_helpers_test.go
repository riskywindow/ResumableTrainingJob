package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	rtjjobset "github.com/example/checkpoint-native-preemption-controller/internal/jobset"
)

// phase5RTJView extends rtjView with Phase 5 priority shaping status fields.
type phase5RTJView struct {
	Metadata struct {
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Suspend *bool `json:"suspend"`
		Control struct {
			DesiredState string `json:"desiredState"`
		} `json:"control"`
		PriorityPolicyRef *struct {
			Name string `json:"name"`
		} `json:"priorityPolicyRef"`
		WorkloadPriorityClassName string `json:"workloadPriorityClassName"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		PauseRequestID    string `json:"pauseRequestID"`
		CurrentRunAttempt int32  `json:"currentRunAttempt"`
		ActiveJobSetName  string `json:"activeJobSetName"`
		CurrentSuspension *struct {
			Suspended bool   `json:"suspended"`
			Source    string `json:"source"`
			Reason   string `json:"reason"`
		} `json:"currentSuspension"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"conditions"`
		SelectedCheckpoint *struct {
			ManifestURI string `json:"manifestURI"`
		} `json:"selectedCheckpoint"`
		LastCompletedCheckpoint *struct {
			ManifestURI    string `json:"manifestURI"`
			CompletionTime string `json:"completionTime"`
		} `json:"lastCompletedCheckpoint"`
		PriorityShaping *struct {
			BasePriority                int32  `json:"basePriority"`
			EffectivePriority           int32  `json:"effectivePriority"`
			PreemptionState             string `json:"preemptionState"`
			PreemptionStateReason       string `json:"preemptionStateReason"`
			ProtectedUntil              string `json:"protectedUntil"`
			LastCompletedCheckpointTime string `json:"lastCompletedCheckpointTime"`
			CheckpointAge               string `json:"checkpointAge"`
			LastYieldTime               string `json:"lastYieldTime"`
			LastResumeTime              string `json:"lastResumeTime"`
			RecentYieldCount            int32  `json:"recentYieldCount"`
			AppliedPolicyRef            string `json:"appliedPolicyRef"`
		} `json:"priorityShaping"`
	} `json:"status"`
}

// phase5WorkloadView is a Workload view that includes Spec.Priority.
type phase5WorkloadView struct {
	Metadata struct {
		Name            string               `json:"name"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Priority *int32 `json:"priority"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"conditions"`
	} `json:"status"`
}

// phase5Env holds the Phase 5 test environment state.
type phase5Env struct {
	repoRoot      string
	namespace     string
	trainerImage  string
	minioEndpoint string
	minioURL      string
	accessKey     string
	secretKey     string
	region        string
	operatorLogs  *bytes.Buffer
	portForward   *bytes.Buffer
}

func setupPhase5Env(t *testing.T) phase5Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run Phase 5 e2e tests")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PHASE5_TRAINER_IMAGE"))
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE4_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE3_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE2_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PAUSE_FLOW_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		t.Skip("set PHASE5_TRAINER_IMAGE (or PHASE4/PHASE3/PHASE2/PAUSE_FLOW_TRAINER_IMAGE)")
	}

	root := repoRoot(t)
	namespace := envOrDefault("DEV_NAMESPACE", "checkpoint-dev")
	minioEndpoint := "127.0.0.1:9000"
	minioURL := "http://" + minioEndpoint
	accessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123")
	region := envOrDefault("MINIO_REGION", "us-east-1")

	runKubectl(t, root, "cluster-info")
	runKubectl(t, root, "-n", namespace, "get", "deployment", "minio")

	// Verify Phase 5 queue and policy exist.
	output, err := kubectlOutput(root, "get", "clusterqueues.kueue.x-k8s.io", "phase5-cq")
	if err != nil {
		t.Skipf("Phase 5 ClusterQueue not found (run make phase5-up first): %s", output)
	}
	output, err = kubectlOutput(root, "get",
		"checkpointprioritypolicies.training.checkpoint.example.io",
		"dev-checkpoint-priority")
	if err != nil {
		t.Skipf("Phase 5 CheckpointPriorityPolicy not found (run make phase5-up first): %s", output)
	}

	portForwardCtx, stopPortForward := context.WithCancel(context.Background())
	t.Cleanup(stopPortForward)
	portForwardLogs := startBackgroundCommand(t, root, portForwardCtx, nil,
		"kubectl", "-n", namespace, "port-forward", "service/minio", "9000:9000")
	waitForTCP(t, minioEndpoint, 20*time.Second)

	operatorCtx, stopOperator := context.WithCancel(context.Background())
	t.Cleanup(stopOperator)

	operatorLogs := startBackgroundCommand(
		t,
		root,
		operatorCtx,
		[]string{
			"AWS_ENDPOINT_URL=" + minioURL,
			"AWS_ACCESS_KEY_ID=" + accessKey,
			"AWS_SECRET_ACCESS_KEY=" + secretKey,
			"AWS_REGION=" + region,
		},
		"go",
		"run",
		"./cmd/operator",
		"--leader-elect=false",
	)

	return phase5Env{
		repoRoot:      root,
		namespace:     namespace,
		trainerImage:  trainerImage,
		minioEndpoint: minioEndpoint,
		minioURL:      minioURL,
		accessKey:     accessKey,
		secretKey:     secretKey,
		region:        region,
		operatorLogs:  operatorLogs,
		portForward:   portForwardLogs,
	}
}

func getPhase5RTJ(repoRoot, namespace, name string) (phase5RTJView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return phase5RTJView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view phase5RTJView
	if err := json.Unmarshal(output, &view); err != nil {
		return phase5RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func waitForPhase5RTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(phase5RTJView) bool,
) phase5RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase5RTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase5RTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %q; last phase=%s suspend=%v priorityShaping=%+v annotations=%v\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		view.Status.PriorityShaping,
		view.Metadata.Annotations,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase5RTJView{}
}

func waitForPhase5Phase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase5RTJView {
	t.Helper()
	return waitForPhase5RTJState(t, repoRoot, namespace, name,
		fmt.Sprintf("phase=%s", phase), timeout, operatorLogs, portForwardLogs,
		func(view phase5RTJView) bool {
			return view.Status.Phase == phase
		},
	)
}

func getPhase5Workload(repoRoot, namespace, name string) (phase5WorkloadView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"workloads.kueue.x-k8s.io", name, "-o", "json")
	if err != nil {
		return phase5WorkloadView{}, fmt.Errorf("kubectl get workload %s: %w: %s", name, err, output)
	}
	var view phase5WorkloadView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return phase5WorkloadView{}, fmt.Errorf("decode workload json: %w", err)
	}
	return view, nil
}

func findPhase5WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName string) (phase5WorkloadView, bool, error) {
	list, err := getWorkloads(repoRoot, namespace)
	if err != nil {
		return phase5WorkloadView{}, false, err
	}
	for _, item := range list.Items {
		for _, ref := range item.Metadata.OwnerReferences {
			if ref.Kind == ownerKind && ref.Name == ownerName {
				detail, err := getPhase5Workload(repoRoot, namespace, item.Metadata.Name)
				if err != nil {
					return phase5WorkloadView{}, false, err
				}
				return detail, true, nil
			}
		}
	}
	return phase5WorkloadView{}, false, nil
}

func waitForPhase5WorkloadOwnedBy(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase5WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase5WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
		if err == nil && found {
			return workload
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for workload owned by %s/%s in namespace %s\noperator logs:\n%s\nport-forward logs:\n%s",
		ownerKind,
		ownerName,
		namespace,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase5WorkloadView{}
}

// assertPriorityShapingState checks that the RTJ has a priorityShaping status
// with the expected preemption state.
func assertPriorityShapingState(t *testing.T, view phase5RTJView, expectedState string) {
	t.Helper()
	if view.Status.PriorityShaping == nil {
		t.Fatalf("expected priorityShaping status to be present, got nil")
	}
	if view.Status.PriorityShaping.PreemptionState != expectedState {
		t.Fatalf("expected preemptionState %q, got %q (effective=%d, base=%d, reason=%q)",
			expectedState,
			view.Status.PriorityShaping.PreemptionState,
			view.Status.PriorityShaping.EffectivePriority,
			view.Status.PriorityShaping.BasePriority,
			view.Status.PriorityShaping.PreemptionStateReason,
		)
	}
}

// assertEffectivePriorityAbove checks that the RTJ's effective priority exceeds
// a threshold value.
func assertEffectivePriorityAbove(t *testing.T, view phase5RTJView, threshold int32) {
	t.Helper()
	if view.Status.PriorityShaping == nil {
		t.Fatalf("expected priorityShaping status to be present, got nil")
	}
	if view.Status.PriorityShaping.EffectivePriority <= threshold {
		t.Fatalf("expected effectivePriority > %d, got %d (state=%s)",
			threshold,
			view.Status.PriorityShaping.EffectivePriority,
			view.Status.PriorityShaping.PreemptionState,
		)
	}
}

// assertEffectivePriorityBelow checks that the RTJ's effective priority is
// below a threshold value.
func assertEffectivePriorityBelow(t *testing.T, view phase5RTJView, threshold int32) {
	t.Helper()
	if view.Status.PriorityShaping == nil {
		t.Fatalf("expected priorityShaping status to be present, got nil")
	}
	if view.Status.PriorityShaping.EffectivePriority >= threshold {
		t.Fatalf("expected effectivePriority < %d, got %d (state=%s)",
			threshold,
			view.Status.PriorityShaping.EffectivePriority,
			view.Status.PriorityShaping.PreemptionState,
		)
	}
}

// cleanupPhase5RTJ deletes a Phase 5 RTJ and waits for its child JobSet to be deleted.
func cleanupPhase5RTJ(t *testing.T, env phase5Env, name string, runAttempt int32) {
	t.Helper()
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, name, "--ignore-not-found=true")
	childName := rtjjobset.ChildJobSetName(name, runAttempt)
	// Best-effort cleanup of child JobSet.
	_, _ = kubectlOutput(env.repoRoot, "-n", env.namespace, "delete", "jobset", childName, "--ignore-not-found=true")
}

// hasPriorityShapingCondition checks whether the RTJ has a PriorityShaping
// condition with the given status (e.g. "True" or "False").
func hasPriorityShapingCondition(view phase5RTJView, conditionStatus string) bool {
	for _, c := range view.Status.Conditions {
		if c.Type == "PriorityShaping" && c.Status == conditionStatus {
			return true
		}
	}
	return false
}
