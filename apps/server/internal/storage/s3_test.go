package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestObjectURLPathStyle(t *testing.T) {
	s := &Store{bucket: "openchat", endpoint: "http://localhost:9000", pathSty: true}

	got := s.ObjectURL("uploads/2026/09/12/id.md")
	want := "http://localhost:9000/openchat/uploads/2026/09/12/id.md"
	if got != want {
		t.Errorf("ObjectURL = %q, want %q", got, want)
	}
}

func TestObjectURLVirtualHosted(t *testing.T) {
	s := &Store{bucket: "openchat", endpoint: "https://s3.example.com", pathSty: false}

	got := s.ObjectURL("uploads/a b.png")
	want := "https://openchat.s3.example.com/uploads/a%20b.png"
	if got != want {
		t.Errorf("ObjectURL = %q, want %q", got, want)
	}
}

// The bucket must refuse a body of another size than the upload was counted
// at, so the length is one of the signed headers.
func TestPresignPutSignsTheLength(t *testing.T) {
	s, err := New(context.Background(), S3Settings{
		Bucket: "openchat", Region: "us-east-1", Endpoint: "http://localhost:9000", PathStyle: true,
		AccessKeyID: "key", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := s.PresignPut(context.Background(), "uploads/a.png", "image/png", 1234, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if headers := u.Query().Get("X-Amz-SignedHeaders"); !strings.Contains(headers, "content-length") {
		t.Errorf("signed headers = %q, want content-length among them", headers)
	}
}
