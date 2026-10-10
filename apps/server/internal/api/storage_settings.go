package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

// settingStorage holds where attachments are kept, as JSON. The S3 secret lives
// apart, under settingStorageS3Secret, so a read of these never carries it.
const settingStorage = "storage"

// settingStorageS3Secret holds the S3 secret access key. Write-only through the
// API, like a channel's key: reads return whether one is set and its last four
// characters.
const settingStorageS3Secret = "storage_s3_secret"

// settingStorageSigningKey holds the key that signs the local driver's upload
// and download URLs: 32 random bytes, hex, made on first boot. No API reads or
// writes it. Replacing it would invalidate every link already issued, which is
// harmless (links last minutes) but pointless, so nothing does.
const settingStorageSigningKey = "storage_signing_key"

// storageConfig is the stored form of the settings.
type storageConfig struct {
	Driver    string          `json:"driver"`
	LocalDir  string          `json:"localDir"`
	PublicURL string          `json:"publicUrl"`
	S3        storageS3Config `json:"s3"`
}

type storageS3Config struct {
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	AccessKeyID string `json:"accessKeyId"`
	PathStyle   bool   `json:"pathStyle"`
}

// defaultStorageConfig is what an installation starts with: the server's own
// disk, so attachments work without a bucket.
func defaultStorageConfig() storageConfig {
	d := storage.DefaultSettings()
	return storageConfig{
		Driver:   d.Driver,
		LocalDir: d.LocalDir,
		S3:       storageS3Config{Region: d.S3.Region, PathStyle: d.S3.PathStyle},
	}
}

func loadStorage(ctx context.Context, q *sqlc.Queries) (storageConfig, string, error) {
	config := defaultStorageConfig()
	raw, err := q.GetSetting(ctx, settingStorage)
	switch {
	case err == nil:
		// Over the defaults, so a field added later reads as its default in
		// settings saved before it existed.
		if err := json.Unmarshal(raw, &config); err != nil {
			return config, "", err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return config, "", err
	}
	secret, err := stringSetting(ctx, q, settingStorageS3Secret)
	return config, secret, err
}

// settings is what the storage package builds its drivers from.
func (c storageConfig) settings(secret string) storage.Settings {
	return storage.Settings{
		Driver:    c.Driver,
		LocalDir:  c.LocalDir,
		PublicURL: c.PublicURL,
		S3: storage.S3Settings{
			Endpoint:        c.S3.Endpoint,
			Region:          c.S3.Region,
			Bucket:          c.S3.Bucket,
			AccessKeyID:     c.S3.AccessKeyID,
			SecretAccessKey: secret,
			PathStyle:       c.S3.PathStyle,
		},
	}
}

func toStorageSettings(c storageConfig, secret string) StorageSettings {
	resolved, err := filepath.Abs(c.LocalDir)
	if err != nil {
		resolved = c.LocalDir
	}
	out := StorageSettings{
		Driver:           StorageDriver(c.Driver),
		LocalDir:         c.LocalDir,
		ResolvedLocalDir: resolved,
		PublicUrl:        c.PublicURL,
		S3: StorageS3Settings{
			Endpoint:     c.S3.Endpoint,
			Region:       c.S3.Region,
			Bucket:       c.S3.Bucket,
			AccessKeyId:  c.S3.AccessKeyID,
			PathStyle:    c.S3.PathStyle,
			SecretKeySet: secret != "",
		},
	}
	if preview := keyPreview(secret); preview != "" {
		out.S3.SecretKeyPreview = &preview
	}
	return out
}

// storageSigningKey is the key URLs are signed with, made on the first call.
//
// Two processes starting together both find it missing and both make one;
// InsertSettingIfMissing lets only the first write stand, and reading back is
// how each learns which that was.
func storageSigningKey(ctx context.Context, q *sqlc.Queries) ([]byte, error) {
	key, err := stringSetting(ctx, q, settingStorageSigningKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		value, _ := json.Marshal(hex.EncodeToString(fresh))
		if err := q.InsertSettingIfMissing(ctx, sqlc.InsertSettingIfMissingParams{Key: settingStorageSigningKey, Value: value}); err != nil {
			return nil, err
		}
		if key, err = stringSetting(ctx, q, settingStorageSigningKey); err != nil {
			return nil, err
		}
	}
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) < 32 {
		return nil, errors.New("the stored storage signing key is malformed")
	}
	return decoded, nil
}

// OpenStorage builds the blob store from what the database says. Settings that
// cannot be applied — a directory that is gone or unwritable — are logged and
// replaced by the defaults rather than refusing to start: the operator can only
// fix them from the admin panel, which needs the server up.
func OpenStorage(ctx context.Context, pool *pgxpool.Pool) (*storage.Switch, error) {
	q := sqlc.New(pool)
	key, err := storageSigningKey(ctx, q)
	if err != nil {
		return nil, err
	}
	sw := storage.NewSwitch(key)

	config, secret, err := loadStorage(ctx, q)
	if err == nil {
		err = sw.Apply(ctx, config.settings(secret))
	}
	if err != nil {
		slog.Error("the stored file storage settings cannot be used; falling back to the defaults", "error", err)
		config = defaultStorageConfig()
		if err := sw.Apply(ctx, config.settings("")); err != nil {
			return nil, err
		}
	}
	slog.Info("attachment storage ready", "driver", sw.Driver(), "dir", sw.Local().Dir())
	return sw, nil
}

func (s *Server) GetStorageSettings(w http.ResponseWriter, r *http.Request) {
	config, secret, err := loadStorage(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toStorageSettings(config, secret))
}

// mergeStorage lays the fields a request carries over the stored settings and
// checks the result. The secret is returned alongside: absent keeps it, empty
// clears it.
func mergeStorage(config storageConfig, secret string, body UpdateStorageSettingsRequest) (storageConfig, string, *httpx.APIError) {
	if body.Driver != nil {
		config.Driver = string(*body.Driver)
	}
	if body.LocalDir != nil {
		config.LocalDir = strings.TrimSpace(*body.LocalDir)
	}
	if body.PublicUrl != nil {
		config.PublicURL = strings.TrimSpace(*body.PublicUrl)
	}
	if s3 := body.S3; s3 != nil {
		if s3.Endpoint != nil {
			config.S3.Endpoint = strings.TrimSpace(*s3.Endpoint)
		}
		if s3.Region != nil {
			config.S3.Region = strings.TrimSpace(*s3.Region)
		}
		if s3.Bucket != nil {
			config.S3.Bucket = strings.TrimSpace(*s3.Bucket)
		}
		if s3.AccessKeyId != nil {
			config.S3.AccessKeyID = strings.TrimSpace(*s3.AccessKeyId)
		}
		if s3.PathStyle != nil {
			config.S3.PathStyle = *s3.PathStyle
		}
		if s3.SecretAccessKey != nil {
			secret = strings.TrimSpace(*s3.SecretAccessKey)
		}
	}

	switch config.Driver {
	case storage.DriverLocal, storage.DriverS3:
	default:
		return config, secret, httpx.NewFieldError("driver", "invalid_driver", "the driver must be local or s3")
	}
	if config.LocalDir == "" {
		return config, secret, httpx.NewFieldError("localDir", "invalid_local_dir", "a storage directory is required")
	}
	if config.PublicURL != "" {
		origin, err := httpOrigin(config.PublicURL)
		if err != nil {
			return config, secret, httpx.NewFieldError("publicUrl", "invalid_public_url",
				"the public address must be an http(s) origin with no path, like https://chat.example.com")
		}
		config.PublicURL = origin
	}
	if config.S3.Endpoint != "" {
		if err := httpURL(config.S3.Endpoint); err != nil {
			return config, secret, httpx.NewFieldError("s3.endpoint", "invalid_endpoint", "the endpoint must be an http(s) URL")
		}
		config.S3.Endpoint = strings.TrimRight(config.S3.Endpoint, "/")
	}
	if config.S3.Region == "" {
		config.S3.Region = defaultStorageConfig().S3.Region
	}
	if config.Driver == storage.DriverS3 && config.S3.Bucket == "" {
		return config, secret, httpx.NewFieldError("s3.bucket", "bucket_required", "a bucket is required for S3 storage")
	}
	return config, secret, nil
}

// httpURL accepts an http(s) URL with a host.
func httpURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("not an http(s) URL")
	}
	return nil
}

// httpOrigin is raw as scheme://host[:port], or an error when it has anything
// after the host: a path, a query, a fragment or credentials.
func httpOrigin(raw string) (string, error) {
	if err := httpURL(raw); err != nil {
		return "", err
	}
	u, _ := url.Parse(raw)
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("not an origin")
	}
	return u.Scheme + "://" + u.Host, nil
}

func (s *Server) UpdateStorageSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateStorageSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if s.storageSwitch == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotImplemented, "storage_not_configurable", "this server's storage cannot be changed at runtime"))
		return
	}
	ctx := r.Context()

	// Read, apply and save as one step: two admins saving at once would
	// otherwise each apply their own settings and store the other's.
	s.storageMu.Lock()
	defer s.storageMu.Unlock()

	config, secret, err := loadStorage(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	previous, previousSecret := config, secret
	config, secret, invalid := mergeStorage(config, secret, body)
	if invalid != nil {
		httpx.Fail(w, r, invalid)
		return
	}

	// Before saving, so settings that cannot work are never stored.
	if err := s.storageSwitch.Apply(ctx, config.settings(secret)); err != nil {
		field := ""
		if config.Driver == storage.DriverLocal {
			field = "localDir"
		}
		failure := httpx.NewError(http.StatusBadRequest, "storage_unavailable", "cannot use that storage: "+strings.ReplaceAll(err.Error(), "storage: ", ""))
		failure.Field = field
		httpx.Fail(w, r, failure)
		return
	}
	if err := s.saveStorage(ctx, config, secret, body.S3 != nil && body.S3.SecretAccessKey != nil); err != nil {
		// Applied but not saved would revert at the next restart; put the
		// running drivers back instead.
		if restoreErr := s.storageSwitch.Apply(context.WithoutCancel(ctx), previous.settings(previousSecret)); restoreErr != nil {
			slog.ErrorContext(ctx, "could not restore the previous file storage", "error", restoreErr)
		}
		httpx.Fail(w, r, err)
		return
	}
	// Never the secret, not even at debug level.
	slog.InfoContext(ctx, "file storage settings changed",
		"driver", config.Driver, "dir", config.LocalDir, "bucket", config.S3.Bucket,
		"secret_changed", body.S3 != nil && body.S3.SecretAccessKey != nil, "actor", auth.MustUser(ctx).Email)

	httpx.JSON(w, http.StatusOK, toStorageSettings(config, secret))
}

func (s *Server) saveStorage(ctx context.Context, config storageConfig, secret string, secretChanged bool) error {
	value, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingStorage, Value: value}); err != nil {
		return err
	}
	if secretChanged {
		raw, _ := json.Marshal(secret)
		return s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingStorageS3Secret, Value: raw})
	}
	return nil
}

func (s *Server) TestStorage(w http.ResponseWriter, r *http.Request) {
	var body UpdateStorageSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if s.storageSwitch == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotImplemented, "storage_not_configurable", "this server's storage cannot be changed at runtime"))
		return
	}
	config, secret, err := loadStorage(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	failed := func(message string, started time.Time) {
		httpx.JSON(w, http.StatusOK, StorageTestResult{Error: &message, LatencyMs: time.Since(started).Milliseconds()})
	}
	started := time.Now()
	config, secret, invalid := mergeStorage(config, secret, body)
	if invalid != nil {
		failed(invalid.Message, started)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	started = time.Now()
	if err := s.storageSwitch.Probe(ctx, config.settings(secret)); err != nil {
		failed(err.Error(), started)
		return
	}
	httpx.JSON(w, http.StatusOK, StorageTestResult{Ok: true, LatencyMs: time.Since(started).Milliseconds()})
}
