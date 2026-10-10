package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
)

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

// Errors a new run can be refused with.
var (
	errConversationBusy = httpx.NewError(http.StatusConflict, "conversation_busy",
		"this conversation is already answering; wait for it or stop it first")
	errNoModels = httpx.NewError(http.StatusConflict, "no_models",
		"no model is available; an administrator has to enable one first")
	errModelUnavailable = httpx.NewFieldError("model", "model_unavailable",
		"that model is not offered by any enabled channel")

	errTooManyAttachments = httpx.NewFieldError("attachmentIds", "too_many_attachments",
		fmt.Sprintf("a message can carry at most %d files", agent.MaxMessageAttachments))
	errAttachmentUnavailable = httpx.NewFieldError("attachmentIds", "attachment_unavailable",
		"a file was never uploaded, was already sent, or is not yours")
	errAttachmentsTooLarge = httpx.NewFieldError("attachmentIds", "attachments_too_large",
		fmt.Sprintf("a message's files can add up to at most %d MiB", agent.MaxAttachmentBytes>>20))
	errAttachmentUnsupported = httpx.NewFieldError("attachmentIds", "attachment_unsupported",
		"the selected model does not accept this kind of file")
)

// CreateMessage writes the user's input and starts the run that answers it, in
// one transaction, and returns without waiting for the answer. The run is the
// server's from here on: closing the page does not stop it, and the answer is
// followed on the events stream.
func (s *Server) CreateMessage(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	var body CreateMessageRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "empty_content", "content must not be empty"))
		return
	}
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	// A retry of a request that already got through finds what it started.
	requestID := ""
	if body.RequestId != nil {
		requestID = strings.TrimSpace(*body.RequestId)
	}
	if requestID != "" {
		if found, ok := s.existingRun(ctx, conversationId, requestID); ok {
			httpx.JSON(w, http.StatusAccepted, found)
			return
		}
	}

	modelID := ""
	if body.Model != nil {
		modelID = strings.TrimSpace(*body.Model)
	}
	if modelID == "" && conv.Model != nil {
		modelID = *conv.Model
	}
	modelID, channelID, err := s.pickChannel(ctx, modelID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var ids []uuid.UUID
	if body.AttachmentIds != nil {
		ids = *body.AttachmentIds
	}
	files, err := s.admitAttachments(ctx, user.ID, modelID, ids)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	signed, err := s.signAttachments(ctx, files)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// The effort asked for, else the conversation's. Only a level the model's
	// settings offer is sent: some endpoints reject the parameter outright, and
	// an operator may have withheld the level.
	effort := conv.ReasoningEffort
	rememberEffort := false
	if body.ReasoningEffort != nil {
		requested, ok := effortColumn(*body.ReasoningEffort)
		if !ok {
			httpx.Fail(w, r, errInvalidEffort)
			return
		}
		rememberEffort = !sameEffort(requested, conv.ReasoningEffort)
		effort = requested
	}
	runEffort := effort
	if effort != nil {
		// A level this model does not offer — or the operator does not allow —
		// is dropped rather than sent: the conversation may remember it from a
		// model that had it.
		settings, err := loadModelSettings(ctx, s.q, modelID)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if !settings.Profile.Offers(llm.ReasoningEffort(*effort)) {
			runEffort = nil
		}
	}

	// Web access, asked for or else the conversation's.
	webSearch := conv.WebSearch
	if body.WebSearch != nil {
		webSearch = *body.WebSearch
	}

	var (
		input sqlc.Message
		run   sqlc.Run
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		input, err = q.AppendEntry(ctx, sqlc.AppendEntryParams{
			ConversationID: conversationId,
			Role:           sqlc.MessageRoleUser,
			Content:        content,
			Parts:          agent.EncodeParts(inputParts(content, files)),
			Status:         sqlc.MessageStatusComplete,
		})
		if err != nil {
			return err
		}
		if len(files) > 0 {
			// The claim re-checks what admission saw: another request may have
			// taken one of the files in between.
			claimed, err := q.AttachToMessage(ctx, sqlc.AttachToMessageParams{
				MessageID: &input.ID, UserID: &user.ID, Ids: attachmentIDs(files),
			})
			if err != nil {
				return err
			}
			if claimed != int64(len(files)) {
				return errAttachmentUnavailable
			}
		}
		params := sqlc.CreateRunParams{
			ConversationID:  conversationId,
			UserID:          user.ID,
			InputMessageID:  input.ID,
			ModelID:         modelID,
			ChannelID:       &channelID,
			ReasoningEffort: runEffort,
			WebSearch:       webSearch,
		}
		if requestID != "" {
			params.RequestID = &requestID
		}
		if run, err = q.CreateRun(ctx, params); err != nil {
			return err
		}
		if conv.Model == nil || *conv.Model != modelID {
			if _, err := q.SetConversationModel(ctx, sqlc.SetConversationModelParams{
				ID: conversationId, UserID: &user.ID, Model: &modelID,
			}); err != nil {
				return err
			}
		}
		if rememberEffort {
			if _, err := q.SetConversationReasoning(ctx, sqlc.SetConversationReasoningParams{
				ID: conversationId, UserID: &user.ID, ReasoningEffort: effort,
			}); err != nil {
				return err
			}
		}
		if webSearch != conv.WebSearch {
			if _, err := q.SetConversationWebSearch(ctx, sqlc.SetConversationWebSearchParams{
				ID: conversationId, UserID: &user.ID, WebSearch: webSearch,
			}); err != nil {
				return err
			}
		}
		return q.TouchConversation(ctx, conversationId)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			switch pgErr.ConstraintName {
			case "runs_one_active_per_conversation":
				httpx.Fail(w, r, errConversationBusy)
				return
			case "runs_request_id":
				// The same request raced itself; the winner's run is the answer.
				if found, ok := s.existingRun(ctx, conversationId, requestID); ok {
					httpx.JSON(w, http.StatusAccepted, found)
					return
				}
			}
		}
		httpx.Fail(w, r, err)
		return
	}

	s.runner.Hub().Publish(conversationId, agent.Batch{
		RunID:   run.ID,
		Version: run.Version,
		Events:  []agent.Event{agent.RunStarted{Run: run, Input: input}},
	})
	s.runner.Start(run.ID)

	httpx.JSON(w, http.StatusAccepted, CreateMessageResponse{Message: toMessage(input, signed), Run: toRun(run)})
}

// inputParts is a user message: its text, then the files it carries as
// references (see agent.attachmentResolver for how they reach the model).
func inputParts(content string, files []sqlc.Attachment) []llm.Part {
	parts := []llm.Part{llm.TextPart{Text: content}}
	for _, file := range files {
		parts = append(parts, llm.AttachmentPart{Ref: file.ID.String(), Filename: file.Filename, MediaType: file.ContentType})
	}
	return parts
}

func attachmentIDs(files []sqlc.Attachment) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(files))
	for _, file := range files {
		ids = append(ids, file.ID)
	}
	return ids
}

// admitAttachments checks the files a message arrives with and returns them in
// the order they were listed. Everything it refuses is refused before anything
// is written.
func (s *Server) admitAttachments(ctx context.Context, userID uuid.UUID, modelID string, ids []uuid.UUID) ([]sqlc.Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	if len(unique) > agent.MaxMessageAttachments {
		return nil, errTooManyAttachments
	}

	rows, err := s.q.ListPendingAttachments(ctx, sqlc.ListPendingAttachmentsParams{Ids: unique, UserID: &userID})
	if err != nil {
		return nil, err
	}
	if len(rows) != len(unique) {
		return nil, errAttachmentUnavailable
	}
	files := make([]sqlc.Attachment, 0, len(rows))
	for _, id := range unique {
		i := slices.IndexFunc(rows, func(row sqlc.Attachment) bool { return row.ID == id })
		files = append(files, rows[i])
	}

	// What the model takes is settled before storage is asked anything.
	settings, err := loadModelSettings(ctx, s.q, modelID)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		modality := llm.ModalityOf(file.ContentType)
		if modality == "" || !slices.Contains(settings.InputModalities, modality) {
			return nil, errAttachmentUnsupported
		}
	}

	// The sizes the client announced were only announced: what was stored is
	// what counts, and a file that never arrived is not one.
	var total int64
	for _, file := range files {
		size, err := s.store.Size(ctx, file.StorageKey)
		if err != nil {
			slog.WarnContext(ctx, "attachment not in storage", "attachment_id", file.ID, "error", err)
			return nil, errAttachmentUnavailable
		}
		if size > uploadable[file.ContentType] {
			return nil, errAttachmentsTooLarge
		}
		total += size
	}
	if total > agent.MaxAttachmentBytes {
		return nil, errAttachmentsTooLarge
	}
	return files, nil
}

func sameEffort(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *Server) existingRun(ctx context.Context, conversationID uuid.UUID, requestID string) (CreateMessageResponse, bool) {
	run, err := s.q.GetRunByRequestID(ctx, sqlc.GetRunByRequestIDParams{ConversationID: conversationID, RequestID: &requestID})
	if err != nil {
		return CreateMessageResponse{}, false
	}
	input, err := s.q.GetMessage(ctx, run.InputMessageID)
	if err != nil {
		return CreateMessageResponse{}, false
	}
	attached, err := s.attachmentsFor(ctx, []sqlc.Message{input})
	if err != nil {
		return CreateMessageResponse{}, false
	}
	return CreateMessageResponse{Message: toMessage(input, attached[input.ID]), Run: toRun(run)}, true
}

// heartbeatInterval keeps idle event streams from being closed by proxies
// that time out quiet connections, and is how a client knows the stream is
// still alive while a run is quiet — the model has not started, or a tool is
// running. The client gives up on a stream after hearing nothing for three of
// these (stream.ts, idleTimeout), so the two are changed together.
const heartbeatInterval = 5 * time.Second

// StreamConversationEvents sends a snapshot of the conversation's run in
// flight, then every committed change on top of it.
func (s *Server) StreamConversationEvents(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	// Subscribe before reading the snapshot: a commit in between is then in
	// both, and the version check below drops the duplicate, where the other
	// order would lose it.
	sub := s.runner.Hub().Subscribe(conversationId)
	defer sub.Close()

	sse, err := httpx.NewSSEWriter(w)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	snapshot, err := s.snapshot(ctx, conversationId)
	if err != nil {
		slog.ErrorContext(ctx, "conversation snapshot", "error", err, "conversation_id", conversationId)
		return
	}
	if err := sendSnapshot(sse, snapshot); err != nil {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := sse.Comment("ping"); err != nil {
				return
			}
		case <-sub.Wake():
			batches, resync := sub.Drain()
			if resync {
				if snapshot, err = s.snapshot(ctx, conversationId); err != nil {
					return
				}
				if err := sendSnapshot(sse, snapshot); err != nil {
					return
				}
				continue
			}
			for _, batch := range batches {
				if snapshot.Run != nil && batch.RunID == snapshot.Run.Id && batch.Version <= snapshot.Run.Version {
					continue // already in the snapshot
				}
				if err := sendBatch(sse, batch); err != nil {
					return
				}
			}
		}
	}
}

func (s *Server) snapshot(ctx context.Context, conversationID uuid.UUID) (SnapshotEvent, error) {
	snapshot := SnapshotEvent{Type: Snapshot, Messages: []Message{}}
	run, err := s.q.GetActiveRun(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	rows, err := s.q.ListRunMessages(ctx, sqlc.ListRunMessagesParams{RunID: &run.ID, InputMessageID: run.InputMessageID})
	if err != nil {
		return snapshot, err
	}
	live, err := agent.DecodeLive(run.Live)
	if err != nil {
		return snapshot, err
	}
	mapped := toRun(run)
	state := toLiveState(live)
	snapshot.Run = &mapped
	snapshot.Live = &state
	attached, err := s.attachmentsFor(ctx, rows)
	if err != nil {
		return snapshot, err
	}
	for _, row := range rows {
		snapshot.Messages = append(snapshot.Messages, toMessage(row, attached[row.ID]))
	}
	return snapshot, nil
}

func sendSnapshot(sse *httpx.SSEWriter, snapshot SnapshotEvent) error {
	var event ConversationEvent
	if err := event.FromSnapshotEvent(snapshot); err != nil {
		return err
	}
	return sse.Event(event)
}

func sendBatch(sse *httpx.SSEWriter, batch agent.Batch) error {
	var event ConversationEvent
	if err := event.FromBatchEvent(toBatchEvent(batch)); err != nil {
		return err
	}
	return sse.Event(event)
}

// CancelRun asks a run to stop. The request is recorded before the running
// goroutine is told, so a stop that arrives while the process restarts is
// still honoured when the run resumes.
func (s *Server) CancelRun(w http.ResponseWriter, r *http.Request, conversationId ConversationId, runId openapi_types.UUID) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if _, err := s.q.RequestRunCancel(ctx, sqlc.RequestRunCancelParams{ID: runId, ConversationID: conversationId}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Finished already, or not this conversation's: nothing to stop
			// either way, and saying which would tell a stranger the run exists.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		httpx.Fail(w, r, err)
		return
	}
	s.runner.Cancel(runId)
	w.WriteHeader(http.StatusAccepted)
}

// chatModelLimit is far beyond what an installation offers; the picker has no
// paging because a list that needs it is not one a person picks from.
const chatModelLimit = 1000

// GetChatFeatures says what the composer can offer: web search when the
// administrator has it configured and on.
func (s *Server) GetChatFeatures(w http.ResponseWriter, r *http.Request) {
	config, key, err := loadWebSearch(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ChatFeatures{WebSearch: config.Enabled && config.searcher(key) != nil})
}

// ListChatModels lists what an enabled channel offers, for the model picker.
func (s *Server) ListChatModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.chatModels(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, models)
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
		signed, err := s.signAttachments(ctx, []sqlc.Attachment{a})
		if err != nil {
			return nil, err
		}
		out[*a.MessageID] = append(out[*a.MessageID], signed...)
	}
	return out, nil
}

// signAttachments maps attachments onto the contract with a read URL for each.
func (s *Server) signAttachments(ctx context.Context, files []sqlc.Attachment) ([]Attachment, error) {
	out := make([]Attachment, 0, len(files))
	for _, a := range files {
		url, err := s.store.PresignGet(ctx, a.StorageKey, attachmentTTL)
		if err != nil {
			return nil, err
		}
		out = append(out, Attachment{
			Id:          a.ID,
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Size:        a.SizeBytes,
			Url:         url,
		})
	}
	return out, nil
}
