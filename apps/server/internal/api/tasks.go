package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/memory"
	"github.com/qunqin24/openchat/apps/server/internal/schedule"
)

// Scheduled tasks: an instruction sent, at the times its schedule names, as the
// person's own message in the task's own conversation, and answered there as
// if they had sent it. People make them on the tasks page (the handlers below)
// or ask a model to (task_tool.go); the scheduler (task_scheduler.go) starts
// them. A task is its owner's in every query, so another person's reads as
// missing, and the whole feature is behind a switch in System settings.
//
// The prompt reaches a model without being typed at that moment, so it is
// checked by content moderation the way custom instructions are
// (moderation_text.go): when it is saved, and - if what is saved is not what
// was checked (a hash) - before it is sent.

const (
	// settingScheduledTasks switches the feature. On unless an administrator
	// turns it off.
	settingScheduledTasks = "scheduled_tasks"
	// maxEnabledTasks is how many tasks still to run a person may have.
	maxEnabledTasks = 10
	maxTaskTitle    = 100
	maxTaskPrompt   = 4000
)

var (
	errTooManyTasks = httpx.NewError(http.StatusConflict, "too_many_tasks",
		fmt.Sprintf("you can have at most %d scheduled tasks still to run", maxEnabledTasks))
	errTaskInPast = httpx.NewFieldError("schedule", "invalid_schedule", "a one-off task must be in the future")
	// errAccountUnavailable is a due task whose owner cannot use the service
	// now - disabled, or not yet approved. Only ever recorded on the task.
	errAccountUnavailable = httpx.NewError(http.StatusForbidden, "account_unavailable", "the account cannot be used")
)

func invalidSchedule(err error) error {
	return httpx.NewFieldError("schedule", "invalid_schedule", err.Error())
}

// tasksOn is the switch.
func (s *Server) tasksOn(ctx context.Context) (bool, error) {
	return boolSettingOr(ctx, s.q, settingScheduledTasks, true)
}

// requireTasks answers 404 while the feature is off.
func (s *Server) requireTasks(w http.ResponseWriter, r *http.Request) bool {
	on, err := s.tasksOn(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return false
	}
	if !on {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return false
	}
	return true
}

// ---------------------------------------------------------------- schedule

// specOf turns the contract's schedule into one the scheduler reads.
func specOf(in TaskSchedule) (schedule.Spec, error) {
	spec := schedule.Spec{Kind: string(in.Kind), Zone: strings.TrimSpace(in.TimeZone)}
	if in.At != nil {
		spec.At = in.At.UTC()
	}
	if in.Kind != TaskOnce {
		if in.Time == nil {
			return spec, errors.New("a repeating schedule needs a time")
		}
		minute, err := minuteOfDay(*in.Time)
		if err != nil {
			return spec, err
		}
		spec.Minute = minute
	}
	if in.Weekdays != nil {
		spec.Weekdays = *in.Weekdays
	}
	if in.MonthDay != nil {
		spec.MonthDay = *in.MonthDay
	}
	return spec, spec.Validate()
}

// minuteOfDay reads HH:MM.
func minuteOfDay(hhmm string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return 0, errors.New("time must be HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}

// specColumns is how a spec is stored.
type specColumns struct {
	kind     string
	runAt    pgtype.Timestamptz
	minute   *int32
	weekdays []int16
	monthDay *int16
	zone     string
}

func columnsOf(spec schedule.Spec) specColumns {
	c := specColumns{kind: spec.Kind, zone: spec.Zone, weekdays: []int16{}}
	if spec.Kind == schedule.Once {
		c.runAt = pgtype.Timestamptz{Time: spec.At, Valid: true}
		return c
	}
	minute := int32(spec.Minute)
	c.minute = &minute
	for _, day := range spec.Weekdays {
		c.weekdays = append(c.weekdays, int16(day))
	}
	if spec.Kind == schedule.Monthly {
		day := int16(spec.MonthDay)
		c.monthDay = &day
	}
	return c
}

func specOfRow(row sqlc.ScheduledTask) schedule.Spec {
	spec := schedule.Spec{Kind: row.Kind, Zone: row.TimeZone}
	if row.RunAt.Valid {
		spec.At = row.RunAt.Time
	}
	if row.MinuteOfDay != nil {
		spec.Minute = int(*row.MinuteOfDay)
	}
	for _, day := range row.Weekdays {
		spec.Weekdays = append(spec.Weekdays, int(day))
	}
	if row.MonthDay != nil {
		spec.MonthDay = int(*row.MonthDay)
	}
	return spec
}

func timestamptz(t time.Time, ok bool) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: ok}
}

// toTask maps a row onto the contract.
func toTask(row sqlc.ScheduledTask) Task {
	spec := specOfRow(row)
	out := Task{
		Id:             row.ID,
		Title:          row.Title,
		Prompt:         row.Prompt,
		ConversationId: row.ConversationID,
		CreatedAt:      row.CreatedAt.Time,
		LastError:      row.LastError,
		Schedule:       TaskSchedule{Kind: TaskScheduleKind(row.Kind), TimeZone: row.TimeZone},
	}
	if spec.Kind == schedule.Once {
		at := spec.At
		out.Schedule.At = &at
	} else {
		hhmm := fmt.Sprintf("%02d:%02d", spec.Minute/60, spec.Minute%60)
		out.Schedule.Time = &hhmm
	}
	if spec.Kind == schedule.Weekly {
		days := spec.Weekdays
		out.Schedule.Weekdays = &days
	}
	if spec.Kind == schedule.Monthly {
		day := spec.MonthDay
		out.Schedule.MonthDay = &day
	}
	switch {
	case !row.Enabled:
		out.Status = TaskPaused
	case !row.NextRunAt.Valid:
		out.Status = TaskCompleted
	default:
		out.Status = TaskActive
		next := row.NextRunAt.Time
		out.NextRunAt = &next
	}
	if row.LastRunAt.Valid {
		last := row.LastRunAt.Time
		out.LastRunAt = &last
	}
	return out
}

// ---------------------------------------------------------------- service

// taskDraft is a new task, or the whole of a changed one.
type taskDraft struct {
	title, prompt string
	spec          schedule.Spec
	model         string
	enabled       bool
}

func (d *taskDraft) tidy() error {
	d.title = strings.TrimSpace(d.title)
	d.prompt = strings.TrimSpace(d.prompt)
	switch {
	case d.title == "":
		return httpx.NewFieldError("title", "invalid_title", "title must not be empty")
	case utf8.RuneCountInString(d.title) > maxTaskTitle:
		return httpx.NewFieldError("title", "invalid_title", fmt.Sprintf("title is at most %d characters", maxTaskTitle))
	case d.prompt == "":
		return httpx.NewFieldError("prompt", "invalid_prompt", "prompt must not be empty")
	case utf8.RuneCountInString(d.prompt) > maxTaskPrompt:
		return httpx.NewFieldError("prompt", "invalid_prompt", fmt.Sprintf("prompt is at most %d characters", maxTaskPrompt))
	}
	return nil
}

// checkPrompt moderates a prompt being saved. The hash that comes back is
// stored as what was checked; nil when nothing looked at it.
func (s *Server) checkPrompt(ctx context.Context, user sqlc.User, conversationID *uuid.UUID, prompt string, source ModerationSource) (*string, error) {
	who := moderationSubject{userID: user.ID, admin: user.Role == sqlc.UserRoleAdmin, conversationID: conversationID, source: source}
	checked, err := s.checkText(ctx, who, prompt)
	if err != nil || !checked {
		return nil, err
	}
	hash := memory.Hash(prompt)
	return &hash, nil
}

// createTask makes a task and its conversation. source is whose words the
// prompt is: the owner's (`task`) or a model's (`task_run`).
func (s *Server) createTask(ctx context.Context, user sqlc.User, d taskDraft, source ModerationSource) (sqlc.ScheduledTask, error) {
	if err := d.tidy(); err != nil {
		return sqlc.ScheduledTask{}, err
	}
	next, ok := d.spec.Next(time.Now())
	if !ok {
		return sqlc.ScheduledTask{}, errTaskInPast
	}
	// The model the conversation will answer with, as the person may use it.
	ctx = auth.WithUser(ctx, user)
	modelID, _, err := s.pickChannel(ctx, strings.TrimSpace(d.model), askerFrom(ctx))
	if err != nil {
		return sqlc.ScheduledTask{}, err
	}
	checked, err := s.checkPrompt(ctx, user, nil, d.prompt, source)
	if err != nil {
		return sqlc.ScheduledTask{}, err
	}

	var task sqlc.ScheduledTask
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockTaskOwner(ctx, user.ID); err != nil {
			return err
		}
		if n, err := q.CountEnabledTasks(ctx, user.ID); err != nil {
			return err
		} else if n >= maxEnabledTasks {
			return errTooManyTasks
		}
		conv, err := q.CreateConversation(ctx, sqlc.CreateConversationParams{UserID: &user.ID, Title: d.title, Model: &modelID})
		if err != nil {
			return err
		}
		c := columnsOf(d.spec)
		task, err = q.CreateTask(ctx, sqlc.CreateTaskParams{
			UserID: user.ID, ConversationID: conv.ID, Title: d.title, Prompt: d.prompt, PromptChecked: checked,
			Kind: c.kind, RunAt: c.runAt, MinuteOfDay: c.minute, Weekdays: c.weekdays, MonthDay: c.monthDay,
			TimeZone: c.zone, Enabled: true, NextRunAt: timestamptz(next, true),
		})
		return err
	})
	return task, err
}

// taskChange is what an update asks for; nil leaves a field as it is.
type taskChange struct {
	title, prompt *string
	spec          *schedule.Spec
	enabled       *bool
}

// updateTask changes a task. A task that will run again is counted against
// the limit if it was not before; its next time is worked out again whenever
// its schedule changes or it is resumed. The change is merged into the row as
// it stands under its lock, so a next time the scheduler has just moved on, or
// a pause made meanwhile, is kept rather than written over with an older read.
func (s *Server) updateTask(ctx context.Context, user sqlc.User, id uuid.UUID, change taskChange, source ModerationSource) (sqlc.ScheduledTask, error) {
	old, err := s.q.GetTask(ctx, sqlc.GetTaskParams{ID: id, UserID: user.ID})
	if err != nil {
		return old, dbErr(err)
	}
	// Moderation is a network call, so a new prompt is checked before the
	// transaction; the receipt is for the text itself, whatever the row says.
	var checked *string
	if change.prompt != nil {
		prompt := strings.TrimSpace(*change.prompt)
		if prompt == old.Prompt {
			checked = old.PromptChecked
		} else if prompt != "" {
			if checked, err = s.checkPrompt(ctx, user, &old.ConversationID, prompt, source); err != nil {
				return old, err
			}
		}
	}

	var task sqlc.ScheduledTask
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// The owner's lock first, as creating a task takes it, then the row.
		if err := q.LockTaskOwner(ctx, user.ID); err != nil {
			return err
		}
		cur, err := q.LockTask(ctx, sqlc.LockTaskParams{ID: id, UserID: user.ID})
		if err != nil {
			return dbErr(err)
		}
		d := taskDraft{title: cur.Title, prompt: cur.Prompt, spec: specOfRow(cur), enabled: cur.Enabled}
		if change.title != nil {
			d.title = *change.title
		}
		if change.prompt != nil {
			d.prompt = *change.prompt
		}
		if change.spec != nil {
			d.spec = *change.spec
		}
		if change.enabled != nil {
			d.enabled = *change.enabled
		}
		if err := d.tidy(); err != nil {
			return err
		}
		promptChecked := cur.PromptChecked
		if change.prompt != nil {
			promptChecked = checked
		}

		next := cur.NextRunAt
		resumed := d.enabled && !cur.Enabled
		switch {
		case !d.enabled:
			next = pgtype.Timestamptz{}
		case change.spec != nil || resumed:
			at, ok := d.spec.Next(time.Now())
			if !ok {
				return errTaskInPast
			}
			next = timestamptz(at, true)
		}
		counted := cur.Enabled && cur.NextRunAt.Valid
		if next.Valid && !counted {
			if n, err := q.CountEnabledTasks(ctx, user.ID); err != nil {
				return err
			} else if n >= maxEnabledTasks {
				return errTooManyTasks
			}
		}
		c := columnsOf(d.spec)
		task, err = q.UpdateTask(ctx, sqlc.UpdateTaskParams{
			ID: id, UserID: user.ID, Title: d.title, Prompt: d.prompt, PromptChecked: promptChecked,
			Kind: c.kind, RunAt: c.runAt, MinuteOfDay: c.minute, Weekdays: c.weekdays, MonthDay: c.monthDay,
			TimeZone: c.zone, Enabled: d.enabled, NextRunAt: next,
			ClearError: change.spec != nil || change.prompt != nil || resumed,
		})
		if err != nil {
			return dbErr(err)
		}
		// The conversation keeps the task's name while it has it.
		if d.title != cur.Title {
			conv, err := q.GetConversation(ctx, sqlc.GetConversationParams{ID: cur.ConversationID, UserID: &user.ID})
			if err == nil && conv.Title == cur.Title {
				_, err = q.UpdateConversationTitle(ctx, sqlc.UpdateConversationTitleParams{ID: conv.ID, UserID: &user.ID, Title: d.title})
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return old, err
	}
	return task, nil
}

// ---------------------------------------------------------------- running

// startTaskRun sends a task's prompt to its conversation as its owner's
// message and starts the run that answers it: CreateMessage's path, with the
// task's checked prompt in place of what a page sends.
func (s *Server) startTaskRun(ctx context.Context, user sqlc.User, task sqlc.ScheduledTask) (sqlc.Run, error) {
	ctx = auth.WithUser(ctx, user)
	conv, err := s.q.GetConversation(ctx, sqlc.GetConversationParams{ID: task.ConversationID, UserID: &user.ID})
	if err != nil {
		return sqlc.Run{}, dbErr(err)
	}
	if err := requireRevised(ctx, s.q, conv.ID); err != nil {
		return sqlc.Run{}, err
	}
	choice, err := s.chooseRun(ctx, conv, nil, nil, nil)
	if err != nil {
		return sqlc.Run{}, err
	}
	// A prompt not checked as it is now is checked before it goes. Not a
	// strike: nobody is typing it now (CountUserStrikes).
	if task.PromptChecked == nil || *task.PromptChecked != memory.Hash(task.Prompt) {
		checked, err := s.checkPrompt(ctx, user, &conv.ID, task.Prompt, ModerationSourceTaskRun)
		if err != nil {
			return sqlc.Run{}, err
		}
		if checked != nil {
			if err := s.q.SetTaskPromptChecked(ctx, sqlc.SetTaskPromptCheckedParams{ID: task.ID, Prompt: task.Prompt, PromptChecked: checked}); err != nil {
				return sqlc.Run{}, err
			}
		}
	}
	if err := s.prepareSnapshot(ctx, user, conv); err != nil {
		return sqlc.Run{}, err
	}

	var (
		input sqlc.Message
		run   sqlc.Run
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockLive(ctx, q, conv.ID); err != nil {
			return err
		}
		if err := requireRevised(ctx, q, conv.ID); err != nil {
			return err
		}
		var err error
		if input, err = appendInput(ctx, q, conv.ID, task.Prompt, inputParts(task.Prompt, nil)); err != nil {
			return err
		}
		run, err = createRun(ctx, q, conv, user.ID, input.ID, "", choice)
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "runs_one_active_per_conversation" {
			return sqlc.Run{}, errConversationBusy
		}
		return sqlc.Run{}, err
	}
	s.startRun(conv.ID, run, input)
	if err := s.q.TaskStarted(ctx, sqlc.TaskStartedParams{ID: task.ID, RunID: &run.ID}); err != nil {
		return run, err
	}
	return run, nil
}

// taskErrorCode is how a turn that started nothing is recorded: the API
// error's code, or `failed`.
func taskErrorCode(err error) string {
	var api *httpx.APIError
	if errors.As(err, &api) && api.Code != "" {
		return api.Code
	}
	return "failed"
}

// ---------------------------------------------------------------- handlers

func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
	if !s.requireTasks(w, r) {
		return
	}
	user := auth.MustUser(r.Context())
	rows, err := s.q.ListTasks(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := TaskList{Items: make([]Task, 0, len(rows)), Limit: maxEnabledTasks}
	for _, row := range rows {
		out.Items = append(out.Items, toTask(row))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) CreateTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireTasks(w, r) {
		return
	}
	var body CreateTaskRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	spec, err := specOf(body.Schedule)
	if err != nil {
		httpx.Fail(w, r, invalidSchedule(err))
		return
	}
	d := taskDraft{title: body.Title, prompt: body.Prompt, spec: spec, enabled: true}
	if body.Model != nil {
		d.model = *body.Model
	}
	task, err := s.createTask(r.Context(), auth.MustUser(r.Context()), d, ModerationSourceTask)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toTask(task))
}

func (s *Server) UpdateTask(w http.ResponseWriter, r *http.Request, taskId TaskId) {
	if !s.requireTasks(w, r) {
		return
	}
	var body UpdateTaskRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	change := taskChange{title: body.Title, prompt: body.Prompt, enabled: body.Enabled}
	if body.Schedule != nil {
		spec, err := specOf(*body.Schedule)
		if err != nil {
			httpx.Fail(w, r, invalidSchedule(err))
			return
		}
		change.spec = &spec
	}
	task, err := s.updateTask(r.Context(), auth.MustUser(r.Context()), taskId, change, ModerationSourceTask)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toTask(task))
}

func (s *Server) DeleteTask(w http.ResponseWriter, r *http.Request, taskId TaskId) {
	if !s.requireTasks(w, r) {
		return
	}
	user := auth.MustUser(r.Context())
	n, err := s.q.DeleteTask(r.Context(), sqlc.DeleteTaskParams{ID: taskId, UserID: user.ID})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if n == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) RunTask(w http.ResponseWriter, r *http.Request, taskId TaskId) {
	if !s.requireTasks(w, r) {
		return
	}
	ctx := r.Context()
	user := auth.MustUser(ctx)
	task, err := s.q.GetTask(ctx, sqlc.GetTaskParams{ID: taskId, UserID: user.ID})
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	if _, err := s.startTaskRun(ctx, user, task); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if task, err = s.q.GetTask(ctx, sqlc.GetTaskParams{ID: taskId, UserID: user.ID}); err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}
	httpx.JSON(w, http.StatusAccepted, toTask(task))
}
