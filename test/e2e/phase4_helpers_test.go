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

// phase4RTJView extends phase3RTJView with Phase 4 status fields.
type phase4RTJView struct {
	Metadata struct {
		UID         string            `json:"uid"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Suspend *bool `json:"suspend"`
		Control struct {
			DesiredState string `json:"desiredState"`
		} `json:"control"`
		Resume struct {
			AllowWorldSizeChange bool `json:"allowWorldSizeChange"`
		} `json:"resume"`
		Topology *struct {
			Mode          string `json:"mode"`
			TopologyLevel string `json:"topologyLevel"`
		} `json:"topology"`
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
		SelectedCheckpoint *struct {
			ManifestURI string `json:"manifestURI"`
			WorldSize   int32  `json:"worldSize"`
		} `json:"selectedCheckpoint"`
		LastCompletedCheckpoint *struct {
			ManifestURI string `json:"manifestURI"`
			WorldSize   int32  `json:"worldSize"`
		} `json:"lastCompletedCheckpoint"`
		Admission *struct {
			AdmittedWorkerCount  int32             `json:"admittedWorkerCount"`
			PreferredWorkerCount int32             `json:"preferredWorkerCount"`
			AdmittedFlavors      map[string]string `json:"admittedFlavors"`
		} `json:"admission"`
		Restore *struct {
			LastCheckpointWorldSize int32  `json:"lastCheckpointWorldSize"`
			LastRestoreWorldSize    int32  `json:"lastRestoreWorldSize"`
			RestoreMode             string `json:"restoreMode"`
		} `json:"restore"`
		LaunchReadiness *struct {
			Ready     bool   `json:"ready"`
			GateState string `json:"gateState"`
			Reason    string `json:"reason"`
			Message   string `json:"message"`
		} `json:"launchReadiness"`
		Topology *struct {
			Levels  []string `json:"levels"`
			Domains []struct {
				Values []string `json:"values"`
				Count  int32    `json:"count"`
			} `json:"domains"`
		} `json:"topology"`
		EffectiveLaunchShape *struct {
			WorkerCount          int32  `json:"workerCount"`
			WorldSize            int32  `json:"worldSize"`
			ResumeMode           string `json:"resumeMode"`
			SelectedCheckpointID string `json:"selectedCheckpointID"`
		} `json:"effectiveLaunchShape"`
		WorkloadReference *struct {
			Name string `json:"name"`
		} `json:"workloadReference"`
	} `json:"status"`
}

// phase4Env holds the Phase 4 test environment state.
type phase4Env struct {
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

func setupPhase4Env(t *testing.T) phase4Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run e2e tests")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PHASE4_TRAINER_IMAGE"))
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
		t.Skip("set PHASE4_TRAINER_IMAGE (or PHASE3/PHASE2/PAUSE_FLOW_TRAINER_IMAGE)")
	}

	root := repoRoot(t)
	namespace := envOrDefault("PHASE4_NAMESPACE", "checkpoint-dev")
	minioEndpoint := "127.0.0.1:9000"
	minioURL := "http://" + minioEndpoint
	accessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123")
	region := envOrDefault("MINIO_REGION", "us-east-1")

	runKubectl(t, root, "cluster-info")
	runKubectl(t, root, "-n", namespace, "get", "deployment", "minio")

	// Verify Phase 4 queue exists.
	output, err := kubectlOutput(root, "get", "clusterqueues.kueue.x-k8s.io", "phase4-cq")
	if err != nil {
		t.Skipf("Phase 4 ClusterQueue not found (run make phase4-up first): %s", output)
	}

	// Verify AdmissionCheck exists.
	output, err = kubectlOutput(root, "get", "admissionchecks.kueue.x-k8s.io", "resume-readiness")
	if err != nil {
		t.Skipf("resume-readiness AdmissionCheck not found (run make phase4-up first): %s", output)
	}

	// Verify topology labels exist on at least one worker node.
	output, err = kubectlOutput(root, "get", "nodes", "-l", "topology.example.io/rack=rack-1", "-o", "name")
	if err != nil || strings.TrimSpace(output) == "" {
		t.Skipf("no nodes with topology.example.io/rack=rack-1 label (run make phase4-up first): %s", output)
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

	return phase4Env{
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

func getPhase4RTJ(repoRoot, namespace, name string) (phase4RTJView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return phase4RTJView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view phase4RTJView
	if err := json.Unmarshal(output, &view); err != nil {
		return phase4RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func waitForPhase4RTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(phase4RTJView) bool,
) phase4RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase4RTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase4RTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %q; last phase=%s suspend=%v launchReadiness=%+v topology=%+v\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		view.Status.LaunchReadiness,
		view.Status.Topology,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase4RTJView{}
}

func waitForPhase4Phase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase4RTJView {
	t.Helper()
	return waitForPhase4RTJState(t, repoRoot, namespace, name,
		fmt.Sprintf("phase=%s", phase), timeout, operatorLogs, portForwardLogs,
		func(view phase4RTJView) bool {
			return view.Status.Phase == phase
		},
	)
}

// workloadDetailView is a Workload view with admission check states and admission info.
type workloadDetailView struct {
	Metadata struct {
		Name            string               `json:"name"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
	Status struct {
		Admission *struct {
			ClusterQueue      string `json:"clusterQueue"`
			PodSetAssignments []struct {
				Name              string `json:"name"`
				Count             *int32 `json:"count"`
				TopologyAssignment *struct {
					Levels []string `json:"levels"`
				} `json:"topologyAssignment"`
			} `json:"podSetAssignments"`
		} `json:"admission"`
		AdmissionChecks []struct {
			Name    string `json:"name"`
			State   string `json:"state"`
			Message string `json:"message"`
		} `json:"admissionChecks"`
	} `json:"status"`
}

func getWorkloadDetail(repoRoot, namespace, name string) (workloadDetailView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get", "workloads.kueue.x-k8s.io", name, "-o", "json")
	if err != nil {
		return workloadDetailView{}, fmt.Errorf("kubectl get workload %s: %w: %s", name, err, output)
	}
	var view workloadDetailView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return workloadDetailView{}, fmt.Errorf("decode workload json: %w", err)
	}
	return view, nil
}

func waitForWorkloadDetailOwnedBy(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) workloadDetailView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		list, err := getWorkloads(repoRoot, namespace)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		for _, item := range list.Items {
			for _, ref := range item.Metadata.OwnerReferences {
				if ref.Kind == ownerKind && ref.Name == ownerName {
					// Found the workload, now get the detail view.
					detail, err := getWorkloadDetail(repoRoot, namespace, item.Metadata.Name)
					if err == nil {
						return detail
					}
				}
			}
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for workload detail owned by %s/%s in namespace %s\noperator logs:\n%s\nport-forward logs:\n%s",
		ownerKind,
		ownerName,
		namespace,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return workloadDetailView{}
}

// assertNoChildJobSetExists verifies that no child JobSet exists for the given RTJ
// within a brief observation window.
func assertNoChildJobSetExists(t *testing.T, repoRoot, namespace, rtjName string, runAttempt int32) {
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
