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

// phase8RTJView extends rtjView with Phase 8 DRA device status fields.
type phase8RTJView struct {
	Metadata struct {
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Suspend *bool `json:"suspend"`
		Control struct {
			DesiredState string `json:"desiredState"`
		} `json:"control"`
		Devices *struct {
			Mode   string `json:"mode"`
			Claims []struct {
				Name       string   `json:"name"`
				Containers []string `json:"containers"`
				Request    struct {
					DeviceClassName string   `json:"deviceClassName"`
					Count           int32    `json:"count"`
					Selectors       []string `json:"selectors"`
				} `json:"request"`
			} `json:"claims"`
		} `json:"devices"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		PauseRequestID    string `json:"pauseRequestID"`
		CurrentRunAttempt int32  `json:"currentRunAttempt"`
		ActiveJobSetName  string `json:"activeJobSetName"`
		CurrentSuspension *struct {
			Suspended bool   `json:"suspended"`
			Source    string `json:"source"`
			Reason    string `json:"reason"`
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
		Devices *struct {
			DeviceMode                     string `json:"deviceMode"`
			CurrentDeviceProfileFingerprint string `json:"currentDeviceProfileFingerprint"`
			RequestedDeviceClasses         []string `json:"requestedDeviceClasses"`
			ResourceClaimTemplateRefs      []struct {
				Name      string `json:"name"`
				ClaimName string `json:"claimName"`
			} `json:"resourceClaimTemplateRefs"`
			ClaimAllocationState                   string `json:"claimAllocationState"`
			AllocatedClaimCount                    int32  `json:"allocatedClaimCount"`
			LastClaimFailureReason                 string `json:"lastClaimFailureReason"`
			LastCheckpointDeviceProfileFingerprint string `json:"lastCheckpointDeviceProfileFingerprint"`
			LastResumeDeviceProfileFingerprint     string `json:"lastResumeDeviceProfileFingerprint"`
		} `json:"devices"`
	} `json:"status"`
}

// phase8Env holds the Phase 8 test environment state.
type phase8Env struct {
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

func setupPhase8Env(t *testing.T) phase8Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run Phase 8 e2e tests")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PHASE8_TRAINER_IMAGE"))
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
		t.Skip("set PHASE8_TRAINER_IMAGE (or any earlier phase trainer image env var)")
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

	// Verify Phase 8 ClusterQueue exists.
	output, err := kubectlOutput(root, "get", "clusterqueues.kueue.x-k8s.io", "phase8-cq")
	if err != nil {
		t.Skipf("Phase 8 ClusterQueue not found (run make phase8-up first): %s", output)
	}

	// Verify example-gpu DeviceClass exists.
	output, err = kubectlOutput(root, "get", "deviceclasses.resource.k8s.io", "example-gpu")
	if err != nil {
		t.Skipf("example-gpu DeviceClass not found (run make phase8-up first): %s", output)
	}

	// Verify DRA driver is running (ResourceSlices should be published).
	output, err = kubectlOutput(root, "get", "resourceslices.resource.k8s.io",
		"-l", "checkpoint-native.dev/driver=dra-example")
	if err != nil {
		// Also try without label in case the driver doesn't set it.
		output, err = kubectlOutput(root, "get", "resourceslices.resource.k8s.io",
			"-o", "jsonpath={.items[*].spec.driver}")
		if err != nil || !strings.Contains(output, "dra.example.dev") {
			t.Skipf("example DRA driver ResourceSlices not found (run make phase8-up first): %s", output)
		}
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

	return phase8Env{
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

func getPhase8RTJ(repoRoot, namespace, name string) (phase8RTJView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return phase8RTJView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view phase8RTJView
	if err := json.Unmarshal(output, &view); err != nil {
		return phase8RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func waitForPhase8RTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(phase8RTJView) bool,
) phase8RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase8RTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase8RTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %q; last phase=%s suspend=%v devices=%+v conditions=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		view.Status.Devices,
		view.Status.Conditions,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase8RTJView{}
}

func waitForPhase8Phase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase8RTJView {
	t.Helper()
	return waitForPhase8RTJState(t, repoRoot, namespace, name,
		fmt.Sprintf("phase=%s", phase), timeout, operatorLogs, portForwardLogs,
		func(view phase8RTJView) bool {
			return view.Status.Phase == phase
		},
	)
}

// resourceClaimTemplateListView is a list of ResourceClaimTemplate objects.
type resourceClaimTemplateListView struct {
	Items []resourceClaimTemplateView `json:"items"`
}

type resourceClaimTemplateView struct {
	Metadata struct {
		Name            string               `json:"name"`
		Labels          map[string]string     `json:"labels"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Spec struct {
			Devices struct {
				Requests []struct {
					Name            string `json:"name"`
					DeviceClassName string `json:"deviceClassName"`
					Count           int64  `json:"count"`
				} `json:"requests"`
			} `json:"devices"`
		} `json:"spec"`
	} `json:"spec"`
}

// getResourceClaimTemplatesForRTJ lists ResourceClaimTemplates with the RTJ label.
func getResourceClaimTemplatesForRTJ(repoRoot, namespace, rtjName string) ([]resourceClaimTemplateView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"resourceclaimtemplates.resource.k8s.io",
		"-l", "training.checkpoint.example.io/rtj-name="+rtjName,
		"-o", "json")
	if err != nil {
		return nil, fmt.Errorf("get ResourceClaimTemplates: %w: %s", err, output)
	}
	var list resourceClaimTemplateListView
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("decode ResourceClaimTemplates json: %w", err)
	}
	return list.Items, nil
}

// waitForResourceClaimTemplates waits for the expected number of
// ResourceClaimTemplates to appear for a given RTJ.
func waitForResourceClaimTemplates(
	t *testing.T,
	repoRoot string,
	namespace string,
	rtjName string,
	expectedCount int,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) []resourceClaimTemplateView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		templates, err := getResourceClaimTemplatesForRTJ(repoRoot, namespace, rtjName)
		if err == nil && len(templates) >= expectedCount {
			return templates
		}
		time.Sleep(2 * time.Second)
	}

	templates, _ := getResourceClaimTemplatesForRTJ(repoRoot, namespace, rtjName)
	t.Fatalf(
		"timed out waiting for %d ResourceClaimTemplates for RTJ %s/%s; found %d templates=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		expectedCount,
		namespace,
		rtjName,
		len(templates),
		templates,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return nil
}

// resourceClaimListView is a list of ResourceClaim objects.
type resourceClaimListView struct {
	Items []resourceClaimView `json:"items"`
}

type resourceClaimView struct {
	Metadata struct {
		Name        string            `json:"name"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Devices struct {
			Requests []struct {
				Name            string `json:"name"`
				DeviceClassName string `json:"deviceClassName"`
				Count           int64  `json:"count"`
			} `json:"requests"`
		} `json:"devices"`
	} `json:"spec"`
	Status struct {
		Allocation *struct {
			Devices struct {
				Results []struct {
					Request string `json:"request"`
					Driver  string `json:"driver"`
					Pool    string `json:"pool"`
					Device  string `json:"device"`
				} `json:"results"`
			} `json:"devices"`
		} `json:"allocation"`
	} `json:"status"`
}

// getResourceClaimsInNamespace lists all ResourceClaims in the namespace.
func getResourceClaimsInNamespace(repoRoot, namespace string) ([]resourceClaimView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"resourceclaims.resource.k8s.io", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("get ResourceClaims: %w: %s", err, output)
	}
	var list resourceClaimListView
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("decode ResourceClaims json: %w", err)
	}
	return list.Items, nil
}

// filterClaimsForRTJ returns ResourceClaims matching the RTJ name label
// or claim-template-name annotation.
func filterClaimsForRTJ(claims []resourceClaimView, rtjName string) []resourceClaimView {
	var filtered []resourceClaimView
	for _, claim := range claims {
		if claim.Metadata.Labels["training.checkpoint.example.io/rtj-name"] == rtjName {
			filtered = append(filtered, claim)
			continue
		}
		if tmpl, ok := claim.Metadata.Annotations["resource.kubernetes.io/claim-template-name"]; ok {
			if strings.HasPrefix(tmpl, rtjName+"-") {
				filtered = append(filtered, claim)
			}
		}
	}
	return filtered
}

// phase8WorkloadView is a Workload view with DRA-related podSet fields.
type phase8WorkloadView struct {
	Metadata struct {
		Name            string               `json:"name"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		PodSets []struct {
			Name  string `json:"name"`
			Count int32  `json:"count"`
			Template struct {
				Spec struct {
					ResourceClaims []struct {
						Name                      string  `json:"name"`
						ResourceClaimTemplateName *string `json:"resourceClaimTemplateName"`
					} `json:"resourceClaims"`
					Containers []struct {
						Name      string `json:"name"`
						Resources struct {
							Claims []struct {
								Name string `json:"name"`
							} `json:"claims"`
						} `json:"resources"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"podSets"`
	} `json:"spec"`
	Status struct {
		Admission *struct {
			ClusterQueue      string `json:"clusterQueue"`
			PodSetAssignments []struct {
				Name            string           `json:"name"`
				Count           *int32           `json:"count"`
				ResourceUsage   map[string]string `json:"resourceUsage"`
			} `json:"podSetAssignments"`
		} `json:"admission"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

func getPhase8Workload(repoRoot, namespace, name string) (phase8WorkloadView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get",
		"workloads.kueue.x-k8s.io", name, "-o", "json")
	if err != nil {
		return phase8WorkloadView{}, fmt.Errorf("kubectl get workload %s: %w: %s", name, err, output)
	}
	var view phase8WorkloadView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return phase8WorkloadView{}, fmt.Errorf("decode workload json: %w", err)
	}
	return view, nil
}

func findPhase8WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName string) (phase8WorkloadView, bool, error) {
	list, err := getWorkloads(repoRoot, namespace)
	if err != nil {
		return phase8WorkloadView{}, false, err
	}
	for _, item := range list.Items {
		for _, ref := range item.Metadata.OwnerReferences {
			if ref.Kind == ownerKind && ref.Name == ownerName {
				detail, err := getPhase8Workload(repoRoot, namespace, item.Metadata.Name)
				if err != nil {
					return phase8WorkloadView{}, false, err
				}
				return detail, true, nil
			}
		}
	}
	return phase8WorkloadView{}, false, nil
}

func waitForPhase8WorkloadOwnedBy(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase8WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase8WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
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
	return phase8WorkloadView{}
}

// waitForPhase8WorkloadAdmitted waits for a Workload to be admitted to a ClusterQueue.
func waitForPhase8WorkloadAdmitted(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase8WorkloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findPhase8WorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
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
	return phase8WorkloadView{}
}

// cleanupPhase8RTJ deletes a Phase 8 RTJ and its child JobSet (best-effort).
func cleanupPhase8RTJ(t *testing.T, env phase8Env, name string, runAttempt int32) {
	t.Helper()
	runKubectl(t, env.repoRoot, "-n", env.namespace, "delete", pauseFlowResource, name, "--ignore-not-found=true")
	childName := rtjjobset.ChildJobSetName(name, runAttempt)
	_, _ = kubectlOutput(env.repoRoot, "-n", env.namespace, "delete", "jobset", childName, "--ignore-not-found=true")
}

// hasPhase8Condition checks whether the RTJ has a condition with the given type and status.
func hasPhase8Condition(view phase8RTJView, condType, condStatus string) bool {
	for _, c := range view.Status.Conditions {
		if c.Type == condType && c.Status == condStatus {
			return true
		}
	}
	return false
}

// findPhase8Condition returns the first condition matching the given type, or nil.
func findPhase8Condition(view phase8RTJView, condType string) *struct {
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

// getClusterQueueUsage returns the resource usage for a given ClusterQueue and resource name.
func getClusterQueueUsage(repoRoot, cqName, resourceName string) (string, error) {
	jsonpath := fmt.Sprintf(
		`{.status.flavorsReservation[*].resources[?(@.name=="%s")].total}`,
		resourceName,
	)
	output, err := kubectlOutput(repoRoot, "get", "clusterqueues.kueue.x-k8s.io", cqName,
		"-o", "jsonpath="+jsonpath)
	if err != nil {
		return "", fmt.Errorf("get ClusterQueue usage: %w: %s", err, output)
	}
	return strings.TrimSpace(output), nil
}
