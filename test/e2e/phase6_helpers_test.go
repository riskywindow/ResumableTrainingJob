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

// ---------------------------------------------------------------------------
// Phase 6 RTJ view types
// ---------------------------------------------------------------------------

// phase6RTJView captures the Phase 6 RTJ status shape, including
// multiCluster fields used by the remote-execution e2e tests.
type phase6RTJView struct {
	Metadata struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Suspend   *bool  `json:"suspend"`
		ManagedBy string `json:"managedBy"`
		Control   struct {
			DesiredState string `json:"desiredState"`
		} `json:"control"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		CurrentRunAttempt int32  `json:"currentRunAttempt"`
		ActiveJobSetName  string `json:"activeJobSetName"`
		Reason            string `json:"reason"`
		Message           string `json:"message"`
		MultiCluster      *struct {
			DispatchPhase            string `json:"dispatchPhase"`
			ExecutionCluster         string `json:"executionCluster"`
			LocalExecutionSuppressed bool   `json:"localExecutionSuppressed"`
			RemotePhase              string `json:"remotePhase"`
			RemoteObjectRef          *struct {
				Cluster   string `json:"cluster"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"remoteObjectRef"`
			RemoteCheckpoint *struct {
				LastCompletedCheckpointID string `json:"lastCompletedCheckpointID"`
				StorageURI               string `json:"storageURI"`
			} `json:"remoteCheckpoint"`
		} `json:"multiCluster"`
	} `json:"status"`
}

// phase6JobSetListView captures a list of JobSets for existence checks.
type phase6JobSetListView struct {
	Items []struct {
		Metadata struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	} `json:"items"`
}

// ---------------------------------------------------------------------------
// Phase 6 environment
// ---------------------------------------------------------------------------

const (
	phase6ManagerContext = "kind-phase6-manager"
	phase6Worker1Context = "kind-phase6-worker-1"
	phase6Worker2Context = "kind-phase6-worker-2"
)

// phase6Env holds the Phase 6 multi-cluster test environment.
type phase6Env struct {
	repoRoot       string
	namespace      string
	trainerImage   string
	managerContext string
	worker1Context string
	worker2Context string
	minioEndpoint  string
	accessKey      string
	secretKey      string
	region         string
	managerLogs    *bytes.Buffer
	worker1Logs    *bytes.Buffer
	worker2Logs    *bytes.Buffer
	portForward    *bytes.Buffer
}

func setupPhase6Env(t *testing.T) phase6Env {
	t.Helper()

	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run Phase 6 e2e tests")
	}

	trainerImage := resolvePhase6TrainerImage()
	if trainerImage == "" {
		t.Skip("set PHASE6_TRAINER_IMAGE (or PHASE5/PHASE4/PHASE3/PHASE2/PAUSE_FLOW_TRAINER_IMAGE)")
	}

	root := repoRoot(t)
	namespace := envOrDefault("DEV_NAMESPACE", "checkpoint-dev")
	minioEndpoint := "127.0.0.1:9002"
	minioURL := "http://" + minioEndpoint
	accessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123")
	region := envOrDefault("MINIO_REGION", "us-east-1")

	// Verify all three clusters exist.
	for _, ctx := range []string{phase6ManagerContext, phase6Worker1Context, phase6Worker2Context} {
		output, err := kubectlContext(root, ctx, "cluster-info")
		if err != nil {
			t.Skipf("Phase 6 cluster %s not found (run make phase6-up first): %s", ctx, output)
		}
	}

	// Verify MultiKueue admission check on manager.
	output, err := kubectlContext(root, phase6ManagerContext,
		"get", "admissionchecks.kueue.x-k8s.io", "multikueue")
	if err != nil {
		t.Skipf("MultiKueue AdmissionCheck not found on manager (run make phase6-up first): %s", output)
	}

	// Verify worker queues exist.
	for _, ctx := range []string{phase6Worker1Context, phase6Worker2Context} {
		output, err := kubectlContext(root, ctx,
			"get", "clusterqueues.kueue.x-k8s.io", "phase6-worker-cq")
		if err != nil {
			t.Skipf("Worker ClusterQueue not found on %s (run make phase6-up first): %s", ctx, output)
		}
	}

	// Extract per-cluster kubeconfigs for operator instances.
	managerKubeconfig := extractKubeconfig(t, root, phase6ManagerContext)
	worker1Kubeconfig := extractKubeconfig(t, root, phase6Worker1Context)
	worker2Kubeconfig := extractKubeconfig(t, root, phase6Worker2Context)

	// Port-forward MinIO from worker-1 to localhost:9002.
	portForwardCtx, stopPortForward := context.WithCancel(context.Background())
	t.Cleanup(stopPortForward)
	portForwardLogs := startBackgroundCommand(t, root, portForwardCtx, nil,
		"kubectl", "--context="+phase6Worker1Context, "-n", namespace,
		"port-forward", "service/minio", "9002:9000")
	waitForTCP(t, minioEndpoint, 30*time.Second)

	// Start manager operator (--mode=manager, no S3 needed).
	managerOpCtx, stopManager := context.WithCancel(context.Background())
	t.Cleanup(stopManager)
	managerLogs := startBackgroundCommand(
		t, root, managerOpCtx,
		[]string{"KUBECONFIG=" + managerKubeconfig},
		"go", "run", "./cmd/operator",
		"--leader-elect=false",
		"--mode=manager",
		"--metrics-bind-address=:8090",
		"--health-probe-bind-address=:8091",
	)

	// Start worker-1 operator (--mode=worker, with S3).
	worker1OpCtx, stopWorker1 := context.WithCancel(context.Background())
	t.Cleanup(stopWorker1)
	worker1Logs := startBackgroundCommand(
		t, root, worker1OpCtx,
		[]string{
			"KUBECONFIG=" + worker1Kubeconfig,
			"AWS_ENDPOINT_URL=" + minioURL,
			"AWS_ACCESS_KEY_ID=" + accessKey,
			"AWS_SECRET_ACCESS_KEY=" + secretKey,
			"AWS_REGION=" + region,
		},
		"go", "run", "./cmd/operator",
		"--leader-elect=false",
		"--mode=worker",
		"--metrics-bind-address=:8092",
		"--health-probe-bind-address=:8093",
	)

	// Start worker-2 operator (--mode=worker, with S3).
	worker2OpCtx, stopWorker2 := context.WithCancel(context.Background())
	t.Cleanup(stopWorker2)
	worker2Logs := startBackgroundCommand(
		t, root, worker2OpCtx,
		[]string{
			"KUBECONFIG=" + worker2Kubeconfig,
			"AWS_ENDPOINT_URL=" + minioURL,
			"AWS_ACCESS_KEY_ID=" + accessKey,
			"AWS_SECRET_ACCESS_KEY=" + secretKey,
			"AWS_REGION=" + region,
		},
		"go", "run", "./cmd/operator",
		"--leader-elect=false",
		"--mode=worker",
		"--metrics-bind-address=:8094",
		"--health-probe-bind-address=:8095",
	)

	return phase6Env{
		repoRoot:       root,
		namespace:      namespace,
		trainerImage:   trainerImage,
		managerContext: phase6ManagerContext,
		worker1Context: phase6Worker1Context,
		worker2Context: phase6Worker2Context,
		minioEndpoint:  minioEndpoint,
		accessKey:      accessKey,
		secretKey:      secretKey,
		region:         region,
		managerLogs:    managerLogs,
		worker1Logs:    worker1Logs,
		worker2Logs:    worker2Logs,
		portForward:    portForwardLogs,
	}
}

// ---------------------------------------------------------------------------
// Trainer image resolution
// ---------------------------------------------------------------------------

func resolvePhase6TrainerImage() string {
	for _, key := range []string{
		"PHASE6_TRAINER_IMAGE",
		"PHASE5_TRAINER_IMAGE",
		"PHASE4_TRAINER_IMAGE",
		"PHASE3_TRAINER_IMAGE",
		"PHASE2_TRAINER_IMAGE",
		"PAUSE_FLOW_TRAINER_IMAGE",
	} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Kubeconfig helpers
// ---------------------------------------------------------------------------

// extractKubeconfig writes a standalone kubeconfig for the given context
// to a temp file and returns the path. The file is cleaned up at test end.
func extractKubeconfig(t *testing.T, repoRoot, kubeContext string) string {
	t.Helper()
	cmd := exec.Command("kubectl", "config", "view", "--minify",
		"--context="+kubeContext, "--flatten")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("extract kubeconfig for %s: %v\n%s", kubeContext, err, string(output))
	}
	tmpFile, err := os.CreateTemp("", "phase6-kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("create temp kubeconfig: %v", err)
	}
	if _, err := tmpFile.Write(output); err != nil {
		t.Fatalf("write temp kubeconfig: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("close temp kubeconfig: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

// ---------------------------------------------------------------------------
// kubectl helpers
// ---------------------------------------------------------------------------

// kubectlContext runs kubectl with --context targeting a specific cluster.
func kubectlContext(repoRoot, kubeContext string, args ...string) (string, error) {
	fullArgs := append([]string{"--context=" + kubeContext}, args...)
	cmd := exec.Command("kubectl", fullArgs...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// runKubectlContext runs kubectl against a specific cluster, failing on error.
func runKubectlContext(t *testing.T, repoRoot, kubeContext string, args ...string) string {
	t.Helper()
	output, err := kubectlContext(repoRoot, kubeContext, args...)
	if err != nil {
		t.Fatalf("kubectl --context=%s %s failed: %v\n%s",
			kubeContext, strings.Join(args, " "), err, output)
	}
	return output
}

// ---------------------------------------------------------------------------
// RTJ helpers
// ---------------------------------------------------------------------------

// getPhase6RTJ reads the RTJ from a specific cluster.
func getPhase6RTJ(repoRoot, kubeContext, namespace, name string) (phase6RTJView, error) {
	output, err := kubectlContext(repoRoot, kubeContext,
		"-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	if err != nil {
		return phase6RTJView{}, fmt.Errorf("kubectl get rtj on %s: %w: %s", kubeContext, err, output)
	}
	var view phase6RTJView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return phase6RTJView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

// waitForPhase6RTJState polls until the RTJ on the given cluster satisfies
// the predicate, or times out with a fatal error including all operator logs.
func waitForPhase6RTJState(
	t *testing.T,
	env phase6Env,
	kubeContext string,
	name string,
	description string,
	timeout time.Duration,
	predicate func(phase6RTJView) bool,
) phase6RTJView {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getPhase6RTJ(env.repoRoot, kubeContext, env.namespace, name)
		if err == nil && predicate(view) {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getPhase6RTJ(env.repoRoot, kubeContext, env.namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s on %s to satisfy %q;\n"+
			"last: phase=%s managedBy=%q multiCluster=%+v\n"+
			"manager logs:\n%s\nworker-1 logs:\n%s\nworker-2 logs:\n%s\nport-forward logs:\n%s",
		env.namespace, name, kubeContext, description,
		view.Status.Phase, view.Spec.ManagedBy, view.Status.MultiCluster,
		env.managerLogs.String(),
		env.worker1Logs.String(),
		env.worker2Logs.String(),
		env.portForward.String(),
	)
	return phase6RTJView{}
}

// ---------------------------------------------------------------------------
// JobSet helpers
// ---------------------------------------------------------------------------

// listJobSetsOnCluster lists all JobSets in a namespace on a specific cluster.
func listJobSetsOnCluster(repoRoot, kubeContext, namespace string) (phase6JobSetListView, error) {
	output, err := kubectlContext(repoRoot, kubeContext,
		"-n", namespace, "get", "jobsets.jobset.x-k8s.io", "-o", "json")
	if err != nil {
		return phase6JobSetListView{}, fmt.Errorf("kubectl get jobsets on %s: %w: %s", kubeContext, err, output)
	}
	var list phase6JobSetListView
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return phase6JobSetListView{}, fmt.Errorf("decode jobsets json: %w", err)
	}
	return list, nil
}

// assertNoJobSetsWithPrefix verifies that no JobSets with the given name
// prefix exist on the specified cluster.
func assertNoJobSetsWithPrefix(t *testing.T, repoRoot, kubeContext, namespace, prefix string) {
	t.Helper()
	list, err := listJobSetsOnCluster(repoRoot, kubeContext, namespace)
	if err != nil {
		// If the CRD doesn't exist or namespace is empty, no JobSets exist.
		return
	}
	for _, item := range list.Items {
		if strings.HasPrefix(item.Metadata.Name, prefix) {
			t.Fatalf("expected no JobSets with prefix %q on %s, found %q",
				prefix, kubeContext, item.Metadata.Name)
		}
	}
}

// waitForJobSetOnCluster polls until a JobSet with the given name exists
// on the specified cluster, or times out.
func waitForJobSetOnCluster(
	t *testing.T,
	env phase6Env,
	kubeContext string,
	jobSetName string,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := kubectlContext(env.repoRoot, kubeContext,
			"-n", env.namespace, "get", "jobset", jobSetName, "-o", "name")
		if err == nil && strings.TrimSpace(output) != "" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf(
		"timed out waiting for JobSet %s/%s on %s\n"+
			"manager logs:\n%s\nworker-1 logs:\n%s\nworker-2 logs:\n%s",
		env.namespace, jobSetName, kubeContext,
		env.managerLogs.String(),
		env.worker1Logs.String(),
		env.worker2Logs.String(),
	)
}

// ---------------------------------------------------------------------------
// Deterministic cluster selection
// ---------------------------------------------------------------------------

// biasWorkerSelection stops worker-2's ClusterQueue from admitting new
// workloads, forcing MultiKueue to dispatch only to worker-1.
//
// Mechanism: patches spec.stopPolicy=Hold on worker-2's ClusterQueue.
// Kueue v0.15.1 ClusterQueue.spec.stopPolicy=Hold stops all new admissions
// while keeping running workloads alive.
//
// This is the smallest practical mechanism for deterministic selection:
// one field change, one cluster, no custom queue configurations.
func biasWorkerSelection(t *testing.T, env phase6Env) {
	t.Helper()
	output, err := kubectlContext(env.repoRoot, env.worker2Context,
		"patch", "clusterqueue", "phase6-worker-cq",
		"--type=merge",
		"-p", `{"spec":{"stopPolicy":"Hold"}}`)
	if err != nil {
		t.Fatalf("bias worker selection: patch worker-2 CQ: %v\n%s", err, output)
	}
	t.Log("biased cluster selection: worker-2 CQ on Hold, worker-1 is the only target")
}

// restoreWorkerSelection re-enables worker-2's ClusterQueue for admission.
func restoreWorkerSelection(t *testing.T, env phase6Env) {
	t.Helper()
	_, _ = kubectlContext(env.repoRoot, env.worker2Context,
		"patch", "clusterqueue", "phase6-worker-cq",
		"--type=merge",
		"-p", `{"spec":{"stopPolicy":"None"}}`)
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

// cleanupPhase6RTJ deletes an RTJ from all clusters and best-effort
// removes child JobSets.
func cleanupPhase6RTJ(t *testing.T, env phase6Env, name string) {
	t.Helper()
	for _, ctx := range []string{env.managerContext, env.worker1Context, env.worker2Context} {
		kubectlContext(env.repoRoot, ctx,
			"-n", env.namespace, "delete", pauseFlowResource, name,
			"--ignore-not-found=true")
	}
	// Best-effort child JobSet cleanup on workers.
	for _, attempt := range []int32{1, 2} {
		jsName := rtjjobset.ChildJobSetName(name, attempt)
		for _, ctx := range []string{env.worker1Context, env.worker2Context} {
			kubectlContext(env.repoRoot, ctx,
				"-n", env.namespace, "delete", "jobset", jsName,
				"--ignore-not-found=true")
		}
	}
}
