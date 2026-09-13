package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// consoleHandler writes lines meant for a person watching a terminal.
//
// slog's TextHandler is a machine format: a full RFC3339 timestamp, a quoted
// message, and every attribute given equal weight. Reading it means skipping
// past forty characters of preamble to reach the six that matter. This puts the
// message first in the eye's path — short clock time, a padded level, then the
// text — and dims the attributes trailing it, so a line can be skimmed and only
// read in full when something looks wrong.
//
// Production still gets JSON: there the reader is a log pipeline, and its
// preferences are the opposite of a person's.
type consoleHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	colour bool

	// Accumulated through WithAttrs/WithGroup, which the logger calls to build
	// a child handler rather than mutating this one.
	attrs  []slog.Attr
	groups []string
}

const (
	ansiReset = "\033[0m"
	ansiDim   = "\033[2m"
	ansiRed   = "\033[31m"
	ansiAmber = "\033[33m"
	ansiBlue  = "\033[34m"
	ansiGrey  = "\033[90m"
)

func newConsoleHandler(w io.Writer, level slog.Leveler) *consoleHandler {
	return &consoleHandler{mu: &sync.Mutex{}, w: w, level: level, colour: isTerminal(w)}
}

// isTerminal reports whether writes go to a screen rather than a file or a
// pipe. Escape codes in a redirected log are noise that outlives the session
// they were meant to help.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	child := *h
	child.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &child
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	child := *h
	child.groups = append(append([]string{}, h.groups...), name)
	return &child
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	b.WriteString(h.paint(ansiGrey, r.Time.Format("15:04:05")))
	b.WriteByte(' ')
	b.WriteString(h.paint(levelColour(r.Level), levelLabel(r.Level)))
	b.WriteByte(' ')
	b.WriteString(r.Message)

	// Attributes are the detail behind the message, so they trail it and are
	// dimmed: present when you go looking, out of the way when you are not.
	var tail strings.Builder
	appendAttrs(&tail, h.groups, h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		appendAttrs(&tail, h.groups, []slog.Attr{a})
		return true
	})
	if tail.Len() > 0 {
		b.WriteString(h.paint(ansiDim, tail.String()))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func appendAttrs(b *strings.Builder, groups []string, attrs []slog.Attr) {
	for _, a := range attrs {
		a.Value = a.Value.Resolve()
		if a.Equal(slog.Attr{}) {
			continue
		}
		if a.Value.Kind() == slog.KindGroup {
			appendAttrs(b, append(groups, a.Key), a.Value.Group())
			continue
		}
		b.WriteByte(' ')
		if len(groups) > 0 {
			b.WriteString(strings.Join(groups, "."))
			b.WriteByte('.')
		}
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(formatValue(a.Value))
	}
}

// formatValue quotes only what would otherwise run into the next attribute, so
// the common case reads as bare text rather than as a wall of quotation marks.
func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if s == "" || strings.ContainsAny(s, " \t\"") {
			return strconv.Quote(s)
		}
		return s
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return fmt.Sprint(v.Any())
	}
}

// levelLabel pads to five so the messages after it line up into a column the
// eye can run down.
func levelLabel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO "
	case level < slog.LevelError:
		return "WARN "
	default:
		return "ERROR"
	}
}

func levelColour(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return ansiGrey
	case level < slog.LevelWarn:
		return ansiBlue
	case level < slog.LevelError:
		return ansiAmber
	default:
		return ansiRed
	}
}

func (h *consoleHandler) paint(colour, s string) string {
	if !h.colour {
		return s
	}
	return colour + s + ansiReset
}
