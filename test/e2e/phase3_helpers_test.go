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
)

// phase3RTJView extends rtjView with Phase 3 status fields.
type phase3RTJView struct {
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
	} `json:"status"`
}

// jobSetDetailView is a detailed child JobSet view including replicatedJobs.
type jobSetDetailView struct {
	Metadata struct {
		Name        string            `json:"name"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		ReplicatedJobs []replicatedJobView `json:"replicatedJobs"`
	} `json:"spec"`
}

type replicatedJobView struct {
	Name     string `json:"name"`
	Replicas int32  `json:"replicas"`
	Template struct {
		Spec struct {
			Template struct {
				Spec podSpecView `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	} `json:"template"`
}

type podSpecView struct {
	NodeSelector map[string]string `json:"nodeSelector"`
	Tolerations  []tolerationView  `json:"tolerations"`
	Containers   []containerView   `json:"containers"`
}

type tolerationView struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Effect   string `json:"effect"`
	Operator string `json:"operator"`
}

type containerView struct {
	Name string    `json:"name"`
	Env  []envView `json:"env"`
}

type envView struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type podListView struct {
	Items []podItemView `json:"items"`
}

type podItemView struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		NodeName     string            `json:"nodeName"`
		NodeSelector map[string]string `json:"nodeSelector"`
	} `json:"spec"`
}

type nodeListView struct {
	Items []nodeItemView `json:"items"`
}

type nodeItemView struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

// phase3Env holds the Phase 3 test environment state.
type phase3Env struct {
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

func setupPhase3Env(t *testing.T, experimentalPartialAdmission bool) phase3Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run e2e tests")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PHASE3_TRAINER_IMAGE"))
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PHASE2_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PAUSE_FLOW_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		t.Skip("set PHASE3_TRAINER_IMAGE, PHASE2_TRAINER_IMAGE, or PAUSE_FLOW_TRAINER_IMAGE")
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

	// Verify Phase 3 queue exists.
	output, err := kubectlOutput(root, "get", "clusterqueues.kueue.x-k8s.io", "phase3-cq")
	if err != nil {
		t.Skipf("Phase 3 ClusterQueue not found (run make phase3-up first): %s", output)
	}

	portForwardCtx, stopPortForward := context.WithCancel(context.Background())
	t.Cleanup(stopPortForward)
	portForwardLogs := startBackgroundCommand(t, root, portForwardCtx, nil,
		"kubectl", "-n", namespace, "port-forward", "service/minio", "9000:9000")
	waitForTCP(t, minioEndpoint, 20*time.Second)

	operatorCtx, stopOperator := context.WithCancel(context.Background())
	t.Cleanup(stopOperator)

	operatorArgs := []string{"run", "./cmd/operator", "--leader-elect=false"}
	if experimentalPartialAdmission {
		operatorArgs = append(operatorArgs, "--enable-experimental-partial-admission")
	}
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
		operatorArgs...,
	)

	return phase3Env{
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

func getPhase3RTJ(repoRoot, namespace, name string) (phase3RTJView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return phase3RTJView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view phase3RTJView
	if err := json.Unmarshal(output, &view); err != nil {
		return phase3RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func waitForPhase3RTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(phase3RTJView) bool,
) phase3RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase3RTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase3RTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %q; last phase=%s suspend=%v\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return phase3RTJView{}
}

func waitForPhase3Phase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) phase3RTJView {
	t.Helper()
	return waitForPhase3RTJState(t, repoRoot, namespace, name,
		fmt.Sprintf("phase=%s", phase), timeout, operatorLogs, portForwardLogs,
		func(view phase3RTJView) bool {
			return view.Status.Phase == phase
		},
	)
}

func getJobSetDetail(repoRoot, namespace, name string) (jobSetDetailView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get", "jobset", name, "-o", "json")
	if err != nil {
		if isKubectlNotFound(output) {
			return jobSetDetailView{}, apiNotFoundError(output)
		}
		return jobSetDetailView{}, fmt.Errorf("kubectl get jobset %s: %w: %s", name, err, output)
	}
	var view jobSetDetailView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return jobSetDetailView{}, fmt.Errorf("decode jobset json: %w", err)
	}
	return view, nil
}

func waitForJobSetDetailPresent(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) jobSetDetailView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getJobSetDetail(repoRoot, namespace, name)
		if err == nil {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for JobSet %s/%s to exist\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace, name, operatorLogs.String(), portForwardLogs.String(),
	)
	return jobSetDetailView{}
}

func getPods(repoRoot, namespace, labelSelector string) (podListView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get", "pods", "-l", labelSelector, "-o", "json")
	if err != nil {
		return podListView{}, fmt.Errorf("get pods: %w: %s", err, output)
	}
	var list podListView
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return podListView{}, fmt.Errorf("decode pods json: %w", err)
	}
	return list, nil
}

func getNodeLabels(repoRoot, nodeName string) (map[string]string, error) {
	output, err := kubectlOutput(repoRoot, "get", "node", nodeName, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w: %s", nodeName, err, output)
	}
	var node nodeItemView
	if err := json.Unmarshal([]byte(output), &node); err != nil {
		return nil, fmt.Errorf("decode node json: %w", err)
	}
	return node.Metadata.Labels, nil
}

func findEnvValue(envs []envView, name string) (string, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func assertChildJobSetPlainRuntime(t *testing.T, js jobSetDetailView) {
	t.Helper()
	for key := range js.Metadata.Labels {
		if strings.HasPrefix(key, "kueue.x-k8s.io/") {
			t.Fatalf("child JobSet has Kueue management label %q; expected plain runtime resource", key)
		}
	}
	for key := range js.Metadata.Annotations {
		if strings.HasPrefix(key, "kueue.x-k8s.io/") || strings.HasPrefix(key, "provreq.kueue.x-k8s.io/") {
			t.Fatalf("child JobSet has Kueue management annotation %q; expected plain runtime resource", key)
		}
	}
}
