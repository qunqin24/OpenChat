// Package storage wraps S3-compatible blob storage (MinIO locally, S3/R2 in
// production). Uploads never pass through the API: clients PUT to a presigned URL.
package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/qunqin/openchat/apps/server/internal/config"
)

type Store struct {
	client   *s3.Client
	presign  *s3.PresignClient
	bucket   string
	endpoint string
	pathSty  bool
}

// New builds a store from config. Static credentials win when present,
// otherwise the standard AWS credential chain is used.
func New(ctx context.Context, cfg *config.Config) (*Store, error) {
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3Region)}
	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = &cfg.S3Endpoint
		}
		o.UsePathStyle = cfg.S3UsePathStyle
	})

	return &Store{
		client:   client,
		presign:  s3.NewPresignClient(client),
		bucket:   cfg.S3Bucket,
		endpoint: strings.TrimRight(cfg.S3Endpoint, "/"),
		pathSty:  cfg.S3UsePathStyle,
	}, nil
}

// PresignPut returns a URL the browser can PUT the file to directly.
func (s *Store) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		ContentType: &contentType,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign put %q: %w", key, err)
	}
	return req.URL, nil
}

// PresignGet returns a time-limited read URL for a stored object.
func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign get %q: %w", key, err)
	}
	return req.URL, nil
}

// ObjectURL is the canonical (unsigned) location of a key.
func (s *Store) ObjectURL(key string) string {
	if s.pathSty {
		return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, escapeKey(key))
	}
	return fmt.Sprintf("%s/%s", strings.Replace(s.endpoint, "://", "://"+s.bucket+".", 1), escapeKey(key))
}

// escapeKey percent-encodes each path segment, leaving the separators intact.
func escapeKey(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func (s *Store) Bucket() string { return s.bucket }
