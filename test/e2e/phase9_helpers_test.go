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

// phase9RTJView extends rtjView with Phase 9 elasticity status fields.
type phase9RTJView struct {
	Metadata struct {
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Suspend *bool `json:"suspend"`
		Control struct {
			DesiredState string `json:"desiredState"`
		} `json:"control"`
		Elasticity *struct {
			Mode                string `json:"mode"`
			TargetWorkerCount   *int32 `json:"targetWorkerCount"`
			InPlaceShrinkPolicy string `json:"inPlaceShrinkPolicy"`
			ReclaimMode         string `json:"reclaimMode"`
		} `json:"elasticity"`
		Parallelism *struct {
			MinCount             int32 `json:"minCount"`
			PreferredCount       int32 `json:"preferredCount"`
			AllowWorldSizeChange bool  `json:"allowWorldSizeChange"`
		} `json:"parallelism"`
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
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
		WorkloadReference *struct {
			Name string `json:"name"`
		} `json:"workloadReference"`
		SelectedCheckpoint *struct {
			ManifestURI         string `json:"manifestURI"`
			CompatibilityState  string `json:"compatibilityState"`
			CompatibilityReason string `json:"compatibilityReason"`
		} `json:"selectedCheckpoint"`
		LastCompletedCheckpoint *struct {
			ManifestURI string `json:"manifestURI"`
		} `json:"lastCompletedCheckpoint"`
		Admission *struct {
			AdmittedWorkerCount  int32             `json:"admittedWorkerCount"`
			PreferredWorkerCount int32             `json:"preferredWorkerCount"`
			AdmittedFlavors      map[string]string `json:"admittedFlavors"`
		} `json:"admission"`
		Elasticity *struct {
			DesiredWorkerCount       int32  `json:"desiredWorkerCount"`
			TargetWorkerCount        int32  `json:"targetWorkerCount"`
			ActiveWorkerCount        int32  `json:"activeWorkerCount"`
			AdmittedWorkerCount      int32  `json:"admittedWorkerCount"`
			ResizeState              string `json:"resizeState"`
			ResizeReason             string `json:"resizeReason"`
			CurrentExecutionMode     string `json:"currentExecutionMode"`
			ResizePath               string `json:"resizePath"`
			ReclaimableWorkerCount   int32  `json:"reclaimableWorkerCount"`
			ReclaimablePodsPublished bool   `json:"reclaimablePodsPublished"`
			InPlaceShrinkSupported   bool   `json:"inPlaceShrinkSupported"`
			LastResizeEvent          string `json:"lastResizeEvent"`
			LastResizeFailureReason  string `json:"lastResizeFailureReason"`
		} `json:"elasticity"`
	} `json:"status"`
}

// phase9WorkloadView is a Workload view with reclaimablePods for Phase 9.
type phase9WorkloadView struct {
	Metadata struct {
		Name            string               `json:"name"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		PodSets []struct {
			Name  string `json:"name"`
			Count int32  `json:"count"`
		} `json:"podSets"`
	} `json:"spec"`
	Status struct {
		Admission *struct {
			ClusterQueue      string `json:"clusterQueue"`
			PodSetAssignments []struct {
				Name          string            `json:"name"`
				Count         *int32            `json:"count"`
				ResourceUsage map[string]string `json:"resourceUsage"`
			} `json:"podSetAssignments"`
		} `json:"admission"`
		ReclaimablePods []struct {
			Name  string `json:"name"`
			Count int32  `json:"count"`
		} `json:"reclaimablePods"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

// phase9Env holds the Phase 9 test environment state.
type phase9Env struct {
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

func setupPhase9Env(t *testing.T) phase9Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run Phase 9 e2e tests")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PHASE9_TRAINER_IMAGE"))
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE8_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE7_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE6_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE5_TRAINER_IMAGE"))
	}
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
		t.Skip("set PHASE9_TRAINER_IMAGE (or any earlier phase trainer image env var)")
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

	// Verify Phase 9 ClusterQueue exists.
	output, err := kubectlOutput(root, "get", "clusterqueues.kueue.x-k8s.io", "phase9-cq")
	if err != nil {
		t.Skipf("Phase 9 ClusterQueue not found (run make phase9-up first): %s", output)
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

	return phase9Env{
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

func getPhase9RTJ(repoRoot, namespace, name string) (phase9RTJView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return phase9RTJView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view phase9RTJView
	if err := json.Unmarshal(output, &view); err != nil {
		return phase9RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func waitForPhase9RTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(phase9RTJView) bool,
) phase9RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase9RTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase9RTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %q; last phase=%s suspend=%v elasticity=%+v conditions=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		view.Status.Elasticity,
		view.Status.Conditions,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase9RTJView{}
}

func waitForPhase9Phase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase9RTJView {
	t.Helper()
	return waitForPhase9RTJState(t, repoRoot, namespace, name,
		fmt.Sprintf("phase=%s", phase), timeout, operatorLogs, portForwardLogs,
		func(view phase9RTJView) bool {
			return view.Status.Phase == phase
		},
	)
}

func getPhase9Workload(repoRoot, namespace, name string) (phase9WorkloadView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"workloads.kueue.x-k8s.io", name, "-o", "json")
	if err != nil {
		return phase9WorkloadView{}, fmt.Errorf("kubectl get workload %s: %w: %s", name, err, output)
	}
	var view phase9WorkloadView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return phase9WorkloadView{}, fmt.Errorf("decode workload json: %w", err)
	}
	return view, nil
}

func findPhase9WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName string) (phase9WorkloadView, bool, error) {
	list, err := getWorkloads(repoRoot, namespace)
	if err != nil {
		return phase9WorkloadView{}, false, err
	}
	for _, item := range list.Items {
		for _, ref := range item.Metadata.OwnerReferences {
			if ref.Kind == ownerKind && ref.Name == ownerName {
				detail, err := getPhase9Workload(repoRoot, namespace, item.Metadata.Name)
				if err != nil {
					return phase9WorkloadView{}, false, err
				}
				return detail, true, nil
			}
		}
	}
	return phase9WorkloadView{}, false, nil
}

func waitForPhase9WorkloadAdmitted(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase9WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase9WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
		if err == nil && found && workload.Status.Admission != nil {
			return workload
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for admitted workload owned by %s/%s in namespace %s\noperator logs:\n%s\nport-forward logs:\n%s",
		ownerKind,
		ownerName,
		namespace,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase9WorkloadView{}
}

// waitForPhase9WorkloadReclaimablePods waits for the Workload owned by the
// given RTJ to have at least one reclaimablePods entry matching the given
// PodSet name with count > 0.
func waitForPhase9WorkloadReclaimablePods(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerName string,
	podSetName string,
	expectedCount int32,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase9WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase9WorkloadOwnedBy(repoRoot, namespace,
			"ResumableTrainingJob", ownerName)
		if err == nil && found {
			for _, rp := range workload.Status.ReclaimablePods {
				if rp.Name == podSetName && rp.Count >= expectedCount {
					return workload
				}
			}
		}
		time.Sleep(2 * time.Second)
	}

	// Dump the last observed state for diagnosis.
	workload, _, _ := findPhase9WorkloadOwnedBy(repoRoot, namespace,
		"ResumableTrainingJob", ownerName)
	t.Fatalf(
		"timed out waiting for reclaimablePods[%s].count >= %d on Workload for RTJ %s; last reclaimablePods=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		podSetName,
		expectedCount,
		ownerName,
		workload.Status.ReclaimablePods,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase9WorkloadView{}
}

// cleanupPhase9RTJ deletes a Phase 9 RTJ and its child JobSets (best-effort).
func cleanupPhase9RTJ(t *testing.T, env phase9Env, name string, maxRunAttempt int32) {
	t.Helper()
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, name, "--ignore-not-found=true")
	for attempt := int32(1); attempt <= maxRunAttempt; attempt++ {
		childName := rtjjobset.ChildJobSetName(name, attempt)
		_, _ = kubectlOutput(env.repoRoot, "-n", env.namespace, "delete", "jobset", childName, "--ignore-not-found=true")
	}
}

// hasPhase9Condition checks whether the RTJ has a condition with the given type and status.
func hasPhase9Condition(view phase9RTJView, condType, condStatus string) bool {
	for _, c := range view.Status.Conditions {
		if c.Type == condType && c.Status == condStatus {
			return true
		}
	}
	return false
}

// findPhase9Condition returns the first condition matching the given type, or nil.
func findPhase9Condition(view phase9RTJView, condType string) *struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
} {
	for i := range view.Status.Conditions {
		if view.Status.Conditions[i].Type == condType {
			return &view.Status.Conditions[i]
		}
	}
	return nil
}

// patchPhase9RTJStatus patches the RTJ status subresource using kubectl.
// This is used to set fixture knobs like inPlaceShrinkSupported that
// are normally set by runtime detection.
func patchPhase9RTJStatus(t *testing.T, repoRoot, namespace, name, patch string) {
	t.Helper()
	runKubectl(t, repoRoot, "-n", namespace, "patch", pauseFlowResource, name,
		"--subresource=status", "--type=merge", "-p", patch)
}

// patchPhase9RTJSpec patches the RTJ spec using kubectl merge-patch.
func patchPhase9RTJSpec(t *testing.T, repoRoot, namespace, name, patch string) {
	t.Helper()
	runKubectl(t, repoRoot, "-n", namespace, "patch", pauseFlowResource, name,
		"--type=merge", "-p", patch)
}
