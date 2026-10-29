package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A message can refer to other conversations of its author's (conversationIds;
// agent/references.go for how they reach the model). Checked when it is sent:
// the author's own, chat conversations, not the one being written in, with
// something in them. Each is taken up to its last question or answer now.

const maxReferences = 5

var (
	errReferenceUnavailable = httpx.NewFieldError("conversationIds", "reference_unavailable",
		"a conversation to refer to must be another of your own")
	errReferenceEmpty = httpx.NewFieldError("conversationIds", "reference_empty",
		"that conversation has nothing in it yet")
	errTooManyReferences = httpx.NewFieldError("conversationIds", "too_many_references",
		"a message can refer to at most 5 conversations")
	errReferencesNotEditable = httpx.NewFieldError("conversationIds", "references_not_editable",
		"only a message that moderation refused takes conversations when it is edited; any other keeps its own")
)

// referenceParts are the parts a message carries for the conversations it
// refers to, in the order given, each once.
func (s *Server) referenceParts(ctx context.Context, userID, current uuid.UUID, ids *[]uuid.UUID) ([]llm.Part, error) {
	if ids == nil || len(*ids) == 0 {
		return nil, nil
	}
	seen := map[uuid.UUID]bool{}
	var unique []uuid.UUID
	for _, id := range *ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) > maxReferences {
		return nil, errTooManyReferences
	}
	var parts []llm.Part
	for _, id := range unique {
		if id == current {
			return nil, errReferenceUnavailable
		}
		conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: id, UserID: &userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errReferenceUnavailable
		}
		if err != nil {
			return nil, err
		}
		at, err := s.q.LatestReferableMessageAt(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errReferenceEmpty
		}
		if err != nil {
			return nil, err
		}
		part := agent.ConversationReference(conv, at.Time, 0)
		// Its size now, for what it costs a request and whether it goes as a card.
		text, note, err := agent.ConversationText(ctx, s.q, userID, part)
		if err != nil {
			return nil, err
		}
		if note != "" {
			return nil, errReferenceUnavailable
		}
		part.Size, part.TextSize = int64(len(text)), int64(len(text))
		parts = append(parts, part)
	}
	return parts, nil
}

// withReferenceTitles adds the titles of the conversations a message refers
// to to what is checked of it. A title is whatever its owner named the
// conversation, never moderated when it was set, and the model is given it as
// the reference's name (agent/references.go), so it is checked here, with the
// message that sends it. What the model reads is the title as it was now -
// the part's Filename - so renaming the conversation afterwards changes
// nothing the model sees.
func withReferenceTitles(in moderationInput, refs []llm.Part) moderationInput {
	var titles []string
	for _, part := range refs {
		if ref, ok := part.(llm.AttachmentPart); ok && strings.TrimSpace(ref.Filename) != "" {
			titles = append(titles, ref.Filename)
		}
	}
	if len(titles) > 0 {
		in.text = strings.TrimSpace(in.text + "\n\n" + strings.Join(titles, "\n"))
	}
	return in
}

// messageReferences are the conversations a message's parts refer to, for the
// page to show.
func messageReferences(parts []llm.Part) []ConversationReference {
	var out []ConversationReference
	for _, part := range parts {
		ref, ok := part.(llm.AttachmentPart)
		if !ok {
			continue
		}
		if id, _, ok := agent.ReferencedConversation(ref); ok {
			out = append(out, ConversationReference{ConversationId: id, Title: ref.Filename})
		}
	}
	return out
}
