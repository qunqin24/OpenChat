package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter emits server-sent events. Every frame is flushed immediately so the
// client renders tokens as they arrive.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter sets the streaming headers and returns a writer, or an error if
// the ResponseWriter cannot flush.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, NewError(http.StatusInternalServerError, "streaming_unsupported", "response writer does not support streaming")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &SSEWriter{w: w, flusher: flusher}, nil
}

// Event marshals v as JSON and writes it as one `data:` frame.
func (s *SSEWriter) Event(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sse: marshal event: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", payload); err != nil {
		return fmt.Errorf("sse: write event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// Comment writes a keep-alive comment frame.
func (s *SSEWriter) Comment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
