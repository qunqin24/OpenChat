package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/google/uuid"
)

// Session is one conversation's browser: an isolated context (its own cookies
// and storage) with one tab. Its actions run one at a time.
type Session struct {
	ctx    context.Context
	cancel context.CancelFunc
	user   uuid.UUID

	mu     sync.Mutex
	used   time.Time
	refs   map[string]cdp.BackendNodeID
	labels map[string]string

	// What the page does on its own, from its events: apart from mu, which
	// an action holds while the page may be waiting on a dialog.
	evMu     sync.Mutex
	dialogs  []string
	inflight map[network.RequestID]bool
	netAt    time.Time
}

const (
	viewportWidth  = 1280
	viewportHeight = 800
	navTimeout     = 30 * time.Second
	actionTimeout  = 15 * time.Second
	settleWait     = 400 * time.Millisecond
	// A page is settled when it has had no request in flight for netQuiet,
	// or after netSettleMax whatever it still loads (a poll, a stream).
	netQuiet     = 500 * time.Millisecond
	netSettleMax = 5 * time.Second
)

// ErrStaleRef is a ref from an older snapshot.
var ErrStaleRef = errors.New("browser: stale ref")

// ErrNoPage is a ref asked of a session that has shown no page: a new one,
// after the last closed (idle, or the server restarted).
var ErrNoPage = errors.New("browser: no page open")

// keepInTab makes links that open a new tab, and window.open, stay in this
// tab: the session drives one tab and would never see another.
const keepInTab = `(() => {
  document.addEventListener('click', (e) => {
    const a = e.target && e.target.closest && e.target.closest('a[target]');
    if (a) a.target = '_self';
  }, true);
  window.open = (url) => { if (url) location.href = url; return window; };
})();`

func (s *Session) prepare() error {
	ctx, cancel := context.WithTimeout(s.ctx, navTimeout)
	defer cancel()
	// First Run on the new context creates its browser context and tab.
	if _, err := chromedp.Run(s.ctx, chromedp.Func(func(context.Context, *chromedp.Target) error { return nil })); err != nil {
		return err
	}
	if _, err := chromedp.Call(ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width: viewportWidth, Height: viewportHeight, DeviceScaleFactor: 1,
	}); err != nil {
		return err
	}
	if _, err := chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: keepInTab}); err != nil {
		return err
	}
	// No downloads: nothing of the page lands on the server's disk.
	if id := chromedp.FromContext(s.ctx).BrowserContextID; id != "" {
		_, _ = chromedp.CallBrowser(ctx, cdpbrowser.SetDownloadBehavior, cdpbrowser.SetDownloadBehaviorParams{
			Behavior: cdpbrowser.SetDownloadBehaviorBehaviorDeny, BrowserContextID: id,
		})
	}
	// The page's requests, to wait for a page that renders after fetching
	// (settle): a single-page app is "complete" before its content arrives.
	s.inflight = map[network.RequestID]bool{}
	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		return err
	}
	go func() {
		for ev, err := range chromedp.Events(s.ctx, network.RequestWillBeSent) {
			if err != nil {
				return
			}
			// A stream or a socket stays open: it is not loading.
			if ev.Type == network.ResourceTypeEventSource || ev.Type == network.ResourceTypeWebSocket {
				continue
			}
			s.evMu.Lock()
			s.inflight[ev.RequestID], s.netAt = true, time.Now()
			s.evMu.Unlock()
		}
	}()
	done := func(id network.RequestID) {
		s.evMu.Lock()
		delete(s.inflight, id)
		s.netAt = time.Now()
		s.evMu.Unlock()
	}
	go func() {
		for ev, err := range chromedp.Events(s.ctx, network.LoadingFinished) {
			if err != nil {
				return
			}
			done(ev.RequestID)
		}
	}()
	go func() {
		for ev, err := range chromedp.Events(s.ctx, network.LoadingFailed) {
			if err != nil {
				return
			}
			done(ev.RequestID)
		}
	}()
	// A dialog (alert, confirm, beforeunload) would hold the page: each is
	// dismissed, and what it said is told with the next result.
	go func() {
		for ev, err := range chromedp.Events(s.ctx, page.JavascriptDialogOpening) {
			if err != nil {
				return
			}
			s.evMu.Lock()
			s.dialogs = append(s.dialogs, fmt.Sprintf("%s: %s", ev.Type, cut(ev.Message, 300)))
			s.evMu.Unlock()
			go func() {
				c, cancel := context.WithTimeout(s.ctx, actionTimeout)
				defer cancel()
				_, _ = chromedp.Call(c, page.HandleJavaScriptDialog, page.HandleJavaScriptDialogParams{Accept: ev.Type == page.DialogTypeAlert})
			}()
		}
	}()
	return nil
}

func (s *Session) alive() bool { return s.ctx.Err() == nil }

func (s *Session) touch() {
	s.mu.Lock()
	s.used = time.Now()
	s.mu.Unlock()
}

func (s *Session) idleFor() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.used)
}

func (s *Session) close() { s.cancel() }

// within runs f on the session's tab, bounded by d and by the caller's ctx.
func (s *Session) within(caller context.Context, d time.Duration, f func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(s.ctx, d)
	defer cancel()
	stop := context.AfterFunc(caller, cancel)
	defer stop()
	return f(ctx)
}

// Page is where the session is, and what it shows.
type Page struct {
	URL       string
	Title     string
	Snapshot  string
	Truncated bool
	Dialogs   []string
}

// lock takes the session for one action.
func (s *Session) lock() func() {
	s.mu.Lock()
	s.used = time.Now()
	return s.mu.Unlock
}

// takeDialogs is what dialogs said since the last result.
func (s *Session) takeDialogs() []string {
	s.evMu.Lock()
	defer s.evMu.Unlock()
	d := s.dialogs
	s.dialogs = nil
	return d
}

// Open goes to url and waits for it to load.
func (s *Session) Open(caller context.Context, url string) (Page, error) {
	defer s.lock()()
	err := s.within(caller, navTimeout, func(ctx context.Context) error {
		resp, err := chromedp.RunResponse(ctx, chromedp.Navigate(url))
		if err != nil {
			return err
		}
		// The proxy's own refusal of a plain-HTTP page arrives as a response
		// (an HTTPS one fails the tunnel instead): it is marked.
		if resp != nil {
			for name := range resp.Headers {
				if strings.EqualFold(name, RefusedHeader) {
					return errors.New("ERR_PROXY refused")
				}
			}
		}
		return nil
	})
	if err != nil {
		return Page{}, navError(err)
	}
	s.settle(caller)
	return s.snapshotLocked(caller, 0)
}

// Snapshot is the page as it is now, or one node's subtree.
func (s *Session) Snapshot(caller context.Context, ref string) (Page, error) {
	defer s.lock()()
	var scope cdp.BackendNodeID
	if ref != "" {
		id, err := s.node(ref)
		if err != nil {
			return Page{}, err
		}
		scope = id
	}
	return s.snapshotLocked(caller, scope)
}

// Find is the snapshot's lines that contain text, with their paths.
func (s *Session) Find(caller context.Context, text string) (string, int, error) {
	defer s.lock()()
	snap, _, _, err := s.tree(caller, 0)
	if err != nil {
		return "", 0, err
	}
	s.keep(snap)
	out, n := snap.find(text, 20)
	return out, n, nil
}

func (s *Session) snapshotLocked(caller context.Context, scope cdp.BackendNodeID) (Page, error) {
	snap, url, title, err := s.tree(caller, 0)
	if err != nil {
		return Page{}, err
	}
	// The whole page is numbered every time, so a subtree shows the same
	// refs the page would.
	s.keep(snap)
	if scope != 0 {
		snap = snap.subtree(scope)
	}
	text, truncated := snap.String(SnapshotLimit)
	return Page{URL: url, Title: title, Snapshot: text, Truncated: truncated, Dialogs: s.takeDialogs()}, nil
}

func (s *Session) tree(caller context.Context, scope cdp.BackendNodeID) (snapshot, string, string, error) {
	var (
		nodes      []*accessibility.Node
		url, title string
	)
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		res, err := chromedp.Call(ctx, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
		if err != nil {
			return err
		}
		nodes = res.Nodes
		if url, err = chromedp.Run(ctx, chromedp.Location()); err != nil {
			return err
		}
		title, err = chromedp.Run(ctx, chromedp.Title())
		return err
	})
	if err != nil {
		return snapshot{}, "", "", err
	}
	return render(nodes, scope), url, title, nil
}

// keep makes snap's refs the ones actions take. Caller holds mu.
func (s *Session) keep(snap snapshot) {
	s.refs = snap.refs
	s.labels = make(map[string]string, len(snap.refs))
	for _, l := range snap.lines {
		if i := strings.LastIndex(l.text, " [ref="); i >= 0 {
			ref := strings.TrimSuffix(l.text[i+len(" [ref="):], "]")
			s.labels[ref] = l.text[:i]
		}
	}
}

// Label is how the last snapshot showed ref: `button "Buy now"`, with its
// states.
func (s *Session) Label(ref string) string {
	defer s.lock()()
	return s.labels[ref]
}

// node is the DOM node a ref names. Caller holds mu.
func (s *Session) node(ref string) (cdp.BackendNodeID, error) {
	if s.refs == nil {
		return 0, ErrNoPage
	}
	id, ok := s.refs[ref]
	if !ok {
		return 0, ErrStaleRef
	}
	return id, nil
}

// call runs a function on the element, as `this`, and returns its JSON value.
func call(ctx context.Context, id cdp.BackendNodeID, fn string, args ...any) (json.RawMessage, error) {
	obj, err := chromedp.Call(ctx, dom.ResolveNode, dom.ResolveNodeParams{BackendNodeID: id})
	if err != nil {
		return nil, err
	}
	if obj.Object == nil {
		return nil, errors.New("browser: the element is gone")
	}
	params := runtime.CallFunctionOnParams{FunctionDeclaration: fn, ObjectID: obj.Object.ObjectID, ReturnByValue: ptr(true), AwaitPromise: ptr(true)}
	for _, a := range args {
		raw, _ := json.Marshal(a)
		params.Arguments = append(params.Arguments, &runtime.CallArgument{Value: raw})
	}
	res, err := chromedp.Call(ctx, runtime.CallFunctionOn, params)
	if err != nil {
		return nil, err
	}
	if res.ExceptionDetails != nil {
		return nil, fmt.Errorf("browser: %s", res.ExceptionDetails.Text)
	}
	if res.Result == nil {
		return nil, nil
	}
	return json.RawMessage(res.Result.Value), nil
}

// Element is what a ref points at, for the checks made before acting on it.
type Element struct {
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	Text     string `json:"text"`
	Password bool   `json:"password"`
	// Payment: its form asks for a card.
	Payment bool `json:"payment"`
	// FormMethod is its form's method, get or post; empty outside a form.
	FormMethod string `json:"formMethod"`
	// Search: its form is a search (role=search, or a search box in it).
	Search bool `json:"search"`
}

const describe = `function() {
  const el = this;
  const form = el.form || (el.closest && el.closest('form'));
  const text = (el.innerText || el.value || el.getAttribute('aria-label') || el.getAttribute('title') || '').trim().slice(0, 200);
  return {
    tag: (el.tagName || '').toLowerCase(),
    type: (el.type || el.getAttribute && el.getAttribute('type') || '').toLowerCase(),
    text,
    password: !!(form && form.querySelector('input[type=password]')) || (el.type || '').toLowerCase() === 'password',
    payment: !!(form && form.querySelector('input[autocomplete^="cc-"], input[name*="card" i], input[name*="cvv" i], input[name*="cvc" i]')),
    formMethod: form ? (form.getAttribute('method') || 'get').toLowerCase() : '',
    search: !!(form && (form.getAttribute('role') === 'search' || (form.closest && form.closest('[role=search]')) || form.querySelector('input[type=search]'))),
  };
}`

// Describe is what ref points at.
func (s *Session) Describe(caller context.Context, ref string) (Element, error) {
	defer s.lock()()
	id, err := s.node(ref)
	if err != nil {
		return Element{}, err
	}
	var out Element
	err = s.within(caller, actionTimeout, func(ctx context.Context) error {
		raw, err := call(ctx, id, describe)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &out)
	})
	return out, err
}

// DescribeFocused is the element that has the keyboard: what a key press
// acts on.
func (s *Session) DescribeFocused(caller context.Context) (Element, error) {
	defer s.lock()()
	var out Element
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		var err error
		out, err = chromedp.Run(ctx, chromedp.Evaluate[Element]("("+describe+").call(document.activeElement || document.body)"))
		return err
	})
	return out, err
}

// Click clicks the element where it is on the screen, as a person would.
func (s *Session) Click(caller context.Context, ref string) (Page, error) {
	defer s.lock()()
	id, err := s.node(ref)
	if err != nil {
		return Page{}, err
	}
	err = s.within(caller, actionTimeout, func(ctx context.Context) error {
		_, _ = chromedp.Call(ctx, dom.ScrollIntoViewIfNeeded, dom.ScrollIntoViewIfNeededParams{BackendNodeID: id})
		box, err := chromedp.Call(ctx, dom.GetBoxModel, dom.GetBoxModelParams{BackendNodeID: id})
		if err != nil || box.Model == nil || len(box.Model.Content) < 8 {
			// No box (hidden, or not laid out): a script click.
			_, err := call(ctx, id, `function() { this.click(); }`)
			return err
		}
		q := box.Model.Content
		x, y := (q[0]+q[2]+q[4]+q[6])/4, (q[1]+q[3]+q[5]+q[7])/4
		for _, kind := range []input.DispatchMouseEventType{input.DispatchMouseEventTypeMousePressed, input.DispatchMouseEventTypeMouseReleased} {
			if _, err := chromedp.Call(ctx, input.DispatchMouseEvent, input.DispatchMouseEventParams{
				Type: kind, X: x, Y: y, Button: input.MouseButtonLeft, ClickCount: 1,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	s.settle(caller)
	return s.snapshotLocked(caller, 0)
}

// ErrPassword is typing into a password field, which the model never does.
var ErrPassword = errors.New("browser: password field")

// Type replaces the field's text with text, and presses Enter if submit.
func (s *Session) Type(caller context.Context, ref, text string, submit bool) (Page, error) {
	defer s.lock()()
	id, err := s.node(ref)
	if err != nil {
		return Page{}, err
	}
	err = s.within(caller, actionTimeout, func(ctx context.Context) error {
		raw, err := call(ctx, id, `function() { return (this.type || '').toLowerCase() === 'password'; }`)
		if err != nil {
			return err
		}
		if string(raw) == "true" {
			return ErrPassword
		}
		if _, err := chromedp.Call(ctx, dom.Focus, dom.FocusParams{BackendNodeID: id}); err != nil {
			return err
		}
		if _, err := call(ctx, id, `function() {
  if (typeof this.select === 'function') { this.select(); return; }
  const r = document.createRange(); r.selectNodeContents(this);
  const sel = getSelection(); sel.removeAllRanges(); sel.addRange(r);
}`); err != nil {
			return err
		}
		if _, err := chromedp.Call(ctx, input.InsertText, input.InsertTextParams{Text: text}); err != nil {
			return err
		}
		if submit {
			_, err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Enter))
			return err
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	s.settle(caller)
	return s.snapshotLocked(caller, 0)
}

// Select picks the options of a select element by value or label.
func (s *Session) Select(caller context.Context, ref string, values []string) (Page, error) {
	defer s.lock()()
	id, err := s.node(ref)
	if err != nil {
		return Page{}, err
	}
	err = s.within(caller, actionTimeout, func(ctx context.Context) error {
		raw, err := call(ctx, id, `function(values) {
  if (!this.options) return 0;
  let n = 0;
  for (const o of this.options) {
    o.selected = values.includes(o.value) || values.includes(o.label) || values.includes(o.text.trim());
    if (o.selected) n++;
  }
  this.dispatchEvent(new Event('input', { bubbles: true }));
  this.dispatchEvent(new Event('change', { bubbles: true }));
  return n;
}`, values)
		if err != nil {
			return err
		}
		if string(raw) == "0" {
			return fmt.Errorf("browser: no option of that element is %q", strings.Join(values, ", "))
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	s.settle(caller)
	return s.snapshotLocked(caller, 0)
}

// keys the press action knows.
var keys = map[string]string{
	"enter": kb.Enter, "tab": kb.Tab, "escape": kb.Escape, "backspace": kb.Backspace, "delete": kb.Delete,
	"arrowup": kb.ArrowUp, "arrowdown": kb.ArrowDown, "arrowleft": kb.ArrowLeft, "arrowright": kb.ArrowRight,
	"pageup": kb.PageUp, "pagedown": kb.PageDown, "home": kb.Home, "end": kb.End, "space": " ",
}

// KnownKey reports whether press takes key.
func KnownKey(key string) bool {
	_, ok := keys[strings.ToLower(key)]
	return ok
}

// Press presses a key on whatever has focus.
func (s *Session) Press(caller context.Context, key string) (Page, error) {
	defer s.lock()()
	k, ok := keys[strings.ToLower(key)]
	if !ok {
		return Page{}, fmt.Errorf("browser: unknown key %q", key)
	}
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		_, err := chromedp.Run(ctx, chromedp.KeyEvent(k))
		return err
	})
	if err != nil {
		return Page{}, err
	}
	s.settle(caller)
	return s.snapshotLocked(caller, 0)
}

// Scroll scrolls the page, or an element, a screen up or down.
func (s *Session) Scroll(caller context.Context, ref string, down bool) (Page, error) {
	defer s.lock()()
	sign := 1
	if !down {
		sign = -1
	}
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		if ref == "" {
			_, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf("window.scrollBy(0, %d * window.innerHeight * 0.8)", sign)))
			return err
		}
		id, err := s.node(ref)
		if err != nil {
			return err
		}
		_, err = call(ctx, id, `function(sign) { this.scrollBy(0, sign * Math.max(200, this.clientHeight * 0.8)); }`, sign)
		return err
	})
	if err != nil {
		return Page{}, err
	}
	s.settle(caller)
	return s.snapshotLocked(caller, 0)
}

// Back goes back a page.
func (s *Session) Back(caller context.Context) (Page, error) {
	defer s.lock()()
	err := s.within(caller, navTimeout, func(ctx context.Context) error {
		_, err := chromedp.Run(ctx, chromedp.NavigateBack())
		return err
	})
	if err != nil {
		return Page{}, navError(err)
	}
	return s.snapshotLocked(caller, 0)
}

// Wait waits for text to show on the page, or for d.
func (s *Session) Wait(caller context.Context, text string, d time.Duration) (Page, bool, error) {
	defer s.lock()()
	found := text == ""
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) && !found {
		var has bool
		err := s.within(caller, actionTimeout, func(ctx context.Context) error {
			expr := fmt.Sprintf("(document.body && document.body.innerText || '').toLowerCase().includes(%s)", jsString(strings.ToLower(text)))
			v, err := chromedp.Run(ctx, chromedp.Evaluate[bool](expr))
			has = v
			return err
		})
		if err != nil {
			return Page{}, false, err
		}
		if has {
			found = true
			break
		}
		select {
		case <-caller.Done():
			return Page{}, false, caller.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if text == "" {
		select {
		case <-caller.Done():
			return Page{}, false, caller.Err()
		case <-time.After(d):
		}
	}
	p, err := s.snapshotLocked(caller, 0)
	return p, found, err
}

// Screenshot is a JPEG of the screen, or of the element ref.
func (s *Session) Screenshot(caller context.Context, ref string) ([]byte, error) {
	defer s.lock()()
	var out []byte
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		params := page.CaptureScreenshotParams{Format: page.CaptureScreenshotFormatJpeg, Quality: ptr(int64(75))}
		if ref != "" {
			id, err := s.node(ref)
			if err != nil {
				return err
			}
			_, _ = chromedp.Call(ctx, dom.ScrollIntoViewIfNeeded, dom.ScrollIntoViewIfNeededParams{BackendNodeID: id})
			box, err := chromedp.Call(ctx, dom.GetBoxModel, dom.GetBoxModelParams{BackendNodeID: id})
			if err == nil && box.Model != nil && len(box.Model.Border) >= 8 {
				q := box.Model.Border
				params.Clip = &page.Viewport{X: q[0], Y: q[1], Width: q[4] - q[0], Height: q[5] - q[1], Scale: 1}
				params.CaptureBeyondViewport = ptr(true)
			}
		}
		res, err := chromedp.Call(ctx, page.CaptureScreenshot, params)
		if err != nil {
			return err
		}
		out = res.Data
		return nil
	})
	return out, err
}

// URL is the address of the page now.
func (s *Session) URL(caller context.Context) (string, error) {
	defer s.lock()()
	var url string
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		var err error
		url, err = chromedp.Run(ctx, chromedp.Location())
		return err
	})
	return url, err
}

// HTML is the page's markup and address, for reading it as text.
func (s *Session) HTML(caller context.Context) (string, string, error) {
	defer s.lock()()
	var html, url string
	err := s.within(caller, actionTimeout, func(ctx context.Context) error {
		var err error
		if html, err = chromedp.Run(ctx, chromedp.Evaluate[string]("document.documentElement ? document.documentElement.outerHTML : ''")); err != nil {
			return err
		}
		url, err = chromedp.Run(ctx, chromedp.Location())
		return err
	})
	return html, url, err
}

// settle gives the page a moment after an action - a navigation it started,
// a script it ran - and waits for a load in progress to finish.
func (s *Session) settle(caller context.Context) {
	select {
	case <-caller.Done():
		return
	case <-time.After(settleWait):
	}
	_ = s.within(caller, navTimeout, func(ctx context.Context) error {
		for i := 0; i < 40; i++ {
			state, err := chromedp.Run(ctx, chromedp.Evaluate[string]("document.readyState"))
			if err != nil || state == "complete" || state == "interactive" {
				return err
			}
			time.Sleep(250 * time.Millisecond)
		}
		return nil
	})
	// Then for what the page fetches to arrive.
	deadline := time.Now().Add(netSettleMax)
	for time.Now().Before(deadline) {
		s.evMu.Lock()
		idle := len(s.inflight) == 0 && time.Since(s.netAt) >= netQuiet
		s.evMu.Unlock()
		if idle {
			return
		}
		select {
		case <-caller.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// navError says why a page could not be opened in words for the model.
func navError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "ERR_TUNNEL_CONNECTION_FAILED"), strings.Contains(msg, "ERR_PROXY"):
		return errors.New("the page could not be reached: the address is not public, or the site is blocked")
	case strings.Contains(msg, "ERR_NAME_NOT_RESOLVED"):
		return errors.New("the page could not be reached: no such site")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("the page took too long to load")
	}
	return fmt.Errorf("the page could not be opened: %s", cut(msg, 200))
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func ptr[T any](v T) *T { return &v }
