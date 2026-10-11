package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qunqin/openchat/apps/server/internal/config"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.1.0", "1.0.9", true},
		{"2.0.0", "1.99.99", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"1.0.0-rc.10", "1.0.0-rc.9", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.1", "dev", false},
		{"nonsense", "1.0.0", false},
	} {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
	for built, want := range map[string]string{"1.0.1": "1.0.1", "v1.0.1": "1.0.1", "": "dev", "dev": "dev", "main-abc123": "dev"} {
		if got := currentVersion(built); got != want {
			t.Errorf("currentVersion(%q) = %q, want %q", built, got, want)
		}
	}
}

// fakeReleases is GitHub's releases/latest answering with status and body.
func fakeReleases(t *testing.T, status *atomic.Int32, body *atomic.Value, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVersionInfoLooksAndCaches(t *testing.T) {
	var status, calls atomic.Int32
	var body atomic.Value
	status.Store(http.StatusOK)
	body.Store(`{"tag_name":"v1.0.2","html_url":"https://github.com/qunqin24/OpenChat/releases/tag/v1.0.2","published_at":"2026-10-12T08:00:00Z"}`)
	srv := fakeReleases(t, &status, &body, &calls)

	clock := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	s := &Server{cfg: &config.Config{Version: "1.0.1"}}
	s.updates.url = srv.URL
	s.updates.now = func() time.Time { return clock }
	ctx := context.Background()

	info := s.versionInfo(ctx, updateCheckEvery)
	if info.Current != "1.0.1" || info.Latest == nil || *info.Latest != "1.0.2" || !info.UpdateAvailable || info.CheckError != nil {
		t.Fatalf("first look: %+v", info)
	}
	if info.LatestUrl == nil || *info.LatestUrl == "" || info.PublishedAt == nil || info.CheckedAt == nil {
		t.Errorf("release details missing: %+v", info)
	}

	// Opening the page again within six hours does not ask GitHub.
	clock = clock.Add(5 * time.Hour)
	s.versionInfo(ctx, updateCheckEvery)
	if calls.Load() != 1 {
		t.Errorf("%d requests within the six hours, want 1", calls.Load())
	}
	// The button asks again - but two presses within a minute share one look.
	s.versionInfo(ctx, 0)
	s.versionInfo(ctx, 0)
	if calls.Load() != 2 {
		t.Errorf("%d requests after two presses, want 2", calls.Load())
	}

	// A failed look keeps what was known and says it failed.
	status.Store(http.StatusForbidden)
	clock = clock.Add(2 * time.Minute)
	info = s.versionInfo(ctx, 0)
	if info.CheckError == nil || *info.CheckError != VersionInfoCheckErrorRateLimited || info.Latest == nil || *info.Latest != "1.0.2" {
		t.Errorf("rate limited: %+v", info)
	}
	// And a good one clears the failure.
	status.Store(http.StatusOK)
	body.Store(`{"tag_name":"v1.0.1","html_url":"x","published_at":"2026-10-12T08:00:00Z"}`)
	clock = clock.Add(2 * time.Minute)
	info = s.versionInfo(ctx, 0)
	if info.CheckError != nil || info.UpdateAvailable {
		t.Errorf("up to date: %+v", info)
	}
}

func TestVersionInfoOddAnswers(t *testing.T) {
	var status, calls atomic.Int32
	var body atomic.Value
	srv := fakeReleases(t, &status, &body, &calls)
	clock := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		status int
		body   string
		want   VersionInfoCheckError
	}{
		{http.StatusNotFound, `{}`, VersionInfoCheckErrorUnexpected},
		{http.StatusOK, `not json`, VersionInfoCheckErrorUnexpected},
		{http.StatusOK, `{"tag_name":"latest-build"}`, VersionInfoCheckErrorUnexpected},
		{http.StatusTooManyRequests, `{}`, VersionInfoCheckErrorRateLimited},
	} {
		status.Store(int32(c.status))
		body.Store(c.body)
		s := &Server{cfg: &config.Config{Version: "dev"}}
		s.updates.url = srv.URL
		s.updates.now = func() time.Time { return clock }
		info := s.versionInfo(context.Background(), 0)
		if info.CheckError == nil || *info.CheckError != c.want || info.Latest != nil || info.UpdateAvailable || info.Current != "dev" {
			t.Errorf("%d %s: %+v", c.status, c.body, info)
		}
	}

	// Unreachable.
	s := &Server{cfg: &config.Config{Version: "1.0.0"}}
	s.updates.url = "http://127.0.0.1:1/unreachable"
	info := s.versionInfo(context.Background(), 0)
	if info.CheckError == nil || *info.CheckError != VersionInfoCheckErrorUnreachable {
		t.Errorf("unreachable: %+v", info)
	}
}
