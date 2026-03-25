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

type ownerReferenceView struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type workloadListView struct {
	Items []workloadView `json:"items"`
}

type workloadView struct {
	Metadata struct {
		Name            string               `json:"name"`
		OwnerReferences []ownerReferenceView `json:"ownerReferences"`
	} `json:"metadata"`
}

type jobSetView struct {
	Metadata struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
}

type phase2Env struct {
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

func setupPhase2Env(t *testing.T) phase2Env {
	t.Helper()

	trainerImage := strings.TrimSpace(os.Getenv("PHASE2_TRAINER_IMAGE"))
	if trainerImage == "" {
		trainerImage = strings.TrimSpace(os.Getenv("PAUSE_FLOW_TRAINER_IMAGE"))
	}
	if trainerImage == "" {
		t.Skip("set PHASE2_TRAINER_IMAGE or PAUSE_FLOW_TRAINER_IMAGE to a trainer image already loaded into the kind cluster")
	}

	repoRoot := repoRoot(t)
	namespace := envOrDefault("DEV_NAMESPACE", "checkpoint-dev")
	minioEndpoint := "127.0.0.1:9000"
	minioURL := "http://" + minioEndpoint
	accessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123")
	region := envOrDefault("MINIO_REGION", "us-east-1")

	runKubectl(t, repoRoot, "cluster-info")
	runKubectl(t, repoRoot, "-n", namespace, "get", "deployment", "minio")

	portForwardCtx, stopPortForward := context.WithCancel(context.Background())
	t.Cleanup(stopPortForward)
	portForwardLogs := startBackgroundCommand(t, repoRoot, portForwardCtx, nil, "kubectl", "-n", namespace, "port-forward", "service/minio", "9000:9000")
	waitForTCP(t, minioEndpoint, 20*time.Second)

	operatorCtx, stopOperator := context.WithCancel(context.Background())
	t.Cleanup(stopOperator)
	operatorLogs := startBackgroundCommand(
		t,
		repoRoot,
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

	return phase2Env{
		repoRoot:      repoRoot,
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

func kubectlOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func isKubectlNotFound(output string) bool {
	return strings.Contains(output, "NotFound") || strings.Contains(strings.ToLower(output), "not found")
}

func waitForRTJState(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	description string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
	predicate func(rtjView) bool,
) rtjView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getRTJ(repoRoot, namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getRTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to satisfy %s; last phase=%s suspend=%v currentSuspension=%#v pauseRequestID=%q\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		description,
		view.Status.Phase,
		view.Spec.Suspend,
		view.Status.CurrentSuspension,
		view.Status.PauseRequestID,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return rtjView{}
}

func getWorkloads(repoRoot, namespace string) (workloadListView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get", "workloads.kueue.x-k8s.io", "-o", "json")
	if err != nil {
		return workloadListView{}, fmt.Errorf("kubectl get workloads: %w: %s", err, output)
	}
	var list workloadListView
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return workloadListView{}, fmt.Errorf("decode workloads json: %w", err)
	}
	return list, nil
}

func findWorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName string) (workloadView, bool, error) {
	list, err := getWorkloads(repoRoot, namespace)
	if err != nil {
		return workloadView{}, false, err
	}
	for _, item := range list.Items {
		for _, ref := range item.Metadata.OwnerReferences {
			if ref.Kind == ownerKind && ref.Name == ownerName {
				return item, true, nil
			}
		}
	}
	return workloadView{}, false, nil
}

func waitForWorkloadOwnedBy(
	t *testing.T,
	repoRoot string,
	namespace string,
	ownerKind string,
	ownerName string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) workloadView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		workload, found, err := findWorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
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
	return workloadView{}
}

func assertNoWorkloadOwnedBy(t *testing.T, repoRoot, namespace, ownerKind, ownerName string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		workload, found, err := findWorkloadOwnedBy(repoRoot, namespace, ownerKind, ownerName)
		if err != nil {
			t.Fatalf("find workload owned by %s/%s: %v", ownerKind, ownerName, err)
		}
		if found {
			t.Fatalf("expected no workload owned by %s/%s, found %s", ownerKind, ownerName, workload.Metadata.Name)
		}
		time.Sleep(1 * time.Second)
	}
}

func getJobSet(repoRoot, namespace, name string) (jobSetView, error) {
	output, err := kubectlOutput(repoRoot, "-n", namespace, "get", "jobset", name, "-o", "json")
	if err != nil {
		if isKubectlNotFound(output) {
			return jobSetView{}, apiNotFoundError(output)
		}
		return jobSetView{}, fmt.Errorf("kubectl get jobset %s: %w: %s", name, err, output)
	}
	var view jobSetView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return jobSetView{}, fmt.Errorf("decode jobset json: %w", err)
	}
	return view, nil
}

func waitForJobSetPresent(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) jobSetView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getJobSet(repoRoot, namespace, name)
		if err == nil {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for JobSet %s/%s to exist\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return jobSetView{}
}

func waitForJobSetDeleted(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := getJobSet(repoRoot, namespace, name)
		if err != nil && isNotFoundError(err) {
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for JobSet %s/%s to be deleted\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
}

func waitForRTJDeleted(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := getRTJ(repoRoot, namespace, name)
		if err != nil && strings.Contains(err.Error(), "NotFound") {
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf(
		"timed out waiting for RTJ %s/%s to be deleted\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
}

func assertChildJobSetNotKueueManaged(t *testing.T, jobSet jobSetView) {
	t.Helper()

	if jobSet.Metadata.Labels[rtjjobset.QueueLabelKey] != "" {
		t.Fatalf("expected child JobSet queue label to be absent, got %q", jobSet.Metadata.Labels[rtjjobset.QueueLabelKey])
	}
	if jobSet.Metadata.Labels[rtjjobset.WorkloadPriorityLabelKey] != "" {
		t.Fatalf("expected child JobSet priority label to be absent, got %q", jobSet.Metadata.Labels[rtjjobset.WorkloadPriorityLabelKey])
	}
	for key := range jobSet.Metadata.Labels {
		if strings.HasPrefix(key, "kueue.x-k8s.io/") {
			t.Fatalf("expected child JobSet to have no Kueue management label, found %q", key)
		}
	}
	for key := range jobSet.Metadata.Annotations {
		if strings.HasPrefix(key, "kueue.x-k8s.io/") || strings.HasPrefix(key, "provreq.kueue.x-k8s.io/") {
			t.Fatalf("expected child JobSet to have no Kueue management annotation, found %q", key)
		}
	}
}

type notFoundError struct {
	message string
}

func (e notFoundError) Error() string {
	return e.message
}

func apiNotFoundError(message string) error {
	return notFoundError{message: message}
}

func isNotFoundError(err error) bool {
	var target notFoundError
	return err != nil && (strings.Contains(err.Error(), "NotFound") || strings.Contains(strings.ToLower(err.Error()), "not found") || errorAs(err, &target))
}

func errorAs(err error, target *notFoundError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(notFoundError)
	if !ok {
		return false
	}
	*target = value
	return true
}
