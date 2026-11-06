// Package plugins speaks MCP to the services plugins reach
// (docs/design/plugins-plan.md): listing a service's tools for the
// administrators' page, and calling them from conversations (call.go).
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listTimeout bounds connecting to a service and listing its tools.
const listTimeout = 30 * time.Second

// ErrRefused is a service answering 401: it would not take the credential it
// was sent (a person's token revoked, run out, or never valid).
var ErrRefused = errors.New("the service refused the credential")

// ErrForbidden is a service answering 403: the credential is good but may not
// do this - a token without the scope a tool needs (MCP's insufficient_scope)
// - which says nothing about the rest of what it may do.
var ErrForbidden = errors.New("the service forbade this with the credential")

// maxTools is how many tools of one service are kept: a service listing more
// is not one plugin's worth.
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
	tools, err := listTools(ctx, end)
	return tools, scrub(err, end)
}

func listTools(ctx context.Context, end Endpoint) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	var refused atomic.Int32
	session, err := connect(ctx, end, listTimeout, &refused)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	var out []Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, refusal(fmt.Errorf("listing tools: %w", err), &refused)
		}
		if len(out) == maxTools {
			return nil, fmt.Errorf("the service lists more than %d tools", maxTools)
		}
		out = append(out, toTool(tool))
	}
	return out, nil
}

// connect opens a session; timeout bounds each HTTP request of it, and
// refused keeps the last 401 or 403 the service answered any of them with.
func connect(ctx context.Context, end Endpoint, timeout time.Duration, refused *atomic.Int32) (*mcp.ClientSession, error) {
	version := end.Version
	if version == "" {
		version = "dev"
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "OpenChat", Version: version}, nil)
	if IsLocal(end.URL) {
		return connectLocal(ctx, client, end, version)
	}
	var next http.RoundTripper = http.DefaultTransport
	if end.HeaderValue != "" {
		next = withHeader{name: end.HeaderName, value: end.HeaderValue, next: next}
	}
	httpClient := &http.Client{Timeout: timeout, Transport: refusalWatch{next: next, refused: refused}, CheckRedirect: sameOrigin}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   end.URL,
		HTTPClient: httpClient,
		// Requests and their answers only: nothing here listens for the
		// service's own messages yet.
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, refusal(fmt.Errorf("connecting: %w", err), refused)
	}
	return session, nil
}

// sameOrigin lets a service redirect only within its own origin, and never
// from https down to http: withHeader puts the credential on every hop, and a
// person's token must reach no one but the service it was given for.
func sameOrigin(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		return fmt.Errorf("the service redirected to another origin (%s://%s)", req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// refusal is err, marked ErrRefused or ErrForbidden when the service answered
// 401 or 403 on the way to it.
func refusal(err error, refused *atomic.Int32) error {
	switch refused.Load() {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %w", ErrRefused, err)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %w", ErrForbidden, err)
	}
	return err
}

// refusalWatch notes a service answering 401 or 403.
type refusalWatch struct {
	next    http.RoundTripper
	refused *atomic.Int32
}

func (w refusalWatch) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := w.next.RoundTrip(r)
	if err == nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		w.refused.Store(int32(resp.StatusCode))
	}
	return resp, err
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
