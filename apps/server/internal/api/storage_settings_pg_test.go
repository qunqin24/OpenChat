package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// clearStorageSettings removes the rows these tests write, before and after,
// from the scratch database they run against.
func clearStorageSettings(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	clear := func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM app_settings WHERE key IN ($1, $2, $3)", settingStorage, settingStorageS3Secret, settingStorageSigningKey)
	}
	clear()
	t.Cleanup(clear)
}

func TestStorageSigningKeyIsMadeOnceAndStaysStable(t *testing.T) {
	pool := channelTestPool(t)
	clearStorageSettings(t, pool)
	ctx := context.Background()
	q := sqlc.New(pool)

	// Several boots at once, each finding the key missing.
	keys := make([][]byte, 8)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := storageSigningKey(ctx, q)
			if err != nil {
				t.Error(err)
			}
			keys[i] = key
		}()
	}
	wg.Wait()
	if t.Failed() {
		return
	}
	if len(keys[0]) != 32 {
		t.Fatalf("key is %d bytes, want 32", len(keys[0]))
	}
	for i, key := range keys {
		if string(key) != string(keys[0]) {
			t.Fatalf("boot %d got a different key", i)
		}
	}

	// And on later boots.
	again, err := storageSigningKey(ctx, q)
	if err != nil || string(again) != string(keys[0]) {
		t.Errorf("a later boot changed the key (%v)", err)
	}

	// A malformed stored key is an error, not a weak key.
	if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingStorageSigningKey, Value: []byte(`"abc"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := storageSigningKey(ctx, q); err == nil {
		t.Error("a malformed key was accepted")
	}
}

func TestOpenStorageFallsBackToTheDefaultsWhenStoredSettingsFail(t *testing.T) {
	pool := channelTestPool(t)
	clearStorageSettings(t, pool)
	ctx := context.Background()
	q := sqlc.New(pool)

	// Run from a scratch directory: the defaults put files in ./data.
	t.Chdir(t.TempDir())

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stored, _ := json.Marshal(map[string]any{"driver": "local", "localDir": filepath.Join(blocker, "sub")})
	if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingStorage, Value: stored}); err != nil {
		t.Fatal(err)
	}

	sw, err := OpenStorage(ctx, pool)
	if err != nil {
		t.Fatalf("OpenStorage refused to start: %v", err)
	}
	if sw.Driver() != storage.DriverLocal || !strings.HasSuffix(sw.Local().Dir(), "data") {
		t.Errorf("fell back to %q in %q", sw.Driver(), sw.Local().Dir())
	}
}

// admin calls a handler as a signed-in admin.
func adminCall(handler func(http.ResponseWriter, *http.Request), method, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	handler(rec, req.WithContext(auth.WithUser(req.Context(), sqlc.User{Email: "admin@test.local"})))
	return rec
}

func TestStorageSettingsHandlers(t *testing.T) {
	pool := channelTestPool(t)
	clearStorageSettings(t, pool)
	ctx := context.Background()
	q := sqlc.New(pool)

	key, err := storageSigningKey(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	sw := storage.NewSwitch(key)
	server := NewServer(&config.Config{}, pool, sw, nil)

	dir := filepath.Join(t.TempDir(), "files")
	get := func() (StorageSettings, string) {
		t.Helper()
		rec := adminCall(server.GetStorageSettings, http.MethodGet, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", rec.Code, rec.Body)
		}
		var out StorageSettings
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out, rec.Body.String()
	}
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return adminCall(server.UpdateStorageSettings, http.MethodPatch, body)
	}

	// Defaults, before anything was saved.
	got, _ := get()
	if got.Driver != Local || got.LocalDir != "./data" || got.S3.Region != "us-east-1" || !got.S3.PathStyle || got.S3.SecretKeySet {
		t.Errorf("defaults = %+v", got)
	}
	if !filepath.IsAbs(got.ResolvedLocalDir) {
		t.Errorf("resolvedLocalDir = %q", got.ResolvedLocalDir)
	}

	// Refusals save nothing and change nothing.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"bad driver":      `{"driver":"ftp"}`,
		"s3 no bucket":    `{"driver":"s3"}`,
		"bad endpoint":    `{"s3":{"endpoint":"minio:9000"}}`,
		"public path":     `{"publicUrl":"https://x.test/api"}`,
		"unusable dir":    `{"localDir":` + jsonString(filepath.Join(blocker, "sub")) + `}`,
		"unknown field":   `{"nope":1}`,
		"secret with bad": `{"driver":"s3","s3":{"secretAccessKey":"leaked-secret-9999"}}`,
	} {
		rec := patch(body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d %s", name, rec.Code, rec.Body)
		}
	}
	if got, _ := get(); got.LocalDir != "./data" || got.S3.SecretKeySet {
		t.Errorf("a refused change was saved: %+v", got)
	}
	if sw.Local() != nil {
		t.Error("a refused change was applied")
	}

	// An accepted change is applied at once and saved.
	rec := patch(`{"localDir":` + jsonString(dir) + `,"publicUrl":"https://chat.example.com/","s3":{"endpoint":"http://localhost:9000","bucket":"openchat","accessKeyId":"AKIAEXAMPLE","secretAccessKey":"super-secret-value-1234"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if sw.Local() == nil || sw.Local().Dir() != dir || sw.Driver() != storage.DriverLocal {
		t.Errorf("not applied: %v", sw.Local())
	}
	if !strings.HasPrefix(sw.ObjectURL("uploads/a.png"), "https://chat.example.com"+storage.ContentPath) {
		t.Errorf("ObjectURL = %q", sw.ObjectURL("uploads/a.png"))
	}

	// The secret is write-only: not in the PATCH response, not in a GET, not in
	// the settings row.
	got, raw := get()
	if strings.Contains(raw, "super-secret-value-1234") || strings.Contains(rec.Body.String(), "super-secret-value-1234") {
		t.Fatal("the secret access key was returned")
	}
	if !got.S3.SecretKeySet || got.S3.SecretKeyPreview == nil || *got.S3.SecretKeyPreview != "…1234" {
		t.Errorf("secret state = %+v", got.S3)
	}
	if got.LocalDir != dir || got.ResolvedLocalDir != dir || got.PublicUrl != "https://chat.example.com" || got.S3.Bucket != "openchat" || got.S3.Endpoint != "http://localhost:9000" {
		t.Errorf("settings = %+v", got)
	}
	row, err := q.GetSetting(ctx, settingStorage)
	if err != nil || strings.Contains(string(row), "super-secret") {
		t.Errorf("the settings row holds the secret (%v): %s", err, row)
	}
	signing, _ := q.GetSetting(ctx, settingStorageSigningKey)
	if strings.Contains(raw, strings.Trim(string(signing), `"`)) {
		t.Error("the signing key was returned")
	}

	// A field left out keeps its value, including the secret; an empty secret
	// clears it.
	if rec := patch(`{"s3":{"region":"eu-west-1"}}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if got, _ := get(); !got.S3.SecretKeySet || got.S3.Region != "eu-west-1" || got.S3.Bucket != "openchat" {
		t.Errorf("a partial PATCH lost fields: %+v", got)
	}
	if rec := patch(`{"s3":{"secretAccessKey":""}}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if got, _ := get(); got.S3.SecretKeySet || got.S3.SecretKeyPreview != nil {
		t.Errorf("the secret was not cleared: %+v", got.S3)
	}

	// Switching to S3 takes effect without a restart.
	if rec := patch(`{"driver":"s3"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if sw.Driver() != storage.DriverS3 {
		t.Errorf("driver = %q", sw.Driver())
	}

	// A fresh process reads the same settings back.
	reopened, err := OpenStorage(ctx, pool)
	if err != nil || reopened.Driver() != storage.DriverS3 || reopened.Local().Dir() != dir {
		t.Errorf("reopened = %v, %v", reopened, err)
	}
}

func TestTestStorageProbesWithoutSaving(t *testing.T) {
	pool := channelTestPool(t)
	clearStorageSettings(t, pool)
	ctx := context.Background()
	key, err := storageSigningKey(ctx, sqlc.New(pool))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(&config.Config{}, pool, storage.NewSwitch(key), nil)

	probe := func(body string) StorageTestResult {
		t.Helper()
		rec := adminCall(server.TestStorage, http.MethodPost, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("test: %d %s", rec.Code, rec.Body)
		}
		var out StorageTestResult
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	dir := filepath.Join(t.TempDir(), "probe-me")
	if res := probe(`{"localDir":` + jsonString(dir) + `}`); !res.Ok || res.Error != nil {
		t.Errorf("a usable directory: %+v", res)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "probes")); len(left) != 0 {
		t.Errorf("the probe left %d files behind", len(left))
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if res := probe(`{"localDir":` + jsonString(filepath.Join(blocker, "sub")) + `}`); res.Ok || res.Error == nil || strings.Contains(*res.Error, "storage:") {
		t.Errorf("an unusable directory: %+v", res)
	}
	// An invalid form is a failed probe with the reason, not an HTTP error.
	if res := probe(`{"driver":"s3"}`); res.Ok || res.Error == nil || !strings.Contains(*res.Error, "bucket") {
		t.Errorf("S3 with no bucket: %+v", res)
	}
	// A bucket nobody answers for.
	res := probe(`{"driver":"s3","s3":{"endpoint":"http://127.0.0.1:1","bucket":"nope","accessKeyId":"a","secretAccessKey":"b"}}`)
	if res.Ok || res.Error == nil || len(*res.Error) > 300 || strings.Contains(*res.Error, "RequestID") {
		t.Errorf("an unreachable bucket: %+v", res)
	}

	// Nothing was saved.
	if _, err := sqlc.New(pool).GetSetting(ctx, settingStorage); err == nil {
		t.Error("testing saved the settings")
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
