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
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/doctext"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
)

// attachmentTTL is how long the read URLs handed to the client stay valid.
const attachmentTTL = time.Hour

func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, conversationId ConversationId, params ListMessagesParams) {
	if err := s.conversationExists(r.Context(), conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	cursorAt, cursorID, err := decodeCursor(params.Before)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	page, err := s.transcriptPage(r.Context(), conversationId, cursorAt, cursorID, clampLimit(params.Limit))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}

// transcriptPage is a page of the conversation, from its end: the newest rows,
// or with a cursor the rows older than it.
func (s *Server) transcriptPage(ctx context.Context, conversationID uuid.UUID, cursorAt pgtype.Timestamptz, cursorID *uuid.UUID, limit int32) (MessagePage, error) {
	rows, err := s.q.ListMessages(ctx, sqlc.ListMessagesParams{
		ConversationID:  conversationID,
		CursorCreatedAt: cursorAt,
		CursorID:        cursorID,
		RowLimit:        limit + 1,
	})
	if err != nil {
		return MessagePage{}, err
	}

	// rows are newest first and one longer than the page when older ones exist:
	// the cursor is the page's oldest row, and the page itself is turned around
	// so that it reads oldest first.
	var older *string
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		oldest := rows[limit-1]
		cursor := encodeCursor(oldest.CreatedAt, oldest.ID)
		older = &cursor
	}
	slices.Reverse(rows)

	page, err := s.messagePage(ctx, rows)
	if err != nil {
		return MessagePage{}, err
	}
	page.OlderCursor = older
	return page, nil
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
	errAttachmentUnreadable = httpx.NewFieldError("attachmentIds", "attachment_unreadable",
		"a document could not be read: it is damaged, protected, or not what its name says")
	errAttachmentTextTooLong = httpx.NewError(http.StatusRequestEntityTooLarge, "attachment_text_too_long",
		fmt.Sprintf("a document's text can be at most %d MiB", doctext.MaxTextBytes>>20))
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
	requestID := trimmed(body.RequestId)
	if found, ok := s.existingRun(ctx, conversationId, requestID); ok {
		httpx.JSON(w, http.StatusAccepted, found)
		return
	}

	// A refused message is reworded, not gone past (moderation_kept.go): asked
	// here as well as under the lock below, so that a page that did not know
	// costs neither a moderation call nor a strike.
	if err := requireRevised(ctx, s.q, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, body.WebSearch)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var ids []uuid.UUID
	if body.AttachmentIds != nil {
		ids = *body.AttachmentIds
	}
	files, digests, err := s.admitAttachments(ctx, user.ID, choice.modelID, ids)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The words and the pictures, before a run is started or a file claimed. A
	// message that fails is kept as what it is - a refused message - and
	// nothing else of it is (moderation_kept.go).
	//
	// The check's reading of the pictures and text files is also what is recorded of them
	// (attachment_digest.go): the model is sent those bytes or none.
	in := s.checkedFiles(content, files, digests)
	if refused, err := s.moderated(ctx, s.accountSubject(ctx, user, conversationId, ModerationSourceSend), in); err != nil {
		if refused {
			err = s.keepRefused(ctx, conversationId, content, err)
		}
		httpx.Fail(w, r, err)
		return
	}
	// The conversation's first run takes its memory snapshot here, with whatever
	// it carries that was never moderated checked first (moderation_text.go).
	if err := s.prepareSnapshot(ctx, user, conv); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	signed, err := s.signAttachments(ctx, files)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	var (
		input sqlc.Message
		run   sqlc.Run
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// Held, so that the message is written after whatever an edit or a
		// regeneration queued ahead of this deleted.
		if _, err := lockLive(ctx, q, conversationId); err != nil {
			return err
		}
		// An edit of the refused message, or a send that was refused first, may
		// have got in ahead.
		if err := requireRevised(ctx, q, conversationId); err != nil {
			return err
		}
		var err error
		input, err = appendInput(ctx, q, conversationId, content, inputPartsChecked(content, files, digests))
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
		run, err = createRun(ctx, q, conv, user.ID, input.ID, requestID, choice)
		return err
	})
	if err != nil {
		s.failRun(w, r, conversationId, requestID, err)
		return
	}
	s.startRun(conversationId, run, input)
	httpx.JSON(w, http.StatusAccepted, CreateMessageResponse{Message: toMessage(input, signed), Run: toRun(run)})
}

// runChoice is what a new run answers with: the model and where it is served
// from, the effort and the web switch — and what of that the conversation
// should remember.
type runChoice struct {
	modelID   string
	channelID uuid.UUID
	// effort is what the conversation remembers; runEffort what this run sends.
	effort, runEffort *string
	rememberEffort    bool
	webSearch         bool
	// continues is set when the run answers a question the model asked: the
	// first run of the answer it carries on (see ask_user.go).
	continues *uuid.UUID
}

// chooseRun settles a run's model, effort and web access from what the request
// asked for, else the conversation's.
func (s *Server) chooseRun(ctx context.Context, conv sqlc.Conversation, model *string, askedEffort *ReasoningEffort, askedWeb *bool) (runChoice, error) {
	var c runChoice
	modelID := ""
	if model != nil {
		modelID = strings.TrimSpace(*model)
	}
	if modelID == "" && conv.Model != nil {
		modelID = *conv.Model
	}
	// The asker's own standing, so a conversation that remembers a model since
	// kept for administrators cannot go on using it.
	modelID, channelID, err := s.pickChannel(ctx, modelID, askerFrom(ctx))
	if err != nil {
		return c, err
	}
	c.modelID, c.channelID = modelID, channelID

	// The effort asked for, else the conversation's. Only a level the model's
	// settings offer is sent: some endpoints reject the parameter outright, and
	// an operator may have withheld the level.
	c.effort = conv.ReasoningEffort
	if askedEffort != nil {
		requested, ok := effortColumn(*askedEffort)
		if !ok {
			return c, errInvalidEffort
		}
		c.rememberEffort = !sameEffort(requested, conv.ReasoningEffort)
		c.effort = requested
	}
	c.runEffort = c.effort
	if c.effort != nil {
		// A level this model does not offer — or the operator does not allow —
		// is dropped rather than sent: the conversation may remember it from a
		// model that had it.
		settings, err := loadModelSettings(ctx, s.q, modelID)
		if err != nil {
			return c, err
		}
		if !settings.Profile.Offers(llm.ReasoningEffort(*c.effort)) {
			c.runEffort = nil
		}
	}

	// Web access, asked for or else the conversation's.
	c.webSearch = conv.WebSearch
	if askedWeb != nil {
		c.webSearch = *askedWeb
	}
	return c, nil
}

// createRun starts a run on an input inside the caller's transaction, and has
// the conversation remember the choices it was started with.
//
// It first checks the account has a slot: how
// many runs may go at once is bounded, and the count and this insert are one
// step under a lock held until the transaction ends (run_limits.go). Every way
// of starting a run comes through here, so none of them can go around it; a
// refusal is returned for the caller to roll back, with nothing written.
func createRun(ctx context.Context, q *sqlc.Queries, conv sqlc.Conversation, userID, inputID uuid.UUID, requestID string, c runChoice) (sqlc.Run, error) {
	if err := admitRun(ctx, q, userID); err != nil {
		return sqlc.Run{}, err
	}
	params := sqlc.CreateRunParams{
		ConversationID:  conv.ID,
		UserID:          userID,
		InputMessageID:  inputID,
		ModelID:         c.modelID,
		ChannelID:       &c.channelID,
		ReasoningEffort: c.runEffort,
		WebSearch:       c.webSearch,
		Continues:       c.continues,
	}
	if requestID != "" {
		params.RequestID = &requestID
	}
	run, err := q.CreateRun(ctx, params)
	if err != nil {
		return run, err
	}
	if conv.Model == nil || *conv.Model != c.modelID {
		if _, err := q.SetConversationModel(ctx, sqlc.SetConversationModelParams{
			ID: conv.ID, UserID: &userID, Model: &c.modelID,
		}); err != nil {
			return run, err
		}
	}
	if c.rememberEffort {
		if _, err := q.SetConversationReasoning(ctx, sqlc.SetConversationReasoningParams{
			ID: conv.ID, UserID: &userID, ReasoningEffort: c.effort,
		}); err != nil {
			return run, err
		}
	}
	if c.webSearch != conv.WebSearch {
		if _, err := q.SetConversationWebSearch(ctx, sqlc.SetConversationWebSearchParams{
			ID: conv.ID, UserID: &userID, WebSearch: c.webSearch,
		}); err != nil {
			return run, err
		}
	}
	return run, q.TouchConversation(ctx, conv.ID)
}

// failRun answers a transaction that could not start a run: busy, a request
// that raced itself (whose winner is the answer), or anything else.
func (s *Server) failRun(w http.ResponseWriter, r *http.Request, conversationID uuid.UUID, requestID string, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		switch pgErr.ConstraintName {
		case "runs_one_active_per_conversation":
			httpx.Fail(w, r, errConversationBusy)
			return
		case "runs_request_id":
			// The same request raced itself; the winner's run is the answer.
			if found, ok := s.existingRun(r.Context(), conversationID, requestID); ok {
				httpx.JSON(w, http.StatusAccepted, found)
				return
			}
		}
	}
	httpx.Fail(w, r, err)
}

// startRun announces a committed run to the conversation's followers and sets
// it going. `also` are rows the same commit wrote, announced with it.
func (s *Server) startRun(conversationID uuid.UUID, run sqlc.Run, input sqlc.Message, also ...agent.Event) {
	s.runner.Hub().Publish(conversationID, agent.Batch{
		RunID:   run.ID,
		Version: run.Version,
		Events:  append([]agent.Event{agent.RunStarted{Run: run, Input: input}}, also...),
	})
	s.runner.Start(run.ID)
}

// inputParts is a user message: its text, then the files it carries as
// references (see agent.attachmentResolver for how they reach the model).
func inputParts(content string, files []sqlc.Attachment) []llm.Part {
	parts := []llm.Part{llm.TextPart{Text: content}}
	for _, file := range files {
		parts = append(parts, llm.AttachmentPart{Ref: file.ID.String(), Filename: file.Filename, MediaType: file.ContentType, Size: file.SizeBytes})
	}
	return parts
}

// modelTakes is whether a model with these input modalities can be sent a file
// of this type. A text file goes as words, which every model takes, and so do
// a Word or Excel file, and a PDF where this server can read its words.
func modelTakes(modalities []string, contentType string) bool {
	switch modality := llm.ModalityOf(contentType); modality {
	case "":
		return false
	case llm.ModalityText, llm.ModalityDocument:
		return true
	case llm.ModalityPDF:
		return slices.Contains(modalities, modality) || doctext.PDFReadable()
	default:
		return slices.Contains(modalities, modality)
	}
}

func attachmentIDs(files []sqlc.Attachment) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(files))
	for _, file := range files {
		ids = append(ids, file.ID)
	}
	return ids
}

// admitAttachments checks the files a message arrives with and returns them in
// the order they were listed, with the words read out of its documents
// (readDocuments). Everything it refuses is refused before anything is
// written.
func (s *Server) admitAttachments(ctx context.Context, userID uuid.UUID, modelID string, ids []uuid.UUID) ([]sqlc.Attachment, *fileDigests, error) {
	if len(ids) == 0 {
		return nil, &fileDigests{}, nil
	}
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	if len(unique) > agent.MaxMessageAttachments {
		return nil, nil, errTooManyAttachments
	}

	rows, err := s.q.ListPendingAttachments(ctx, sqlc.ListPendingAttachmentsParams{Ids: unique, UserID: &userID})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) != len(unique) {
		return nil, nil, errAttachmentUnavailable
	}
	files := make([]sqlc.Attachment, 0, len(rows))
	for _, id := range unique {
		i := slices.IndexFunc(rows, func(row sqlc.Attachment) bool { return row.ID == id })
		files = append(files, rows[i])
	}

	// What the model takes is settled before storage is asked anything.
	settings, err := loadModelSettings(ctx, s.q, modelID)
	if err != nil {
		return nil, nil, err
	}
	for _, file := range files {
		if !modelTakes(settings.InputModalities, file.ContentType) {
			return nil, nil, errAttachmentUnsupported
		}
	}

	// The sizes the client announced were only announced: what was stored is
	// what counts, and a file that never arrived is not one.
	var total int64
	for _, file := range files {
		size, err := s.store.Size(ctx, file.StorageKey)
		if err != nil {
			slog.WarnContext(ctx, "attachment not in storage", "attachment_id", file.ID, "error", err)
			return nil, nil, errAttachmentUnavailable
		}
		if size > uploadable[file.ContentType] {
			return nil, nil, errAttachmentsTooLarge
		}
		total += size
	}
	if total > agent.MaxAttachmentBytes {
		return nil, nil, errAttachmentsTooLarge
	}
	digests, err := s.readDocuments(ctx, files, slices.Contains(settings.InputModalities, llm.ModalityPDF))
	if err != nil {
		return nil, nil, err
	}
	return files, digests, nil
}

func sameEffort(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *Server) existingRun(ctx context.Context, conversationID uuid.UUID, requestID string) (CreateMessageResponse, bool) {
	if requestID == "" {
		return CreateMessageResponse{}, false
	}
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
	return CreateMessageResponse{Message: messageWithFiles(input, attached), Run: toRun(run)}, true
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
		snapshot.Messages = append(snapshot.Messages, messageWithFiles(row, attached))
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

// GetChatFeatures says what the chat can offer: web search when the
// administrator has it configured and on, and uploading a picture when that
// is allowed.
func (s *Server) GetChatFeatures(w http.ResponseWriter, r *http.Request) {
	config, key, err := loadWebSearch(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	avatars, err := boolSettingOr(r.Context(), s.q, settingAvatarUploads, false)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	tasks, err := s.tasksOn(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	traces, err := s.tracesOn(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	subagents, err := s.subagentsOn(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ChatFeatures{
		Subagents:      &subagents,
		WebSearch:      config.Enabled && config.searcher(key) != nil,
		AvatarUploads:  avatars,
		ScheduledTasks: tasks,
		RequestTraces:  traces && isStaff(r.Context()),
	})
}

// ListChatModels lists what an enabled channel offers, for the model picker.
func (s *Server) ListChatModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.chatModels(r.Context(), askerFrom(r.Context()))
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
