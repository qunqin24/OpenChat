package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	"github.com/qunqin/openchat/apps/server/internal/memory"
	"github.com/qunqin/openchat/apps/server/internal/tools"
)

// A guest's run.
//
// The agent runner reads everything from the database: the user, the
// conversation, the transcript, the attachments, the memories. A guest has none
// of that on the server - their browser keeps it - so each run is staged: the
// request carries the conversation's path, the rows are written under a user
// that exists for this request alone (users.guest), the run is started through
// the same code as an account's (chooseRun, createRun, startRun), its events
// are streamed back as they are committed, and the user is deleted when the
// stream ends. Cascades remove the rest; the files are removed after.
//
// The rows are written in the shape older conversations have, before runs
// existed: the rows of the transcript without a run, ordered by time. A
// conversation is its rows in order, so the transcript the model is sent is
// exactly what an account's would be. An edit needs nothing of the server:
// the browser has already deleted the question and what followed it, and its
// request ends before the new wording.

const (
	// guestBodyLimit bounds a request: the path with its tool results, and the
	// files that fit a request, base64.
	guestBodyLimit = 64 << 20
	// guestMaxRows bounds the transcript a request carries: the page sends the
	// last 300 rows, two more when it holds a summary (the rows the summary
	// keeps and ends at), and the model's context is held within its window by
	// compression, not by how many rows there are.
	guestMaxRows = 400
	// guestSummaryChars bounds a summary the browser sends back.
	guestSummaryChars = 400_000
	// guestFileBytes bounds the files of one request that are written to storage.
	guestFileBytes = 2 * agent.MaxAttachmentBytes
	// guestStaleAfter is how long a guest user may live before the sweep takes
	// it: far longer than any run, short enough that a crash leaves little.
	guestStaleAfter = 2 * time.Hour
	// guestRunWait is how long the end of a request waits for a stopped run to
	// finish before deleting its rows anyway.
	guestRunWait = 30 * time.Second
)

// guestFile is a file a message carries.
type guestFile struct {
	id          uuid.UUID
	filename    string
	contentType string
	size        int64
	// data is nil for a file the browser did not send (it no longer fits what
	// a request carries). It still has a row, with its size, so the model is
	// told what happened to it.
	data []byte
	key  string
	// receipt is the server's signature for the bytes (see guest_receipts.go).
	receipt string
}

// guestRow is one row of the browser's transcript, checked.
type guestRow struct {
	id      uuid.UUID
	role    sqlc.MessageRole
	content string
	parts   []llm.Part
	status  sqlc.MessageStatus
	stop    *string
	model   *string
	code    *string
	usage   []byte
	at      time.Time
	files   []guestFile
	// receipt is the server's signature for the row (see guest_receipts.go).
	receipt string
}

// guestPlan is a request, checked and ready to be written: nothing has touched
// the database yet.
type guestPlan struct {
	req       GuestRunRequest
	rows      []guestRow
	input     *guestRow // the new message, for send
	choice    runChoice
	temporary bool
	titled    bool
	memories  []GuestMemory
	// ids translates between the browser's memory ids and the ones the staged
	// rows have (see guestIDs).
	ids      *guestIDs
	snapshot []byte
	summary  *ContextSummary
	memoryOn bool
	about    string
	style    string
	// For an answer: the result to record, and the run it joins.
	result llm.ToolResultPart
	head   uuid.UUID

	// What nothing in the request vouches for (guest_receipts.go).
	receipts      GuestReceipts
	snapshotCanon []byte
	snapshotWords []string
	// signer signs what the run writes. active is whether moderation was on
	// when the request came: only then are words receipted as checked.
	signer  *receiptSigner
	active  bool
	pending []checkedWords
	fresh   guestFresh
}

func guestBad(code, message string) error {
	return httpx.NewError(http.StatusBadRequest, code, message)
}

// GuestRun answers one message of a guest, stateless.
func (s *Server) GuestRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body GuestRunRequest
	if err := decodeLenient(r, &body, guestBodyLimit); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ip := clientIP(r)
	// Before the request is counted against the hourly limit or anything is
	// stored: an address with all its runs going is told so at once. The
	// authoritative check is the one createRun makes under a lock.
	if err := s.guestRunSlotFree(ctx, ip); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.takeGuestRequest(ctx, ip); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	plan, err := s.planGuestRun(ctx, body)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Before a row is written or a file stored. A guest's message is not sent
	// unchecked when the check cannot be made. First what only the server could
	// have written (refused without a call to anyone), then the new message,
	// then whatever else of the request nothing vouches for, in one call
	// (guest_receipts.go).
	if err := s.verifyGuest(ctx, plan); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.moderateGuest(ctx, ip, plan); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.moderateWords(ctx, ip, plan.pending); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	plan.choice.clientIP = ip
	st, err := s.seedGuest(ctx, plan)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Last to run: the stream and the subscription are over by then, and the
	// run with them.
	defer s.endGuest(st)

	// Before the run starts, so the first batch is not missed.
	sub := s.runner.Hub().Subscribe(st.conv.ID)
	defer sub.Close()
	s.startRun(st.conv.ID, st.run, st.input, st.also...)

	sse, err := httpx.NewSSEWriter(w)
	if err != nil {
		s.stopGuestRun(st.run.ID)
		return
	}
	s.followGuestRun(ctx, sse, sub, plan, st)
}

// verifyGuest reads the request for what is not the browser's to claim: the
// conversation it carries, the instructions, the notes, the summary. What the
// server has signed passes; words a person could have written and no receipt
// covers are set aside to be moderated (plan.pending); a row of the model's or a
// summary no receipt covers is refused. With moderation off nothing is
// enforced, and nothing that says "checked" is signed.
func (s *Server) verifyGuest(ctx context.Context, plan *guestPlan) error {
	signer, err := s.guestSigner(ctx)
	if err != nil {
		return err
	}
	plan.signer = signer
	if plan.active, err = s.guestModerationOn(ctx); err != nil || !plan.active {
		return err
	}
	plan.pending, plan.fresh, err = signer.verify(guestContent{
		rows: plan.rows, about: plan.about, style: plan.style, receipts: plan.receipts,
		memories: plan.memories, snapshot: plan.snapshotCanon, snapTexts: plan.snapshotWords, summary: plan.summary,
	})
	return err
}

// moderateGuest checks what a guest is sending on: a new message, its pictures
// (those the page sent the bytes of), or the words typed in answer to a
// question. A regeneration sends nothing new. The conversation is not on the
// server, so the log has the address and no conversation.
func (s *Server) moderateGuest(ctx context.Context, ip string, plan *guestPlan) error {
	who := moderationSubject{guestIP: ip, source: ModerationSourceGuest}
	switch plan.req.Action {
	case "send":
		in := moderationInput{text: plan.input.content}
		var images []moderationImage
		for _, file := range plan.input.files {
			if file.data != nil && llm.ModalityOf(file.contentType) == llm.ModalityImage {
				images = append(images, moderationImage{contentType: file.contentType, data: file.data})
			}
		}
		if len(images) > 0 {
			in.images = func(context.Context) ([]moderationImage, error) { return images, nil }
		}
		return s.moderate(ctx, who, in)
	case "answer":
		var answers []tools.Answer
		for _, a := range derefOr(plan.req.Answers, nil) {
			answers = append(answers, tools.Answer{Selected: a.Selected, Other: derefOr(a.Other, "")})
		}
		return s.moderate(ctx, who, moderationInput{text: typedAnswers(answers)})
	}
	return nil
}

// CancelGuestRun stops a guest's run on request. The run's id is the capability:
// it was only given to the page that started the run.
func (s *Server) CancelGuestRun(w http.ResponseWriter, r *http.Request, runId openapi_types.UUID) {
	if _, err := s.q.RequestGuestRunCancel(r.Context(), runId); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Over already, or not a guest's: nothing to stop either way.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		httpx.Fail(w, r, err)
		return
	}
	s.runner.Cancel(runId)
	w.WriteHeader(http.StatusAccepted)
}

// stopGuestRun asks a run to stop without the request that follows it, which
// has gone or cannot go on.
func (s *Server) stopGuestRun(runID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.q.RequestGuestRunCancel(ctx, runID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("cancel guest run", "run_id", runID, "error", err)
	}
	s.runner.Cancel(runID)
}

// ---------------------------------------------------------------- the plan

// planGuestRun checks a request, and settles the model, before anything is
// written: every refusal a person can act on is made here.
func (s *Server) planGuestRun(ctx context.Context, body GuestRunRequest) (*guestPlan, error) {
	plan := &guestPlan{req: body, temporary: body.Temporary != nil && *body.Temporary, titled: body.Titled != nil && *body.Titled, ids: newGuestIDs(),
		receipts: derefOr(body.Receipts, GuestReceipts{})}
	switch body.Action {
	case "send", "regenerate", "answer":
	default:
		return nil, guestBad("invalid_action", "action must be send, regenerate or answer")
	}
	if body.ConversationId == uuid.Nil {
		return nil, guestBad("invalid_conversation", "conversationId is required")
	}
	if len(body.Messages) > guestMaxRows {
		return nil, guestBad("too_many_messages", fmt.Sprintf("a request carries at most %d rows", guestMaxRows))
	}

	// Personalization.
	plan.about, plan.style = derefOr(body.AboutYou, ""), derefOr(body.ResponseStyle, "")
	for field, value := range map[string]string{"aboutYou": plan.about, "responseStyle": plan.style} {
		if utf8.RuneCountInString(value) > memory.MaxInstructions {
			return nil, httpx.NewFieldError(field, "instructions_too_long", "the instructions are too long")
		}
	}
	plan.memoryOn = derefOr(body.MemoryEnabled, true)
	if body.Memories != nil {
		if len(*body.Memories) > memory.MaxMemories {
			return nil, guestBad("too_many_memories", fmt.Sprintf("at most %d memories", memory.MaxMemories))
		}
		ids := map[uuid.UUID]bool{}
		for _, m := range *body.Memories {
			content, err := memory.Clean(m.Content)
			if err != nil || m.Id == uuid.Nil || ids[m.Id] {
				return nil, guestBad("invalid_memory", "a memory is empty, too long or repeated")
			}
			ids[m.Id] = true
			// In the order sent: the staged ids ascend, and the list is read by id (ListMemories)
			// among rows written at one instant.
			plan.ids.server(m.Id)
			m.Content = content
			plan.memories = append(plan.memories, m)
		}
	}
	if body.MemorySnapshot != nil {
		// Through the type it is read as, so what the prompt is rendered from
		// is a snapshot and nothing else the browser could write there.
		raw, err := json.Marshal(*body.MemorySnapshot)
		if err != nil {
			return nil, guestBad("invalid_snapshot", "memorySnapshot is not a snapshot")
		}
		var snap memory.Snapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return nil, guestBad("invalid_snapshot", "memorySnapshot is not a snapshot")
		}
		// As the browser has it, before its ids are exchanged.
		plan.snapshotCanon, plan.snapshotWords = snapshotCanon(snap), snapshotWords(snap)
		// Its ids are the browser's; the rows the run reads are staged under
		// their own (guestIDs), and the snapshot has to name those.
		for i := range snap.Memories {
			snap.Memories[i].ID = plan.ids.server(snap.Memories[i].ID)
		}
		for i := range snap.Added {
			snap.Added[i].ID = plan.ids.server(snap.Added[i].ID)
		}
		if plan.snapshot, err = json.Marshal(snap); err != nil {
			return nil, err
		}
	}

	// The transcript.
	ids := map[uuid.UUID]bool{}
	files := map[uuid.UUID]bool{}
	var total int64
	for _, m := range body.Messages {
		row, err := guestRowOf(m, ids, files, &total)
		if err != nil {
			return nil, err
		}
		plan.rows = append(plan.rows, row)
	}
	spaceOut(plan.rows, time.Now())

	// The conversation's compressed context, written over the rows as it is on
	// an account: its last row and the question it keeps have to be rows of
	// the request, or it would describe a transcript this run does not have.
	if sum := body.Summary; sum != nil {
		if strings.TrimSpace(sum.Content) == "" || len(sum.Content) > guestSummaryChars || sum.InputTokens < 0 || sum.OutputTokens < 0 {
			return nil, guestBad("invalid_summary", "the summary is empty or too long")
		}
		through := slices.IndexFunc(plan.rows, func(r guestRow) bool { return r.id == sum.ThroughMessageId })
		if through < 0 {
			return nil, guestBad("invalid_summary", "the summary ends at a message the request does not carry")
		}
		if sum.KeptMessageId != nil {
			kept := slices.IndexFunc(plan.rows, func(r guestRow) bool { return r.id == *sum.KeptMessageId })
			if kept < 0 || kept > through || plan.rows[kept].role != sqlc.MessageRoleUser {
				return nil, guestBad("invalid_summary", "the summary keeps a message that is not a user message of the request")
			}
		}
		plan.summary = sum
	}

	switch body.Action {
	case "send":
		if body.Input == nil || body.Input.Role != RoleUser {
			return nil, guestBad("invalid_input", "send needs an input that is a user message")
		}
		row, err := guestRowOf(*body.Input, ids, files, &total)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(row.content) == "" {
			return nil, httpx.NewError(http.StatusBadRequest, "empty_content", "content must not be empty")
		}
		row.content = strings.TrimSpace(row.content)
		if len(row.files) > agent.MaxMessageAttachments {
			return nil, errTooManyAttachments
		}
		row.parts = inputPartsOf(row.content, row.files)
		plan.input = &row
	case "regenerate":
		if len(plan.rows) == 0 || plan.rows[len(plan.rows)-1].role != sqlc.MessageRoleUser {
			return nil, errNothingToAnswer
		}
	case "answer":
		if body.CallId == nil || *body.CallId == "" || len(plan.rows) == 0 {
			return nil, errNotPending
		}
		questions, err := askedQuestions(plan.rows[len(plan.rows)-1], *body.CallId)
		if err != nil {
			return nil, err
		}
		var answers []tools.Answer
		if body.Answers != nil {
			for _, a := range *body.Answers {
				answers = append(answers, tools.Answer{Selected: a.Selected, Other: derefOr(a.Other, "")})
			}
		}
		if plan.result, err = answerPart(questions, *body.CallId, answers); err != nil {
			return nil, err
		}
		if body.Continues == nil || *body.Continues == uuid.Nil {
			return nil, guestBad("invalid_continues", "an answer needs the run that began the reply")
		}
		plan.head = *body.Continues
		if !slices.ContainsFunc(plan.rows, func(r guestRow) bool { return r.role == sqlc.MessageRoleUser }) {
			return nil, errNotPending
		}
	}

	// The model, as an ordinary user's: the models kept for administrators are
	// not offered (isStaff is false with no signed-in user).
	conv := sqlc.Conversation{WebSearch: true}
	choice, err := s.chooseRun(ctx, conv, body.Model, body.ReasoningEffort, body.WebSearch)
	if err != nil {
		return nil, err
	}
	plan.choice = choice
	plan.ids.seal()
	if plan.input != nil && len(plan.input.files) > 0 {
		settings, err := loadModelSettings(ctx, s.q, choice.modelID)
		if err != nil {
			return nil, err
		}
		for _, file := range plan.input.files {
			modality := llm.ModalityOf(file.contentType)
			if modality == "" || !slices.Contains(settings.InputModalities, modality) {
				return nil, errAttachmentUnsupported
			}
		}
	}
	return plan, nil
}

// guestRowOf checks one row of the browser's transcript and turns it into what
// the database holds. ids and files are the ones seen so far: a repeated id is
// refused, and total counts the bytes of the files sent.
func guestRowOf(m GuestMessage, ids, files map[uuid.UUID]bool, total *int64) (guestRow, error) {
	row := guestRow{id: m.Id, role: sqlc.MessageRole(m.Role), content: m.Content, at: m.CreatedAt, receipt: derefOr(m.Receipt, "")}
	if m.Id == uuid.Nil || ids[m.Id] {
		return row, guestBad("invalid_message", "a message has no id, or its id is repeated")
	}
	ids[m.Id] = true
	switch row.role {
	case sqlc.MessageRoleUser, sqlc.MessageRoleAssistant, sqlc.MessageRoleTool:
	default:
		return row, guestBad("invalid_message", "a message is not from the user, the assistant or a tool")
	}
	row.status = sqlc.MessageStatusComplete

	if row.role == sqlc.MessageRoleUser {
		for _, f := range derefOr(m.Attachments, nil) {
			file, err := guestFileOf(f, files, total)
			if err != nil {
				return row, err
			}
			row.files = append(row.files, file)
		}
		row.parts = inputPartsOf(row.content, row.files)
		return row, nil
	}

	row.parts = guestParts(m)
	row.content = agent.PlainText(row.parts)
	if m.StopReason != nil {
		stop := string(*m.StopReason)
		row.stop = &stop
		if stop == agent.StopError {
			row.status = sqlc.MessageStatusFailed
		}
	}
	row.model, row.code = m.Model, m.ErrorCode
	if m.Usage != nil {
		row.usage, _ = json.Marshal(m.Usage)
	}
	return row, nil
}

func guestFileOf(f GuestAttachment, seen map[uuid.UUID]bool, total *int64) (guestFile, error) {
	file := guestFile{id: f.Id, filename: strings.TrimSpace(f.Filename), size: f.Size, receipt: derefOr(f.Receipt, "")}
	contentType, _, _ := mime.ParseMediaType(f.ContentType)
	limit, allowed := uploadable[contentType]
	switch {
	case f.Id == uuid.Nil || seen[f.Id]:
		return file, guestBad("invalid_attachment", "a file has no id, or its id is repeated")
	case file.filename == "":
		return file, httpx.NewFieldError("attachments", "invalid_filename", "filename must not be empty")
	case !allowed:
		return file, httpx.NewError(http.StatusUnsupportedMediaType, "unsupported_file_type",
			"only images (PNG, JPEG, WebP, GIF) and PDFs can be attached")
	}
	seen[f.Id] = true
	file.contentType = contentType
	if f.Data != nil {
		file.data = *f.Data
		file.size = int64(len(file.data))
		if *total += file.size; *total > guestFileBytes {
			return file, errAttachmentsTooLarge
		}
	}
	if file.size <= 0 || file.size > limit {
		return file, errAttachmentsTooLarge
	}
	// The key is given when the file is written (seedGuest).
	return file, nil
}

// guestFileKey is where a file of one request is stored. The name carries a
// fresh id and nothing of the browser's: the browser chooses its ids, a
// duplicate request (two tabs, a double submit) carries the same ones, and a key
// both of them wrote would be deleted by whichever is refused, taking the file
// out from under the one that was accepted. Every file of every request gets a
// key no other has; the date keeps the layout of the other uploads.
func guestFileKey(file guestFile) string {
	return fmt.Sprintf("uploads/guest/%s/%s%s", time.Now().UTC().Format("2006/01/02"), uuid.NewString(), keyExt(file.filename))
}

// inputPartsOf is a user message: its text, then its files as references (see
// inputParts).
func inputPartsOf(content string, files []guestFile) []llm.Part {
	parts := []llm.Part{llm.TextPart{Text: content}}
	for _, file := range files {
		parts = append(parts, llm.AttachmentPart{Ref: file.id.String(), Filename: file.filename, MediaType: file.contentType})
	}
	return parts
}

// guestParts are the blocks of an assistant or tool row. An assistant row that
// came with its provider state is taken from that, exactly as stored: the
// signatures a provider wants back are in it and nowhere else. Otherwise the
// blocks the browser holds are mapped back.
func guestParts(m GuestMessage) []llm.Part {
	if m.ProviderState != nil && m.Role == RoleAssistant {
		if raw, err := base64.StdEncoding.DecodeString(*m.ProviderState); err == nil {
			if parts, err := agent.DecodeParts(raw); err == nil {
				kept := make([]llm.Part, 0, len(parts))
				for _, part := range parts {
					// Files are for user messages, and by reference.
					if _, ok := part.(llm.AttachmentPart); !ok {
						kept = append(kept, part)
					}
				}
				return kept
			}
		}
	}
	parts := make([]llm.Part, 0, len(m.Parts))
	for _, part := range m.Parts {
		kind, err := part.Discriminator()
		if err != nil {
			continue
		}
		switch kind {
		case "text":
			if p, err := part.AsTextPart(); err == nil {
				parts = append(parts, llm.TextPart{Text: p.Text})
			}
		case "reasoning":
			if p, err := part.AsReasoningPart(); err == nil {
				parts = append(parts, llm.ReasoningPart{Text: p.Text, DurationMS: p.DurationMs})
			}
		case "tool_call":
			if p, err := part.AsToolCallPart(); err == nil {
				call := llm.ToolCallPart{ID: p.Id, Name: p.Name, RawArguments: p.RawArguments}
				if p.Arguments != nil {
					call.Arguments, _ = json.Marshal(*p.Arguments)
				}
				parts = append(parts, call)
			}
		case "tool_result":
			if p, err := part.AsToolResultPart(); err == nil {
				result := llm.ToolResultPart{
					CallID: p.CallId, Name: p.Name, Content: p.Content,
					Status: llm.ToolResultStatus(p.Status), DurationMS: p.DurationMs,
				}
				if result.Status != llm.ToolSucceeded {
					result.Error = p.Content
				}
				if p.Details != nil {
					result.Details, _ = json.Marshal(*p.Details)
				}
				parts = append(parts, result)
			}
		}
	}
	return parts
}

// askedQuestions finds the ask_user call this answer is for in the last row of
// the transcript, which is where a question waiting for its answer is (see
// ask_user.go).
func askedQuestions(last guestRow, callID string) (tools.Questions, error) {
	var none tools.Questions
	if last.role != sqlc.MessageRoleAssistant {
		return none, errNotPending
	}
	for _, part := range last.parts {
		call, ok := part.(llm.ToolCallPart)
		if !ok || call.ID != callID || call.Name != agent.ToolAskUser {
			continue
		}
		questions, err := tools.ParseQuestions(call.Arguments)
		if err != nil {
			return none, errNotPending
		}
		return questions, nil
	}
	return none, errNotPending
}

// spaceOut gives the rows the times they are ordered by. The browser's clock is
// not the database's: the rows keep its times (a user message's date is what
// the model is told, and must not change between turns) but are put in strictly
// increasing order and before now, so that what the run writes comes after
// them whatever the browser's clock said.
func spaceOut(rows []guestRow, now time.Time) {
	const step = time.Millisecond
	var prev time.Time
	for i := range rows {
		if rows[i].at.IsZero() || !rows[i].at.After(prev) {
			rows[i].at = prev.Add(step)
			if prev.IsZero() {
				rows[i].at = now.Add(-time.Duration(len(rows)-i+1) * step)
			}
		}
		prev = rows[i].at
	}
	limit := now.Add(-5 * step)
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].at.After(limit) {
			rows[i].at = limit
		}
		limit = rows[i].at.Add(-step)
	}
}

// ----------------------------------------------------------------- staging

// guestRunState is a run staged for a guest.
type guestRunState struct {
	user  sqlc.User
	conv  sqlc.Conversation
	input sqlc.Message
	run   sqlc.Run
	// also are rows the run's first commit wrote besides its input.
	also []agent.Event
	// stored are the files written for the request, removed when it ends.
	stored []string
}

// seedGuest writes the guest's rows and starts nothing: the user, the
// conversation, the transcript, the files, the memories, and the run.
func (s *Server) seedGuest(ctx context.Context, plan *guestPlan) (st *guestRunState, err error) {
	st = &guestRunState{}
	// Kept apart from the result: a failure returns a nil state, and the files
	// written so far still have to be removed.
	staged := st
	// The files first: a row is wanted only once the file it names exists.
	defer func() {
		if err != nil {
			s.removeFiles(context.WithoutCancel(ctx), staged.stored)
		}
	}()
	all := make([]*guestFile, 0)
	for i := range plan.rows {
		for j := range plan.rows[i].files {
			all = append(all, &plan.rows[i].files[j])
		}
	}
	if plan.input != nil {
		for j := range plan.input.files {
			all = append(all, &plan.input.files[j])
		}
	}
	for _, file := range all {
		if file.data == nil {
			file.key = "uploads/guest/unsent/" + file.id.String()
			continue
		}
		file.key = guestFileKey(*file)
		if err := s.store.PutObject(ctx, file.key, file.contentType, file.data); err != nil {
			return nil, err
		}
		st.stored = append(st.stored, file.key)
	}

	tag := uuid.NewString()
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		if st.user, err = q.CreateGuestUser(ctx, sqlc.CreateGuestUserParams{
			Email: "guest-" + tag + "@guest.invalid", Name: "guest-" + tag,
			InstructionsAbout: plan.about, InstructionsStyle: plan.style, MemoryEnabled: plan.memoryOn,
		}); err != nil {
			return err
		}
		userID := st.user.ID
		effort := plan.choice.effort
		if st.conv, err = q.CreateGuestConversation(ctx, sqlc.CreateGuestConversationParams{
			ID: plan.req.ConversationId, UserID: &userID, Title: defaultTitle,
			Model: &plan.choice.modelID, ReasoningEffort: effort, WebSearch: plan.choice.webSearch,
			Temporary: plan.temporary, MemorySnapshot: plan.snapshot,
		}); err != nil {
			return err
		}
		for _, m := range plan.memories {
			id := plan.ids.server(m.Id)
			source := memory.SourceUser
			if m.Source != nil && *m.Source == MemorySource(memory.SourceModel) {
				source = memory.SourceModel
			}
			if _, err := q.InsertMemory(ctx, sqlc.InsertMemoryParams{ID: &id, UserID: userID, Content: m.Content, Source: source}); err != nil {
				return err
			}
		}

		// The transcript, in order; the rows have no run and are read by their time.
		var lastInput sqlc.Message
		for _, row := range plan.rows {
			written, err := writeGuestRow(ctx, q, st.conv.ID, userID, row)
			if err != nil {
				return err
			}
			if row.role == sqlc.MessageRoleUser {
				lastInput = written
			}
		}

		if sum := plan.summary; sum != nil {
			if _, err := q.UpsertContextSummary(ctx, sqlc.UpsertContextSummaryParams{
				ConversationID: st.conv.ID, ThroughMessageID: sum.ThroughMessageId, KeptMessageID: sum.KeptMessageId,
				Content: sum.Content, ModelID: sum.Model, InputTokens: int32(sum.InputTokens), OutputTokens: int32(sum.OutputTokens),
			}); err != nil {
				return err
			}
		}

		switch plan.req.Action {
		case "send":
			in := plan.input
			id := in.id
			if st.input, err = appendInput(ctx, q, st.conv.ID, &id, in.content, in.parts); err != nil {
				return err
			}
			if err := attachGuestFiles(ctx, q, st.input.ID, userID, in.files); err != nil {
				return err
			}
			st.run, err = createRun(ctx, q, st.conv, userID, st.input.ID, "", plan.choice)
		case "regenerate":
			st.input = lastInput
			st.run, err = createRun(ctx, q, st.conv, userID, st.input.ID, "", plan.choice)
		case "answer":
			st.input = lastInput
			// The run that asked, standing in for the one the browser has.
			if _, err = q.CreateGuestStubRun(ctx, sqlc.CreateGuestStubRunParams{
				ID: plan.head, ConversationID: st.conv.ID, UserID: userID, InputMessageID: st.input.ID,
				ModelID: plan.choice.modelID, ChannelID: &plan.choice.channelID,
			}); err != nil {
				return err
			}
			choice := plan.choice
			head := plan.head
			choice.continues = &head
			var written sqlc.Message
			st.run, written, err = startAnswerRun(ctx, q, st.conv, userID, st.input.ID, "", choice, plan.result)
			st.also = []agent.Event{agent.MessageAppended{Message: written}}
		}
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			// An id the browser chose is already in use: the same conversation
			// sent from two places at once.
			return nil, errConversationBusy
		}
		return nil, err
	}
	return st, nil
}

// writeGuestRow writes one row of the browser's transcript.
func writeGuestRow(ctx context.Context, q *sqlc.Queries, conversationID, userID uuid.UUID, row guestRow) (sqlc.Message, error) {
	id := row.id
	params := sqlc.AppendEntryParams{
		ID:             &id,
		ConversationID: conversationID,
		Role:           row.role,
		Content:        row.content,
		Parts:          agent.EncodeParts(row.parts),
		Status:         row.status,
		StopReason:     row.stop,
		Model:          row.model,
		Usage:          row.usage,
		ErrorCode:      row.code,
		CreatedAt:      pgtype.Timestamptz{Time: row.at, Valid: true},
	}
	written, err := q.AppendEntry(ctx, params)
	if err != nil {
		return written, err
	}
	return written, attachGuestFiles(ctx, q, written.ID, userID, row.files)
}

func attachGuestFiles(ctx context.Context, q *sqlc.Queries, messageID, userID uuid.UUID, files []guestFile) error {
	for _, file := range files {
		if _, err := q.CreateGuestAttachment(ctx, sqlc.CreateGuestAttachmentParams{
			ID: file.id, MessageID: &messageID, UserID: &userID, StorageKey: file.key,
			Filename: file.filename, ContentType: file.contentType, SizeBytes: file.size,
		}); err != nil {
			return err
		}
	}
	return nil
}

// endGuest deletes what a request staged: the user, with the conversation,
// transcript, runs, attachments and memories that cascade from it, and then
// the files. The run is waited for first - it was stopped if the request is
// ending early - so that it does not write to rows that are going.
func (s *Server) endGuest(st *guestRunState) {
	ctx, cancel := context.WithTimeout(context.Background(), guestRunWait+30*time.Second)
	defer cancel()

	deadline := time.Now().Add(guestRunWait)
	for time.Now().Before(deadline) {
		run, err := s.q.GetRun(ctx, st.run.ID)
		if err != nil || run.Status != sqlc.RunStatusRunning {
			break
		}
		s.stopGuestRun(st.run.ID)
		time.Sleep(50 * time.Millisecond)
	}
	userID := st.user.ID
	keys, err := s.q.DeleteGuestUser(ctx, &userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// The sweep takes it within hours; the files with it.
		slog.Error("delete guest", "user_id", st.user.ID, "error", err)
		return
	}
	s.removeFiles(ctx, append(keys, st.stored...))
}

// sweepGuests deletes the guests a process that died left behind.
func (s *Server) sweepGuests(ctx context.Context, before time.Time) {
	s.removeConversations(ctx, "stale guests", func() (int, []string, error) {
		rows, err := s.q.DeleteStaleGuestUsers(ctx, sqlc.DeleteStaleGuestUsersParams{
			Before: pgtype.Timestamptz{Time: before, Valid: true}, RowLimit: cleanupBatch,
		})
		var keys []string
		for _, r := range rows {
			keys = append(keys, r.StorageKeys...)
		}
		return len(rows), keys, err
	})
	if n, err := s.q.PurgeGuestRequests(ctx, pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}); err != nil {
		slog.WarnContext(ctx, "purge guest requests", "error", err)
	} else if n > 0 {
		slog.DebugContext(ctx, "purged guest requests", "count", n)
	}
}

// ----------------------------------------------------------------- streaming

// followGuestRun sends the run's events as they are committed, then the last
// frame, and returns when the run is over or the client is gone.
func (s *Server) followGuestRun(ctx context.Context, sse *httpx.SSEWriter, sub *agent.Subscription, plan *guestPlan, st *guestRunState) {
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	var finished *sqlc.Run
	for finished == nil {
		select {
		case <-ctx.Done():
			s.stopGuestRun(st.run.ID)
			return
		case <-heartbeat.C:
			if err := sse.Comment("ping"); err != nil {
				s.stopGuestRun(st.run.ID)
				return
			}
		case <-sub.Wake():
			batches, resync := sub.Drain()
			if resync {
				// Too far behind to be sent every step: the state as it is,
				// which is the whole of the run if it has ended meanwhile.
				run, err := s.q.GetRun(ctx, st.run.ID)
				if err != nil {
					s.stopGuestRun(st.run.ID)
					return
				}
				if err := s.sendGuestSnapshot(ctx, sse, run, plan); err != nil {
					s.stopGuestRun(st.run.ID)
					return
				}
				if run.Status != sqlc.RunStatusRunning {
					finished = &run
				}
				continue
			}
			for _, batch := range batches {
				if err := sendGuestBatch(sse, batch, plan); err != nil {
					s.stopGuestRun(st.run.ID)
					return
				}
				for _, event := range batch.Events {
					if done, ok := event.(agent.RunFinished); ok && done.Run.ID == st.run.ID {
						run := done.Run
						finished = &run
					}
				}
			}
		}
	}

	title := ""
	if !plan.titled && !plan.temporary &&
		(finished.Status == sqlc.RunStatusCompleted || finished.Status == sqlc.RunStatusAwaitingInput) {
		title = s.guestTitleAfterRun(ctx, sse, heartbeat, *finished, st)
	}
	end, err := s.guestEnd(ctx, st, plan, title)
	if err != nil {
		slog.ErrorContext(ctx, "guest end", "error", err)
		return
	}
	_ = plan.ids.send(sse, end)
}

// guestTitleAfterRun names the conversation from the exchange just made, as an
// account's is named in the background, but here before the stream ends: after
// it the rows are gone. The stream keeps its heartbeat meanwhile.
func (s *Server) guestTitleAfterRun(ctx context.Context, sse *httpx.SSEWriter, heartbeat *time.Ticker, run sqlc.Run, st *guestRunState) string {
	ctx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()
	done := make(chan string, 1)
	go func() {
		exchange, err := s.exchangeOf(ctx, run)
		if err != nil || exchange == "" {
			done <- ""
			return
		}
		title, err := s.generateTitle(ctx, st.user.ID, s.endUsers.ForRun(ctx, run), st.conv.ID, run.ModelID, run.ChannelID, exchange)
		if err != nil {
			slog.WarnContext(ctx, "guest title", "error", err)
			title = ""
		}
		done <- title
	}()
	for {
		select {
		case title := <-done:
			return title
		case <-ctx.Done():
			return ""
		case <-heartbeat.C:
			if err := sse.Comment("ping"); err != nil {
				return ""
			}
		}
	}
}

// guestEnd is the last frame: what the browser keeps.
func (s *Server) guestEnd(ctx context.Context, st *guestRunState, plan *guestPlan, title string) (GuestEndEvent, error) {
	ctx = context.WithoutCancel(ctx)
	end := GuestEndEvent{Type: GuestEnd, Memories: []Memory{}, MemoryEvents: []MemoryEvent{}}
	run, err := s.q.GetRun(ctx, st.run.ID)
	if err != nil {
		return end, err
	}
	end.Run = toRun(run)
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: st.conv.ID, UserID: &st.user.ID})
	if err != nil {
		return end, err
	}
	if len(conv.MemorySnapshot) > 0 {
		if err := json.Unmarshal(conv.MemorySnapshot, &end.MemorySnapshot); err != nil {
			return end, err
		}
	}
	notes := s.notes()
	memories, err := notes.List(ctx, st.user.ID)
	if err != nil {
		return end, err
	}
	for _, m := range memories {
		end.Memories = append(end.Memories, toMemory(m))
	}
	events, err := notes.Events(ctx, st.user.ID, memory.MaxMemories)
	if err != nil {
		return end, err
	}
	for _, e := range events {
		end.MemoryEvents = append(end.MemoryEvents, toMemoryEvent(e))
	}
	if title != "" {
		end.Title = &title
	}
	// The summary as the run left it: the one it was sent, or the one it made.
	summary, err := agent.LoadSummary(ctx, s.q, st.conv.ID)
	if err != nil {
		return end, err
	}
	end.Summary = toContextSummary(summary)
	plan.signSummary(end.Summary)
	plan.endReceipts(&end)
	return end, nil
}

// endReceipts puts in the last frame the receipts for what this request had
// moderated, so that the browser need not have it moderated again: the new
// message and any row that came without a receipt, the pictures among them, the
// instructions, the notes as they are now (the model may have saved some), and
// the snapshot as it stands.
func (p *guestPlan) endReceipts(end *GuestEndEvent) {
	if p == nil || p.signer == nil || !p.active {
		return
	}
	var r GuestReceipts
	if p.about != "" {
		v := p.signer.text(p.about)
		r.AboutYou = &v
	}
	if p.style != "" {
		v := p.signer.text(p.style)
		r.ResponseStyle = &v
	}
	messages, files := map[string]string{}, map[string]string{}
	rows := p.rows
	if p.input != nil {
		rows = append(append([]guestRow(nil), rows...), *p.input)
	}
	for _, row := range rows {
		if row.role != sqlc.MessageRoleUser {
			continue
		}
		isInput := p.input != nil && row.id == p.input.id
		if isInput || p.fresh.rows[row.id] {
			messages[row.id.String()] = p.signer.text(row.content)
		}
		for _, file := range row.files {
			if file.data != nil && (isInput || p.fresh.files[file.id]) && llm.ModalityOf(file.contentType) == llm.ModalityImage {
				files[file.id.String()] = p.signer.file(file.contentType, file.data)
			}
		}
	}
	r.Messages, r.Files = &messages, &files
	for i := range end.Memories {
		v := p.signer.text(p.ids.text(end.Memories[i].Content))
		end.Memories[i].Receipt = &v
	}
	if end.MemorySnapshot != nil {
		if raw, err := json.Marshal(end.MemorySnapshot); err == nil {
			var snap memory.Snapshot
			if json.Unmarshal(raw, &snap) == nil {
				v := p.signer.snapshot(snap)
				r.MemorySnapshot = &v
			}
		}
	}
	end.Receipts = &r
}

// sendGuestBatch maps a batch for a guest. It differs from an account's in one
// thing: an assistant row carries its provider state, which the browser must
// hold since the server will not.
func sendGuestBatch(sse *httpx.SSEWriter, batch agent.Batch, plan *guestPlan) error {
	events := make([]RunEvent, 0, len(batch.Events))
	for _, event := range batch.Events {
		mapped, ok := guestRunEvent(event, plan)
		if ok {
			events = append(events, mapped)
		}
	}
	return plan.ids.send(sse, BatchEvent{Type: Batch, RunId: batch.RunID, Version: batch.Version, Events: events})
}

func guestRunEvent(event agent.Event, plan *guestPlan) (RunEvent, bool) {
	var out RunEvent
	switch e := event.(type) {
	case agent.RunStarted:
		message := toMessage(e.Input, nil)
		plan.sign(&message)
		return out, out.FromRunStartedEvent(RunStartedEvent{Type: RunStarted, Run: toRun(e.Run), Message: message}) == nil
	case agent.MessageAppended:
		message := toMessage(e.Message, nil)
		message.ProviderState = providerState(e.Message)
		plan.sign(&message)
		return out, out.FromMessageAppendedEvent(MessageAppendedEvent{Type: MessageAppended, Message: message}) == nil
	case agent.CompactionFinished:
		summary := toContextSummary(e.Summary)
		plan.signSummary(summary)
		return out, out.FromCompactionFinishedEvent(CompactionFinishedEvent{Type: CompactionFinished, Summary: summary}) == nil
	}
	return toRunEvent(event)
}

// sign puts the receipt on a row on its way to the browser: a row of the
// model's always (it is the server's own work), a user row's words only when
// they were checked, which in this request they have been - the new message, a
// regenerated one, or one that came with a receipt.
func (p *guestPlan) sign(m *Message) {
	if p == nil || p.signer == nil {
		return
	}
	switch m.Role {
	case RoleUser:
		if p.active {
			r := p.signer.text(m.Content)
			m.Receipt = &r
		}
	case RoleAssistant, RoleTool:
		// Of the row the browser will have: its ids are turned back on the way
		// out (guestIDs), and what it sends back is that form.
		r := p.signer.messageReceipt(p.ids.forBrowser(*m))
		m.Receipt = &r
	}
}

// signSummary signs a summary the server's model wrote.
func (p *guestPlan) signSummary(s *ContextSummary) {
	if p == nil || p.signer == nil || s == nil {
		return
	}
	r := p.signer.summary(p.ids.text(s.Content))
	s.Receipt = &r
}

// providerState is an assistant row's blocks as stored, for the browser to hand
// back with the next request.
func providerState(row sqlc.Message) *string {
	if row.Role != sqlc.MessageRoleAssistant || len(row.Parts) == 0 {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString(row.Parts)
	return &encoded
}

// sendGuestSnapshot sends a run as it is: the rows it has written and, if it
// is still going, where it is.
func (s *Server) sendGuestSnapshot(ctx context.Context, sse *httpx.SSEWriter, run sqlc.Run, plan *guestPlan) error {
	rows, err := s.q.ListRunMessages(ctx, sqlc.ListRunMessagesParams{RunID: &run.ID, InputMessageID: run.InputMessageID})
	if err != nil {
		return err
	}
	live, err := agent.DecodeLive(run.Live)
	if err != nil {
		return err
	}
	mapped, state := toRun(run), toLiveState(live)
	snapshot := SnapshotEvent{Type: Snapshot, Run: &mapped, Live: &state, Messages: make([]Message, 0, len(rows))}
	for _, row := range rows {
		message := toMessage(row, nil)
		message.ProviderState = providerState(row)
		plan.sign(&message)
		snapshot.Messages = append(snapshot.Messages, message)
	}
	return plan.ids.send(sse, snapshot)
}
