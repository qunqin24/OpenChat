package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/llm"
	ledger "github.com/qunqin/openchat/apps/server/internal/usage"
)

// Titling a conversation from its first exchange.
//
// After a run finishes, a conversation still under the default title is
// given one by the task model (System settings), or by the conversation's own
// model when none is set. It is a background task: the answer is already
// shown and the run is over, so a slow or failed title costs nothing but the
// title — the conversation keeps "New chat" and the next answer tries again.
// A title the user typed meanwhile is never overwritten (SetGeneratedTitle).

// defaultTitle is what a conversation is called until it is titled.
const defaultTitle = "New chat"

// settingTaskModel is the app_settings key holding the task model's id; empty
// or absent means each conversation's own model.
const settingTaskModel = "task_model"

const (
	// titleRecentRows is how much of a conversation a title on request reads:
	// its last few exchanges.
	titleRecentRows = 8
	titleTimeout    = 45 * time.Second
	titleMaxRunes   = 40
	titleInputCap   = 1200
	titleAnswerCap  = 1800
)

const titlePrompt = `Write a short title for the conversation below: at most 6 words in English, or at most 12 characters in Chinese — in the language the user wrote in. Name the topic. No quotes, no emoji, no punctuation at the end. Reply with the title alone.`

// TitleAfterRun is the runner's after-run hook. It returns at once; the title
// is made on a goroutine of its own.
func (s *Server) TitleAfterRun(run sqlc.Run) {
	// A run that stopped to ask a question has been read by the user too, and
	// the question is often all there is to name the conversation by.
	if run.Status != sqlc.RunStatusCompleted && run.Status != sqlc.RunStatusAwaitingInput {
		return
	}
	go s.titleConversation(run)
}

func (s *Server) titleConversation(run sqlc.Run) {
	ctx, cancel := context.WithTimeout(context.Background(), titleTimeout)
	defer cancel()
	log := slog.With("conversation_id", run.ConversationID, "run_id", run.ID)

	// A guest's conversation is named by the request that ran it (guest_run.go),
	// which hands the title to the browser that keeps the conversation.
	if guest, err := s.q.IsGuestUser(ctx, run.UserID); err != nil || guest {
		return
	}
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: run.ConversationID, UserID: &run.UserID})
	// A temporary chat is never named: it is gone in a day and not in the list.
	if err != nil || conv.Title != defaultTitle || conv.Temporary {
		return
	}
	exchange, err := s.exchangeOf(ctx, run)
	if err != nil {
		log.Warn("title: read the exchange", "error", err)
		return
	}
	if exchange == "" {
		return
	}
	title, err := s.generateTitle(ctx, run.UserID, run.ConversationID, run.ModelID, run.ChannelID, exchange)
	if errors.Is(err, errModelCapacity) {
		// Not a failure: the model's spending cap spares the request.
		log.Info("title: skipped, the model has reached its spending cap", "model", run.ModelID)
		return
	}
	if err != nil {
		log.Warn("title: generate", "error", err)
		return
	}

	conv, err = s.q.SetGeneratedTitle(ctx, sqlc.SetGeneratedTitleParams{ID: run.ConversationID, Title: title, DefaultTitle: defaultTitle})
	if errors.Is(err, pgx.ErrNoRows) {
		return // renamed or deleted meanwhile
	}
	if err != nil {
		log.Warn("title: save", "error", err)
		return
	}
	s.runner.Hub().Publish(conv.ID, agent.Batch{
		RunID:   run.ID,
		Version: run.Version,
		Events:  []agent.Event{agent.TitleChanged{Title: conv.Title}},
	})
}

// exchangeOf is a run's question and answer as the title prompt reads them, or
// "" when there is no question to name the conversation by.
func (s *Server) exchangeOf(ctx context.Context, run sqlc.Run) (string, error) {
	rows, err := s.q.ListRunMessages(ctx, sqlc.ListRunMessagesParams{RunID: &run.ID, InputMessageID: run.InputMessageID})
	if err != nil {
		return "", err
	}
	var question, answer strings.Builder
	for _, row := range rows {
		switch row.Role {
		case sqlc.MessageRoleUser:
			question.WriteString(row.Content)
		case sqlc.MessageRoleAssistant:
			answer.WriteString(row.Content)
		}
	}
	if strings.TrimSpace(question.String()) == "" {
		return "", nil
	}
	return "<user>\n" + clip(question.String(), titleInputCap) + "\n</user>\n<assistant>\n" +
		clip(answer.String(), titleAnswerCap) + "\n</assistant>", nil
}

// errNoTitle is a model that answered with nothing usable as a title.
var errNoTitle = errors.New("the model gave no title")

// generateTitle has the task model — else the given model, the conversation's
// own — title a transcript excerpt.
//
// The request goes in the usage ledger as the conversation owner's, kind
// `title`: it is spent on their behalf, and an operator reading the log should
// be able to tell it from the answer it follows.
func (s *Server) generateTitle(ctx context.Context, userID, conversationID uuid.UUID, ownModel string, ownChannel *uuid.UUID, transcript string) (string, error) {
	admin, err := s.capacity().isAdmin(ctx, userID)
	if err != nil {
		return "", err
	}
	modelID, channelID, err := s.taskModel(ctx, ownModel, ownChannel, admin)
	if err != nil {
		return "", err
	}
	client, err := NewModelResolver(s.pool).Client(ctx, &channelID, modelID)
	if err != nil {
		return "", err
	}
	maxTokens := 4096 // room for a reasoning model's thinking before the title; a Claude 5 model thinks even at its least effort
	// Without a conversation (a guest's title on request) the request has
	// neither an owner nor one to be listed under in the ledger.
	var ledgerConversation *uuid.UUID
	session := ""
	if conversationID != uuid.Nil {
		ledgerConversation, session = &conversationID, conversationID.String()
	}
	meter := s.usage.Begin(ledger.Call{
		Kind:           ledger.KindTitle,
		UserID:         userID,
		ConversationID: ledgerConversation,
		ChannelID:      &channelID,
		ModelID:        modelID,
	}, nil)
	resp, err := client.Stream(ctx, llm.Request{
		Model: modelID,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, titlePrompt),
			llm.TextMessage(llm.RoleUser, transcript),
		},
		Options: llm.Options{MaxTokens: &maxTokens, Reasoning: leastThinking(ctx, s.q, modelID)},
		Session: session,
	}, meter.Wrap(nil))
	meter.End(ctx, resp, err)
	if err != nil {
		return "", err
	}
	title := cleanTitle(resp.Message.Text())
	if title == "" {
		return "", errNoTitle
	}
	return title, nil
}

// taskModel is the configured task model where an enabled channel still
// offers it, else the conversation's own model: on its pinned channel when
// one is given, else wherever it is offered.
//
// A model at its spending cap is not used for a task, which would spend the
// money the cap is there to keep: the configured task model falls back to the
// conversation's own, and if that is at its cap too the task has no model
// (errModelCapacity) and is skipped. admin is whether the person the task is
// for is an administrator, whom a cap's exemption lets through exactly as for
// their own requests. The server's tasks are otherwise staff: no person asked
// for them, so a model kept for administrators is not held back.
func (s *Server) taskModel(ctx context.Context, ownModel string, ownChannel *uuid.UUID, admin bool) (string, uuid.UUID, error) {
	who := asker{staff: true, admin: admin}
	configured, err := stringSetting(ctx, s.q, settingTaskModel)
	if err != nil {
		return "", uuid.Nil, err
	}
	if configured != "" {
		modelID, channelID, err := s.pickChannel(ctx, configured, who)
		if err == nil {
			return modelID, channelID, nil
		}
		slog.Warn("title: the task model is not available; using the conversation's", "model", configured, "error", err)
	}
	if ownChannel != nil {
		if err := s.refusedForCapacity(ctx, ownModel, who); err != nil {
			return "", uuid.Nil, err
		}
		return ownModel, *ownChannel, nil
	}
	return s.pickChannel(ctx, ownModel, who)
}

// leastThinking asks a reasoning model to think as little as it allows: a
// title does not need it, and every token of it is latency.
func leastThinking(ctx context.Context, q *sqlc.Queries, modelID string) *llm.ReasoningOptions {
	settings, err := loadModelSettings(ctx, q, modelID)
	if err != nil {
		return nil
	}
	for _, level := range []llm.ReasoningEffort{llm.EffortNone, llm.EffortMinimal, llm.EffortLow} {
		if settings.Profile.Offers(level) {
			return &llm.ReasoningOptions{Effort: level}
		}
	}
	return nil
}

var thinkTags = regexp.MustCompile(`(?s)<think>.*?</think>`)

// cleanTitle takes the first line of what the model wrote and strips what
// models add around a title anyway: quotes, markdown, a "Title:" label, a
// closing full stop.
func cleanTitle(text string) string {
	text = thinkTags.ReplaceAllString(text, "")
	var line string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	for _, label := range []string{"Title:", "title:", "标题：", "标题:"} {
		line = strings.TrimPrefix(line, label)
	}
	line = strings.Trim(line, " \t*#`\"'“”‘’「」『』《》")
	line = strings.TrimRightFunc(line, func(r rune) bool {
		return unicode.IsPunct(r) && !strings.ContainsRune(")）]】", r)
	})
	if runes := []rune(line); len(runes) > titleMaxRunes {
		line = strings.TrimSpace(string(runes[:titleMaxRunes])) + "…"
	}
	return line
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}

// GenerateConversationTitle names a conversation on request, from its latest
// exchanges — what it is about now, which is the reason to ask.
func (s *Server) GenerateConversationTitle(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: conversationId, UserID: &user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	if conv.Temporary {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "temporary_conversation", "a temporary chat is not titled"))
		return
	}
	rows, err := s.q.ListRecentMessages(ctx, sqlc.ListRecentMessagesParams{ConversationID: conversationId, RowLimit: titleRecentRows})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var transcript strings.Builder
	for _, row := range rows {
		text := strings.TrimSpace(row.Content)
		if text == "" || (row.Role != sqlc.MessageRoleUser && row.Role != sqlc.MessageRoleAssistant) {
			continue
		}
		limit := titleAnswerCap / 2
		if row.Role == sqlc.MessageRoleUser {
			limit = titleInputCap / 2
		}
		fmt.Fprintf(&transcript, "<%s>\n%s\n</%s>\n", row.Role, clip(text, limit), row.Role)
	}
	if transcript.Len() == 0 {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "nothing_to_title", "this conversation has nothing to title yet"))
		return
	}

	ownModel := ""
	if conv.Model != nil {
		ownModel = *conv.Model
	}
	title, err := s.generateTitle(ctx, user.ID, conversationId, ownModel, nil, transcript.String())
	if errors.Is(err, errModelCapacity) {
		// Nothing was asked of the model, and the reason is not a failure of it.
		httpx.Fail(w, r, errModelCapacity)
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "title on request", "conversation_id", conversationId, "error", err)
		httpx.Fail(w, r, httpx.NewError(http.StatusBadGateway, "title_failed", "the model gave no title"))
		return
	}
	conv, err = s.q.UpdateConversationTitle(ctx, sqlc.UpdateConversationTitleParams{ID: conversationId, UserID: &user.ID, Title: title})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	httpx.JSON(w, http.StatusOK, toConversation(conv))
}
