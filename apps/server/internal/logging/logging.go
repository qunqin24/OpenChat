// Package logging owns the process's log level, which the admin panel can
// change while the server is running.
//
// A restart to raise the level is the wrong tool when the thing you are chasing
// is intermittent: by the time you have redeployed, the request you wanted is
// gone. slog.LevelVar exists for exactly this, and is safe to write from a
// handler while other goroutines log.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
)

var level = new(slog.LevelVar)

// File is where a copy of the log goes, if anywhere.
type File struct {
	Path     string // empty: stdout only
	MaxBytes int64  // the size at which it starts over
	Keep     int    // how many earlier files are kept beside it
}

// Setup installs the process logger. `json` picks the production format; the
// level starts at Info either way and moves only through SetDebug. With a file
// the same JSON lines go there too, rotated by size; a file that cannot be
// opened is reported and skipped rather than stopping the server.
func Setup(json bool, file File) {
	level.Set(slog.LevelInfo)

	// JSON in production, where the reader is a log pipeline. A terminal gets
	// the console handler instead; slog's TextHandler serves neither well.
	var handler slog.Handler = newConsoleHandler(os.Stdout, level)
	if json {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	var fileErr error
	if file.Path != "" {
		var out *rotatingFile
		if out, fileErr = openRotating(file.Path, file.MaxBytes, file.Keep); fileErr == nil {
			fileHandler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})
			if json {
				handler = slog.NewJSONHandler(io.MultiWriter(os.Stdout, out), &slog.HandlerOptions{Level: level})
			} else {
				handler = fanout{handler, fileHandler}
			}
		}
	}
	slog.SetDefault(slog.New(contextHandler{handler}))
	if fileErr != nil {
		slog.Error("log file unavailable; logging to stdout only", "path", file.Path, "error", fileErr)
	}
}

// SetDebug raises the level to Debug, or drops it back to Info.
func SetDebug(on bool) {
	if on {
		level.Set(slog.LevelDebug)
		return
	}
	level.Set(slog.LevelInfo)
}

// Debug reports whether debug logging is currently on.
func Debug() bool { return level.Level() == slog.LevelDebug }

// fanout sends each record to every handler: the console for a person at a
// terminal, JSON for the file.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
