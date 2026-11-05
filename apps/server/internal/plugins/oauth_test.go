package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// A service on the administrator's (internal) host is discovered in full when
// everything it names stays on that host.
func TestDiscoverOnTheServiceHost(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": srv.URL + "/mcp", "authorization_servers": []string{srv.URL}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token",
			})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	found, err := Discover(context.Background(), srv.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if found.Server.TokenEndpoint != srv.URL+"/token" {
		t.Fatalf("token endpoint = %q", found.Server.TokenEndpoint)
	}
}

// The service's word cannot send the server anywhere else on the internal
// network: not by a redirect to another port of an internal host, nor by an
// https address that resolves inward.
func TestDiscoverRefusesInternalRedirects(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{"https://example.com"}})
	}))
	defer internal.Close()
	internalTLS := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer internalTLS.Close()

	for name, target := range map[string]string{
		"http on another port":   internal.URL + "/.well-known/oauth-protected-resource",
		"https resolving inward": strings.Replace(internalTLS.URL, "127.0.0.1", "localhost", 1) + "/meta",
	} {
		t.Run(name, func(t *testing.T) {
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/.well-known/") {
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer service.Close()

			if _, err := Discover(context.Background(), service.URL+"/mcp"); err == nil {
				t.Fatal("discovery followed the service elsewhere")
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("the internal server was reached %d times", n)
			}
		})
	}
}

// Token requests go through the same guard: a token endpoint that is not on
// the service's host and is not public is never dialled.
func TestTokenRequestsStayGuarded(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer internal.Close()
	service := httptest.NewServer(http.NotFoundHandler())
	defer service.Close()

	var err error
	found := Discovery{Server: ServerMeta{TokenEndpoint: internal.URL + "/token", AuthorizationEndpoint: internal.URL + "/authorize"}}
	if found.service, err = url.Parse(service.URL); err != nil {
		t.Fatal(err)
	}
	client := Client{Discovery: found, ClientID: "id"}
	if _, err := client.Exchange(context.Background(), "code", "verifier"); err == nil {
		t.Fatal("exchange succeeded against an internal token endpoint")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the internal token endpoint was reached %d times", n)
	}
}
