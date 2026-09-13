// Package sse reads the server-sent events half of a streaming HTTP response.
//
// This is the client side. httpx writes SSE to browsers; this reads it back
// from model endpoints, which is a different job with a different failure mode:
// a browser that stops reading is routine, a model endpoint that stops writing
// mid-frame means the answer is incomplete and nobody must be told otherwise.
package sse

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaxEventBytes caps the data of a single event. Tool arguments and
// echoed attachments are the two things that grow without a natural bound, and
// an endpoint that never sends a blank line would otherwise buffer until the
// process dies.
const DefaultMaxEventBytes = 8 << 20

var (
	// ErrTruncated is a stream that ended in the middle of a line.
	ErrTruncated = errors.New("sse: stream ended mid-line")

	// ErrEventTooLarge is a single event larger than the limit.
	ErrEventTooLarge = errors.New("sse: event exceeds the size limit")
)

// Event is one dispatched frame. Name is the `event:` field, empty when the
// endpoint does not use one; Data is the `data:` lines joined with newlines,
// exactly as the spec requires.
type Event struct {
	Name string
	Data string
}

type Scanner struct {
	r    *bufio.Reader
	max  int
	ev   Event
	err  error
	done bool
}

func NewScanner(r io.Reader) *Scanner {
	return &Scanner{r: bufio.NewReader(r), max: DefaultMaxEventBytes}
}

// SetMaxEventBytes overrides the per-event limit. Zero or negative removes it,
// which is only ever appropriate in a test that controls the input.
func (s *Scanner) SetMaxEventBytes(n int) { s.max = n }

// Event returns the frame the last successful Scan produced.
func (s *Scanner) Event() Event { return s.ev }

// Err returns why scanning stopped. A clean end of stream reports nil.
func (s *Scanner) Err() error { return s.err }

// Scan reads until the next complete frame. It returns false at the end of the
// stream and on error; the two are told apart by Err.
func (s *Scanner) Scan() bool {
	if s.err != nil || s.done {
		return false
	}

	var (
		data    strings.Builder
		name    string
		started bool
		size    int
	)

	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.err = err
				return false
			}
			s.done = true
			if line != "" {
				// Bytes with no newline behind them: the connection died while
				// the frame was still being written. Dispatching what arrived
				// would turn a cut-off stream into a short answer.
				s.err = fmt.Errorf("%w: %q", ErrTruncated, clip(line))
				return false
			}
			if !started {
				return false
			}
			// A complete final frame missing only its blank separator is common
			// enough in the wild to accept: every line arrived intact.
			s.ev = Event{Name: name, Data: data.String()}
			return true
		}

		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if !started {
				continue // padding between frames
			}
			s.ev = Event{Name: name, Data: data.String()}
			return true
		}
		if strings.HasPrefix(line, ":") {
			continue // comment, which is how most endpoints send a heartbeat
		}

		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			size += len(value) + 1
			if s.max > 0 && size > s.max {
				s.err = fmt.Errorf("%w: %d bytes", ErrEventTooLarge, size)
				return false
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			started = true
		case "event":
			name = value
			started = true
		default:
			// id and retry drive EventSource reconnection, which we do not use:
			// resuming a model stream is the run layer's cursor protocol, not
			// something a transport may do behind its back.
		}
	}
}

func clip(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
