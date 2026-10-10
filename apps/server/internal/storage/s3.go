// Package storage keeps attachments' bytes: in S3-compatible blob storage
// (MinIO locally, S3/R2 in production) or on the server's own disk. Either way a
// client PUTs to a presigned URL and reads through a signed one; with S3 the
// bytes never pass through the API, with the local driver the URLs point at it.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Backend is what the rest of the server needs of a blob store, whichever
// driver is behind it.
type Backend interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	ObjectURL(key string) string
	Size(ctx context.Context, key string) (int64, error)
	Read(ctx context.Context, key string, limit int64) ([]byte, error)
}

var (
	_ Backend = (*Store)(nil)
	_ Backend = (*Local)(nil)
	_ Backend = (*Switch)(nil)
)

// Store is the S3 driver.
type Store struct {
	client   *s3.Client
	presign  *s3.PresignClient
	bucket   string
	endpoint string
	pathSty  bool
}

// S3Settings is what the S3 driver needs to reach a bucket.
type S3Settings struct {
	// Endpoint is the service's base URL; empty is AWS itself.
	Endpoint string
	Region   string
	Bucket   string
	// AccessKeyID and SecretAccessKey are static credentials. Without both, the
	// standard AWS credential chain is used.
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle addresses the bucket in the URL path, which MinIO needs, rather
	// than in the host name.
	PathStyle bool
}

// New builds a store from its settings. Static credentials win when present,
// otherwise the standard AWS credential chain is used. It does not touch the
// network; Probe does.
func New(ctx context.Context, cfg S3Settings) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("storage: an S3 store needs a bucket")
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
		}
		o.UsePathStyle = cfg.PathStyle
	})

	return &Store{
		client:   client,
		presign:  s3.NewPresignClient(client),
		bucket:   cfg.Bucket,
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		pathSty:  cfg.PathStyle,
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

// ErrTooLarge is an object bigger than the limit a read was given.
var ErrTooLarge = errors.New("storage: object exceeds the read limit")

// Size is the stored size of an object, which is what the bytes actually
// uploaded add up to, whatever the client said it would send.
func (s *Store) Size(ctx context.Context, key string) (int64, error) {
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return 0, fmt.Errorf("storage: head %q: %w", key, err)
	}
	if head.ContentLength == nil {
		return 0, fmt.Errorf("storage: head %q: no content length", key)
	}
	return *head.ContentLength, nil
}

// Read returns an object's bytes, refusing one larger than limit rather than
// holding it in memory to find out how big it is.
func (s *Store) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, fmt.Errorf("storage: get %q: %w", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(io.LimitReader(out.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("storage: read %q: %w", key, err)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
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

// Delete removes an object. Deleting one that is not there is not an error, as
// with S3 itself.
func (s *Store) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

// Probe checks that the bucket can be written, read and deleted from, by doing
// each to a small object under probes/. The error is short and in English, for
// showing to the operator who configured the bucket.
func (s *Store) Probe(ctx context.Context) error {
	key, payload := probeObject()
	body := strings.NewReader(string(payload))
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, Body: body}); err != nil {
		return fmt.Errorf("write failed: %s", describeS3Error(err))
	}
	// From here on the object exists; try to take it away again whatever else
	// goes wrong.
	defer func() { _ = s.Delete(context.WithoutCancel(ctx), key) }()
	got, err := s.Read(ctx, key, int64(len(payload)))
	if err != nil {
		return fmt.Errorf("read failed: %s", describeS3Error(err))
	}
	if string(got) != string(payload) {
		return errors.New("read back different bytes than were written")
	}
	if err := s.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete failed: %s", describeS3Error(err))
	}
	return nil
}

// describeS3Error is an S3 failure in a sentence, where the SDK's own message is
// a paragraph of operation names and request ids.
func describeS3Error(err error) string {
	if err == nil {
		return "unknown error"
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.ErrorCode()
		if detail := apiErr.ErrorMessage(); detail != "" && detail != msg {
			msg += ": " + detail
		}
		return msg
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timed out"
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}
