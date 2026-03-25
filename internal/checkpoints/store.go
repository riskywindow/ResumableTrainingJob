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
