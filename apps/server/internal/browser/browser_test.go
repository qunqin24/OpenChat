package browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/tools"
)

// chromium is a headless Chromium to test against: BROWSER_TEST_EXEC, or one
// Playwright installed. The tests skip without one.
func chromium(t *testing.T) string {
	if path := os.Getenv("BROWSER_TEST_EXEC"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, pattern := range []string{
		filepath.Join(home, "Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-*/chrome-headless-shell"),
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-*/chrome-headless-shell"),
	} {
		if found, _ := filepath.Glob(pattern); len(found) > 0 {
			return found[len(found)-1]
		}
	}
	t.Skip("no headless Chromium (set BROWSER_TEST_EXEC)")
	return ""
}

const shop = `<!doctype html><html><head><title>Test shop</title></head><body>
<h1>Shop</h1>
<form action="/search"><input name="q" aria-label="Search"><button>Go</button></form>
<a href="/page2" target="_blank">Next page</a>
<select aria-label="Size"><option>S</option><option value="m">Medium</option></select>
<form action="/login" method="post"><input type="password" aria-label="Password"><button type="submit">Sign in</button></form>
<div id="later"></div>
<script>setTimeout(() => { document.getElementById('later').textContent = 'Loaded later'; }, 300)</script>
</body></html>`

func testSite(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(shop)) })
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<title>Results</title><h1>Results for %s</h1>`, r.URL.Query().Get("q"))
	})
	mux.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<title>Two</title><h1>Page two</h1>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func ref(t *testing.T, snapshot, contains string) string {
	t.Helper()
	for _, l := range strings.Split(snapshot, "\n") {
		if strings.Contains(l, contains) {
			if i := strings.Index(l, "[ref="); i >= 0 {
				return strings.TrimSuffix(l[i+5:], "]")[:strings.Index(l[i+5:], "]")]
			}
		}
	}
	t.Fatalf("no line with %q in\n%s", contains, snapshot)
	return ""
}

func TestSessionActions(t *testing.T) {
	exec := chromium(t)
	site := testSite(t)
	pool := NewPool()
	t.Cleanup(pool.Close)
	pool.Configure(Config{Endpoint: "exec:" + exec})
	ctx := context.Background()
	s, err := pool.Open(uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}

	page, err := s.Open(ctx, site.URL)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Test shop" || !strings.Contains(page.Snapshot, `heading "Shop" [level=1]`) || !strings.Contains(page.Snapshot, `link "Next page"`) {
		t.Fatalf("snapshot:\n%s", page.Snapshot)
	}

	// Typing and submitting goes where the form says.
	page, err = s.Type(ctx, ref(t, page.Snapshot, `textbox "Search"`), "red shoes", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Snapshot, "Results for red shoes") {
		t.Fatalf("after search:\n%s", page.Snapshot)
	}
	if page, err = s.Back(ctx); err != nil || page.Title != "Test shop" {
		t.Fatalf("back: %v %s", err, page.Title)
	}

	// A refused password field, and a select by label.
	if _, err := s.Type(ctx, ref(t, page.Snapshot, `textbox "Password"`), "hunter2", false); !errors.Is(err, ErrPassword) {
		t.Fatalf("password: %v", err)
	}
	if page, err = s.Select(ctx, ref(t, page.Snapshot, `combobox "Size"`), []string{"Medium"}); err != nil || !strings.Contains(page.Snapshot, `combobox "Size" [value="Medium"]`) {
		t.Fatalf("select: %v\n%s", err, page.Snapshot)
	}

	// What a ref points at, for the confirmation rules.
	el, err := s.Describe(ctx, ref(t, page.Snapshot, `button "Sign in"`))
	if err != nil || !el.InForm || !el.Password || el.Type != "submit" {
		t.Fatalf("describe: %+v %v", el, err)
	}

	// Waiting for text a script adds; finding a line; a stale ref.
	if _, found, err := s.Wait(ctx, "loaded later", 5*time.Second); err != nil || !found {
		t.Fatalf("wait: %v %v", found, err)
	}
	if out, n, err := s.Find(ctx, "next page"); err != nil || n != 1 || !strings.Contains(out, `link "Next page"`) {
		t.Fatalf("find: %d %v\n%s", n, err, out)
	}
	if _, err := s.Click(ctx, "e999"); !errors.Is(err, ErrStaleRef) {
		t.Fatalf("stale ref: %v", err)
	}

	// A link that opens a new tab stays in this one.
	if page, err = s.Click(ctx, ref(t, page.Snapshot, `link "Next page"`)); err != nil || page.Title != "Two" {
		t.Fatalf("new-tab link: %v %q", err, page.Title)
	}
	if shot, err := s.Screenshot(ctx, ""); err != nil || len(shot) < 1000 {
		t.Fatalf("screenshot: %d %v", len(shot), err)
	}
}

func TestProxyKeepsTheBrowserOffTheLocalNetwork(t *testing.T) {
	exec := chromium(t)
	site := testSite(t)
	proxy := &Proxy{Blocklist: func() tools.Blocklist { return tools.Blocklist{} }}
	if err := proxy.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	pool := NewPool()
	t.Cleanup(pool.Close)
	pool.Configure(Config{Endpoint: "exec:" + exec, ProxyURL: "http://" + proxy.Addr().String()})
	s, err := pool.Open(uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	// The test site is on loopback: through the proxy, it is not public.
	if p, err := s.Open(context.Background(), site.URL); err == nil || !strings.Contains(err.Error(), "not public") {
		t.Fatalf("a loopback page opened: %v %q %q\n%s", err, p.URL, p.Title, p.Snapshot)
	}
}

func TestProxyChecks(t *testing.T) {
	var dialled []string
	proxy := &Proxy{
		Blocklist: func() tools.Blocklist { b, _ := tools.ParseBlocklist([]string{"blocked.example"}); return b },
		Refuse:    func(host string) bool { return host == "chat.example" },
		Dial: func(_ context.Context, _, address string) (net.Conn, error) {
			dialled = append(dialled, address)
			return nil, errors.New("not dialling in a test")
		},
	}
	if err := proxy.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	status := func(target string) string {
		conn, err := net.Dial("tcp", proxy.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		line, _ := bufio.NewReader(conn).ReadString('\n')
		return strings.TrimSpace(line)
	}
	for target, want := range map[string]string{
		"blocked.example:443": "403", "chat.example:443": "403", "example.com:22": "403", "example.com:443": "502",
	} {
		if got := status(target); !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %s", target, got, want)
		}
	}
	if len(dialled) != 1 || dialled[0] != "example.com:443" {
		t.Errorf("dialled %v", dialled)
	}
	// Only the browser may use it: another source is dropped.
	if proxy.allowedSource(&net.TCPAddr{IP: net.ParseIP("10.1.2.3")}) {
		t.Error("a stranger may use the proxy")
	}
	proxy.Source = netip.MustParsePrefix("10.1.0.0/16").Contains
	if !proxy.allowedSource(&net.TCPAddr{IP: net.ParseIP("10.1.2.3")}) {
		t.Error("the browser's network may not use the proxy")
	}
}
