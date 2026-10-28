package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decodes WebP uploads; image/jpeg and image/png come with the encoder below

	// Registered for image.Decode.
	_ "image/jpeg"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/storage"
)

// Account pictures.
//
// A picture is uploaded as the image itself (UpdateMyAvatar), decoded, cut to
// the square at its centre, scaled down and stored again as a PNG. Storing
// what was made here rather than what was sent is the point: whatever else the
// file carried (metadata, a second payload after the image's end) never leaves
// the request, and every picture in storage has one known type and size.
//
// The object lives in the blob store under avatars/<user>/<id>.png and the
// users row keeps the key. It is read through the same signed read URLs as an
// attachment, so it works with either storage driver and needs no endpoint of
// its own.

const (
	// avatarMaxBytes is the largest upload taken. The browser sends a 256 px
	// PNG, a few tens of kilobytes; the room is for other clients.
	avatarMaxBytes = 1 << 20
	// avatarMaxSide bounds what is decoded, so a small file that expands into
	// a huge bitmap cannot be used to make the server allocate it.
	avatarMaxSide = 4096
	// avatarSide is the stored picture's side, or less for a smaller original.
	avatarSide = 256
	// avatarURLTTL is how long the read URL handed out stays valid.
	avatarURLTTL = time.Hour
	// avatarPrefix is where pictures are kept in the blob store, and what the
	// signed-URL handler recognises them by.
	avatarPrefix = "avatars/"
)

// avatarTypes are what may be uploaded.
var avatarTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

var (
	errAvatarType = httpx.NewError(http.StatusUnsupportedMediaType, "unsupported_file_type",
		"the picture must be a PNG, JPEG or WebP image")
	errAvatarTooLarge = httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large",
		fmt.Sprintf("the picture can be at most %d MiB", avatarMaxBytes>>20))
	errAvatarUnreadable = httpx.NewError(http.StatusBadRequest, "invalid_image", "the picture could not be read")
	errAvatarUploadsOff = httpx.NewError(http.StatusForbidden, "avatar_uploads_off",
		"uploading a picture is turned off on this server")
)

// UpdateMyAvatar replaces the caller's picture.
func (s *Server) UpdateMyAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	allowed, err := boolSettingOr(ctx, s.q, settingAvatarUploads, false)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !allowed {
		httpx.Fail(w, r, errAvatarUploadsOff)
		return
	}

	declared, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !avatarTypes[declared] {
		httpx.Fail(w, r, errAvatarType)
		return
	}
	if r.ContentLength > avatarMaxBytes {
		httpx.Fail(w, r, errAvatarTooLarge)
		return
	}
	// Without a declared length the limit still holds: the body is cut off.
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, avatarMaxBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		httpx.Fail(w, r, errAvatarTooLarge)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The bytes must be the image the header says they are.
	if len(raw) == 0 || http.DetectContentType(raw) != declared {
		httpx.Fail(w, r, errAvatarType)
		return
	}
	picture, err := squareAvatar(raw)
	if err != nil {
		httpx.Fail(w, r, errAvatarUnreadable)
		return
	}

	key := fmt.Sprintf("%s%s/%s.png", avatarPrefix, user.ID, uuid.New())
	if err := s.putObject(ctx, key, "image/png", picture); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	updated, err := s.q.SetUserAvatar(ctx, sqlc.SetUserAvatarParams{ID: user.ID, AvatarKey: &key})
	if err != nil {
		// Written, but nobody points at it.
		s.forget(ctx, key)
		httpx.Fail(w, r, dbErr(err))
		return
	}
	if user.AvatarKey != nil {
		s.forget(ctx, *user.AvatarKey)
	}
	httpx.JSON(w, http.StatusOK, s.sessionUser(ctx, updated))
}

// DeleteMyAvatar goes back to the initial.
func (s *Server) DeleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	updated, err := s.q.SetUserAvatar(ctx, sqlc.SetUserAvatarParams{ID: user.ID, AvatarKey: nil})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	if user.AvatarKey != nil {
		s.forget(ctx, *user.AvatarKey)
	}
	httpx.JSON(w, http.StatusOK, s.sessionUser(ctx, updated))
}

// squareAvatar is the picture as it is stored: the original's centre square,
// at most avatarSide pixels across, as PNG.
func squareAvatar(raw []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > avatarMaxSide || cfg.Height > avatarMaxSide {
		return nil, errors.New("image dimensions out of range")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	crop.Max = image.Pt(crop.Min.X+side, crop.Min.Y+side)

	out := min(side, avatarSide)
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// putObject writes a small object from the server itself. The local driver
// takes it directly; a bucket takes the presigned PUT a browser would have
// used, so it works with whatever the storage settings say without a write
// method on the store's interface.
func (s *Server) putObject(ctx context.Context, key, contentType string, data []byte) error {
	if s.storageSwitch != nil && s.storageSwitch.Driver() == storage.DriverS3 {
		url, err := s.store.PresignPut(ctx, key, contentType, uploadTTL)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", contentType)
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return fmt.Errorf("store the picture: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("store the picture: the bucket answered %s", resp.Status)
		}
		return nil
	}
	local := s.local()
	if local == nil {
		return errors.New("no storage is configured")
	}
	_, err := local.Put(key, bytes.NewReader(data), int64(len(data)))
	return err
}

// forget removes a picture that nothing points at any more. Best effort: a
// failure leaves a few kilobytes behind, which is not worth failing a request.
func (s *Server) forget(ctx context.Context, key string) {
	remover, ok := s.store.(interface {
		Delete(ctx context.Context, key string) error
	})
	if !ok {
		return
	}
	// Not the request's context: the picture is gone from the account whether
	// or not the client waited for this.
	if err := remover.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.WarnContext(ctx, "remove a replaced picture", "key", key, "error", err)
	}
}

// avatarCache keeps the read URL issued for a picture while it still has most
// of its life left. A URL carries its expiry, so signing afresh on every call
// would give a different address each time, and a browser that is handed a
// new one for the same picture on every session refresh fetches it again.
type avatarCache struct {
	mu      sync.Mutex
	entries map[string]signedURL
}

type signedURL struct {
	url   string
	until time.Time
}

// avatarCacheMax bounds the cache; past it the expired entries are dropped.
const avatarCacheMax = 4096

// avatarURL is the picture's read URL, or nil for an account without one (or
// when the store cannot sign: the initial is drawn instead).
func (s *Server) avatarURL(ctx context.Context, key *string) *AvatarUrl {
	if key == nil || *key == "" || s.store == nil {
		return nil
	}
	now := time.Now()
	s.avatars.mu.Lock()
	defer s.avatars.mu.Unlock()
	if hit, ok := s.avatars.entries[*key]; ok && hit.until.Sub(now) > avatarURLTTL/2 {
		return &hit.url
	}
	url, err := s.store.PresignGet(ctx, *key, avatarURLTTL)
	if err != nil {
		slog.WarnContext(ctx, "sign a picture's URL", "key", *key, "error", err)
		return nil
	}
	if s.avatars.entries == nil {
		s.avatars.entries = map[string]signedURL{}
	}
	if len(s.avatars.entries) >= avatarCacheMax {
		for k, v := range s.avatars.entries {
			if v.until.Before(now) {
				delete(s.avatars.entries, k)
			}
		}
		if len(s.avatars.entries) >= avatarCacheMax {
			clear(s.avatars.entries)
		}
	}
	s.avatars.entries[*key] = signedURL{url: url, until: now.Add(avatarURLTTL)}
	return &url
}

// avatarOwner is the account a stored picture belongs to, for the signed-URL
// handlers: what they know about a key under avatars/ comes from the users
// table, not from an attachment row.
func (s *Server) avatarOwner(ctx context.Context, key string) (sqlc.User, error) {
	if !strings.HasPrefix(key, avatarPrefix) {
		return sqlc.User{}, httpx.ErrNotFound
	}
	row, err := s.q.GetUserByAvatarKey(ctx, &key)
	return row, dbErr(err)
}
