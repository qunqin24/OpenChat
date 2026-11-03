package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return &mcp.CallToolResult{}, nil })
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
