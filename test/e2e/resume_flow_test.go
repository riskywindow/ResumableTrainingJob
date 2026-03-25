package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

func TestResumeFlow(t *testing.T) {
	if os.Getenv("RUN_KIND_E2E") != "1" {
		t.Skip("set RUN_KIND_E2E=1 to run the kind resume-flow smoke test")
	}

	trainerImage := strings.TrimSpace(os.Getenv("PAUSE_FLOW_TRAINER_IMAGE"))
	if trainerImage == "" {
		t.Skip("set PAUSE_FLOW_TRAINER_IMAGE to a trainer image already loaded into the kind cluster")
	}

	repoRoot := repoRoot(t)
	namespace := envOrDefault("DEV_NAMESPACE", "checkpoint-dev")
	rtjName := fmt.Sprintf("resume-flow-%d", time.Now().UnixNano())
	minioEndpoint := "127.0.0.1:9000"
	minioURL := "http://" + minioEndpoint
	accessKey := envOrDefault("MINIO_ROOT_USER", "minioadmin")
	secretKey := envOrDefault("MINIO_ROOT_PASSWORD", "minioadmin123")
	region := envOrDefault("MINIO_REGION", "us-east-1")

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

	runKubectl(t, repoRoot, "-n", namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")
	runKubectl(t, repoRoot, "apply", "-f", renderedManifest)
	defer runKubectl(t, repoRoot, "-n", namespace, "delete", pauseFlowResource, rtjName, "--ignore-not-found=true")

	waitForPhase(t, repoRoot, namespace, rtjName, "Running", 3*time.Minute, operatorLogs, portForwardLogs)

	runKubectl(
		t,
		repoRoot,
		"-n", namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	firstPaused := waitForPhase(t, repoRoot, namespace, rtjName, "Paused", 5*time.Minute, operatorLogs, portForwardLogs)
	if firstPaused.Status.LastCompletedCheckpoint == nil || firstPaused.Status.LastCompletedCheckpoint.ManifestURI == "" {
		t.Fatalf("expected first pause to record a manifest URI")
	}
	firstManifest := loadManifestFromObjectStore(
		t,
		minioEndpoint,
		accessKey,
		secretKey,
		region,
		firstPaused.Status.LastCompletedCheckpoint.ManifestURI,
	)
	firstStep := firstManifest.GlobalStep

	runKubectl(
		t,
		repoRoot,
		"-n", namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Running"}}}`,
	)

	resumed := waitForPhase(t, repoRoot, namespace, rtjName, "Running", 5*time.Minute, operatorLogs, portForwardLogs)
	if resumed.Status.SelectedCheckpoint == nil || resumed.Status.SelectedCheckpoint.ManifestURI != firstPaused.Status.LastCompletedCheckpoint.ManifestURI {
		t.Fatalf("expected selectedCheckpoint to match the first paused manifest; got %#v", resumed.Status.SelectedCheckpoint)
	}

	time.Sleep(5 * time.Second)

	runKubectl(
		t,
		repoRoot,
		"-n", namespace,
		"patch", pauseFlowResource, rtjName,
		"--type=merge",
		"-p", `{"spec":{"control":{"desiredState":"Paused"}}}`,
	)

	secondPaused := waitForPhase(t, repoRoot, namespace, rtjName, "Paused", 5*time.Minute, operatorLogs, portForwardLogs)
	if secondPaused.Status.LastCompletedCheckpoint == nil || secondPaused.Status.LastCompletedCheckpoint.ManifestURI == "" {
		t.Fatalf("expected second pause to record a manifest URI")
	}
	secondManifest := loadManifestFromObjectStore(
		t,
		minioEndpoint,
		accessKey,
		secretKey,
		region,
		secondPaused.Status.LastCompletedCheckpoint.ManifestURI,
	)
	if secondManifest.GlobalStep <= firstStep {
		t.Fatalf("expected resume to continue training beyond step %d, got step %d", firstStep, secondManifest.GlobalStep)
	}
}

func loadManifestFromObjectStore(
	t *testing.T,
	endpoint, accessKey, secretKey, region, manifestURI string,
) checkpoints.CheckpointManifest {
	t.Helper()

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
		Region: region,
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}

	location, err := checkpoints.ParseS3URI(manifestURI)
	if err != nil {
		t.Fatalf("parse manifest URI %q: %v", manifestURI, err)
	}
	object, err := client.GetObject(context.Background(), location.Bucket, location.Key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get manifest object %s: %v", manifestURI, err)
	}
	defer object.Close()

	rawBytes, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read manifest object %s: %v", manifestURI, err)
	}
	manifest, err := checkpoints.DecodeManifest(rawBytes, manifestURI)
	if err != nil {
		t.Fatalf("decode manifest %s: %v", manifestURI, err)
	}
	return manifest
}
