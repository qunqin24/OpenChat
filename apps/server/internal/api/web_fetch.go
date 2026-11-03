package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// Web page reading (后台 → 联网搜索 → 网页读取): whether web_fetch reads pages
// itself, asks Jina Reader when a site turns it away, or asks Jina first
// (tools.ReaderMode), and Jina's key. The key is kept apart from the rest and
// never sent back, as the search key is.
const (
	settingWebFetch    = "web_fetch"
	settingWebFetchKey = "web_fetch_jina_key"
)

type webFetchConfig struct {
	Reader string `json:"reader"`
}

func loadWebFetch(ctx context.Context, q *sqlc.Queries) (webFetchConfig, string, error) {
	config := webFetchConfig{Reader: string(tools.ReaderFallback)}
	raw, err := q.GetSetting(ctx, settingWebFetch)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &config); err != nil {
			return config, "", err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return config, "", err
	}
	if !tools.ReaderMode(config.Reader).Valid() {
		config.Reader = string(tools.ReaderFallback)
	}
	key, err := stringSetting(ctx, q, settingWebFetchKey)
	return config, key, err
}

// tool is web_fetch as these settings make it.
func (c webFetchConfig) tool(key string, blocklist tools.Blocklist) tools.WebFetch {
	return tools.WebFetch{Blocklist: blocklist, Mode: tools.ReaderMode(c.Reader), ReaderKey: key}
}

func toWebFetchSettings(c webFetchConfig, key string) WebFetchSettings {
	out := WebFetchSettings{Reader: WebFetchReader(c.Reader), JinaKeySet: key != ""}
	if preview := keyPreview(key); preview != "" {
		out.JinaKeyPreview = &preview
	}
	return out
}

func (s *Server) GetWebFetchSettings(w http.ResponseWriter, r *http.Request) {
	config, key, err := loadWebFetch(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toWebFetchSettings(config, key))
}

func (s *Server) UpdateWebFetchSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateWebFetchSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, _, err := loadWebFetch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Reader != nil {
		if !tools.ReaderMode(*body.Reader).Valid() {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_reader", "unknown reading mode"))
			return
		}
		config.Reader = string(*body.Reader)
	}
	value, _ := json.Marshal(config)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingWebFetch, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.JinaKey != nil {
		key, _ := json.Marshal(strings.TrimSpace(*body.JinaKey))
		if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingWebFetchKey, Value: key}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	// Never the key itself.
	slog.InfoContext(ctx, "web fetch settings changed",
		"reader", config.Reader, "key_changed", body.JinaKey != nil, "actor", auth.MustUser(ctx).Email)

	config, key, err := loadWebFetch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toWebFetchSettings(config, key))
}

// fetchPreviewChars is how much of a tested page's text is shown.
const fetchPreviewChars = 400

func (s *Server) TestWebFetch(w http.ResponseWriter, r *http.Request) {
	var body WebFetchTestRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, key, err := loadWebFetch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	search, _, err := loadWebSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	started := time.Now()
	read, err := config.tool(key, search.searchConfig().Blocklist).ReadPage(ctx, body.Url)
	out := WebFetchTestResult{LatencyMs: time.Since(started).Milliseconds()}
	if err != nil {
		message := err.Error()
		var toolErr *agent.ToolError
		if errors.As(err, &toolErr) {
			message = toolErr.Message
		}
		out.Error = &message
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	out.Ok = true
	via := WebFetchTestResultVia(read.Via)
	chars := utf8.RuneCountInString(read.Text)
	preview := []rune(strings.TrimSpace(read.Text))
	if len(preview) > fetchPreviewChars {
		preview = preview[:fetchPreviewChars]
	}
	text := string(preview)
	out.Via, out.Chars, out.Preview = &via, &chars, &text
	if read.Title != "" {
		out.Title = &read.Title
	}
	httpx.JSON(w, http.StatusOK, out)
}
