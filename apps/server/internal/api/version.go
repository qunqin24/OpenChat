package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// The version an administrator runs, and whether a newer one has been released.
//
// The running version is what the binary was built as (config.buildVersion,
// written in from the tag by the release workflow; `dev` for a local build). The
// newest release is the project's GitHub `releases/latest` - never a draft or
// a pre-release - asked for only when an administrator opens the settings (at
// most every six hours) or presses "check for updates" (at most once a
// minute), so a server nobody administers never calls out. GitHub allows 60
// unauthenticated requests an hour per address; this stays far below.

const (
	// releasesLatestURL is the newest published release of the project.
	releasesLatestURL = "https://api.github.com/repos/qunqin24/OpenChat/releases/latest"
	// updateCheckEvery is how long a look is trusted when the page is opened.
	updateCheckEvery = 6 * time.Hour
	// updateCheckAtMost is the least time between two looks, pressed or not.
	updateCheckAtMost  = time.Minute
	updateCheckTimeout = 8 * time.Second
)

// updateChecker remembers the last look at the releases. The zero value is
// ready; tests set url, client and now.
type updateChecker struct {
	mu        sync.Mutex
	url       string
	client    *http.Client
	now       func() time.Time
	triedAt   time.Time // the last look, successful or not
	checkedAt time.Time // the last successful look
	latest    *githubRelease
	lastErr   VersionInfoCheckError
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// GetVersionInfo implements ServerInterface.
func (s *Server) GetVersionInfo(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, s.versionInfo(r.Context(), updateCheckEvery))
}

// CheckForUpdate implements ServerInterface.
func (s *Server) CheckForUpdate(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, s.versionInfo(r.Context(), 0))
}

// versionInfo answers from the last look, looking again first when the last
// one is older than maxAge (and than updateCheckAtMost).
func (s *Server) versionInfo(ctx context.Context, maxAge time.Duration) VersionInfo {
	u := &s.updates
	u.mu.Lock()
	defer u.mu.Unlock()

	now := u.clock()
	stale := u.checkedAt.IsZero() || now.Sub(u.checkedAt) >= maxAge
	if stale && (u.triedAt.IsZero() || now.Sub(u.triedAt) >= updateCheckAtMost) {
		u.triedAt = now
		release, failure := u.look(ctx)
		if failure == "" {
			u.latest, u.checkedAt, u.lastErr = release, now, ""
		} else {
			u.lastErr = failure
		}
	}

	current := currentVersion(s.cfg.Version)
	info := VersionInfo{Current: current}
	if u.lastErr != "" {
		failure := u.lastErr
		info.CheckError = &failure
	}
	if u.latest != nil {
		latest := strings.TrimPrefix(u.latest.TagName, "v")
		info.Latest = &latest
		if u.latest.HTMLURL != "" {
			url := u.latest.HTMLURL
			info.LatestUrl = &url
		}
		if !u.latest.PublishedAt.IsZero() {
			published := u.latest.PublishedAt
			info.PublishedAt = &published
		}
		checked := u.checkedAt
		info.CheckedAt = &checked
		info.UpdateAvailable = newerVersion(latest, current)
	}
	return info
}

// look asks GitHub for the newest release. Called with u.mu held, so one look
// at a time; it is short (updateCheckTimeout) and only administrators wait.
func (u *updateChecker) look(ctx context.Context) (*githubRelease, VersionInfoCheckError) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	url := u.url
	if url == "" {
		url = releasesLatestURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, VersionInfoCheckErrorUnexpected
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "OpenChat update check")
	client := u.client
	if client == nil {
		client = &http.Client{Timeout: updateCheckTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "update check: releases unreachable", "error", err)
		return nil, VersionInfoCheckErrorUnreachable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case err != nil:
		return nil, VersionInfoCheckErrorUnreachable
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		slog.WarnContext(ctx, "update check: rate limited", "status", resp.StatusCode)
		return nil, VersionInfoCheckErrorRateLimited
	case resp.StatusCode != http.StatusOK:
		slog.WarnContext(ctx, "update check: unexpected answer", "status", resp.StatusCode)
		return nil, VersionInfoCheckErrorUnexpected
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil || release.TagName == "" {
		slog.WarnContext(ctx, "update check: unreadable release", "error", errors.Join(err, errNoTag(release.TagName)))
		return nil, VersionInfoCheckErrorUnexpected
	}
	if _, ok := parseVersion(strings.TrimPrefix(release.TagName, "v")); !ok {
		slog.WarnContext(ctx, "update check: release tag is not a version", "tag", release.TagName)
		return nil, VersionInfoCheckErrorUnexpected
	}
	return &release, ""
}

func errNoTag(tag string) error {
	if tag == "" {
		return errors.New("no tag_name")
	}
	return nil
}

func (u *updateChecker) clock() time.Time {
	if u.now != nil {
		return u.now()
	}
	return time.Now()
}

// currentVersion is the build's version without a leading v; `dev` when the
// build said nothing or something that is not a version.
func currentVersion(built string) string {
	v := strings.TrimPrefix(strings.TrimSpace(built), "v")
	if _, ok := parseVersion(v); !ok {
		return "dev"
	}
	return v
}

// semver is major.minor.patch with an optional pre-release (1.2.3-rc.1).
type semver struct {
	core [3]int
	pre  string
}

func parseVersion(v string) (semver, bool) {
	var out semver
	core, pre, _ := strings.Cut(v, "-")
	core, _, _ = strings.Cut(core, "+") // build metadata does not order
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return out, false
		}
		out.core[i] = n
	}
	out.pre, _, _ = strings.Cut(pre, "+")
	return out, true
}

// newerVersion reports whether latest is a newer version than current. A
// version that is not one (`dev`) is never older than anything.
func newerVersion(latest, current string) bool {
	l, okL := parseVersion(latest)
	c, okC := parseVersion(current)
	if !okL || !okC {
		return false
	}
	for i := range 3 {
		if l.core[i] != c.core[i] {
			return l.core[i] > c.core[i]
		}
	}
	// Same core: a release is newer than its own pre-release.
	switch {
	case l.pre == c.pre:
		return false
	case l.pre == "":
		return true
	case c.pre == "":
		return false
	}
	return comparePre(l.pre, c.pre) > 0
}

// comparePre orders two pre-release strings the semver way: dot-separated
// identifiers, numbers numerically and below words, more identifiers later.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				return cmpInt(an, bn)
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(as), len(bs))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
