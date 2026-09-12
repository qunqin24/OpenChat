// Package chat defines the model-provider seam. The HTTP layer only ever sees
// Provider, so swapping the echo stub for a real LLM client is a one-line change
// in main.go.
package chat

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	Model    string
	Messages []Message
}

// Provider streams an assistant reply. emit is called once per token-ish chunk;
// returning an error from emit (e.g. the client disconnected) aborts the stream.
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request, emit func(delta string) error) error
}

// Echo is a dependency-free provider that streams the user's last message back
// word by word. It keeps the scaffold runnable before any model key exists.
type Echo struct {
	// Delay between chunks. Zero streams as fast as the client can read.
	Delay time.Duration
}

func (e Echo) Name() string { return "echo" }

func (e Echo) Stream(ctx context.Context, req Request, emit func(string) error) error {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == RoleUser {
			last = req.Messages[i].Content
			break
		}
	}

	reply := fmt.Sprintf("You said: %s\n\n(This is the `echo` provider. Wire a real model in `cmd/openchat/main.go`.)", last)
	for i, word := range strings.Fields(reply) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chunk := word
		if i > 0 {
			chunk = " " + word
		}
		if err := emit(chunk); err != nil {
			return err
		}
		if e.Delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(e.Delay):
			}
		}
	}
	return nil
}
