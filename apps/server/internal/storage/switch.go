package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Drivers.
const (
	DriverLocal = "local"
	DriverS3    = "s3"
)

// Settings is everything a Switch needs to choose and build its drivers. The
// signing key is not in it: that is the Switch's own, fixed at creation.
type Settings struct {
	// Driver is where new uploads go: DriverLocal or DriverS3.
	Driver string
	// LocalDir is the local driver's directory, created when missing.
	LocalDir string
	// PublicURL is the origin the local driver's URLs are issued under. Empty
	// issues them relative, which is right whenever the web app and the API
	// share an origin.
	PublicURL string
	// S3 is the bucket. With a bucket set, files stored there stay readable
	// even while the driver is local.
	S3 S3Settings
}

// DefaultSettings is where attachments go until somebody says otherwise: the
// local disk, so a fresh checkout stores them without a MinIO beside it.
func DefaultSettings() Settings {
	return Settings{
		Driver:   DriverLocal,
		LocalDir: "./data",
		S3:       S3Settings{Region: "us-east-1", PathStyle: true},
	}
}

// Switch is a Backend whose drivers can be replaced while the server runs. New
// uploads go to the current driver; reads go to wherever the file actually is,
// so changing the driver strands nothing that is still in the old place.
//
// It always holds a Local, and an S3 store whenever a bucket is configured,
// whichever driver is current. That is what lets files written under the
// previous choice stay readable after the switch.
type Switch struct {
	secret []byte

	// Replaceable in tests; the real constructors otherwise.
	newLocal func(dir string, secret []byte, publicURL string) (*Local, error)
	newS3    func(ctx context.Context, cfg S3Settings) (Backend, error)

	mu       sync.RWMutex
	driver   string
	local    *Local
	s3       Backend
	settings Settings
}

// NewSwitch returns a Switch with nothing configured. Apply must succeed
// before it is used. secret signs the local driver's URLs.
func NewSwitch(secret []byte) *Switch {
	return &Switch{
		secret:   secret,
		newLocal: NewLocal,
		newS3: func(ctx context.Context, cfg S3Settings) (Backend, error) {
			store, err := New(ctx, cfg)
			if err != nil {
				return nil, err
			}
			return store, nil
		},
	}
}

// Apply builds the drivers settings describe and swaps them in. They are all
// built before any is swapped, so a failure leaves the Switch exactly as it
// was. Nothing here contacts a bucket; Probe is the check that does.
func (sw *Switch) Apply(ctx context.Context, settings Settings) error {
	local, s3, err := sw.build(ctx, settings)
	if err != nil {
		return err
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()
	// The old Local is dropped, not closed: a request may be mid-read on it, and
	// its directory handle is released when the last reference goes.
	sw.driver, sw.local, sw.s3, sw.settings = settings.Driver, local, s3, settings
	return nil
}

// build makes the drivers for settings, reusing the current Local when nothing
// about it changed.
func (sw *Switch) build(ctx context.Context, settings Settings) (*Local, Backend, error) {
	switch settings.Driver {
	case DriverLocal, DriverS3:
	default:
		return nil, nil, fmt.Errorf("storage: unknown driver %q", settings.Driver)
	}
	if settings.Driver == DriverS3 && settings.S3.Bucket == "" {
		return nil, nil, errors.New("storage: the S3 driver needs a bucket")
	}

	sw.mu.RLock()
	current, previous := sw.local, sw.settings
	sw.mu.RUnlock()

	local := current
	if local == nil || previous.LocalDir != settings.LocalDir || previous.PublicURL != settings.PublicURL {
		var err error
		if local, err = sw.newLocal(settings.LocalDir, sw.secret, settings.PublicURL); err != nil {
			return nil, nil, err
		}
	}

	var s3 Backend
	if settings.S3.Bucket != "" {
		var err error
		if s3, err = sw.newS3(ctx, settings.S3); err != nil {
			return nil, nil, err
		}
	}
	return local, s3, nil
}

// Probe checks one driver against settings without applying them: it writes a
// small object under probes/, reads it back and deletes it. Only the chosen
// driver is probed.
func (sw *Switch) Probe(ctx context.Context, settings Settings) error {
	if settings.Driver == DriverS3 {
		if settings.S3.Bucket == "" {
			return errors.New("a bucket is required")
		}
		store, err := New(ctx, settings.S3)
		if err != nil {
			return err
		}
		return store.Probe(ctx)
	}
	// Not the live Local: its directory may not be the one being tried.
	local, err := sw.newLocal(settings.LocalDir, sw.secret, settings.PublicURL)
	if err != nil {
		return fmt.Errorf("cannot use the directory: %s", describeFileError(err))
	}
	defer local.Close()
	return local.Probe(ctx)
}

// Local is the current local driver, whichever driver is current: it is what
// serves the signed URLs, and files written there stay readable. Nil before the
// first Apply.
func (sw *Switch) Local() *Local {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return sw.local
}

// Driver is where new uploads go: DriverLocal or DriverS3.
func (sw *Switch) Driver() string {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	return sw.driver
}

// current is the driver new uploads go to.
func (sw *Switch) current() Backend {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	if sw.driver == DriverS3 && sw.s3 != nil {
		return sw.s3
	}
	return sw.local
}

// holding is the store a stored file is in: the local disk if it is there, else
// the bucket if there is one, else the local disk, which then reports the file
// missing.
func (sw *Switch) holding(key string) Backend {
	sw.mu.RLock()
	local, s3 := sw.local, sw.s3
	sw.mu.RUnlock()
	if s3 == nil || local.Has(key) {
		return local
	}
	return s3
}

func (sw *Switch) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	return sw.current().PresignPut(ctx, key, contentType, ttl)
}

func (sw *Switch) ObjectURL(key string) string { return sw.current().ObjectURL(key) }

func (sw *Switch) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return sw.holding(key).PresignGet(ctx, key, ttl)
}

// PutObject stores the bytes with the driver that is current, like an upload.
func (sw *Switch) PutObject(ctx context.Context, key, contentType string, data []byte) error {
	return sw.current().PutObject(ctx, key, contentType, data)
}

func (sw *Switch) Size(ctx context.Context, key string) (int64, error) {
	return sw.holding(key).Size(ctx, key)
}

func (sw *Switch) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	return sw.holding(key).Read(ctx, key, limit)
}

// Delete removes the object from every store that might hold it. Reads look in
// the disk and then the bucket, so a file can be in either (or, after a switch
// back and forth, both); deleting from only the current driver would strand
// the other copy. Both are tried even if one fails, and the errors are joined.
func (sw *Switch) Delete(ctx context.Context, key string) error {
	sw.mu.RLock()
	local, s3 := sw.local, sw.s3
	sw.mu.RUnlock()

	var errs []error
	if local != nil {
		errs = append(errs, local.Delete(ctx, key))
	}
	if s3 != nil {
		errs = append(errs, s3.Delete(ctx, key))
	}
	return errors.Join(errs...)
}
