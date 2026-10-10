package api

import "testing"

// The rules a storage change is held to, without a database: what is accepted,
// what is refused and which input the refusal names.
func TestMergeStorage(t *testing.T) {
	type tc struct {
		name      string
		body      UpdateStorageSettingsRequest
		wantField string // "" means accepted
	}
	for _, c := range []tc{
		{"nothing changes nothing", UpdateStorageSettingsRequest{}, ""},
		{"an unknown driver", UpdateStorageSettingsRequest{Driver: ptr(StorageDriver("ftp"))}, "driver"},
		{"S3 needs a bucket", UpdateStorageSettingsRequest{Driver: ptr(S3)}, "s3.bucket"},
		{"S3 with a bucket", UpdateStorageSettingsRequest{Driver: ptr(S3), S3: &UpdateStorageS3Settings{Bucket: ptr("b")}}, ""},
		{"a bucket is not needed on the local driver", UpdateStorageSettingsRequest{Driver: ptr(Local)}, ""},
		{"a blank bucket does not count", UpdateStorageSettingsRequest{Driver: ptr(S3), S3: &UpdateStorageS3Settings{Bucket: ptr("  ")}}, "s3.bucket"},
		{"an endpoint that is not a URL", UpdateStorageSettingsRequest{S3: &UpdateStorageS3Settings{Endpoint: ptr("minio:9000")}}, "s3.endpoint"},
		{"an endpoint that is not http", UpdateStorageSettingsRequest{S3: &UpdateStorageS3Settings{Endpoint: ptr("ftp://minio")}}, "s3.endpoint"},
		{"an http endpoint", UpdateStorageSettingsRequest{S3: &UpdateStorageS3Settings{Endpoint: ptr("http://localhost:9000/")}}, ""},
		{"an empty endpoint is AWS", UpdateStorageSettingsRequest{S3: &UpdateStorageS3Settings{Endpoint: ptr("")}}, ""},
		{"an empty directory", UpdateStorageSettingsRequest{LocalDir: ptr("  ")}, "localDir"},
		{"a public address with a path", UpdateStorageSettingsRequest{PublicUrl: ptr("https://chat.example.com/api")}, "publicUrl"},
		{"a public address with a query", UpdateStorageSettingsRequest{PublicUrl: ptr("https://chat.example.com/?a=1")}, "publicUrl"},
		{"a public address with credentials", UpdateStorageSettingsRequest{PublicUrl: ptr("https://u:p@chat.example.com")}, "publicUrl"},
		{"a public address that is not http", UpdateStorageSettingsRequest{PublicUrl: ptr("chat.example.com")}, "publicUrl"},
		{"an origin", UpdateStorageSettingsRequest{PublicUrl: ptr("https://chat.example.com:8443/")}, ""},
		{"an empty public address", UpdateStorageSettingsRequest{PublicUrl: ptr("")}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := mergeStorage(defaultStorageConfig(), "", c.body)
			switch {
			case c.wantField == "" && err != nil:
				t.Errorf("refused: %v", err)
			case c.wantField != "" && err == nil:
				t.Errorf("accepted, want a refusal naming %q", c.wantField)
			case err != nil && err.Field != c.wantField:
				t.Errorf("named %q, want %q", err.Field, c.wantField)
			}
		})
	}
}

func TestMergeStorageNormalizesAndKeepsTheSecret(t *testing.T) {
	base := defaultStorageConfig()
	base.S3.Bucket = "b"

	config, secret, err := mergeStorage(base, "kept-secret", UpdateStorageSettingsRequest{
		PublicUrl: ptr("https://chat.example.com/"),
		S3:        &UpdateStorageS3Settings{Endpoint: ptr(" http://localhost:9000/ "), Region: ptr("")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.PublicURL != "https://chat.example.com" || config.S3.Endpoint != "http://localhost:9000" {
		t.Errorf("not normalized: %+v", config)
	}
	if config.S3.Region != "us-east-1" {
		t.Errorf("an empty region should fall back to the default, got %q", config.S3.Region)
	}
	if secret != "kept-secret" {
		t.Errorf("an absent secret should be kept, got %q", secret)
	}

	_, secret, _ = mergeStorage(base, "kept-secret", UpdateStorageSettingsRequest{S3: &UpdateStorageS3Settings{SecretAccessKey: ptr("")}})
	if secret != "" {
		t.Errorf("an empty secret should clear it, got %q", secret)
	}
	_, secret, _ = mergeStorage(base, "kept-secret", UpdateStorageSettingsRequest{S3: &UpdateStorageS3Settings{SecretAccessKey: ptr(" new ")}})
	if secret != "new" {
		t.Errorf("secret = %q", secret)
	}
}
