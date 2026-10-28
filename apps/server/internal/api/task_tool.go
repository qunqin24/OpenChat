package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/llm"
	"github.com/qunqin24/openchat/apps/server/internal/schedule"
)

// ToolScheduledTasks is the task tool's name.
const ToolScheduledTasks = "scheduled_tasks"

// TaskTool is the `scheduled_tasks` tool: a model schedules, lists, changes,
// pauses, resumes and deletes the person's tasks, as they could on the tasks
// page. It lives here rather than in package tools because it works through
// the server - its moderation, its model choice, its limit - and is added to
// the runner once the server exists (Runner.AddTools, TaskTools).
//
// Everything it touches is the run's user's (agent.ScopeFrom), never anyone the
// arguments name. Times are the person's wall-clock times in the time zone
// their browser last reported (users.time_zone), UTC when it never has; every
// result says which, and what time it is there now, since the model is told
// only the date. A prompt the model writes is checked by content moderation
// before it is saved, logged as `task_run`: not a strike, as the words are the
// model's.
type TaskTool struct {
	s *Server
}

var _ agent.Tool = TaskTool{}

// TaskTools is the runner's extra tool source: the task tool, for a run of a
// signed-in person outside a temporary conversation, while the feature is on.
func (s *Server) TaskTools(ctx context.Context) []agent.Tool {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return nil
	}
	on, err := s.tasksOn(ctx)
	if err != nil {
		slog.WarnContext(ctx, "read scheduled tasks setting", "error", err)
		return nil
	}
	if !on {
		return nil
	}
	who, err := s.q.TaskToolContext(ctx, sqlc.TaskToolContextParams{ConversationID: scope.ConversationID, UserID: &scope.UserID})
	if err != nil || who.Temporary {
		return nil
	}
	return []agent.Tool{TaskTool{s: s}}
}

// TaskToolArgs are a call's arguments.
type TaskToolArgs struct {
	Op       string            `json:"op"`
	ID       string            `json:"id"`
	Title    *string           `json:"title"`
	Prompt   *string           `json:"prompt"`
	Schedule *TaskToolSchedule `json:"schedule"`
}

// TaskToolSchedule is a schedule as the model writes it: local times, no zone.
type TaskToolSchedule struct {
	Kind     string `json:"kind"`
	At       string `json:"at"`
	Time     string `json:"time"`
	Weekdays []int  `json:"weekdays"`
	MonthDay int    `json:"monthDay"`
}

// TaskToolDetails is what the interface is shown of a call.
type TaskToolDetails struct {
	Op   string `json:"op"`
	Task *Task  `json:"task,omitempty"`
}

func (TaskTool) Definition() llm.Tool {
	return llm.Tool{
		Name: ToolScheduledTasks,
		Description: "Manage the user's scheduled tasks. A scheduled task sends its prompt, at the times its schedule " +
			"names, as a new message from the user in the task's own conversation, where it is answered with the " +
			"tools available then (such as web search). Use it when the user asks for something to be done later " +
			"or repeatedly: a reminder, a daily briefing, a weekly check. Do not ask for confirmation before " +
			"creating a task the user asked for; they can change it on their tasks page. " +
			"op=list shows the tasks with their ids. op=create needs title, prompt and schedule. op=update " +
			"changes title, prompt or schedule of a task (id). op=pause, op=resume and op=delete act on a task (id). " +
			"Write the prompt as the instruction to carry out at that time, complete in itself: the run will not " +
			"see this conversation. For a reminder, have the prompt tell you what to remind the user of. " +
			"Times are the user's local wall-clock times; every result states the user's time zone and current " +
			"local time, so call op=list first if you do not know what time it is for them.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "op": {"type": "string", "enum": ["list", "create", "update", "pause", "resume", "delete"]},
    "id": {"type": "string", "description": "The task's id, from op=list. For update, pause, resume and delete."},
    "title": {"type": "string", "description": "A short name for the task, in the user's language."},
    "prompt": {"type": "string", "description": "The instruction sent at each run, complete in itself."},
    "schedule": {
      "type": "object",
      "properties": {
        "kind": {"type": "string", "enum": ["once", "daily", "weekly", "monthly"]},
        "at": {"type": "string", "description": "For once: the local date and time, YYYY-MM-DDTHH:MM."},
        "time": {"type": "string", "description": "For daily, weekly and monthly: the local time, HH:MM (24-hour)."},
        "weekdays": {"type": "array", "items": {"type": "integer", "minimum": 0, "maximum": 6}, "description": "For weekly: 0 = Sunday to 6 = Saturday."},
        "monthDay": {"type": "integer", "minimum": 1, "maximum": 31, "description": "For monthly: the day of the month."}
      },
      "required": ["kind"]
    }
  },
  "required": ["op"]
}`),
	}
}

// Replayable is false: a call cut off may have made its task already.
func (TaskTool) Replayable() bool { return false }

func (t TaskTool) Execute(ctx context.Context, raw json.RawMessage) (agent.ToolResult, error) {
	scope, ok := agent.ScopeFrom(ctx)
	if !ok {
		return agent.ToolResult{}, errors.New("scheduled_tasks: no run in context")
	}
	var args TaskToolArgs
	if err := agent.DecodeArgs(raw, &args); err != nil {
		return agent.ToolResult{}, err
	}
	s := t.s
	if on, err := s.tasksOn(ctx); err != nil {
		return agent.ToolResult{}, err
	} else if !on {
		return agent.ToolResult{}, agent.Errorf("Scheduled tasks are turned off. Nothing was changed.")
	}
	user, err := s.q.GetUser(ctx, scope.UserID)
	if err != nil {
		return agent.ToolResult{}, err
	}
	where, err := s.q.TaskToolContext(ctx, sqlc.TaskToolContextParams{ConversationID: scope.ConversationID, UserID: &scope.UserID})
	if err != nil {
		return agent.ToolResult{}, err
	}
	zone := "UTC"
	if where.TimeZone != nil && *where.TimeZone != "" {
		zone = *where.TimeZone
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		zone, loc = "UTC", time.UTC
	}
	clock := fmt.Sprintf("The user's time zone is %s; it is now %s there.", zone, time.Now().In(loc).Format("Monday 2006-01-02 15:04"))

	op := strings.ToLower(strings.TrimSpace(args.Op))
	var row sqlc.ScheduledTask
	switch op {
	case "list":
		rows, err := s.q.ListTasks(ctx, user.ID)
		if err != nil {
			return agent.ToolResult{}, err
		}
		return agent.ToolResult{Content: listTasksText(rows, loc, clock), Details: TaskToolDetails{Op: op}}, nil

	case "create":
		if args.Title == nil || args.Prompt == nil || args.Schedule == nil {
			return agent.ToolResult{}, agent.Errorf("invalid arguments: create needs title, prompt and schedule")
		}
		spec, err := toolSpec(*args.Schedule, zone, loc)
		if err != nil {
			return agent.ToolResult{}, err
		}
		d := taskDraft{title: *args.Title, prompt: *args.Prompt, spec: spec, enabled: true}
		if where.Model != nil {
			d.model = *where.Model
		}
		row, err = s.createTask(ctx, user, d, ModerationSourceTaskRun)
		if err != nil {
			return agent.ToolResult{}, taskToolError(err)
		}

	case "update", "pause", "resume", "delete":
		id, err := uuid.Parse(strings.TrimSpace(args.ID))
		if err != nil {
			return agent.ToolResult{}, agent.Errorf("invalid arguments: id must be a task id from op=list")
		}
		if op == "delete" {
			n, err := s.q.DeleteTask(ctx, sqlc.DeleteTaskParams{ID: id, UserID: user.ID})
			if err != nil {
				return agent.ToolResult{}, err
			}
			if n == 0 {
				return agent.ToolResult{}, agent.Errorf("There is no task with that id. Call op=list for the ids.")
			}
			return agent.ToolResult{Content: "Deleted the task. " + clock, Details: TaskToolDetails{Op: op}}, nil
		}
		change := taskChange{title: args.Title, prompt: args.Prompt}
		switch op {
		case "pause":
			change = taskChange{enabled: new(false)}
		case "resume":
			change = taskChange{enabled: new(true)}
		default:
			if args.Schedule != nil {
				spec, err := toolSpec(*args.Schedule, zone, loc)
				if err != nil {
					return agent.ToolResult{}, err
				}
				change.spec = &spec
			}
		}
		row, err = s.updateTask(ctx, user, id, change, ModerationSourceTaskRun)
		if err != nil {
			return agent.ToolResult{}, taskToolError(err)
		}

	default:
		return agent.ToolResult{}, agent.Errorf("invalid arguments: op must be list, create, update, pause, resume or delete")
	}

	task := toTask(row)
	verb := map[string]string{"create": "Scheduled", "update": "Updated", "pause": "Paused", "resume": "Resumed"}[op]
	content := fmt.Sprintf("%s: %s\n%s", verb, taskLine(row, loc), clock)
	return agent.ToolResult{Content: content, Details: TaskToolDetails{Op: op, Task: &task}}, nil
}

// toolSpec reads a schedule the model wrote, in the person's zone.
func toolSpec(in TaskToolSchedule, zone string, loc *time.Location) (schedule.Spec, error) {
	spec := schedule.Spec{Kind: strings.ToLower(strings.TrimSpace(in.Kind)), Zone: zone, Weekdays: in.Weekdays, MonthDay: in.MonthDay}
	if spec.Kind == schedule.Once {
		at, err := parseLocal(in.At, loc)
		if err != nil {
			return spec, agent.Errorf("invalid arguments: schedule.at must be a local date and time, YYYY-MM-DDTHH:MM")
		}
		spec.At = at.UTC()
	} else if spec.Kind != "" {
		minute, err := minuteOfDay(in.Time)
		if err != nil {
			return spec, agent.Errorf("invalid arguments: schedule.time must be HH:MM")
		}
		spec.Minute = minute
	}
	if err := spec.Validate(); err != nil {
		return spec, agent.Errorf("invalid arguments: %s", strings.TrimPrefix(err.Error(), "schedule: "))
	}
	return spec, nil
}

// parseLocal reads a wall-clock time in loc, or a time that names its offset.
func parseLocal(text string, loc *time.Location) (time.Time, error) {
	text = strings.TrimSpace(text)
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, text, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("not a time")
}

var weekdayNames = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// taskLine is one task as the model reads it.
func taskLine(row sqlc.ScheduledTask, loc *time.Location) string {
	spec := specOfRow(row)
	var when string
	hhmm := fmt.Sprintf("%02d:%02d", spec.Minute/60, spec.Minute%60)
	switch spec.Kind {
	case schedule.Once:
		when = "once at " + spec.At.In(loc).Format("2006-01-02 15:04")
	case schedule.Daily:
		when = "daily at " + hhmm
	case schedule.Weekly:
		days := make([]string, 0, len(spec.Weekdays))
		for _, d := range spec.Weekdays {
			days = append(days, weekdayNames[d])
		}
		when = "weekly on " + strings.Join(days, ", ") + " at " + hhmm
	case schedule.Monthly:
		when = fmt.Sprintf("monthly on day %d at %s", spec.MonthDay, hhmm)
	}
	if spec.Kind != schedule.Once && spec.Zone != loc.String() {
		when += " (" + spec.Zone + ")"
	}
	status := "next run " + row.NextRunAt.Time.In(loc).Format("Mon 2006-01-02 15:04")
	switch {
	case !row.Enabled:
		status = "paused"
	case !row.NextRunAt.Valid:
		status = "done"
	}
	return fmt.Sprintf("[%s] %q, %s, %s. Prompt: %s", row.ID, row.Title, when, status, row.Prompt)
}

func listTasksText(rows []sqlc.ScheduledTask, loc *time.Location, clock string) string {
	if len(rows) == 0 {
		return "The user has no scheduled tasks. " + clock
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d scheduled task(s), at most %d may be active:\n", len(rows), maxEnabledTasks)
	for _, row := range rows {
		b.WriteString("- " + taskLine(row, loc) + "\n")
	}
	b.WriteString(clock)
	return b.String()
}

// taskToolError says in words a model can act on why a call did nothing.
func taskToolError(err error) error {
	var api *httpx.APIError
	if !errors.As(err, &api) {
		return err
	}
	switch api.Code {
	case "too_many_tasks":
		return agent.Errorf("The user already has %d active scheduled tasks, the most allowed. Nothing was created. "+
			"Pause or delete one first, if the user agrees.", maxEnabledTasks)
	case "content_blocked":
		return agent.Errorf("Content moderation refused this prompt. Nothing was saved.")
	case "not_found":
		return agent.Errorf("There is no task with that id. Call op=list for the ids.")
	}
	return agent.Errorf("%s. Nothing was changed.", strings.TrimSuffix(api.Message, "."))
}
