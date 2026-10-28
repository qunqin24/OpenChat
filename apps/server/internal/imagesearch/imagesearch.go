// Package imagesearch finds the pictures answers describe. An answer's
// interface components never give an image address: they describe a picture in
// a few words ("laptop keyboard") and a search service finds one - Openverse,
// which needs no key, or Pixabay, with an administrator's key.
package imagesearch

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Image is a picture a service found: where its bytes are, and what its
// licence asks to be shown with it.
type Image struct {
	// Provider and ID name it at the service, which is how the same picture
	// found by other words is known to be the same.
	Provider string
	ID       string
	// URL is the picture to keep; Fallback, when URL cannot be had, a smaller
	// copy of it.
	URL      string
	Fallback string
	Width    int
	Height   int
	Title    string
	Creator  string
	// CreatorURL, LicenseURL and Landing are links: the creator's page, the
	// licence, the page the picture is from.
	CreatorURL string
	License    string
	LicenseURL string
	Landing    string
	// Source is where the service found it (flickr, wikimedia, pixabay).
	Source string
}

// Searcher is a picture search service.
type Searcher interface {
	// Search returns the first suitable picture for the words, or nil when
	// nothing matches.
	Search(ctx context.Context, query string) (*Image, error)
}

// ErrUnavailable is a search that could not be made: the service did not
// answer in time, answered with an error, or is limiting this server.
var ErrUnavailable = errors.New("imagesearch: unavailable")

const (
	timeout = 12 * time.Second
	// minWidth leaves out pictures too small to fill a card.
	minWidth = 400
	// userAgent names the caller to the services.
	userAgent = "OpenChat (+https://github.com/qunqin24/OpenChat)"
)

// limiter keeps a service's searches within its share of a minute, and stops
// them until the time a service named when it refused one.
type limiter struct {
	perMinute int

	mu      sync.Mutex
	window  time.Time
	used    int
	blocked time.Time
}

func (l *limiter) take(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.blocked) {
		return false
	}
	if now.Sub(l.window) >= time.Minute {
		l.window, l.used = now, 0
	}
	if l.used >= l.perMinute {
		return false
	}
	l.used++
	return true
}

// block stops searches for the seconds a service named (Retry-After,
// X-RateLimit-Reset), or a minute when it named none.
func (l *limiter) block(now time.Time, seconds string) {
	wait := time.Minute
	if n, err := strconv.Atoi(strings.TrimSpace(seconds)); err == nil && n > 0 {
		wait = time.Duration(min(n, 24*60*60)) * time.Second
	}
	l.mu.Lock()
	l.blocked = now.Add(wait)
	l.mu.Unlock()
}
