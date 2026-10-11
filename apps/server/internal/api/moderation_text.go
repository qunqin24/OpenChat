package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/memory"
)

// Content moderation of the texts a person keeps, as opposed to the messages
// they send (moderation.go).
//
// A person's custom instructions and the memories they write themselves reach
// the model without being sent: they are copied into the system prompt of every
// conversation (memory.Snapshot). So they are checked, in two places:
//
//   - when they are saved (UpdatePersonalization, CreateMemory, UpdateMemory),
//     the way a message is sent: a refusal is the same 422 content_blocked, is
//     logged with source `instructions` / `memory` and counts as a strike, and
//     nothing is saved;
//   - when a conversation takes its snapshot (prepareSnapshot, at the start of
//     the conversation's first run), for whatever was never checked - saved
//     before moderation was turned on, or while it was off. What fails is left
//     out of the snapshot and logged with source `snapshot`, which is not a
//     strike: the words were written some time ago and nothing is being sent now,
//     so it would be unfair to disable an account over a "hello".
//
// "Checked" is remembered as the hash of the text that passed (memory.Hash), per
// instruction field and per memory, so a text is asked about once and an edit
// makes it unchecked again. Memories the model writes are not moderated: they
// are model output, and are recorded as vouched for.

// moderationTrace is how a caller of moderate finds out whether the check was
// really made: moderate answers nil both for a message that passed and for one
// that was let through because the service could not be asked (on_error=allow),
// and a text that was never looked at must not be recorded as checked.
type moderationTrace struct{ failed bool }

type moderationTraceKey struct{}

func withModerationTrace(ctx context.Context, t *moderationTrace) context.Context {
	return context.WithValue(ctx, moderationTraceKey{}, t)
}

// noteModerationFailed is called where moderate decides what to do about a
// check that could not be made.
func noteModerationFailed(ctx context.Context) {
	if t, ok := ctx.Value(moderationTraceKey{}).(*moderationTrace); ok {
		t.failed = true
	}
}

// moderationLive says whether a text of this person would be looked at now:
// moderation is on, and they are not an exempt administrator.
func (s *Server) moderationLive(ctx context.Context, who moderationSubject) bool {
	cfg, err := loadModeration(ctx, s.q)
	if err != nil {
		return false
	}
	return cfg.Mode != ModerationModeOff && !(who.admin && cfg.ExemptAdmins)
}

// checkText moderates one text. checked says it really was looked at and passed;
// err is moderate's: the 422 content_blocked of a text that failed, a 503, or a
// sanction. A text that is let through unchecked - moderation off, an exempt
// administrator, the service down and the setting "allow" - is nil, false.
func (s *Server) checkText(ctx context.Context, who moderationSubject, text string) (checked bool, err error) {
	if strings.TrimSpace(text) == "" {
		return true, nil
	}
	// Live before and after: a setting flipped while the check ran leaves the
	// text unrecorded rather than recorded as looked at.
	before := s.moderationLive(ctx, who)
	trace := &moderationTrace{}
	if err := s.moderate(withModerationTrace(ctx, trace), who, moderationInput{text: text}); err != nil {
		return false, err
	}
	return before && !trace.failed && s.moderationLive(ctx, who), nil
}

// isContentBlocked says err is the refusal of a text that failed the check, as
// opposed to a service that could not be asked or a sanction.
func isContentBlocked(err error) bool {
	var apiErr *httpx.APIError
	return errors.As(err, &apiErr) && apiErr.Code == "content_blocked"
}

// moderateSaved checks a text a person is saving: their custom instructions or
// a memory. checked is whether it was really looked at (to be recorded with it).
func (s *Server) moderateSaved(ctx context.Context, user sqlc.User, source ModerationSource, text string) (checked bool, err error) {
	who := moderationSubject{userID: user.ID, admin: user.Role == sqlc.UserRoleAdmin, source: source}
	return s.checkText(ctx, who, text)
}

// prepareSnapshot gives a conversation its memory snapshot before its first run
// starts, checking what the snapshot would carry that was never checked. It is
// called where a run is chosen - before anything is written, so a refusal here
// starts nothing: 503 moderation_unavailable when the check cannot be made and
// the settings say not to go on without it. A conversation that has its snapshot
// already is left alone: it is not read again, and nothing is checked again.
func (s *Server) prepareSnapshot(ctx context.Context, user sqlc.User, conv sqlc.Conversation) error {
	if conv.MemorySnapshot != nil {
		return nil
	}
	_, err := s.notes().EnsureSnapshot(ctx, user.ID, conv.ID, s.snapshotVet(user, conv.ID))
	return err
}

// snapshotVet moderates the unchecked texts of a snapshot in one call, and,
// only if that call blocks, each text by itself to find which one it was (the
// others stay in).
func (s *Server) snapshotVet(user sqlc.User, conversationID uuid.UUID) memory.Vet {
	return func(ctx context.Context, pieces []memory.Piece) ([]memory.Verdict, error) {
		who := s.accountSubject(ctx, user, conversationID, ModerationSourceSnapshot)
		out := make([]memory.Verdict, len(pieces))

		checked, err := s.checkText(ctx, who, joinPieces(pieces))
		switch {
		case err == nil:
			for i := range out {
				out[i].Checked = checked
			}
			return out, nil
		case !isContentBlocked(err):
			return nil, err
		case len(pieces) == 1:
			out[0] = memory.Verdict{Blocked: true, Checked: true}
			return out, nil
		}

		for i, piece := range pieces {
			checked, err := s.checkText(ctx, who, piece.Text)
			switch {
			case err == nil:
				out[i].Checked = checked
			case isContentBlocked(err):
				out[i] = memory.Verdict{Blocked: true, Checked: true}
			default:
				return nil, err
			}
		}
		return out, nil
	}
}

// joinPieces is the texts of a snapshot as one text to check, each under a label
// that says what it is.
func joinPieces(pieces []memory.Piece) string {
	var b strings.Builder
	for i, piece := range pieces {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch piece.Kind {
		case memory.PieceAbout:
			b.WriteString("Custom instructions, about the user:\n")
		case memory.PieceStyle:
			b.WriteString("Custom instructions, response style:\n")
		default:
			b.WriteString("Memory:\n")
		}
		b.WriteString(piece.Text)
	}
	return b.String()
}
