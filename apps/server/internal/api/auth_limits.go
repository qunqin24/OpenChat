package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// Brute-force protection for signing in and signing up.
//
// Attempts are rows in auth_attempts, counted per client address (clientIP:
// the address the RealIP middleware leaves in RemoteAddr, an IPv6
// address as its /64 - which is only as good as the proxy in front that sets
// X-Forwarded-For; see deploy/Caddyfile). The limits are deliberately about the
// address and never about the account alone: locking an account by its
// identifier would let anyone on the internet lock the administrator out by
// typing their name wrongly ten times.

// attemptLimit is one kind of attempt and how much of it an address gets.
type attemptLimit struct {
	kind string
	// perIP is how many attempts one address may make in the window; perSubject
	// how many it may make against one identifier (0: no such limit). The
	// second is the smaller, so that one address can neither walk through
	// accounts unthrottled nor try one account at the full per-address rate.
	perIP, perSubject int
	window            time.Duration
	// code and message are the 429 answer.
	code, message string
}

var (
	// A failed sign-in counts (a right password hands its slot back).
	loginLimit = attemptLimit{
		kind: "login", perIP: 10, perSubject: 5, window: 15 * time.Minute,
		code: "login_rate_limited", message: "too many failed sign-in attempts; try again later",
	}
	// Every sign-up attempt counts, whether or not it made an account: a
	// refused one (an address already registered) is how addresses get probed.
	registerLimit = attemptLimit{
		kind: "register", perIP: 5, window: time.Hour,
		code: "register_rate_limited", message: "too many sign-up attempts from this network; try again later",
	}

	// authAttemptRetention is how long a row is kept: the longest window.
	authAttemptRetention = time.Hour
)

// rateLimited is the error of an attempt turned away; Retry-After says when.
type rateLimited struct {
	limit      attemptLimit
	retryAfter time.Duration
}

func (e *rateLimited) Error() string { return e.limit.code }

// failRateLimited writes the 429, with Retry-After in seconds (also in
// `details`, for a client that cannot read headers across origins).
func failRateLimited(w http.ResponseWriter, r *http.Request, e *rateLimited) {
	seconds := int(e.retryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	err := httpx.NewError(http.StatusTooManyRequests, e.limit.code, e.limit.message)
	err.Details = map[string]any{"retryAfter": seconds}
	httpx.Fail(w, r, err)
}

// subjectOf is the identifier an attempt was made against, as stored: a hash of
// what was typed, lower-cased as the sign-in lookup compares it.
func subjectOf(identifier string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(identifier))))
	return hex.EncodeToString(sum[:])
}

// takeAttempt counts one attempt of this kind from this address (against this
// subject, for a sign-in) and refuses it if a limit is already used up. Counting
// and recording are one step under a lock in the database, so concurrent
// requests at the limit cannot all pass - which matters, because checking a
// password takes long enough for a flood to be in flight before the first
// failure would have been written. A refused attempt is not recorded: it would
// only extend the wait of someone already waiting.
//
// It returns the row's id, so a sign-in that turns out to have the right
// password can give the slot back (releaseAttempt).
func (s *Server) takeAttempt(ctx context.Context, l attemptLimit, ip, subject string) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockAuthAttempts(ctx); err != nil {
			return err
		}
		scopes := []struct {
			subject *string
			limit   int
		}{{nil, l.perIP}}
		if l.perSubject > 0 && subject != "" {
			scopes = append(scopes, struct {
				subject *string
				limit   int
			}{&subject, l.perSubject})
		}
		for _, scope := range scopes {
			seconds, err := q.AuthAttemptsFull(ctx, sqlc.AuthAttemptsFullParams{
				Kind: l.kind, Ip: ip, Subject: scope.subject,
				RowLimit: int32(scope.limit), WindowSeconds: l.window.Seconds(),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			return &rateLimited{limit: l, retryAfter: time.Duration(seconds) * time.Second}
		}
		var err error
		id, err = q.InsertAuthAttempt(ctx, sqlc.InsertAuthAttemptParams{Kind: l.kind, Ip: ip, Subject: subject})
		return err
	})
	return id, err
}

// releaseAttempt gives a slot back. Best effort: a slot that stays is a
// stricter limit, not a hole.
func (s *Server) releaseAttempt(r *http.Request, id int64) {
	if err := s.q.DeleteAuthAttempt(r.Context(), id); err != nil {
		logWarn(r, "release sign-in attempt", err)
	}
}

// purgeAuthAttempts drops the rows no limit looks at any more.
func (s *Server) purgeAuthAttempts(ctx context.Context, now time.Time) {
	before := pgtype.Timestamptz{Time: now.Add(-authAttemptRetention), Valid: true}
	if _, err := s.q.PurgeAuthAttempts(ctx, before); err != nil {
		slog.WarnContext(ctx, "purge sign-in attempts", "error", err)
	}
}

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
