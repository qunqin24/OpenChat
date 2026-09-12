package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/chat"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// historyLimit caps how many prior messages are replayed to the provider.
const historyLimit = 100

// attachmentTTL is how long the read URLs handed to the client stay valid.
const attachmentTTL = time.Hour

func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, conversationId ConversationId, params ListMessagesParams) {
	if err := s.conversationExists(r.Context(), conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	cursorAt, cursorID, err := decodeCursor(params.Cursor)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	limit := clampLimit(params.Limit)
	rows, err := s.q.ListMessages(r.Context(), sqlc.ListMessagesParams{
		ConversationID:  conversationId,
		CursorCreatedAt: cursorAt,
		CursorID:        cursorID,
		RowLimit:        limit + 1,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	page := MessagePage{Items: make([]Message, 0, limit)}
	if int32(len(rows)) > limit {
		last := rows[limit-1]
		next := encodeCursor(last.CreatedAt, last.ID)
		page.NextCursor = &next
		rows = rows[:limit]
	}

	byMessage, err := s.attachmentsFor(r.Context(), rows)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, row := range rows {
		page.Items = append(page.Items, toMessage(row, byMessage[row.ID]))
	}
	httpx.JSON(w, http.StatusOK, page)
}

// CreateMessage persists the user turn, then streams the assistant reply as SSE.
func (s *Server) CreateMessage(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()

	var body CreateMessageRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "empty_content", "content must not be empty"))
		return
	}
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	userMsg, err := s.q.CreateMessage(ctx, sqlc.CreateMessageParams{
		ConversationID: conversationId,
		Role:           sqlc.MessageRoleUser,
		Content:        body.Content,
		Status:         sqlc.MessageStatusComplete,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.AttachmentIds != nil && len(*body.AttachmentIds) > 0 {
		if err := s.q.AttachToMessage(ctx, sqlc.AttachToMessageParams{
			MessageID: &userMsg.ID,
			Ids:       *body.AttachmentIds,
		}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}

	history, err := s.q.ListMessages(ctx, sqlc.ListMessagesParams{
		ConversationID: conversationId,
		RowLimit:       historyLimit,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	assistant, err := s.q.CreateMessage(ctx, sqlc.CreateMessageParams{
		ConversationID: conversationId,
		Role:           sqlc.MessageRoleAssistant,
		Content:        "",
		Status:         sqlc.MessageStatusStreaming,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Past this point the status line is already committed, so failures are
	// reported as an `error` frame inside the stream rather than an HTTP status.
	sse, err := httpx.NewSSEWriter(w)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := sse.Event(StreamStart{Type: Start, MessageId: assistant.ID}); err != nil {
		return
	}

	model := s.provider.Name()
	if body.Model != nil && *body.Model != "" {
		model = *body.Model
	}

	var reply strings.Builder
	streamErr := s.provider.Stream(ctx, chat.Request{
		Model:    model,
		Messages: toChatMessages(history),
	}, func(delta string) error {
		reply.WriteString(delta)
		return sse.Event(StreamDelta{Type: Delta, Text: delta})
	})

	// The client may have disconnected; finish the write with a fresh context so
	// the partial reply is still persisted.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	// A cancelled context means the user hit Stop or the tab closed: the partial
	// reply is intentional, not a failure. Only provider errors are `failed`.
	aborted := ctx.Err() != nil
	status := sqlc.MessageStatusComplete
	if streamErr != nil && !aborted {
		status = sqlc.MessageStatusFailed
	}
	final, err := s.q.FinishMessage(finishCtx, sqlc.FinishMessageParams{
		ID:      assistant.ID,
		Content: reply.String(),
		Status:  status,
	})
	if err != nil {
		slog.ErrorContext(finishCtx, "persist assistant message", "error", err, "message_id", assistant.ID)
		_ = sse.Event(StreamError{Type: StreamErrorTypeError, Error: Error{Code: "persist_failed", Message: "could not save the reply"}})
		return
	}
	if err := s.q.TouchConversation(finishCtx, conversationId); err != nil {
		slog.ErrorContext(finishCtx, "touch conversation", "error", err, "conversation_id", conversationId)
	}

	if streamErr != nil {
		if aborted {
			return // client went away; nothing left to write
		}
		slog.ErrorContext(finishCtx, "provider stream failed", "error", streamErr)
		_ = sse.Event(StreamError{Type: StreamErrorTypeError, Error: Error{Code: "provider_error", Message: "the model stream failed"}})
		return
	}
	_ = sse.Event(StreamDone{Type: Done, Message: toMessage(final, nil)})
}

// attachmentsFor resolves attachments for a page of messages in one query and
// signs a read URL for each.
func (s *Server) attachmentsFor(ctx context.Context, rows []sqlc.Message) (map[uuid.UUID][]Attachment, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	found, err := s.q.ListAttachmentsByMessages(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}

	out := make(map[uuid.UUID][]Attachment, len(found))
	for _, a := range found {
		if a.MessageID == nil {
			continue
		}
		url, err := s.store.PresignGet(ctx, a.StorageKey, attachmentTTL)
		if err != nil {
			return nil, err
		}
		out[*a.MessageID] = append(out[*a.MessageID], Attachment{
			Id:          a.ID,
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Size:        a.SizeBytes,
			Url:         url,
		})
	}
	return out, nil
}
