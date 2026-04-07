package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	v1beta1 "github.com/example/checkpoint-native-preemption-controller/api/v1beta1"
)

// crdFile represents a parsed CRD YAML for testing.
type crdFile struct {
	Spec struct {
		Group      string `json:"group"`
		Names      struct {
			Kind string `json:"kind"`
		} `json:"names"`
		Conversion struct {
			Strategy string `json:"strategy"`
			Webhook  struct {
				ConversionReviewVersions []string `json:"conversionReviewVersions"`
				ClientConfig             struct {
					Service struct {
						Name      string `json:"name"`
						Namespace string `json:"namespace"`
						Path      string `json:"path"`
					} `json:"service"`
				} `json:"clientConfig"`
			} `json:"webhook"`
		} `json:"conversion"`
		Versions []struct {
			Name    string `json:"name"`
			Served  bool   `json:"served"`
			Storage bool   `json:"storage"`
		} `json:"versions"`
	} `json:"spec"`
}

func TestCRDVersionConfig(t *testing.T) {
	crdDir := filepath.Join("..", "..", "config", "crd", "bases")

	tests := []struct {
		file string
		kind string
	}{
		{"training.checkpoint.example.io_resumabletrainingjobs.yaml", "ResumableTrainingJob"},
		{"training.checkpoint.example.io_checkpointprioritypolicies.yaml", "CheckpointPriorityPolicy"},
		{"training.checkpoint.example.io_resumereadinesspolicies.yaml", "ResumeReadinessPolicy"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			path := filepath.Join(crdDir, tt.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read CRD %s: %v", tt.file, err)
			}

			var crd crdFile
			if err := yaml.Unmarshal(data, &crd); err != nil {
				t.Fatalf("failed to parse CRD %s: %v", tt.file, err)
			}

			// Verify both versions are served.
			versionMap := make(map[string]struct{ served, storage bool })
			for _, v := range crd.Spec.Versions {
				versionMap[v.Name] = struct{ served, storage bool }{v.Served, v.Storage}
			}

			v1a1, ok := versionMap["v1alpha1"]
			if !ok {
				t.Fatal("v1alpha1 version not found in CRD")
			}
			if !v1a1.served {
				t.Error("v1alpha1 must be served (backward compat)")
			}
			if v1a1.storage {
				t.Error("v1alpha1 must NOT be the storage version (v1beta1 is the hub)")
			}

			v1b1, ok := versionMap["v1beta1"]
			if !ok {
				t.Fatal("v1beta1 version not found in CRD")
			}
			if !v1b1.served {
				t.Error("v1beta1 must be served")
			}
			if !v1b1.storage {
				t.Error("v1beta1 must be the storage version")
			}

			// Verify exactly one storage version.
			storageCount := 0
			for _, v := range crd.Spec.Versions {
				if v.Storage {
					storageCount++
				}
			}
			if storageCount != 1 {
				t.Errorf("expected exactly 1 storage version, got %d", storageCount)
			}

			// Verify conversion webhook config.
			if crd.Spec.Conversion.Strategy != "Webhook" {
				t.Errorf("conversion strategy: got %q, want %q",
					crd.Spec.Conversion.Strategy, "Webhook")
			}
			wh := crd.Spec.Conversion.Webhook
			if wh.ClientConfig.Service.Path != "/convert" {
				t.Errorf("webhook path: got %q, want %q",
					wh.ClientConfig.Service.Path, "/convert")
			}
			if wh.ClientConfig.Service.Name != "webhook-service" {
				t.Errorf("webhook service name: got %q, want %q",
					wh.ClientConfig.Service.Name, "webhook-service")
			}
			if len(wh.ConversionReviewVersions) == 0 {
				t.Error("conversionReviewVersions is empty")
			} else if wh.ConversionReviewVersions[0] != "v1" {
				t.Errorf("conversionReviewVersions[0]: got %q, want %q",
					wh.ConversionReviewVersions[0], "v1")
			}
		})
	}
}

// TestSchemeRegistration verifies that both API versions register in the runtime scheme
// and that the expected types are available.
func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("v1alpha1.AddToScheme: %v", err)
	}
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("v1beta1.AddToScheme: %v", err)
	}

	group := "training.checkpoint.example.io"
	kinds := []string{"ResumableTrainingJob", "CheckpointPriorityPolicy", "ResumeReadinessPolicy"}
	versions := []string{"v1alpha1", "v1beta1"}

	for _, version := range versions {
		for _, kind := range kinds {
			gvk := schema.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    kind,
			}
			obj, err := scheme.New(gvk)
			if err != nil {
				t.Errorf("scheme.New(%v): %v", gvk, err)
				continue
			}
			if obj == nil {
				t.Errorf("scheme.New(%v) returned nil", gvk)
			}
		}
	}
}

// TestHubSpokeRelationship verifies that v1beta1 types are Hub and v1alpha1 types
// are Convertible, forming the correct conversion topology.
func TestHubSpokeRelationship(t *testing.T) {
	// v1beta1 types must satisfy conversion.Hub.
	var _ conversion.Hub = &v1beta1.ResumableTrainingJob{}
	var _ conversion.Hub = &v1beta1.CheckpointPriorityPolicy{}
	var _ conversion.Hub = &v1beta1.ResumeReadinessPolicy{}

	// v1alpha1 types must satisfy conversion.Convertible.
	var _ conversion.Convertible = &v1alpha1.ResumableTrainingJob{}
	var _ conversion.Convertible = &v1alpha1.CheckpointPriorityPolicy{}
	var _ conversion.Convertible = &v1alpha1.ResumeReadinessPolicy{}
}

// TestWebhookManifestCoversAllVersions verifies that the webhook configuration
// includes both v1alpha1 and v1beta1 in its matched API versions.
func TestWebhookManifestCoversAllVersions(t *testing.T) {
	webhookDir := filepath.Join("..", "..", "config", "webhook")
	data, err := os.ReadFile(filepath.Join(webhookDir, "manifests.yaml"))
	if err != nil {
		t.Fatalf("failed to read webhook manifests: %v", err)
	}

	content := string(data)

	// Both versions should appear in webhook rules.
	if !strings.Contains(content, "v1alpha1") {
		t.Error("webhook manifests missing v1alpha1")
	}
	if !strings.Contains(content, "v1beta1") {
		t.Error("webhook manifests missing v1beta1")
	}

	// All three resource types should be covered.
	for _, resource := range []string{"resumabletrainingjobs", "checkpointprioritypolicies", "resumereadinesspolicies"} {
		if !strings.Contains(content, resource) {
			t.Errorf("webhook manifests missing resource %q", resource)
		}
	}
}
