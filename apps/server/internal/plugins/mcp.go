// Package plugins speaks MCP to the services plugins reach
// (docs/design/plugins-plan.md): for now, listing a service's tools for the
// administrators' page; calling them from conversations builds on it.
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listTimeout bounds connecting to a service and listing its tools.
const listTimeout = 30 * time.Second

// maxTools is how many tools of one service are kept: a service listing more
// is not one plugin's worth (the model is offered at most 40 of them).
const maxTools = 200

// Tool is one tool as the service describes it.
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema json.RawMessage
	// ReadOnly is the service's readOnlyHint; Destructive its destructiveHint,
	// true unless it says otherwise, as the protocol defaults it.
	ReadOnly    bool
	Destructive bool
}

// Endpoint is how to reach a service: its address and the header an
// administrator set for everyone, if any.
type Endpoint struct {
	URL         string
	HeaderName  string
	HeaderValue string
	// Version is OpenChat's, told to the service as the client's.
	Version string
}

// ListTools connects to the service over Streamable HTTP and lists its tools.
// The address is an administrator's and may be on the internal network.
func ListTools(ctx context.Context, end Endpoint) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	session, err := connect(ctx, end)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	var out []Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("listing tools: %w", err)
		}
		if len(out) == maxTools {
			return nil, fmt.Errorf("the service lists more than %d tools", maxTools)
		}
		out = append(out, toTool(tool))
	}
	return out, nil
}

func connect(ctx context.Context, end Endpoint) (*mcp.ClientSession, error) {
	httpClient := &http.Client{Timeout: listTimeout}
	if end.HeaderValue != "" {
		httpClient.Transport = withHeader{name: end.HeaderName, value: end.HeaderValue, next: http.DefaultTransport}
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   end.URL,
		HTTPClient: httpClient,
		// Requests and their answers only: nothing here listens for the
		// service's own messages yet.
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	version := end.Version
	if version == "" {
		version = "dev"
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "OpenChat", Version: version}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}
	return session, nil
}

func toTool(t *mcp.Tool) Tool {
	out := Tool{Name: t.Name, Title: t.Title, Description: strings.TrimSpace(t.Description), Destructive: true}
	if t.Annotations != nil {
		out.ReadOnly = t.Annotations.ReadOnlyHint
		if t.Annotations.DestructiveHint != nil {
			out.Destructive = *t.Annotations.DestructiveHint
		}
		if out.Title == "" {
			out.Title = t.Annotations.Title
		}
	}
	if out.ReadOnly {
		out.Destructive = false
	}
	out.InputSchema = json.RawMessage(`{"type":"object"}`)
	if t.InputSchema != nil {
		if raw, err := json.Marshal(t.InputSchema); err == nil {
			out.InputSchema = raw
		}
	}
	return out
}

// withHeader adds the administrator's header to every request.
type withHeader struct {
	name, value string
	next        http.RoundTripper
}

func (h withHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(h.name, h.value)
	return h.next.RoundTrip(r)
}
