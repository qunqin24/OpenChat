package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

const (
	// temporaryLifetime is how long a temporary chat lives, counted from its
	// creation, however recently it was used.
	temporaryLifetime = 24 * time.Hour
	// deletedRetention is how long a conversation the user deleted is kept
	// before it is removed for good, with its messages, attachments and files.
	// The delete is soft until then, so this is the window in which an operator
	// can still bring one back by clearing deleted_at.
	deletedRetention = 30 * 24 * time.Hour
	// unclaimedUploadLifetime is how long an upload no message has claimed is
	// kept. Sending a message takes minutes at the outside; a day is generous.
	unclaimedUploadLifetime = 24 * time.Hour
	// orphanGrace is how old a file on the local disk must be before it is
	// judged to have no attachment. A row is written before its file, so a
	// file without one is never in the middle of arriving; the grace is for
	// clock skew and for anybody looking at the disk by hand.
	orphanGrace = 24 * time.Hour
	// uploadPrefix is the directory attachments are stored under, which is
	// the only part of the disk the orphan sweep looks at.
	uploadPrefix = "uploads"

	// cleanupEvery is how often expired chats are looked for.
	cleanupEvery = 10 * time.Minute
	// cleanupBatch is how many rows one DELETE removes, so that a backlog (the
	// first pass after retention is introduced, say) is worked off in pieces
	// rather than one statement that holds them all.
	cleanupBatch = 200
	// cleanupMaxBatches bounds one pass; whatever is left waits for the next.
	cleanupMaxBatches = 50
)

// StartCleanup removes what has outlived its time, in the background until ctx
// ends: temporary chats a day after they were made, conversations a user
// deleted once the retention is over, uploads nobody attached, files on the
// disk that no attachment points to, and memory-log entries past their
// retention.
//
// Safe with several servers on one database. Each row is removed by a single
// DELETE with the age in its WHERE clause, and the statement hands back the
// storage keys of what it removed, so a row — and its files — is dealt with by
// whichever server gets to it and the others find it gone; nothing needs
// electing or locking.
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
//
// Rows go before their files: a file is wanted for as long as a row names it,
// so a crash in between leaves an orphan for the sweep to find, never a row
// pointing at nothing.
func (s *Server) CleanupNow(ctx context.Context) {
	now := time.Now()
	at := func(age time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: now.Add(-age), Valid: true}
	}

	s.removeConversations(ctx, "expired temporary chats", func() (int, []string, error) {
		rows, err := s.q.DeleteExpiredTemporaryConversations(ctx,
			sqlc.DeleteExpiredTemporaryConversationsParams{Before: at(temporaryLifetime), RowLimit: cleanupBatch})
		var keys []string
		for _, r := range rows {
			keys = append(keys, r.StorageKeys...)
		}
		return len(rows), keys, err
	})
	s.removeConversations(ctx, "deleted conversations past their retention", func() (int, []string, error) {
		rows, err := s.q.PurgeDeletedConversations(ctx,
			sqlc.PurgeDeletedConversationsParams{Before: at(deletedRetention), RowLimit: cleanupBatch})
		var keys []string
		for _, r := range rows {
			keys = append(keys, r.StorageKeys...)
		}
		return len(rows), keys, err
	})
	s.removeConversations(ctx, "unclaimed uploads", func() (int, []string, error) {
		keys, err := s.q.DeleteStaleUploads(ctx,
			sqlc.DeleteStaleUploadsParams{Before: at(unclaimedUploadLifetime), RowLimit: cleanupBatch})
		return len(keys), keys, err
	})

	s.sweepOrphanFiles(ctx, now.Add(-orphanGrace))

	if _, err := s.notes().PurgeEvents(ctx); err != nil {
		slog.WarnContext(ctx, "purge old memory events", "error", err)
	}
}

// removeConversations runs one batched deletion to the end: each call of step
// removes up to cleanupBatch rows and reports how many, with the storage keys
// of the attachments that went with them. The files are removed after each
// batch, so a long backlog does not leave them all to the end.
func (s *Server) removeConversations(ctx context.Context, what string, step func() (int, []string, error)) {
	total, files := 0, 0
	for range cleanupMaxBatches {
		n, keys, err := step()
		if err != nil {
			slog.WarnContext(ctx, "delete "+what, "error", err)
			break
		}
		total += n
		files += s.removeFiles(ctx, keys)
		if n < cleanupBatch {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "deleted "+what, "count", total, "files", files)
	}
}

// removeFiles deletes stored files whose rows are already gone, best-effort:
// a failure is logged and the rest go on. For a file on the local disk the
// orphan sweep is the backstop; one in a bucket that cannot be reached stays
// there until somebody clears it.
func (s *Server) removeFiles(ctx context.Context, keys []string) int {
	if s.store == nil {
		return 0
	}
	removed := 0
	for _, key := range keys {
		if err := s.store.Delete(ctx, key); err != nil {
			slog.WarnContext(ctx, "delete stored file", "key", key, "error", err)
			continue
		}
		removed++
	}
	return removed
}

// sweepOrphanFiles removes files from the local disk's upload directory that
// no attachment row names and that were last written before the cutoff. It
// finds what rows deleted by cascade used to leave behind (before deletions
// removed their files), and uploads whose row went but whose delete of the
// file failed. Leftovers of an interrupted write are removed with them.
//
// Only the local disk is swept; a bucket is not listed.
func (s *Server) sweepOrphanFiles(ctx context.Context, before time.Time) {
	if s.local == nil {
		return
	}
	local := s.local()
	if local == nil {
		return
	}

	const batch = 500
	var pending []string
	removed := 0
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		keys := pending
		pending = nil
		known, err := s.q.ListKnownStorageKeys(ctx, keys)
		if err != nil {
			return err
		}
		wanted := make(map[string]bool, len(known))
		for _, key := range known {
			wanted[key] = true
		}
		for _, key := range keys {
			if wanted[key] {
				continue
			}
			if err := local.Delete(ctx, key); err != nil {
				slog.WarnContext(ctx, "delete orphaned file", "key", key, "error", err)
				continue
			}
			removed++
		}
		return nil
	}

	err := local.Walk(uploadPrefix, func(o storage.Object) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !o.ModTime.Before(before) {
			return nil
		}
		pending = append(pending, o.Key)
		if len(pending) >= batch {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		slog.WarnContext(ctx, "sweep orphaned files", "error", err)
	}
	if temp, err := local.RemoveStaleTemp(uploadPrefix, before); err != nil {
		slog.WarnContext(ctx, "remove stale temporary files", "error", err)
	} else {
		removed += temp
	}
	if removed > 0 {
		slog.InfoContext(ctx, "removed orphaned files", "count", removed)
	}
}
