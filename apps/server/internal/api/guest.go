package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/memory"
)

// Guest mode.
//
// A guest has no account and the server keeps nothing of them: their
// conversations, memories and instructions live in their browser, and the one
// request that needs a model (guest_run.go) carries everything the run needs.
// This file is the rest: whether guests are let in, the limits on how much
// they may ask, and the few public endpoints around the run.

const (
	// settingGuestMode lets anyone use the chat without an account. Off unless
	// an administrator turns it on.
	settingGuestMode = "guest_mode"
	// settingGuestHourlyLimit is how many requests one address may make per
	// hour, and settingGuestDailyLimit how many all guests together may make in
	// a day (the last 24 hours). Zero is no limit.
	settingGuestHourlyLimit = "guest_hourly_limit"
	settingGuestDailyLimit  = "guest_daily_limit"

	// The most the limits can be set to; ceilings against typos.
	maxGuestHourlyLimit = 100_000
	maxGuestDailyLimit  = 10_000_000
)

var (
	errGuestIPLimit = httpx.NewError(http.StatusTooManyRequests, "guest_ip_limit",
		"this network has used up its guest requests for the hour; try again later or sign in")
	errGuestDailyLimit = httpx.NewError(http.StatusTooManyRequests, "guest_daily_limit",
		"guests have used up today's requests; try again later or sign in")
)

// guestEnabled is whether guest mode is on. A setting that cannot be read is
// off: opening the door on a database error would be the wrong default.
func (s *Server) guestEnabled(ctx context.Context) bool {
	on, err := boolSetting(ctx, s.q, settingGuestMode)
	if err != nil {
		slog.WarnContext(ctx, "read guest_mode setting", "error", err)
		return false
	}
	return on
}

// requireGuestMode answers 404 to everything it guards while guest mode is
// off, as though the endpoints were not there.
func (s *Server) requireGuestMode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.guestEnabled(r.Context()) {
			httpx.Fail(w, r, httpx.ErrNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetGuestStatus tells the sign-in page whether to offer guest mode.
func (s *Server) GetGuestStatus(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, GuestStatus{Enabled: s.guestEnabled(r.Context())})
}

// GetGuestConfig is what the chat reads, for a guest, from `/models`,
// `/features` and `/me/personalization`.
func (s *Server) GetGuestConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Not staff: models kept for administrators stay hidden.
	models, err := s.chatModels(ctx, false)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	config, key, err := loadWebSearch(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	allowed, err := s.notes().GlobalOn(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := GuestConfig{
		Models: models,
		// A picture of their own is an account's setting.
		Features: ChatFeatures{WebSearch: config.Enabled && config.searcher(key) != nil, AvatarUploads: false},
	}
	out.Personalization.MemoryAllowed = allowed
	out.Personalization.InstructionLimit = memory.MaxInstructions
	out.Personalization.MemoryLimit = memory.MaxMemories
	out.Personalization.MemoryLength = memory.MaxContent
	httpx.JSON(w, http.StatusOK, out)
}

// GetGuestFavicon is the favicon proxy for guests.
func (s *Server) GetGuestFavicon(w http.ResponseWriter, r *http.Request, domain string) {
	s.GetFavicon(w, r, domain)
}

// ----------------------------------------------------------------- limits

// clientIP is the address a request came from, as the RealIP middleware in the
// router leaves it in RemoteAddr (it reads X-Forwarded-For and X-Real-IP, so
// the server has to sit behind a proxy that sets them, or the header can be
// forged to take a fresh allowance). An IPv6 address counts as its /64: one
// subscriber is given billions of addresses there.
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return host
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}

// takeGuestRequest counts one request of this address against the limits and
// refuses it if either is used up. Counting and recording are one step under a
// lock in the database, so concurrent requests at the limit cannot all pass,
// and servers sharing the database share the count.
func (s *Server) takeGuestRequest(ctx context.Context, ip string) error {
	hourly, err := intSettingOr(ctx, s.q, settingGuestHourlyLimit, 0)
	if err != nil {
		return err
	}
	daily, err := intSettingOr(ctx, s.q, settingGuestDailyLimit, 0)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockGuestRequests(ctx); err != nil {
			return err
		}
		counts, err := q.CountGuestRequests(ctx, ip)
		if err != nil {
			return err
		}
		// The address first: when both are used up, it is the one a person can
		// do something about.
		if hourly > 0 && counts.FromIp >= int64(hourly) {
			return errGuestIPLimit
		}
		if daily > 0 && counts.Today >= int64(daily) {
			return errGuestDailyLimit
		}
		return q.InsertGuestRequest(ctx, ip)
	})
}

// ------------------------------------------------------------ the image proxy

// imageLimiter keeps one client from using the image proxy as a free fetcher:
// a page of answers shows a handful of pictures, so a burst beyond this is not
// reading. In memory and per process - it is a guard against abuse, not an
// accounting, and the proxy only ever fetches public addresses.
type imageLimiter struct {
	mu     sync.Mutex
	window time.Time
	seen   map[string]int
}

const (
	guestImagesPerMinute = 120
	// guestImageClients bounds the table; a full one starts over.
	guestImageClients = 10_000
)

var guestImages = &imageLimiter{seen: map[string]int{}}

func (l *imageLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.window) >= time.Minute || len(l.seen) >= guestImageClients {
		l.window, l.seen = now, map[string]int{}
	}
	l.seen[ip]++
	return l.seen[ip] <= guestImagesPerMinute
}

// GetGuestImage serves a picture an answer shows. A guest's conversation is
// not on the server, so unlike GetConversationImage there is no list of
// addresses the model was given to check against; what stands in for it is the
// guarded fetch (public addresses only, images only, size-capped) and a limit
// on how often one client may ask.
func (s *Server) GetGuestImage(w http.ResponseWriter, r *http.Request, params GetGuestImageParams) {
	if !guestImages.allow(clientIP(r), time.Now()) {
		httpx.Fail(w, r, httpx.NewError(http.StatusTooManyRequests, "image_rate_limited", "too many pictures asked for"))
		return
	}
	address, err := imageAddress(params.Url)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	serveImage(w, r, address)
}

// -------------------------------------------------------------- title on request

const guestTitleRows = 8

// GuestTitle names a guest's conversation from the exchanges the browser sends.
func (s *Server) GuestTitle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body GuestTitleRequest
	if err := decodeLenient(r, &body, 1<<20); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var transcript strings.Builder
	rows := body.Messages
	if len(rows) > guestTitleRows {
		rows = rows[len(rows)-guestTitleRows:]
	}
	for _, row := range rows {
		text := strings.TrimSpace(row.Content)
		if text == "" {
			continue
		}
		limit := titleAnswerCap / 2
		role := "assistant"
		if row.Role == "user" {
			limit, role = titleInputCap/2, "user"
		}
		fmt.Fprintf(&transcript, "<%s>\n%s\n</%s>\n", role, clip(text, limit), role)
	}
	if transcript.Len() == 0 {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "nothing_to_title", "this conversation has nothing to title yet"))
		return
	}
	if err := s.takeGuestRequest(ctx, clientIP(r)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	own := ""
	if body.Model != nil {
		own = strings.TrimSpace(*body.Model)
	}
	// Billed to no one: the ledger row has no owner, which is what marks a
	// guest's request there (see CreateGuestUser).
	title, err := s.generateTitle(ctx, uuid.Nil, uuid.Nil, own, nil, transcript.String())
	if err != nil {
		slog.WarnContext(ctx, "guest title", "error", err)
		httpx.Fail(w, r, httpx.NewError(http.StatusBadGateway, "title_failed", "the model gave no title"))
		return
	}
	httpx.JSON(w, http.StatusOK, GuestTitle{Title: title})
}

// -------------------------------------------------------------- settings

// guestSettings reads the three settings for the admin page.
func guestSettings(ctx context.Context, q *sqlc.Queries, into *SystemSettings) error {
	var err error
	if into.GuestMode, err = boolSetting(ctx, q, settingGuestMode); err != nil {
		return err
	}
	if into.GuestHourlyLimit, err = intSettingOr(ctx, q, settingGuestHourlyLimit, 0); err != nil {
		return err
	}
	into.GuestDailyLimit, err = intSettingOr(ctx, q, settingGuestDailyLimit, 0)
	return err
}

// validGuestLimits checks the limits of an update before anything is saved.
func validGuestLimits(hourly, daily *int) error {
	for _, l := range []struct {
		value *int
		max   int
	}{{hourly, maxGuestHourlyLimit}, {daily, maxGuestDailyLimit}} {
		if l.value != nil && (*l.value < 0 || *l.value > l.max) {
			return httpx.NewError(http.StatusBadRequest, "invalid_guest_limit",
				fmt.Sprintf("a guest limit must be between 0 and %d", l.max))
		}
	}
	return nil
}

// decodeLenient reads a JSON body of up to limit bytes, ignoring fields it does
// not know: a guest's browser sends what its version of the page has, which
// need not be what this server's version of the contract has.
func decodeLenient(r *http.Request, dst any, limit int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	if err := dec.Decode(dst); err != nil {
		return httpx.NewError(http.StatusBadRequest, "invalid_body", "request body is not valid JSON: "+err.Error())
	}
	return nil
}
