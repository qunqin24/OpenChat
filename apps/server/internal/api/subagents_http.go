package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// The person's side of sub-agents: reading them, stopping one, letting a
// stopped one go on, moving a foreground one to the background - and the
// administrator's settings. Nothing here takes text for a sub-agent: there is
// no way for a person's words to reach one except through the main agent.

// Settings keys and defaults (后台 → 系统 → 子智能体).
const (
	settingSubagents                  = "subagents"
	settingSubagentModel              = "subagent_model"
	settingSubagentBackground         = "subagent_background"
	settingSubagentMaxPerRun          = "subagent_max_per_run"
	settingSubagentMaxPerConversation = "subagent_max_per_conversation"
	settingSubagentMaxPerUser         = "subagent_max_per_user"
	settingSubagentReportChars        = "subagent_max_report_chars"

	defaultSubagentMaxPerRun          = 4
	defaultSubagentMaxPerConversation = 3
	defaultSubagentMaxPerUser         = 5
	defaultSubagentReportChars        = 30000
)

// subagentTranscriptRows bounds the read-only view of one sub-agent.
const subagentTranscriptRows = 500

type subagentSettings struct {
	Enabled            bool
	Model              string
	Background         bool
	MaxPerRun          int
	MaxPerConversation int
	MaxPerUser         int
	MaxReportChars     int
}

func loadSubagentSettings(ctx context.Context, q *sqlc.Queries) (subagentSettings, error) {
	var (
		s   subagentSettings
		err error
	)
	if s.Enabled, err = boolSettingOr(ctx, q, settingSubagents, false); err != nil {
		return s, err
	}
	if s.Model, err = stringSetting(ctx, q, settingSubagentModel); err != nil {
		return s, err
	}
	if s.Background, err = boolSettingOr(ctx, q, settingSubagentBackground, true); err != nil {
		return s, err
	}
	ints := []struct {
		into  *int
		key   string
		unset int
	}{
		{&s.MaxPerRun, settingSubagentMaxPerRun, defaultSubagentMaxPerRun},
		{&s.MaxPerConversation, settingSubagentMaxPerConversation, defaultSubagentMaxPerConversation},
		{&s.MaxPerUser, settingSubagentMaxPerUser, defaultSubagentMaxPerUser},
		{&s.MaxReportChars, settingSubagentReportChars, defaultSubagentReportChars},
	}
	for _, i := range ints {
		if *i.into, err = intSettingOr(ctx, q, i.key, i.unset); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (s subagentSettings) contract() SubagentSettings {
	return SubagentSettings{
		Enabled: s.Enabled, Model: s.Model, Background: s.Background,
		MaxPerRun: s.MaxPerRun, MaxPerConversation: s.MaxPerConversation, MaxPerUser: s.MaxPerUser,
		MaxReportChars: s.MaxReportChars,
	}
}

// subagentsOn is whether models may launch sub-agents.
func (s *Server) subagentsOn(ctx context.Context) (bool, error) {
	return boolSettingOr(ctx, s.q, settingSubagents, false)
}

func (s *Server) GetSubagentSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := loadSubagentSettings(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings.contract())
}

func (s *Server) UpdateSubagentSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body UpdateSubagentSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	limits := []struct {
		value    *int
		key      string
		min, max int
	}{
		{body.MaxPerRun, settingSubagentMaxPerRun, 1, 20},
		{body.MaxPerConversation, settingSubagentMaxPerConversation, 1, 20},
		{body.MaxPerUser, settingSubagentMaxPerUser, 1, 100},
		{body.MaxReportChars, settingSubagentReportChars, 1000, 200000},
	}
	// Checked together before anything is saved, as the system settings are.
	for _, l := range limits {
		if l.value != nil && (*l.value < l.min || *l.value > l.max) {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_subagent_limit",
				fmt.Sprintf("%s must be between %d and %d", l.key, l.min, l.max)))
			return
		}
	}
	var model *string
	if body.Model != nil {
		m := strings.TrimSpace(*body.Model)
		if m != "" {
			// Whether it can be answered with, as for the task model.
			if _, _, err := s.pickChannel(ctx, m, asker{staff: true, admin: true, ignoreCap: true}); err != nil {
				httpx.Fail(w, r, err)
				return
			}
		}
		model = &m
	}
	values := map[string]any{}
	if body.Enabled != nil {
		values[settingSubagents] = *body.Enabled
	}
	if body.Background != nil {
		values[settingSubagentBackground] = *body.Background
	}
	if model != nil {
		values[settingSubagentModel] = *model
	}
	for _, l := range limits {
		if l.value != nil {
			values[l.key] = *l.value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		raw, _ := json.Marshal(values[key])
		if err := s.q.SetSetting(ctx, sqlc.SetSettingParams{Key: key, Value: raw}); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		slog.InfoContext(ctx, "sub-agent setting changed", "setting", key, "value", values[key], "actor", auth.MustUser(ctx).Email)
	}
	s.GetSubagentSettings(w, r)
}

func (s *Server) ListSubagents(w http.ResponseWriter, r *http.Request, conversationId ConversationId) {
	ctx := r.Context()
	if err := s.conversationExists(ctx, conversationId); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	list, err := s.subagentList(ctx, conversationId, auth.MustUser(ctx).ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, SubagentList{Items: list})
}

// ownSubagent is one of the caller's conversation's sub-agents, or not found.
func (s *Server) ownSubagent(ctx context.Context, conversationID, subagentID SubagentId) (sqlc.Conversation, error) {
	if err := s.conversationExists(ctx, conversationID); err != nil {
		return sqlc.Conversation{}, err
	}
	user := auth.MustUser(ctx)
	child, err := s.q.GetSubagent(ctx, sqlc.GetSubagentParams{ID: subagentID, ParentID: &conversationID, UserID: &user.ID})
	if err != nil {
		return child, dbErr(err)
	}
	return child, nil
}

func (s *Server) GetSubagent(w http.ResponseWriter, r *http.Request, conversationId ConversationId, subagentId SubagentId) {
	ctx := r.Context()
	child, err := s.ownSubagent(ctx, conversationId, subagentId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	list, err := s.subagentList(ctx, conversationId, auth.MustUser(ctx).ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	i := slices.IndexFunc(list, func(a Subagent) bool { return a.Id == child.ID })
	if i < 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	rows, err := s.q.ListMessages(ctx, sqlc.ListMessagesParams{ConversationID: child.ID, RowLimit: subagentTranscriptRows})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slices.Reverse(rows)
	detail := SubagentDetail{Subagent: list[i], Messages: make([]Message, 0, len(rows))}
	for _, row := range rows {
		detail.Messages = append(detail.Messages, toMessage(row, nil))
	}
	if run, err := s.q.GetActiveRun(ctx, child.ID); err == nil {
		mapped := toRun(run)
		detail.Run = &mapped
		if live, err := agent.DecodeLive(run.Live); err == nil {
			state := toLiveState(live)
			detail.Live = &state
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

func (s *Server) StopSubagent(w http.ResponseWriter, r *http.Request, conversationId ConversationId, subagentId SubagentId) {
	ctx := r.Context()
	child, err := s.ownSubagent(ctx, conversationId, subagentId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Marked before the run is stopped, so the run ending does not start the
	// next one from a message waiting for it.
	if err := s.q.SetSubagentStopped(ctx, sqlc.SetSubagentStoppedParams{ID: child.ID, Stopped: true}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.stopSubagentRun(ctx, child.ID)
	// One queued in the background has no call waiting on it to give up its
	// place: it goes now. (A foreground one's call does that itself.)
	if child.Background {
		if _, err := s.q.DeleteQueuedSubagent(ctx, child.ID); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	// A queued one's wait follows its conversation: woken, it sees the stop
	// and gives up its place.
	s.runner.Hub().Publish(child.ID, agent.Batch{Events: []agent.Event{agent.SubagentsChanged{}}})
	s.subagentsChanged(conversationId)
	w.WriteHeader(http.StatusAccepted)
}

// resumeText is the input of a run the person's "继续" starts: no words of
// theirs, only that it should go on.
const resumeText = "Go on with your task from where you stopped, then write your report."

func (s *Server) ResumeSubagent(w http.ResponseWriter, r *http.Request, conversationId ConversationId, subagentId SubagentId) {
	ctx := r.Context()
	child, err := s.ownSubagent(ctx, conversationId, subagentId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	settings, err := loadSubagentSettings(ctx, s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var (
		run   sqlc.Run
		input sqlc.Message
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		held, err := holdSubagent(ctx, q, child.ID)
		if err != nil {
			return err
		}
		if _, err := q.GetActiveRun(ctx, child.ID); err == nil {
			return errSubagentBusy
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.SetSubagentStopped(ctx, sqlc.SetSubagentStoppedParams{ID: child.ID, Stopped: false}); err != nil {
			return err
		}
		// Ahead of anything the main agent queued while it was stopped.
		waiting, err := q.TakeSubagentInbox(ctx, child.ID)
		if err != nil {
			return err
		}
		if err := q.QueueSubagentMessage(ctx, sqlc.QueueSubagentMessageParams{ConversationID: child.ID, Content: resumeText}); err != nil {
			return err
		}
		for _, m := range waiting {
			if err := q.QueueSubagentMessage(ctx, sqlc.QueueSubagentMessageParams{ConversationID: child.ID, Content: m.Content}); err != nil {
				return err
			}
		}
		held.StoppedByUser = false
		run, input, err = s.continueSubagent(ctx, q, held, settings)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	s.startSubagentRun(child, run, input)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) BackgroundSubagent(w http.ResponseWriter, r *http.Request, conversationId ConversationId, subagentId SubagentId) {
	ctx := r.Context()
	child, err := s.ownSubagent(ctx, conversationId, subagentId)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := s.q.SetSubagentBackground(ctx, child.ID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// The call waiting for it follows its conversation: woken, it reads the
	// switch and returns. One on another server sees it at its next look.
	s.runner.Hub().Publish(child.ID, agent.Batch{Events: []agent.Event{agent.SubagentsChanged{}}})
	s.subagentsChanged(conversationId)
	w.WriteHeader(http.StatusAccepted)
}
