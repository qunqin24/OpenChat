package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// temporaryLifetime is how long a temporary chat lives, counted from its
	// creation, however recently it was used.
	temporaryLifetime = 24 * time.Hour
	// cleanupEvery is how often expired chats are looked for.
	cleanupEvery = 10 * time.Minute
)

// StartCleanup removes what has outlived its time, in the background until ctx
// ends: temporary chats a day after they were made, and memory-log entries past
// their retention.
//
// Safe with several servers on one database. Each does a single DELETE with the
// age in its WHERE clause, so a row is deleted by whichever gets to it and the
// others find it gone; nothing needs electing or locking.
func (s *Server) StartCleanup(ctx context.Context) {
	go func() {
		s.CleanupNow(ctx)
		ticker := time.NewTicker(cleanupEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CleanupNow(ctx)
			}
		}
	}()
}

// CleanupNow is one pass of StartCleanup.
func (s *Server) CleanupNow(ctx context.Context) {
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-temporaryLifetime), Valid: true}
	n, err := s.q.DeleteExpiredTemporaryConversations(ctx, cutoff)
	if err != nil {
		slog.WarnContext(ctx, "delete expired temporary chats", "error", err)
	} else if n > 0 {
		slog.InfoContext(ctx, "deleted expired temporary chats", "count", n)
	}
	if _, err := s.notes().PurgeEvents(ctx); err != nil {
		slog.WarnContext(ctx, "purge old memory events", "error", err)
	}
}
