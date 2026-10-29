package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// A message can refer to another of its author's conversations (the
// composer's "引用其他对话", after OpenWebUI's "Reference Chats"). The reference
// is not a file: nothing is stored or uploaded. It is an attachment part of
// its own media type (llm.ConversationMediaType) naming the conversation and
// the moment it was referenced, and it is read as words whenever a request is
// built - what was asked and answered up to that moment, as an export takes
// it - so it goes the way a long text file does: whole when short, as a card
// the file tools read when long. It is the person's own conversation, so its
// messages are not put through content moderation again: their questions
// were checked when they were sent, and the answers are the kind of text a
// conversation's own history already sends. Its title is another matter - a
// person can name a conversation anything, unchecked - so the title the model
// is given is the one the reference was made with (the part's Filename),
// which was checked with the message that made it (api/references.go), never
// the conversation's title now.

const conversationRefPrefix = "conversation:"

// MaxReferenceBytes bounds what a reference reads as, as a document's words
// are bounded: a longer conversation is cut, and says so.
const MaxReferenceBytes = doctext.MaxTextBytes

// ConversationReference is the part a message carries for another of its
// author's conversations: up to upTo (its last question or answer when it was
// referenced), titled as the conversation was, of textSize bytes of words.
func ConversationReference(conv sqlc.Conversation, upTo time.Time, textSize int64) llm.AttachmentPart {
	return llm.AttachmentPart{
		Ref:       conversationRefPrefix + conv.ID.String() + "@" + upTo.UTC().Format(time.RFC3339Nano),
		Filename:  conv.Title,
		MediaType: llm.ConversationMediaType,
		Size:      textSize,
		TextSize:  textSize,
	}
}

// ReferencedConversation is the conversation a reference names and the moment
// it reaches to; false for a part that is not one.
func ReferencedConversation(part llm.AttachmentPart) (uuid.UUID, time.Time, bool) {
	if part.MediaType != llm.ConversationMediaType {
		return uuid.Nil, time.Time{}, false
	}
	rest, ok := strings.CutPrefix(part.Ref, conversationRefPrefix)
	if !ok {
		return uuid.Nil, time.Time{}, false
	}
	idText, atText, ok := strings.Cut(rest, "@")
	if !ok {
		return uuid.Nil, time.Time{}, false
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return uuid.Nil, time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, atText)
	if err != nil {
		return uuid.Nil, time.Time{}, false
	}
	return id, at, true
}

// ConversationText is what a reference reads as, for userID - whose
// conversation it must be - or in note why it cannot be read.
func ConversationText(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, ref llm.AttachmentPart) (text, note string, err error) {
	id, at, ok := ReferencedConversation(ref)
	if !ok {
		return "", "the conversation is no longer available", nil
	}
	// Still the user's and not deleted. Its title now is not read: the one the
	// reference was made with is (see above).
	_, err = q.GetConversation(ctx, sqlc.GetConversationParams{ID: id, UserID: &userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "the conversation is no longer available", nil
	}
	if err != nil {
		return "", "", err
	}
	rows, err := q.ListTranscriptUpTo(ctx, sqlc.ListTranscriptUpToParams{
		ConversationID: id,
		UpTo:           pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		return "", "", err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.Role == sqlc.MessageRoleUser {
			ids = append(ids, row.ID)
		}
	}
	files := map[uuid.UUID][]string{}
	if len(ids) > 0 {
		found, err := q.ListAttachmentsByMessages(ctx, ids)
		if err != nil {
			return "", "", err
		}
		for _, f := range found {
			if f.MessageID != nil {
				files[*f.MessageID] = append(files[*f.MessageID], f.Filename)
			}
		}
	}
	return renderConversation(ref.Filename, at, rows, files), "", nil
}

// renderConversation writes a conversation for a model to read: its title, a
// line saying what it is, then each message under a heading naming who said
// it - the model, for an answer - with the names of the files a question
// carried (not the files). English, as any text written for a model here is.
func renderConversation(title string, upTo time.Time, rows []sqlc.Message, files map[uuid.UUID][]string) string {
	var b strings.Builder
	if title = strings.TrimSpace(title); title == "" {
		title = "Conversation"
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("_Another of the user's conversations, which they referred to: what was asked and answered in it, up to " +
		upTo.UTC().Format("2006-01-02 15:04 UTC") + "._\n")
	for _, row := range rows {
		content := strings.TrimSpace(row.Content)
		// An answer that only called tools, or failed before saying anything.
		if row.Role == sqlc.MessageRoleAssistant && content == "" {
			continue
		}
		if row.Role == sqlc.MessageRoleAssistant {
			b.WriteString("\n## Assistant")
			if row.Model != nil && *row.Model != "" {
				b.WriteString(" (" + *row.Model + ")")
			}
			b.WriteString("\n\n")
		} else {
			b.WriteString("\n## User\n\n")
		}
		b.WriteString(content + "\n")
		if names := files[row.ID]; len(names) > 0 {
			b.WriteString("\n_Files attached: " + strings.Join(names, ", ") + "_\n")
		}
		if b.Len() > MaxReferenceBytes {
			break
		}
	}
	text := b.String()
	if len(text) > MaxReferenceBytes {
		cut := MaxReferenceBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "\n\n[The conversation goes on; it is too long to include the rest.]\n"
	}
	return text
}

// referenceReader reads references for one user, for an attachmentResolver.
func referenceReader(q *sqlc.Queries, userID uuid.UUID) func(context.Context, llm.AttachmentPart) (string, string) {
	return func(ctx context.Context, ref llm.AttachmentPart) (string, string) {
		text, note, err := ConversationText(ctx, q, userID, ref)
		if err != nil {
			slog.WarnContext(ctx, "read a referenced conversation", "ref", ref.Ref, "error", err)
			return "", "the conversation could not be read"
		}
		return text, note
	}
}
