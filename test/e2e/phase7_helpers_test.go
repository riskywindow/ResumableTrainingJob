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

// phase7RTJView extends rtjView with Phase 7 status fields for launch gate,
// provisioning, startup recovery, and capacity.
type phase7RTJView struct {
	Metadata struct {
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Suspend *bool `json:"suspend"`
		Control struct {
			DesiredState string `json:"desiredState"`
		} `json:"control"`
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
		LaunchGate *struct {
			State                 string            `json:"state"`
			Reason                string            `json:"reason"`
			Message               string            `json:"message"`
			AdmissionCheckSummary map[string]string `json:"admissionCheckSummary"`
			TopologyGateState     string            `json:"topologyGateState"`
		} `json:"launchGate"`
		Provisioning *struct {
			State                  string `json:"state"`
			ProvisioningRequestRef *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"provisioningRequestRef"`
			Attempt int32  `json:"attempt"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"provisioning"`
		StartupRecovery *struct {
			StartupState          string `json:"startupState"`
			PodsReadyState        string `json:"podsReadyState"`
			LastLaunchFailureReason string `json:"lastLaunchFailureReason"`
			LastEvictionReason     string `json:"lastEvictionReason"`
			LastRequeueReason      string `json:"lastRequeueReason"`
		} `json:"startupRecovery"`
		Capacity *struct {
			GuaranteeActive bool   `json:"guaranteeActive"`
			Reason          string `json:"reason"`
		} `json:"capacity"`
	} `json:"status"`
}

// phase7WorkloadView is a Workload view with admission check details for Phase 7.
type phase7WorkloadView struct {
	Metadata struct {
		Name            string               `json:"name"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Priority *int32 `json:"priority"`
	} `json:"spec"`
	Status struct {
		Admission *struct {
			ClusterQueue      string `json:"clusterQueue"`
			PodSetAssignments []struct {
				Name  string `json:"name"`
				Count *int32 `json:"count"`
			} `json:"podSetAssignments"`
		} `json:"admission"`
		AdmissionChecks []struct {
			Name               string `json:"name"`
			State              string `json:"state"`
			Message            string `json:"message"`
			LastTransitionTime string `json:"lastTransitionTime"`
			PodSetUpdates      []struct {
				Name         string            `json:"name"`
				Labels       map[string]string `json:"labels"`
				Annotations  map[string]string `json:"annotations"`
				NodeSelector map[string]string `json:"nodeSelector"`
			} `json:"podSetUpdates"`
		} `json:"admissionChecks"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

// phase7Env holds the Phase 7 test environment state.
type phase7Env struct {
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

func setupPhase7Env(t *testing.T) phase7Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run Phase 7 e2e tests")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PHASE7_TRAINER_IMAGE"))
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
		t.Skip("set PHASE7_TRAINER_IMAGE (or any earlier phase trainer image env var)")
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

	// Verify Phase 7 ClusterQueue exists.
	output, err := kubectlOutput(root, "get", "clusterqueues.kueue.x-k8s.io", "phase7-cq")
	if err != nil {
		t.Skipf("Phase 7 ClusterQueue not found (run make phase7-up first): %s", output)
	}

	// Verify fake-provisioner is running.
	output, err = kubectlOutput(root, "-n", namespace, "get", "deployment", "fake-provisioner")
	if err != nil {
		t.Skipf("fake-provisioner Deployment not found (run make phase7-up first): %s", output)
	}

	// Verify ProvisioningRequest CRD is installed.
	output, err = kubectlOutput(root, "get", "crd", "provisioningrequests.autoscaling.x-k8s.io")
	if err != nil {
		t.Skipf("ProvisioningRequest CRD not found (run make phase7-up first): %s", output)
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
		"--provisioning-ac-names=dev-provisioning,dev-provisioning-failure,dev-provisioning-expiry",
	)

	return phase7Env{
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

func getPhase7RTJ(repoRoot, namespace, name string) (phase7RTJView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return phase7RTJView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view phase7RTJView
	if err := json.Unmarshal(output, &view); err != nil {
		return phase7RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func waitForPhase7RTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(phase7RTJView) bool,
) phase7RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase7RTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase7RTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %q; last phase=%s suspend=%v launchGate=%+v provisioning=%+v startupRecovery=%+v capacity=%+v conditions=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		view.Status.LaunchGate,
		view.Status.Provisioning,
		view.Status.StartupRecovery,
		view.Status.Capacity,
		view.Status.Conditions,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase7RTJView{}
}

func waitForPhase7Phase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase7RTJView {
	t.Helper()
	return waitForPhase7RTJState(t, repoRoot, namespace, name,
		fmt.Sprintf("phase=%s", phase), timeout, operatorLogs, portForwardLogs,
		func(view phase7RTJView) bool {
			return view.Status.Phase == phase
		},
	)
}

func getPhase7Workload(repoRoot, namespace, name string) (phase7WorkloadView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"workloads.kueue.x-k8s.io", name, "-o", "json")
	if err != nil {
		return phase7WorkloadView{}, fmt.Errorf("kubectl get workload %s: %w: %s", name, err, output)
	}
	var view phase7WorkloadView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return phase7WorkloadView{}, fmt.Errorf("decode workload json: %w", err)
	}
	return view, nil
}

func findPhase7WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName string) (phase7WorkloadView, bool, error) {
	list, err := getWorkloads(repoRoot, namespace)
	if err != nil {
		return phase7WorkloadView{}, false, err
	}
	for _, item := range list.Items {
		for _, ref := range item.Metadata.OwnerReferences {
			if ref.Kind == ownerKind && ref.Name == ownerName {
				detail, err := getPhase7Workload(repoRoot, namespace, item.Metadata.Name)
				if err != nil {
					return phase7WorkloadView{}, false, err
				}
				return detail, true, nil
			}
		}
	}
	return phase7WorkloadView{}, false, nil
}

func waitForPhase7WorkloadOwnedBy(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase7WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase7WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
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
	return phase7WorkloadView{}
}

// waitForPhase7WorkloadAdmissionCheck waits for a Workload's admission check
// to reach the expected state (e.g., "Ready", "Rejected").
func waitForPhase7WorkloadAdmissionCheck(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	checkName string,
	expectedState string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase7WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase7WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
		if err == nil && found {
			for _, ac := range workload.Status.AdmissionChecks {
				if ac.Name == checkName && ac.State == expectedState {
					return workload
				}
			}
		}
		time.Sleep(2 * time.Second)
	}

	workload, _, _ := findPhase7WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
	t.Fatalf(
		"timed out waiting for workload admission check %q to reach state %q; checks=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		checkName,
		expectedState,
		workload.Status.AdmissionChecks,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase7WorkloadView{}
}

// getProvisioningRequests returns a list of ProvisioningRequest names in the namespace.
func getProvisioningRequests(repoRoot, namespace string) ([]string, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"provisioningrequests.autoscaling.x-k8s.io",
		"-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return nil, fmt.Errorf("get provisioningrequests: %w: %s", err, output)
	}
	names := strings.Fields(strings.TrimSpace(output))
	return names, nil
}

// getProvisioningRequestCondition returns the condition status for a given type.
func getProvisioningRequestCondition(repoRoot, namespace, name, conditionType string) (string, error) {
	jsonpath := fmt.Sprintf(`{.status.conditions[?(@.type=="%s")].status}`, conditionType)
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"provisioningrequests.autoscaling.x-k8s.io", name,
		"-o", "jsonpath="+jsonpath)
	if err != nil {
		return "", fmt.Errorf("get provisioningrequest condition: %w: %s", err, output)
	}
	return strings.TrimSpace(output), nil
}

// assertNoChildJobSetExistsPhase7 verifies no child JobSet is created within
// an observation window. Reuses naming from rtjjobset.ChildJobSetName.
func assertNoChildJobSetExistsPhase7(t *testing.T, repoRoot, namespace, rtjName string, runAttempt int32) {
	t.Helper()
	childName := rtjjobset.ChildJobSetName(rtjName, runAttempt)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := getJobSetDetail(repoRoot, namespace, childName)
		if err == nil {
			t.Fatalf("expected no child JobSet %s to exist, but found one", childName)
		}
		time.Sleep(1 * time.Second)
	}
}

// cleanupPhase7RTJ deletes a Phase 7 RTJ and its child JobSet (best-effort).
func cleanupPhase7RTJ(t *testing.T, env phase7Env, name string, runAttempt int32) {
	t.Helper()
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, name, "--ignore-not-found=true")
	childName := rtjjobset.ChildJobSetName(name, runAttempt)
	_, _ = kubectlOutput(env.repoRoot, "-n", env.namespace, "delete", "jobset", childName, "--ignore-not-found=true")
}

// hasCondition checks whether the RTJ has a condition with the given type and status.
func hasPhase7Condition(view phase7RTJView, condType, condStatus string) bool {
	for _, c := range view.Status.Conditions {
		if c.Type == condType && c.Status == condStatus {
			return true
		}
	}
	return false
}

// findPhase7Condition returns the first condition matching the given type, or nil.
func findPhase7Condition(view phase7RTJView, condType string) *struct {
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

// waitForProvisioningRequestExists waits for at least one ProvisioningRequest
// to exist in the namespace that contains the given substring in its name.
func waitForProvisioningRequestExists(
	t *testing.T,
	repoRoot string,
	namespace string,
	nameSubstring string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		names, err := getProvisioningRequests(repoRoot, namespace)
		if err == nil {
			for _, name := range names {
				if strings.Contains(name, nameSubstring) {
					return name
				}
			}
		}
		time.Sleep(2 * time.Second)
	}

	names, _ := getProvisioningRequests(repoRoot, namespace)
	t.Fatalf(
		"timed out waiting for ProvisioningRequest containing %q in namespace %s; found: %v\noperator logs:\n%s\nport-forward logs:\n%s",
		nameSubstring,
		namespace,
		names,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return ""
}
