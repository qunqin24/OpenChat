package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/imagesearch"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// Pictures an answer describes. With image search on, an answer's interface
// components describe a picture in a few words ("laptop keyboard") rather than
// give its address, and the server finds one - a freely licensed photo from
// Wikimedia Commons, Openverse or Pixabay, or any site's through Bing Images,
// asked in the order the settings give, the next when one is down or finds
// nothing - the way ChatGPT's answers get theirs. With it off, an answer shows only pictures its
// own sources listed, through the proxy in images.go.
//
// What a description found is kept (image_searches), so the same words show
// the same picture on every reload and for every reader, and a service, which
// limits this server, is asked once per description. The picture itself is
// downloaded once, through the guarded client (public addresses only), and
// kept in the file store under pictures/ - Pixabay asks for that rather than
// links to it - so the reader's browser asks no third party, and a picture
// does not vanish with the page it came from.

const (
	// settingImageSearch holds whether answers describe their pictures and
	// which service finds them, as JSON.
	settingImageSearch = "image_search"
	// settingPixabayKey holds the Pixabay API key the pixabay service needs.
	// Write-only through the API, like the web search key.
	settingPixabayKey = "image_search_pixabay_key"
	// imageQueryMax is the longest description searched; longer is cut.
	imageQueryMax = 200
	// imageMissAge is how long a search that found nothing stands before the
	// words are searched again.
	imageMissAge = 24 * time.Hour
)

// imageSearchConfig is the stored form of the settings.
type imageSearchConfig struct {
	Enabled bool `json:"enabled"`
	// Providers are the services asked, in order.
	Providers []string `json:"providers"`
	// Provider is the one service settings saved before v1.2.1 named.
	Provider string `json:"provider,omitempty"`
}

// loadImageSearch is the settings and the stored Pixabay key. Off, with
// Wikimedia Commons chosen, until an administrator turns it on.
func loadImageSearch(ctx context.Context, q *sqlc.Queries) (imageSearchConfig, string, error) {
	config := imageSearchConfig{}
	values, err := q.GetSettings(ctx, []string{settingImageSearch, settingPixabayKey})
	if err != nil {
		return config, "", err
	}
	var key string
	for _, row := range values {
		switch row.Key {
		case settingImageSearch:
			_ = json.Unmarshal(row.Value, &config)
		case settingPixabayKey:
			_ = json.Unmarshal(row.Value, &key)
		}
	}
	if len(config.Providers) == 0 && config.Provider != "" {
		config.Providers = []string{config.Provider}
	}
	config.Provider = ""
	config.Providers = validProviders(config.Providers)
	if len(config.Providers) == 0 {
		config.Providers = []string{string(Wikimedia)}
	}
	return config, strings.TrimSpace(key), nil
}

// validProviders is the names that are services, each once, in their order.
func validProviders(names []string) []string {
	valid := make([]string, 0, len(names))
	for _, name := range names {
		if ImageSearchProvider(name).Valid() && !slices.Contains(valid, name) {
			valid = append(valid, name)
		}
	}
	return valid
}

// imageSearchOn is whether answers describe their pictures: on, with a
// service that can search.
func imageSearchOn(ctx context.Context, q *sqlc.Queries) bool {
	config, key, err := loadImageSearch(ctx, q)
	return err == nil && config.Enabled && configuredSearcher(config, key) != nil
}

// configuredSearcher asks the configured services that can search, in order
// (pictureFallback); nil when none can.
func configuredSearcher(config imageSearchConfig, key string) imagesearch.Searcher {
	var searchers []imagesearch.Searcher
	for _, provider := range config.Providers {
		if searcher := pictureSearcher(provider, key); searcher != nil {
			searchers = append(searchers, searcher)
		}
	}
	if len(searchers) == 0 {
		return nil
	}
	return pictureFallback.Of(searchers...)
}

var (
	pictureClient   = tools.GuardedClient(imageTimeout)
	wikimediaSearch = imagesearch.NewWikimedia(pictureClient)
	openverseSearch = imagesearch.NewOpenverse(pictureClient)
	bingSearch      = imagesearch.NewBing(pictureClient)
	// pixabaySearch is the search with the stored key, kept while the key is
	// the same so its count of the minute's searches carries on.
	pixabayMu     sync.Mutex
	pixabaySearch *imagesearch.Pixabay
	// pictureFallback remembers, across searches, which services failed lately.
	pictureFallback = &imagesearch.Fallback{}
	// imageSearches makes one search of words several readers ask for at once.
	imageSearches singleflight.Group
	// userImageSearches bounds how often one account may ask, so nobody can
	// spend the server's share of a service on words no answer holds.
	userImageSearches = &imageLimiter{seen: map[string]int{}}
)

// imageLimiter keeps one client from using a picture endpoint as a free
// fetcher: a page of answers shows a handful of pictures, so a burst beyond
// this is not reading. In memory and per process - it is a guard against
// abuse, not an accounting.
type imageLimiter struct {
	mu     sync.Mutex
	window time.Time
	seen   map[string]int
}

const (
	imagesPerMinute = 120
	// imageClients bounds the table; a full one starts over.
	imageClients = 10_000
)

func (l *imageLimiter) allow(client string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.window) >= time.Minute || len(l.seen) >= imageClients {
		l.window, l.seen = now, map[string]int{}
	}
	l.seen[client]++
	return l.seen[client] <= imagesPerMinute
}

// pictureSearcher is the service by name; nil for Pixabay without a key.
func pictureSearcher(provider, key string) imagesearch.Searcher {
	switch ImageSearchProvider(provider) {
	case Openverse:
		return openverseSearch
	case Bing:
		return bingSearch
	case Pixabay:
		if key == "" {
			return nil
		}
		pixabayMu.Lock()
		defer pixabayMu.Unlock()
		if pixabaySearch == nil || pixabaySearch.Key != key {
			pixabaySearch = imagesearch.NewPixabay(key, pictureClient)
		}
		return pixabaySearch
	default:
		return wikimediaSearch
	}
}

// SearchImage finds a picture for a description in an answer.
func (s *Server) SearchImage(w http.ResponseWriter, r *http.Request, params SearchImageParams) {
	user, _ := auth.UserFrom(r.Context())
	if !userImageSearches.allow(user.ID.String(), time.Now()) {
		httpx.Fail(w, r, httpx.NewError(http.StatusTooManyRequests, "image_rate_limited", "too many pictures asked for"))
		return
	}
	s.writeFoundImage(w, r, params.Q)
}

// GetPicture serves a picture a search here found.
func (s *Server) GetPicture(w http.ResponseWriter, r *http.Request, pictureId uuid.UUID) {
	s.servePicture(w, r, pictureId)
}

func (s *Server) writeFoundImage(w http.ResponseWriter, r *http.Request, raw string) {
	picture, err := s.findImage(r.Context(), raw)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The words keep their picture, so the browser may keep the answer too.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	httpx.JSON(w, http.StatusOK, FoundImage{
		Id:         picture.ID,
		Width:      int(picture.Width),
		Height:     int(picture.Height),
		Title:      picture.Title,
		Creator:    nonEmpty(picture.Creator),
		CreatorUrl: httpOnly(picture.CreatorUrl),
		License:    picture.License,
		LicenseUrl: httpOnly(picture.LicenseUrl),
		LandingUrl: httpOnly(picture.LandingUrl),
		Source:     picture.Source,
	})
}

// findImage is the picture for a description: the one found before, or a new
// search. 404 image_not_found when nothing matches or image search is off,
// 503 image_search_unavailable when the service cannot be asked now (nothing
// is kept then, so the words are searched again next time).
func (s *Server) findImage(ctx context.Context, raw string) (sqlc.Picture, error) {
	query := imageQuery(raw)
	if query == "" {
		return sqlc.Picture{}, httpx.NewFieldError("q", "invalid_query", "describe the picture in a few words")
	}
	notFound := httpx.NewError(http.StatusNotFound, "image_not_found", "no picture matches the description")
	searcher := s.imageSearch
	if searcher == nil {
		config, key, err := loadImageSearch(ctx, s.q)
		if err != nil {
			return sqlc.Picture{}, err
		}
		if !config.Enabled {
			return sqlc.Picture{}, httpx.NewError(http.StatusNotFound, "image_not_found", "image search is off")
		}
		if searcher = configuredSearcher(config, key); searcher == nil {
			return sqlc.Picture{}, httpx.NewError(http.StatusServiceUnavailable, "image_search_unavailable", "pictures cannot be searched for now")
		}
	}
	search, err := s.q.GetImageSearch(ctx, query)
	switch {
	case err == nil && search.PictureID != nil:
		return s.q.GetPicture(ctx, *search.PictureID)
	case err == nil && time.Since(search.SearchedAt.Time) < imageMissAge:
		return sqlc.Picture{}, notFound
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return sqlc.Picture{}, err
	}
	kept, err, _ := imageSearches.Do(query, func() (any, error) {
		// Whoever asked first may leave; the picture is for everyone.
		ctx := context.WithoutCancel(ctx)
		found, err := searcher.Search(ctx, query)
		if err != nil {
			return nil, err
		}
		var picture *sqlc.Picture
		var id *uuid.UUID
		if found != nil {
			kept, err := s.keepPicture(ctx, found)
			if err != nil {
				return nil, err
			}
			picture, id = &kept, &kept.ID
		}
		if err := s.q.SaveImageSearch(ctx, sqlc.SaveImageSearchParams{Query: query, PictureID: id}); err != nil {
			return nil, err
		}
		return picture, nil
	})
	if err != nil {
		slog.WarnContext(ctx, "image search", "query", query, "error", err)
		return sqlc.Picture{}, httpx.NewError(http.StatusServiceUnavailable, "image_search_unavailable", "pictures cannot be searched for now")
	}
	picture, _ := kept.(*sqlc.Picture)
	if picture == nil {
		return sqlc.Picture{}, notFound
	}
	return *picture, nil
}

// keepPicture is a found picture in the file store: the one kept before, or a
// new download of it (its kept size, else the service's other copy).
func (s *Server) keepPicture(ctx context.Context, found *imagesearch.Image) (sqlc.Picture, error) {
	picture, err := s.q.GetPictureAt(ctx, sqlc.GetPictureAtParams{Provider: found.Provider, ProviderID: found.ID})
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return picture, err
	}
	if s.store == nil {
		return picture, errors.New("no file store")
	}
	var entry imageEntry
	for _, address := range []string{found.URL, found.Fallback} {
		if address != "" {
			if entry = images.fetch(ctx, address); entry.body != nil {
				break
			}
		}
	}
	if entry.body == nil {
		return picture, errors.New("the picture could not be downloaded")
	}
	key := "pictures/" + uuid.NewString()
	if err := s.store.PutObject(ctx, key, entry.contentType, entry.body); err != nil {
		return picture, err
	}
	picture, err = s.q.InsertPicture(ctx, sqlc.InsertPictureParams{
		Provider: found.Provider, ProviderID: found.ID, StorageKey: key, ContentType: entry.contentType,
		Width: int32(found.Width), Height: int32(found.Height), Title: found.Title, Creator: found.Creator,
		CreatorUrl: found.CreatorURL, License: found.License, LicenseUrl: found.LicenseURL,
		LandingUrl: found.Landing, Source: found.Source,
	})
	// Another search kept the same picture first: its copy stands, this one goes.
	if err != nil || picture.StorageKey != key {
		if derr := s.store.Delete(ctx, key); derr != nil {
			slog.WarnContext(ctx, "remove a picture kept twice", "key", key, "error", derr)
		}
	}
	return picture, err
}

// imageQuery is a description as it is searched and kept: lower case, single
// spaces, at most imageQueryMax characters.
func imageQuery(raw string) string {
	query := strings.Join(strings.Fields(strings.ToLower(raw)), " ")
	if runes := []rune(query); len(runes) > imageQueryMax {
		query = strings.TrimSpace(string(runes[:imageQueryMax]))
	}
	return query
}

// servePicture writes a picture a search here found, from the file store, and
// nothing else.
func (s *Server) servePicture(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	ctx := r.Context()
	gone := httpx.NewError(http.StatusNotFound, "image_not_found", "no search here found that picture")
	picture, err := s.q.GetPicture(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && s.store == nil) {
		httpx.Fail(w, r, gone)
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	body, err := s.store.Read(ctx, picture.StorageKey, imageMaxBytes)
	if err != nil {
		slog.WarnContext(ctx, "read a picture", "key", picture.StorageKey, "error", err)
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "image_unavailable", "the picture could not be read"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", picture.ContentType)
	// A picture's id is never reused for other bytes.
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(body)
}

// httpOnly is an address to link to, or nil for one that is not http(s).
func httpOnly(address string) *string {
	if !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
		return nil
	}
	return &address
}

// ------------------------------------------------------------------ settings

func toImageSearchSettings(c imageSearchConfig, key string) ImageSearchSettings {
	providers := make([]ImageSearchProvider, len(c.Providers))
	for i, provider := range c.Providers {
		providers[i] = ImageSearchProvider(provider)
	}
	out := ImageSearchSettings{Enabled: c.Enabled, Providers: providers, PixabayKeySet: key != ""}
	if preview := keyPreview(key); preview != "" {
		out.PixabayKeyPreview = &preview
	}
	return out
}

func (s *Server) GetImageSearchSettings(w http.ResponseWriter, r *http.Request) {
	config, key, err := loadImageSearch(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toImageSearchSettings(config, key))
}

func (s *Server) UpdateImageSearchSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateImageSearchSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, _, err := loadImageSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled != nil {
		config.Enabled = *body.Enabled
	}
	if body.Providers != nil {
		names := make([]string, len(*body.Providers))
		for i, provider := range *body.Providers {
			names[i] = string(provider)
		}
		if len(names) == 0 || len(validProviders(names)) != len(names) {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_provider", "name each image search service once"))
			return
		}
		config.Providers = names
	}
	value, _ := json.Marshal(config)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingImageSearch, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.PixabayKey != nil {
		key, _ := json.Marshal(strings.TrimSpace(*body.PixabayKey))
		if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingPixabayKey, Value: key}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	// Never the key itself, not even at debug level.
	slog.InfoContext(ctx, "image search settings changed", "enabled", config.Enabled, "providers", config.Providers,
		"key_changed", body.PixabayKey != nil, "actor", auth.MustUser(ctx).Email)
	config, key, err := loadImageSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toImageSearchSettings(config, key))
}

// TestImageSearch searches once, the service and key given or the configured
// services in their order, keeping nothing.
func (s *Server) TestImageSearch(w http.ResponseWriter, r *http.Request) {
	var body ImageSearchTestRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, key, err := loadImageSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Provider != nil && body.Provider.Valid() {
		config.Providers = []string{string(*body.Provider)}
	}
	if body.PixabayKey != nil && strings.TrimSpace(*body.PixabayKey) != "" {
		key = strings.TrimSpace(*body.PixabayKey)
	}
	searcher := s.imageSearch
	if searcher == nil {
		searcher = configuredSearcher(config, key)
	}
	result := ImageSearchTestResult{}
	if searcher == nil {
		message := "Pixabay needs an API key"
		result.Error = &message
		httpx.JSON(w, http.StatusOK, result)
		return
	}
	started := time.Now()
	found, err := searcher.Search(ctx, imageQuery(body.Query))
	result.LatencyMs = int(time.Since(started).Milliseconds())
	switch {
	case err != nil:
		message := err.Error()
		result.Error = &message
	case found != nil:
		result.Ok = true
		result.Image = &ImageSearchHit{
			Url: found.URL, Width: found.Width, Height: found.Height, Title: found.Title, Creator: nonEmpty(found.Creator),
			License: found.License, LandingUrl: httpOnly(found.Landing), Source: found.Source,
			Provider: ImageSearchProvider(found.Provider),
		}
	default:
		result.Ok = true
	}
	httpx.JSON(w, http.StatusOK, result)
}
