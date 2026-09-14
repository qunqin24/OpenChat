package modellist

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testKey is deliberately distinctive so a test can assert it appears nowhere
// it should not — in an error, in a redirected request, in a log line.
const testKey = "sk-test-CREDENTIAL-1234"

// listFrom points a kind at a test server and lists it. Every test goes through
// For, so the checks it makes — the key, the base URL, the redirect policy —
// are part of what is exercised rather than bypassed.
func listFrom(t *testing.T, kind Kind, srv *httptest.Server) ([]Model, error) {
	t.Helper()

	lister, err := For(kind, Config{BaseURL: srv.URL, APIKey: testKey, HTTP: srv.Client()})
	if err != nil {
		return nil, err
	}
	return lister.List(context.Background())
}

func ids(models []Model) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}

func TestOpenAIList(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"gpt-4o","owned_by":"openai"},
			{"id":"o3-mini","owned_by":"openai"},
			{"id":"","owned_by":"nobody"},
			{"id":"gpt-4o","owned_by":"openai"}]}`))
	}))
	defer srv.Close()

	models, err := listFrom(t, KindOpenAI, srv)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if gotPath != "/models" {
		t.Errorf("path = %q, want /models", gotPath)
	}
	if gotAuth != "Bearer "+testKey {
		t.Errorf("Authorization = %q, want a bearer token", gotAuth)
	}
	// The nameless entry is dropped and the repeat is collapsed: neither is
	// addressable as a distinct model.
	if got := ids(models); len(got) != 2 || got[0] != "gpt-4o" || got[1] != "o3-mini" {
		t.Fatalf("ids = %v", got)
	}
	// This endpoint publishes no display name, and the adapter does not invent
	// one from the id.
	if models[0].DisplayName != "" {
		t.Errorf("displayName = %q, want empty", models[0].DisplayName)
	}
}

func TestAnthropicListPages(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)

		if r.Header.Get("x-api-key") != testKey {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		// A bearer token here would mean the adapter borrowed OpenAI's header.
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization was sent to the Anthropic endpoint")
		}

		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[
				{"type":"model","id":"claude-opus-4","display_name":"Claude Opus 4"}],
				"has_more":true,"last_id":"claude-opus-4"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"type":"model","id":"claude-haiku-4","display_name":"Claude Haiku 4"},
			{"type":"something-else","id":"not-a-model"}],
			"has_more":false,"last_id":"claude-haiku-4"}`))
	}))
	defer srv.Close()

	models, err := listFrom(t, KindAnthropic, srv)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("made %d requests, want 2: %v", len(requests), requests)
	}
	if !strings.Contains(requests[1], "after_id=claude-opus-4") {
		t.Errorf("second page did not follow the cursor: %q", requests[1])
	}
	if got := ids(models); len(got) != 2 || got[1] != "claude-haiku-4" {
		t.Fatalf("ids = %v", got)
	}
	if models[0].DisplayName != "Claude Opus 4" {
		t.Errorf("displayName = %q", models[0].DisplayName)
	}
}

// An endpoint that always says there is more must not be followed forever.
func TestAnthropicStopsPaging(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		// A fresh cursor every time, so the "cursor did not move" guard does not
		// catch this one — only the hard cap does.
		_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"m` +
			r.URL.Query().Get("after_id") + `"}],"has_more":true,"last_id":"m` +
			r.URL.Query().Get("after_id") + `"}`))
	}))
	defer srv.Close()

	_, err := listFrom(t, KindAnthropic, srv)
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("err = %v, want ErrTooManyPages", err)
	}
	if requests != maxPages {
		t.Errorf("made %d requests, want the cap of %d", requests, maxPages)
	}
}

// "has_more" with a cursor that does not move is an endpoint contradicting
// itself; following it would re-read the same page until the deadline.
func TestAnthropicStopsOnStuckCursor(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"claude-x"}],
			"has_more":true,"last_id":""}`))
	}))
	defer srv.Close()

	models, err := listFrom(t, KindAnthropic, srv)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1", requests)
	}
	if got := ids(models); len(got) != 1 {
		t.Errorf("ids = %v", got)
	}
}

func TestGeminiListPages(t *testing.T) {
	var requests []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r)

		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"models":[
				{"name":"models/gemini-2.0-flash","displayName":"Gemini 2.0 Flash",
				 "supportedGenerationMethods":["generateContent","countTokens"]},
				{"name":"models/text-embedding-004","displayName":"Embedding",
				 "supportedGenerationMethods":["embedContent"]}],
				"nextPageToken":"page-2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[
			{"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro"}]}`))
	}))
	defer srv.Close()

	models, err := listFrom(t, KindGemini, srv)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("made %d requests, want 2", len(requests))
	}

	// The key rides in the query string for this vendor, and nowhere else.
	if got := requests[0].URL.Query().Get("key"); got != testKey {
		t.Errorf("key query = %q", got)
	}
	if requests[0].Header.Get("Authorization") != "" || requests[0].Header.Get("x-api-key") != "" {
		t.Error("the key was also sent as a header")
	}
	if got := requests[1].URL.Query().Get("pageToken"); got != "page-2" {
		t.Errorf("pageToken = %q, want page-2", got)
	}

	// The `models/` prefix is how the API spells the resource, not part of the
	// name; the embedding model is filtered out because it cannot be chatted
	// with; the entry that lists no methods is kept, because saying nothing is
	// not the same as saying no.
	want := []string{"gemini-2.0-flash", "gemini-2.5-pro"}
	got := ids(models)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if models[0].DisplayName != "Gemini 2.0 Flash" {
		t.Errorf("displayName = %q", models[0].DisplayName)
	}
}

func TestGeminiStopsPaging(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-x"}],
			"nextPageToken":"t` + r.URL.Query().Get("pageToken") + `"}`))
	}))
	defer srv.Close()

	if _, err := listFrom(t, KindGemini, srv); !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("err = %v, want ErrTooManyPages", err)
	}
	if requests != maxPages {
		t.Errorf("made %d requests, want the cap of %d", requests, maxPages)
	}
}

// A redirect to another host is how a request the operator authorised ends up
// somewhere they did not, with their key replayed onto the hop.
func TestRefusesRedirectToAnotherHost(t *testing.T) {
	var elsewhereSaw *http.Request
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereSaw = r
		_, _ = w.Write([]byte(`{"data":[{"id":"stolen"}]}`))
	}))
	defer elsewhere.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/models", http.StatusFound)
	}))
	defer srv.Close()

	for _, kind := range []Kind{KindOpenAI, KindAnthropic, KindGemini} {
		t.Run(string(kind), func(t *testing.T) {
			elsewhereSaw = nil

			_, err := listFrom(t, kind, srv)
			if !errors.Is(err, ErrRedirect) {
				t.Fatalf("err = %v, want ErrRedirect", err)
			}
			if elsewhereSaw != nil {
				t.Fatal("the redirect target was called; the key may have travelled with it")
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("the error carries the key: %v", err)
			}
		})
	}
}

// Same-host redirects are ordinary — a path normalised, a slash added — and
// stay allowed, because the key goes nowhere new.
func TestFollowsRedirectOnTheSameHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.Redirect(w, r, "/v9/models", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	models, err := listFrom(t, KindOpenAI, srv)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := ids(models); len(got) != 1 || got[0] != "gpt-4o" {
		t.Fatalf("ids = %v", got)
	}
}

// countingTransport fails the test if anything is sent, which is how "before
// any request goes out" is checked rather than assumed.
type countingTransport struct{ t *testing.T }

func (c countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.t.Errorf("a request was made to %s", req.URL.Host)
	return nil, errors.New("no requests expected")
}

func TestMissingKeyFailsBeforeAnyRequest(t *testing.T) {
	client := &http.Client{Transport: countingTransport{t}}

	for _, kind := range []Kind{KindOpenAI, KindAnthropic, KindGemini} {
		lister, err := For(kind, Config{BaseURL: "https://example.invalid/v1", APIKey: "  ", HTTP: client})
		if !errors.Is(err, ErrNoKey) {
			t.Errorf("%s: err = %v, want ErrNoKey", kind, err)
		}
		if lister != nil {
			t.Errorf("%s: got a lister for a channel with no key", kind)
		}
	}
}

func TestRejectsBaseURL(t *testing.T) {
	client := &http.Client{Transport: countingTransport{t}}

	cases := map[string]string{
		"a file path":      "file:///etc/passwd",
		"no scheme":        "api.openai.com/v1",
		"a made-up scheme": "gopher://example.com",
		"no host":          "https:///models",
	}
	for name, base := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := For(KindOpenAI, Config{BaseURL: base, APIKey: testKey, HTTP: client})
			if !errors.Is(err, ErrBaseURL) {
				t.Fatalf("err = %v, want ErrBaseURL", err)
			}
		})
	}
}

// An empty base URL means the vendor's own endpoint, which is knowledge the
// adapter holds so the channel row does not have to.
func TestDefaultBaseURLs(t *testing.T) {
	cases := map[Kind]string{
		KindOpenAI:    "api.openai.com",
		KindAnthropic: "api.anthropic.com",
		KindGemini:    "generativelanguage.googleapis.com",
	}
	for kind, host := range cases {
		built, err := For(kind, Config{APIKey: testKey})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := built.(lister).fetcher.base.Host; got != host {
			t.Errorf("%s: base host = %q, want %q", kind, got, host)
		}
	}
}

// The operator is told the status; the body stays out of the error they see and
// the key stays out of the one we log.
func TestStatusErrorKeepsTheBodyOutOfTheMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// A gateway echoing the request back, credential included, is exactly
		// the case the redaction exists for.
		_, _ = w.Write([]byte("{\"error\":\"invalid key " + testKey + "\"}\n<html>"))
	}))
	defer srv.Close()

	_, err := listFrom(t, KindOpenAI, srv)

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v, want a *StatusError", err)
	}
	if statusErr.StatusCode != http.StatusUnauthorized || !statusErr.Unauthorized() {
		t.Errorf("status = %d", statusErr.StatusCode)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("the error carries the key: %v", err)
	}
	if !strings.Contains(statusErr.Snippet, "[redacted]") {
		t.Errorf("snippet = %q, want the key replaced", statusErr.Snippet)
	}
}

// Gemini puts the key in the URL, so a transport failure — whose error quotes
// that URL — is the one place a credential leaks by accident.
func TestTransportErrorRedactsTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // nothing is listening, so the request fails at the dial

	_, err := listFrom(t, KindGemini, srv)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("the error carries the key: %v", err)
	}
	if !strings.Contains(err.Error(), "key=redacted") {
		t.Errorf("err = %v, want the query redacted", err)
	}
}

func TestUnreadableBodyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()

	if _, err := listFrom(t, KindOpenAI, srv); !errors.Is(err, ErrResponse) {
		t.Fatalf("err = %v, want ErrResponse", err)
	}
}

func TestUnknownKind(t *testing.T) {
	if _, err := For(Kind("mystery"), Config{APIKey: testKey}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("err = %v, want ErrUnknownKind", err)
	}
}
