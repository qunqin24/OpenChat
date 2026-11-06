package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A service's tools come back with their hints; the administrator's header
// reaches the service.
func TestListTools(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "search", Description: "Search things", InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	server.AddTool(&mcp.Tool{Name: "delete_thing", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Api-Key")
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	tools, err := ListTools(context.Background(), Endpoint{URL: srv.URL, HeaderName: "X-Api-Key", HeaderValue: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if seen != "k1" {
		t.Fatalf("header reached the service as %q", seen)
	}
	byName := map[string]Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if s := byName["search"]; !s.ReadOnly || s.Destructive || s.Description != "Search things" {
		t.Fatalf("search: %+v", s)
	}
	if d := byName["delete_thing"]; d.ReadOnly || !d.Destructive {
		t.Fatalf("delete_thing: %+v", d)
	}
}

// A service that answers 401 or 403 refused the credential; one that fails
// otherwise did not say anything about it.
func TestListToolsRefused(t *testing.T) {
	for _, tc := range []struct {
		status  int
		refused bool
	}{{http.StatusUnauthorized, true}, {http.StatusForbidden, false}, {http.StatusInternalServerError, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no", tc.status)
		}))
		_, err := ListTools(context.Background(), Endpoint{URL: srv.URL, HeaderName: "Authorization", HeaderValue: "Bearer x"})
		srv.Close()
		if err == nil || errors.Is(err, ErrRefused) != tc.refused {
			t.Errorf("status %d: %v, refused want %v", tc.status, err, tc.refused)
		}
		// 403 is this request not being allowed, which says nothing of the token.
		if tc.status == http.StatusForbidden && !errors.Is(err, ErrForbidden) {
			t.Errorf("403: %v, want ErrForbidden", err)
		}
	}
}

// A service may not send the credential on to another origin: the header is
// put on every hop, so a redirect elsewhere is refused before it is followed.
func TestListToolsKeepsTheCredentialToItsService(t *testing.T) {
	var leaked atomic.Value
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer elsewhere.Close()
	// Another host name for the same machine: a different origin.
	other := strings.Replace(elsewhere.URL, "127.0.0.1", "localhost", 1)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other+"/mcp", http.StatusTemporaryRedirect)
	}))
	defer service.Close()

	_, err := ListTools(context.Background(), Endpoint{URL: service.URL, HeaderName: "Authorization", HeaderValue: "Bearer secret-token"})
	if err == nil {
		t.Fatal("followed a redirect to another origin")
	}
	if got, _ := leaked.Load().(string); got != "" {
		t.Fatalf("the other origin received %q", got)
	}
}

// What a service says back is cleared of the credential before it reaches a
// log or the model, however it quotes it.
func TestListToolsScrubsTheCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad header: ` + r.Header.Get("Authorization") + `"}}`))
	}))
	defer srv.Close()
	_, err := ListTools(context.Background(), Endpoint{URL: srv.URL, HeaderName: "Authorization", HeaderValue: "Bearer secret-token"})
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("the error carries the token: %v", err)
	}
	refusedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token "+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")+" is invalid", http.StatusUnauthorized)
	}))
	defer refusedSrv.Close()
	_, err = ListTools(context.Background(), Endpoint{URL: refusedSrv.URL, HeaderName: "Authorization", HeaderValue: "Bearer secret-token"})
	if strings.Contains(fmt.Sprint(err), "secret-token") || !errors.Is(err, ErrRefused) {
		t.Fatalf("a refusal quoting the token: %v (refused %v)", err, errors.Is(err, ErrRefused))
	}
}
