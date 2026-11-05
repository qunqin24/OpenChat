package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/browser"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// The browser (后台 → 浏览器, docs/design/browser-plan.md): where the headless
// Chromium is, the proxy every page goes through, and how many sessions may
// be open. Administrators only, for now. The tool is browser_tool.go.
const settingBrowser = "browser"

type browserConfig struct {
	Enabled      bool     `json:"enabled"`
	Endpoint     string   `json:"endpoint"`
	ProxyListen  string   `json:"proxyListen"`
	ProxyURL     string   `json:"proxyUrl"`
	ProxySources []string `json:"proxySources"`
	MaxSessions  int      `json:"maxSessions"`
}

// The defaults are deploy/docker-compose.browser.yml's.
func defaultBrowserConfig() browserConfig {
	return browserConfig{
		Endpoint:     "http://browserd:9222",
		ProxyListen:  ":8188",
		ProxyURL:     "http://openchat:8188",
		ProxySources: []string{},
		MaxSessions:  8,
	}
}

func loadBrowser(ctx context.Context, q *sqlc.Queries) (browserConfig, error) {
	config := defaultBrowserConfig()
	raw, err := q.GetSetting(ctx, settingBrowser)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return config, nil
	case err != nil:
		return config, err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return defaultBrowserConfig(), err
	}
	if config.ProxySources == nil {
		config.ProxySources = []string{}
	}
	return config, nil
}

func (c browserConfig) sources() ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(c.ProxySources))
	for _, source := range c.ProxySources {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(source))
		if err != nil {
			return nil, err
		}
		out = append(out, prefix)
	}
	return out, nil
}

func (c browserConfig) validate() error {
	invalid := func(message string) error {
		return httpx.NewError(http.StatusBadRequest, "invalid_browser_settings", message)
	}
	if _, err := c.sources(); err != nil {
		return invalid("a proxy source is not a network (10.0.0.0/8)")
	}
	if c.MaxSessions < 1 || c.MaxSessions > 64 {
		return invalid("sessions at once must be 1 to 64")
	}
	if !c.Enabled {
		return nil
	}
	if path, ok := strings.CutPrefix(c.Endpoint, "exec:"); ok {
		if strings.TrimSpace(path) == "" {
			return invalid("exec: needs the path of a Chromium")
		}
	} else if u, err := url.Parse(c.Endpoint); err != nil || u.Host == "" ||
		(u.Scheme != "http" && u.Scheme != "ws" && u.Scheme != "https" && u.Scheme != "wss") {
		return invalid("the browser's address is not http://host:port, ws://... or exec:<path>")
	}
	if _, _, err := net.SplitHostPort(c.ProxyListen); err != nil {
		return invalid("the proxy's listening address is not host:port (:8188)")
	}
	if u, err := url.Parse(c.ProxyURL); err != nil || u.Scheme != "http" || u.Host == "" {
		return invalid("the proxy as the browser reaches it is not http://host:port")
	}
	return nil
}

// browsers is the server's side of the browser: the sessions, the proxy, and
// what the proxy checks, as the settings last made them.
type browsers struct {
	mu     sync.Mutex
	pool   *browser.Pool
	proxy  *browser.Proxy
	listen string
	// endpointHost is the browser's host, whose addresses may use the proxy.
	endpointHost string
	sources      []netip.Prefix
	resolved     []netip.Addr
	resolvedAt   time.Time
	blocklist    tools.Blocklist
	blockedAt    time.Time
}

// browserPool is the sessions, nil while the browser is off.
func (s *Server) browserPool() *browser.Pool {
	s.browser.mu.Lock()
	defer s.browser.mu.Unlock()
	return s.browser.pool
}

// browserOffered reports whether the caller may open the browser: it is on,
// and they are an administrator (the only ones, for now).
func (s *Server) browserOffered(ctx context.Context) bool {
	if !isStaff(ctx) {
		return false
	}
	config, err := loadBrowser(ctx, s.q)
	if err != nil {
		slog.WarnContext(ctx, "read browser settings", "error", err)
		return false
	}
	return config.Enabled
}

// StartBrowser applies the stored settings, at start.
func (s *Server) StartBrowser(ctx context.Context) {
	config, err := loadBrowser(ctx, s.q)
	if err != nil {
		slog.WarnContext(ctx, "read browser settings", "error", err)
		return
	}
	if err := s.applyBrowser(config); err != nil {
		slog.WarnContext(ctx, "start the browser proxy", "error", err, "listen", config.ProxyListen)
	}
}

// CloseBrowser ends every session and stops the proxy, at shutdown.
func (s *Server) CloseBrowser() {
	s.browser.mu.Lock()
	defer s.browser.mu.Unlock()
	if s.browser.pool != nil {
		s.browser.pool.Close()
		s.browser.pool = nil
	}
	if s.browser.proxy != nil {
		_ = s.browser.proxy.Close()
		s.browser.proxy, s.browser.listen = nil, ""
	}
}

// applyBrowser puts config into effect: the proxy listening where it says,
// the sessions on the browser it names. Off closes both.
func (s *Server) applyBrowser(config browserConfig) error {
	b := &s.browser
	b.mu.Lock()
	defer b.mu.Unlock()
	if !config.Enabled {
		if b.pool != nil {
			b.pool.Close()
			b.pool = nil
		}
		if b.proxy != nil {
			_ = b.proxy.Close()
			b.proxy, b.listen = nil, ""
		}
		return nil
	}
	sources, err := config.sources()
	if err != nil {
		return err
	}
	b.sources = sources
	b.endpointHost = ""
	if u, err := url.Parse(config.Endpoint); err == nil && !strings.HasPrefix(config.Endpoint, "exec:") {
		b.endpointHost = u.Hostname()
	}
	b.resolvedAt = time.Time{}
	if b.proxy == nil || b.listen != config.ProxyListen {
		if b.proxy != nil {
			_ = b.proxy.Close()
		}
		proxy := &browser.Proxy{Source: s.browserSource, Blocklist: s.browserBlocklist, Refuse: s.browserRefuses}
		if err := proxy.Listen(config.ProxyListen); err != nil {
			b.proxy, b.listen = nil, ""
			return err
		}
		b.proxy, b.listen = proxy, config.ProxyListen
	}
	if b.pool == nil {
		b.pool = browser.NewPool()
	}
	b.pool.Configure(browser.Config{Endpoint: config.Endpoint, ProxyURL: config.ProxyURL, MaxSessions: config.MaxSessions})
	return nil
}

// browserSource reports whether a connection to the proxy comes from the
// browser: its host's addresses (looked up again at most every 30 seconds)
// or a network the settings name.
func (s *Server) browserSource(addr netip.Addr) bool {
	b := &s.browser
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, prefix := range b.sources {
		if prefix.Contains(addr) {
			return true
		}
	}
	if b.endpointHost == "" {
		return false
	}
	if time.Since(b.resolvedAt) > 30*time.Second {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", b.endpointHost)
		cancel()
		if err == nil {
			b.resolved = found
		}
		b.resolvedAt = time.Now()
	}
	for _, known := range b.resolved {
		if known.Unmap() == addr {
			return true
		}
	}
	return false
}

// browserBlocklist is the search settings' blocked sites, read at most every
// 30 seconds: a page opens dozens of connections.
func (s *Server) browserBlocklist() tools.Blocklist {
	b := &s.browser
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Since(b.blockedAt) > 30*time.Second {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		config, _, err := loadWebSearch(ctx, s.q)
		cancel()
		if err != nil {
			slog.Warn("read the blocklist for the browser", "error", err)
		} else {
			b.blocklist = config.searchConfig().Blocklist
			b.blockedAt = time.Now()
		}
	}
	return b.blocklist
}

// browserRefuses names OpenChat's own hosts: a page is never to talk to the
// site it is opened from.
func (s *Server) browserRefuses(host string) bool {
	for _, origin := range s.cfg.CORSOrigins {
		if u, err := url.Parse(origin); err == nil && strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}

func (s *Server) toBrowserSettings(c browserConfig) BrowserSettings {
	sessions := 0
	if pool := s.browserPool(); pool != nil {
		sessions = pool.Count()
	}
	return BrowserSettings{
		Enabled: c.Enabled, Endpoint: c.Endpoint, ProxyListen: c.ProxyListen, ProxyUrl: c.ProxyURL,
		ProxySources: c.ProxySources, MaxSessions: c.MaxSessions, Sessions: sessions,
	}
}

func (s *Server) GetBrowserSettings(w http.ResponseWriter, r *http.Request) {
	config, err := loadBrowser(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.toBrowserSettings(config))
}

func (s *Server) UpdateBrowserSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateBrowserSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	config, err := loadBrowser(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled != nil {
		config.Enabled = *body.Enabled
	}
	if body.Endpoint != nil {
		config.Endpoint = strings.TrimSpace(*body.Endpoint)
	}
	if body.ProxyListen != nil {
		config.ProxyListen = strings.TrimSpace(*body.ProxyListen)
	}
	if body.ProxyUrl != nil {
		config.ProxyURL = strings.TrimSpace(*body.ProxyUrl)
	}
	if body.ProxySources != nil {
		config.ProxySources = []string{}
		for _, source := range *body.ProxySources {
			if source = strings.TrimSpace(source); source != "" {
				config.ProxySources = append(config.ProxySources, source)
			}
		}
	}
	if body.MaxSessions != nil {
		config.MaxSessions = *body.MaxSessions
	}
	if err := config.validate(); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Put into effect first: a proxy that cannot listen is said now, not
	// found later by a run.
	if err := s.applyBrowser(config); err != nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_browser_settings",
			"the proxy cannot listen there: "+shortError(err)))
		return
	}
	value, _ := json.Marshal(config)
	if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: settingBrowser, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(ctx, "browser settings changed", "enabled", config.Enabled, "endpoint", config.Endpoint,
		"actor", auth.MustUser(ctx).Email)
	httpx.JSON(w, http.StatusOK, s.toBrowserSettings(config))
}

// browserTestPreview is how much of a tested page's snapshot is shown.
const browserTestPreview = 600

func (s *Server) TestBrowser(w http.ResponseWriter, r *http.Request) {
	var body BrowserTestRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	started := time.Now()
	out := BrowserTestResult{}
	fail := func(err error) {
		message := shortError(err)
		out.Error = &message
		out.LatencyMs = time.Since(started).Milliseconds()
		httpx.JSON(w, http.StatusOK, out)
	}
	pool := s.browserPool()
	if pool == nil {
		fail(errors.New("the browser is off"))
		return
	}
	// A session of its own, closed after: nobody's conversation loses theirs.
	key := uuid.New()
	defer pool.Release(key)
	session, err := pool.Open(key, uuid.New())
	if err != nil {
		fail(err)
		return
	}
	address, err := browserAddress(body.Url)
	if err != nil {
		fail(err)
		return
	}
	page, err := session.Open(ctx, address)
	if err != nil {
		fail(err)
		return
	}
	out.Ok = true
	out.LatencyMs = time.Since(started).Milliseconds()
	out.Url, out.Title = &page.URL, &page.Title
	preview := cut(page.Snapshot, browserTestPreview)
	out.Preview = &preview
	httpx.JSON(w, http.StatusOK, out)
}

// ListBrowserActions is the administrators' record of the browser actions
// that needed asking (browser_actions): never what was typed.
func (s *Server) ListBrowserActions(w http.ResponseWriter, r *http.Request, params ListBrowserActionsParams) {
	limit := clampLimit(params.Limit)
	var before *int64
	if params.Cursor != nil && *params.Cursor != "" {
		id, err := strconv.ParseInt(*params.Cursor, 10, 64)
		if err != nil {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_cursor", "the cursor is not one this list gave"))
			return
		}
		before = &id
	}
	rows, err := s.q.ListBrowserActions(r.Context(), sqlc.ListBrowserActionsParams{Before: before, RowLimit: limit + 1})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := BrowserActionLogPage{Items: make([]BrowserActionLog, 0, len(rows)), Limit: int(limit)}
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		cursor := strconv.FormatInt(rows[limit-1].ID, 10)
		out.NextCursor = &cursor
	}
	for _, row := range rows {
		out.Items = append(out.Items, BrowserActionLog{
			Id: row.ID, CreatedAt: row.CreatedAt.Time, UserId: row.UserID, UserName: row.UserName, UserEmail: row.UserEmail,
			ConversationId: row.ConversationID, Site: row.Site, Action: row.Action, Target: row.Target, Outcome: row.Outcome,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}
