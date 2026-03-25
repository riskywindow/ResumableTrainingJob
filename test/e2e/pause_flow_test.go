package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

const (
	pauseFlowResource = "resumabletrainingjobs.training.checkpoint.example.io"
)

type rtjView struct {
	Metadata struct {
		UID string `json:"uid"`
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
			Reason    string `json:"reason"`
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
			ManifestURI string `json:"manifestURI"`
		} `json:"lastCompletedCheckpoint"`
	} `json:"status"`
}

func TestPauseFlow(t *testing.T) {
	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run the kind pause-flow smoke test")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PAUSE_FLOW_TRAINER_IMAGE"))
	if trainerImage == "" {
		t.Skip("set PAUSE_FLOW_TRAINER_IMAGE to a trainer image already loaded into the kind cluster")
	}

	repoRoot := repoRoot(t)
	namespace := envOrDefault("DEV_NAMESPACE", "checkpoint-dev")
	rtjName := fmt.Sprintf("pause-flow-%d", time.Now().UnixNano())
	minioEndpoint := "127.0.0.1:9000"
	minioURL := "http://" + minioEndpoint
	accessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123")
	region := envOrDefault("MINIO_REGION", "us-east-1")

	runKubectl(t, repoRoot, "cluster-info")
	runKubectl(t, repoRoot, "-n", namespace, "get", "deployment", "minio")

	renderedManifest := renderTemplate(
		t,
		filepath.Join(repoRoot, "test/e2e/testdata/rtj-pause-flow.yaml"),
		map[string]string{
			"__RTJ_NAME__":      rtjName,
			"__TRAINER_IMAGE__": trainerImage,
			"__DEV_NAMESPACE__": namespace,
		},
	)
	defer os.Remove(renderedManifest)

	portForwardCtx, stopPortForward := context.WithCancel(context.Background())
	defer stopPortForward()
	portForwardLogs := startBackgroundCommand(t, repoRoot, portForwardCtx, nil, "kubectl", "-n", namespace, "port-forward", "service/minio", "9000:9000")
	waitForTCP(t, minioEndpoint, 20*time.Second)

	operatorCtx, stopOperator := context.WithCancel(context.Background())
	defer stopOperator()
	operatorEnv := []string{
		"AWS_ENDPOINT_URL=" + minioURL,
		"AWS_ACCESS_KEY_ID=" + accessKey,
		"AWS_SECRET_ACCESS_KEY=" + secretKey,
		"AWS_REGION=" + region,
	}
	operatorLogs := startBackgroundCommand(t, repoRoot, operatorCtx, operatorEnv, "go", "run", "./cmd/operator", "--leader-elect=false")

	runKubectl(t, repoRoot, "-n", namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")
	runKubectl(t, repoRoot, "apply", "-f", renderedManifest)
	defer runKubectl(t, repoRoot, "-n", namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")

	running := waitForPhase(t, repoRoot, namespace, rtjName, "Running", 3*time.Minute, operatorLogs, portForwardLogs)
	if running.Status.Phase != "Running" {
		t.Fatalf("expected Running phase, got %q", running.Status.Phase)
	}

	runKubectl(
		t,
		repoRoot,
		"-n", namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	paused := waitForPhase(t, repoRoot, namespace, rtjName, "Paused", 5*time.Minute, operatorLogs, portForwardLogs)
	if paused.Status.LastCompletedCheckpoint == nil || paused.Status.LastCompletedCheckpoint.ManifestURI == "" {
		t.Fatalf("expected lastCompletedCheckpoint.manifestURI to be populated, operator logs:\n%s", operatorLogs.String())
	}

	assertObjectExists(t, minioEndpoint, accessKey, secretKey, region, paused.Status.LastCompletedCheckpoint.ManifestURI)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(cwd, "..", ".."))
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func renderTemplate(t *testing.T, path string, replacements map[string]string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template %s: %v", path, err)
	}
	rendered := string(content)
	for oldValue, newValue := range replacements {
		rendered = strings.ReplaceAll(rendered, oldValue, newValue)
	}

	tmpFile, err := os.CreateTemp("", "pause-flow-*.yaml")
	if err != nil {
		t.Fatalf("create temp yaml: %v", err)
	}
	if _, err := tmpFile.WriteString(rendered); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("close temp yaml: %v", err)
	}
	return tmpFile.Name()
}

func runKubectl(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("kubectl", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func startBackgroundCommand(t *testing.T, repoRoot string, ctx context.Context, env []string, name string, args ...string) *bytes.Buffer {
	t.Helper()
	logs := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repoRoot
	cmd.Stdout = logs
	cmd.Stderr = logs
	cmd.Env = os.Environ()
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, env...)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s %s: %v", name, strings.Join(args, " "), err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-stopCtx.Done():
			_ = cmd.Process.Kill()
		}
	})
	return logs
}

func waitForTCP(t *testing.T, address string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to accept TCP connections", address)
}

func waitForPhase(
	t *testing.T,
	repoRoot string,
	namespace string,
	name string,
	phase string,
	timeout time.Duration,
	operatorLogs *bytes.Buffer,
	portForwardLogs *bytes.Buffer,
) rtjView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := getRTJ(repoRoot, namespace, name)
		if err == nil && view.Status.Phase == phase {
			return view
		}
		time.Sleep(2 * time.Second)
	}

	view, _ := getRTJ(repoRoot, namespace, name)
	t.Fatalf(
		"timed out waiting for RTJ %s/%s to reach phase %s; last phase=%s\noperator logs:\n%s\nport-forward logs:\n%s",
		namespace,
		name,
		phase,
		view.Status.Phase,
		operatorLogs.String(),
		portForwardLogs.String(),
	)
	return rtjView{}
}

func getRTJ(repoRoot, namespace, name string) (rtjView, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", pauseFlowResource, name, "-o", "json")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return rtjView{}, fmt.Errorf("kubectl get rtj: %w: %s", err, string(output))
	}
	var view rtjView
	if err := json.Unmarshal(output, &view); err != nil {
		return rtjView{}, fmt.Errorf("decode rtj json: %w", err)
	}
	return view, nil
}

func assertObjectExists(t *testing.T, endpoint, accessKey, secretKey, region, uri string) {
	t.Helper()
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
		Region: region,
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}

	location, err := checkpoints.ParseS3URI(uri)
	if err != nil {
		t.Fatalf("parse manifest uri %q: %v", uri, err)
	}
	if _, err := client.StatObject(context.Background(), location.Bucket, location.Key, minio.StatObjectOptions{}); err != nil {
		t.Fatalf("stat manifest object %s: %v", uri, err)
	}
}
