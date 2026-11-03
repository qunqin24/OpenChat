package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallTimeout bounds one tool call, the request and its answer.
const CallTimeout = 60 * time.Second

// Result is what a call answered, as a script or a model reads it.
type Result struct {
	// Value is the structured result when the service gave one, else the
	// text: parsed when it is JSON, a string otherwise.
	Value any
	// Text is the text content joined, for the model when nothing is structured.
	Text string
	// IsError is the service saying the call failed; Text says why.
	IsError bool
	// Dropped counts content that is not text (images, audio, resources),
	// which is not passed on yet.
	Dropped int
}

// Conn is one session with a service, opened at its first call and shared by
// the calls of one script.
type Conn struct {
	end     Endpoint
	mu      sync.Mutex
	session *mcp.ClientSession
}

// Open prepares a connection; nothing is sent until the first call.
func Open(end Endpoint) *Conn { return &Conn{end: end} }

// Call calls a tool with its arguments, a JSON object.
func (c *Conn) Call(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	session, err := c.open(ctx)
	if err != nil {
		return Result{}, err
	}
	var arguments any = map[string]any{}
	if len(args) > 0 {
		arguments = args
	}
	out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return Result{}, fmt.Errorf("calling %s: %w", name, err)
	}
	return toResult(out), nil
}

// Close ends the session, if one was opened.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
}

func (c *Conn) open(ctx context.Context) (*mcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		return c.session, nil
	}
	session, err := connect(ctx, c.end, CallTimeout)
	if err != nil {
		return nil, err
	}
	c.session = session
	return session, nil
}

func toResult(out *mcp.CallToolResult) Result {
	var texts []string
	dropped := 0
	for _, content := range out.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			texts = append(texts, text.Text)
			continue
		}
		dropped++
	}
	r := Result{Text: strings.Join(texts, "\n"), IsError: out.IsError, Dropped: dropped}
	switch {
	case out.StructuredContent != nil && !out.IsError:
		r.Value = out.StructuredContent
	case json.Valid([]byte(strings.TrimSpace(r.Text))) && strings.TrimSpace(r.Text) != "":
		_ = json.Unmarshal([]byte(r.Text), &r.Value)
	default:
		r.Value = r.Text
	}
	return r
}
