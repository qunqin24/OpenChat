package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"github.com/google/uuid"
)

// Config is how to reach the browser and route its traffic.
type Config struct {
	// Endpoint is the browser's CDP address - http://browserd:9222, or a
	// ws:// one - or exec:<path> to start a local Chromium (development and
	// tests).
	Endpoint string
	// ProxyURL is the proxy as the browser reaches it (http://openchat:8188).
	// Every context is created with it, and loopback goes through it too.
	ProxyURL string
	// MaxSessions bounds the browser contexts open at once.
	MaxSessions int
	// Idle is how long a session lasts after its last use.
	Idle time.Duration
}

var (
	// ErrBusy is every session in use: the run should say so and stop.
	ErrBusy = errors.New("browser: every browser is in use")
	// ErrUnavailable is no browser to reach.
	ErrUnavailable = errors.New("browser: the browser could not be reached")
)

// Pool keeps one browser connection and a session (a browser context with
// one tab) per conversation.
type Pool struct {
	mu       sync.Mutex
	cfg      Config
	root     context.Context
	stop     context.CancelFunc
	sessions map[uuid.UUID]*Session
	closed   chan struct{}
}

func NewPool() *Pool {
	p := &Pool{sessions: map[uuid.UUID]*Session{}, closed: make(chan struct{})}
	go p.sweep()
	return p
}

// Configure sets where the browser is; a change closes every session and the
// old connection.
func (p *Pool) Configure(cfg Config) {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 8
	}
	if cfg.Idle <= 0 {
		cfg.Idle = 10 * time.Minute
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.Endpoint == cfg.Endpoint && p.cfg.ProxyURL == cfg.ProxyURL {
		p.cfg = cfg
		return
	}
	p.closeAllLocked()
	p.cfg = cfg
}

func (p *Pool) closeAllLocked() {
	for key, s := range p.sessions {
		s.close()
		delete(p.sessions, key)
	}
	if p.stop != nil {
		p.stop()
		p.stop, p.root = nil, nil
	}
}

// Close ends every session and the connection.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	p.closeAllLocked()
}

// Open is the session of key (a conversation), made if there is none. A user
// has one session at a time: theirs in another conversation is closed.
func (p *Pool) Open(key, user uuid.UUID) (*Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.sessions[key]; ok && s.alive() {
		s.touch()
		return s, nil
	}
	if p.cfg.Endpoint == "" {
		return nil, ErrUnavailable
	}
	for k, s := range p.sessions {
		if s.user == user || !s.alive() {
			s.close()
			delete(p.sessions, k)
		}
	}
	if len(p.sessions) >= p.cfg.MaxSessions {
		return nil, ErrBusy
	}
	root, err := p.connectLocked()
	if err != nil {
		return nil, err
	}
	proxy := p.cfg.ProxyURL
	ctx, cancel := chromedp.NewContext(root, chromedp.WithNewBrowserContext(func(params *target.CreateBrowserContextParams) {
		if proxy != "" {
			params.ProxyServer = proxy
			// Loopback is not exempt: the page is never to reach the server it runs beside.
			params.ProxyBypassList = "<-loopback>"
		}
	}))
	s := &Session{ctx: ctx, cancel: cancel, user: user, used: time.Now()}
	if err := s.prepare(); err != nil {
		cancel()
		// The connection may be what failed: start it again next time.
		p.stop()
		p.stop, p.root = nil, nil
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	p.sessions[key] = s
	return s, nil
}

// Release closes key's session now (the conversation is gone).
func (p *Pool) Release(key uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.sessions[key]; ok {
		s.close()
		delete(p.sessions, key)
	}
}

// Count is the sessions open.
func (p *Pool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

func (p *Pool) connectLocked() (context.Context, error) {
	if p.root != nil && p.root.Err() == nil {
		return p.root, nil
	}
	var alloc context.Context
	var stopAlloc context.CancelFunc
	if path, ok := strings.CutPrefix(p.cfg.Endpoint, "exec:"); ok {
		alloc, stopAlloc = chromedp.NewExecAllocator(context.Background(),
			chromedp.ExecPath(path), chromedp.Headless, chromedp.NoFirstRun, chromedp.NoDefaultBrowserCheck,
			chromedp.Flag("disable-gpu", true), chromedp.Flag("mute-audio", true))
	} else {
		alloc, stopAlloc = remote.NewAllocator(context.Background(), p.cfg.Endpoint)
	}
	root, stopRoot := chromedp.NewContext(alloc)
	// The first run starts (or attaches to) the browser, on a context with no
	// deadline: a deadline here would end the browser with it.
	if _, err := chromedp.Run(root, chromedp.Func(func(context.Context, *chromedp.Target) error { return nil })); err != nil {
		stopRoot()
		stopAlloc()
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	p.root = root
	p.stop = func() { stopRoot(); stopAlloc() }
	return root, nil
}

// sweep closes sessions idle past the limit.
func (p *Pool) sweep() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.closed:
			return
		case <-tick.C:
		}
		p.mu.Lock()
		for key, s := range p.sessions {
			if !s.alive() || s.idleFor() > p.cfg.Idle {
				s.close()
				delete(p.sessions, key)
			}
		}
		p.mu.Unlock()
	}
}
