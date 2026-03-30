package checkpoints

import (
	"testing"
)

// -------------------------------------------------------------------------
// SharedStoreConfig validation tests
// -------------------------------------------------------------------------

func TestValidateSharedEndpointAcceptsExternalEndpoints(t *testing.T) {
	validEndpoints := []string{
		"https://minio.shared.example.com",
		"https://s3.amazonaws.com",
		"http://minio.example.com:9000",
		"https://storage.googleapis.com",
		"http://192.168.1.100:9000",
		"https://10.0.0.50:443",
	}
	for _, ep := range validEndpoints {
		if err := ValidateSharedEndpoint(ep); err != nil {
			t.Errorf("expected endpoint %q to be accepted, got error: %v", ep, err)
		}
	}
}

func TestValidateSharedEndpointRejectsClusterLocalEndpoints(t *testing.T) {
	clusterLocal := []string{
		"http://minio.minio-system.svc.cluster.local:9000",
		"http://my-service.default.svc.cluster.local",
		"http://minio.ns.svc.cluster.local:9000",
		"https://store.prod.svc",
	}
	for _, ep := range clusterLocal {
		if err := ValidateSharedEndpoint(ep); err == nil {
			t.Errorf("expected cluster-local endpoint %q to be rejected", ep)
		}
	}
}

func TestValidateSharedEndpointRejectsLoopback(t *testing.T) {
	loopbacks := []string{
		"http://localhost:9000",
		"https://localhost",
		"http://127.0.0.1:9000",
		"http://127.0.0.1",
		"http://0.0.0.0:9000",
	}
	for _, ep := range loopbacks {
		if err := ValidateSharedEndpoint(ep); err == nil {
			t.Errorf("expected loopback endpoint %q to be rejected", ep)
		}
	}
}

func TestValidateSharedEndpointRejectsEmpty(t *testing.T) {
	if err := ValidateSharedEndpoint(""); err == nil {
		t.Error("expected empty endpoint to be rejected")
	}
	if err := ValidateSharedEndpoint("   "); err == nil {
		t.Error("expected whitespace-only endpoint to be rejected")
	}
}

func TestValidateSharedEndpointIsCaseInsensitive(t *testing.T) {
	// Kubernetes DNS names should be rejected regardless of case.
	if err := ValidateSharedEndpoint("http://MinIO.NS.SVC.CLUSTER.LOCAL:9000"); err == nil {
		t.Error("expected case-insensitive rejection of cluster-local endpoint")
	}
	if err := ValidateSharedEndpoint("http://LOCALHOST:9000"); err == nil {
		t.Error("expected case-insensitive rejection of localhost")
	}
}

// -------------------------------------------------------------------------
// SharedStoreConfig constructor tests
// -------------------------------------------------------------------------

func TestNewStoreFromConfigRequiresAllFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  SharedStoreConfig
	}{
		{
			name: "missing endpoint",
			cfg:  SharedStoreConfig{AccessKey: "key", SecretKey: "secret"},
		},
		{
			name: "missing access key",
			cfg:  SharedStoreConfig{Endpoint: "http://example.com:9000", SecretKey: "secret"},
		},
		{
			name: "missing secret key",
			cfg:  SharedStoreConfig{Endpoint: "http://example.com:9000", AccessKey: "key"},
		},
		{
			name: "all empty",
			cfg:  SharedStoreConfig{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewStoreFromConfig(tc.cfg)
			if err == nil {
				t.Fatalf("expected error for config with %s", tc.name)
			}
		})
	}
}

func TestNewStoreFromConfigAcceptsValidConfig(t *testing.T) {
	cfg := SharedStoreConfig{
		Endpoint:  "http://minio.shared.example.com:9000",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
	}
	store, err := NewStoreFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

// -------------------------------------------------------------------------
// Shared checkpoint-store contract: not cluster-local in MultiKueue mode
// -------------------------------------------------------------------------

func TestSharedStoreConfigNotClusterLocalInMultiKueueMode(t *testing.T) {
	// This test proves that a shared store configuration rejects cluster-
	// local endpoints, enforcing the Phase 6 contract that all clusters
	// must reach the same S3-compatible store.

	// A cluster-local endpoint that a single-cluster setup might use:
	clusterLocalEndpoint := "http://minio.minio-system.svc.cluster.local:9000"

	// Validation must reject this for MultiKueue.
	if err := ValidateSharedEndpoint(clusterLocalEndpoint); err == nil {
		t.Fatal("expected cluster-local endpoint to be rejected for MultiKueue shared store")
	}

	// A proper shared endpoint should be accepted:
	sharedEndpoint := "https://minio.shared.example.com:9000"
	if err := ValidateSharedEndpoint(sharedEndpoint); err != nil {
		t.Fatalf("expected shared endpoint to be accepted, got: %v", err)
	}
}

// -------------------------------------------------------------------------
// ParseS3URI tests (existing functionality preserved)
// -------------------------------------------------------------------------

func TestParseS3URIValid(t *testing.T) {
	loc, err := ParseS3URI("s3://my-bucket/path/to/object")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loc.Bucket != "my-bucket" {
		t.Fatalf("expected bucket %q, got %q", "my-bucket", loc.Bucket)
	}
	if loc.Key != "path/to/object" {
		t.Fatalf("expected key %q, got %q", "path/to/object", loc.Key)
	}
}

func TestParseS3URIRejectsNonS3(t *testing.T) {
	_, err := ParseS3URI("http://example.com/bucket/key")
	if err == nil {
		t.Fatal("expected error for non-s3 URI")
	}
}

func TestParseS3URIRejectsMalformed(t *testing.T) {
	_, err := ParseS3URI("://broken")
	if err == nil {
		t.Fatal("expected error for malformed URI")
	}
}
