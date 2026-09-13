// Package logging owns the process's log level, which the admin panel can
// change while the server is running.
//
// A restart to raise the level is the wrong tool when the thing you are chasing
// is intermittent: by the time you have redeployed, the request you wanted is
// gone. slog.LevelVar exists for exactly this, and is safe to write from a
// handler while other goroutines log.
package logging

import (
	"log/slog"
	"os"
)

var level = new(slog.LevelVar)

// Setup installs the process logger. `json` picks the production format; the
// level starts at Info either way and moves only through SetDebug.
func Setup(json bool) {
	level.Set(slog.LevelInfo)

	// JSON in production, where the reader is a log pipeline. A terminal gets
	// the console handler instead; slog's TextHandler serves neither well.
	var handler slog.Handler = newConsoleHandler(os.Stdout, level)
	if json {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(handler))
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
