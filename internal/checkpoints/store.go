package checkpoints

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ObjectStore interface {
	ListObjects(ctx context.Context, prefixURI string) ([]string, error)
	ReadObject(ctx context.Context, uri string) ([]byte, error)
	StatObject(ctx context.Context, uri string) error
}

type S3Location struct {
	Bucket string
	Key    string
}

type MinIOObjectStore struct {
	client *minio.Client
}

func NewStoreFromEnv() (ObjectStore, error) {
	endpointURL := strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL"))
	if endpointURL == "" {
		endpointURL = strings.TrimSpace(os.Getenv("YIELD_SDK_S3_ENDPOINT"))
	}
	if endpointURL == "" {
		endpointURL = strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	}

	accessKey := strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	if accessKey == "" {
		accessKey = strings.TrimSpace(os.Getenv("YIELD_SDK_S3_ACCESS_KEY"))
	}
	secretKey := strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if secretKey == "" {
		secretKey = strings.TrimSpace(os.Getenv("YIELD_SDK_S3_SECRET_KEY"))
	}
	if endpointURL == "" || accessKey == "" || secretKey == "" {
		return nil, nil
	}

	client, err := newMinIOClient(endpointURL, accessKey, secretKey, os.Getenv("AWS_REGION"))
	if err != nil {
		return nil, err
	}
	return &MinIOObjectStore{client: client}, nil
}

func newMinIOClient(endpointURL, accessKey, secretKey, region string) (*minio.Client, error) {
	parsed, err := url.Parse(endpointURL)
	if err != nil {
		return nil, fmt.Errorf("parse object-store endpoint: %w", err)
	}

	host := parsed.Host
	secure := parsed.Scheme == "https"
	if host == "" {
		host = parsed.Path
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("create object-store client: %w", err)
	}
	return client, nil
}

func (s *MinIOObjectStore) ListObjects(ctx context.Context, prefixURI string) ([]string, error) {
	location, err := ParseS3URI(prefixURI)
	if err != nil {
		return nil, err
	}

	var uris []string
	for object := range s.client.ListObjects(ctx, location.Bucket, minio.ListObjectsOptions{
		Prefix:    location.Key,
		Recursive: true,
	}) {
		if object.Err != nil {
			return nil, object.Err
		}
		if strings.HasSuffix(object.Key, "/") {
			continue
		}
		uris = append(uris, fmt.Sprintf("s3://%s/%s", location.Bucket, object.Key))
	}
	sort.Strings(uris)
	return uris, nil
}

func (s *MinIOObjectStore) ReadObject(ctx context.Context, uri string) ([]byte, error) {
	location, err := ParseS3URI(uri)
	if err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, location.Bucket, location.Key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return io.ReadAll(object)
}

func (s *MinIOObjectStore) StatObject(ctx context.Context, uri string) error {
	location, err := ParseS3URI(uri)
	if err != nil {
		return err
	}
	_, err = s.client.StatObject(ctx, location.Bucket, location.Key, minio.StatObjectOptions{})
	return err
}

func ParseS3URI(uri string) (S3Location, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return S3Location{}, fmt.Errorf("parse s3 uri: %w", err)
	}
	if parsed.Scheme != "s3" {
		return S3Location{}, fmt.Errorf("unsupported storage uri %q", uri)
	}
	return S3Location{
		Bucket: parsed.Host,
		Key:    strings.TrimPrefix(parsed.Path, "/"),
	}, nil
}

func isNotFound(err error) bool {
	response := minio.ToErrorResponse(err)
	return response.Code == "NoSuchKey" || response.StatusCode == 404
}

// -------------------------------------------------------------------------
// Phase 6: Shared checkpoint store configuration
// -------------------------------------------------------------------------
//
// In MultiKueue mode, all clusters (manager and workers) must access the
// same S3-compatible checkpoint store. The existing NewStoreFromEnv() path
// continues to work—operators set the same environment variables on every
// cluster pointing to a shared endpoint.
//
// SharedStoreConfig makes this contract explicit and adds validation that
// rejects cluster-local endpoints (e.g., *.svc.cluster.local, localhost)
// which would silently break cross-cluster checkpoint access.

// SharedStoreConfig captures the configuration for a checkpoint store that
// is shared across all clusters in a MultiKueue deployment.
type SharedStoreConfig struct {
	// Endpoint is the S3-compatible endpoint URL (e.g., "https://minio.shared.example.com").
	// Must be reachable from all clusters.
	Endpoint string

	// Region is the S3 region (e.g., "us-east-1"). May be empty for MinIO.
	Region string

	// AccessKey is the S3 access key ID.
	AccessKey string

	// SecretKey is the S3 secret access key.
	SecretKey string
}

// NewStoreFromConfig creates an ObjectStore from explicit configuration.
// This is the preferred path for MultiKueue deployments where the shared
// endpoint must be validated before use.
func NewStoreFromConfig(cfg SharedStoreConfig) (ObjectStore, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("shared store config requires endpoint, accessKey, and secretKey")
	}
	client, err := newMinIOClient(cfg.Endpoint, cfg.AccessKey, cfg.SecretKey, cfg.Region)
	if err != nil {
		return nil, err
	}
	return &MinIOObjectStore{client: client}, nil
}

// ValidateSharedEndpoint checks that the endpoint is likely reachable from
// multiple clusters. Rejects endpoints that are obviously cluster-local
// (Kubernetes service DNS, localhost, loopback).
//
// This is a best-effort check. Operators are still responsible for ensuring
// network connectivity from all clusters.
func ValidateSharedEndpoint(endpoint string) error {
	lower := strings.ToLower(strings.TrimSpace(endpoint))
	if lower == "" {
		return fmt.Errorf("endpoint is empty")
	}

	parsed, err := url.Parse(lower)
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		host = lower
	}

	// Reject Kubernetes-internal DNS names.
	if strings.HasSuffix(host, ".svc.cluster.local") ||
		strings.HasSuffix(host, ".cluster.local") ||
		strings.HasSuffix(host, ".svc") {
		return fmt.Errorf(
			"endpoint %q appears to be a cluster-local Kubernetes service; "+
				"MultiKueue requires a shared endpoint reachable from all clusters",
			endpoint,
		)
	}

	// Reject localhost and loopback.
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" {
		return fmt.Errorf(
			"endpoint %q is a loopback address; "+
				"MultiKueue requires a shared endpoint reachable from all clusters",
			endpoint,
		)
	}

	return nil
}

// IsSharedStoreConfigured returns true when the environment variables
// configure a checkpoint store endpoint that passes shared-endpoint
// validation. Used for pre-flight checks in MultiKueue manager mode.
func IsSharedStoreConfigured() (bool, error) {
	endpointURL := strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL"))
	if endpointURL == "" {
		endpointURL = strings.TrimSpace(os.Getenv("YIELD_SDK_S3_ENDPOINT"))
	}
	if endpointURL == "" {
		endpointURL = strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	}
	if endpointURL == "" {
		return false, nil
	}
	if err := ValidateSharedEndpoint(endpointURL); err != nil {
		return false, err
	}
	return true, nil
}
