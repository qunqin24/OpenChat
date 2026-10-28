package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

const (
	// taskCheckEvery is how often due tasks are looked for: a task runs within
	// this of its time.
	taskCheckEvery = 30 * time.Second
	// taskBatch is how many due tasks one pass takes; the rest wait a pass.
	taskBatch = 20
	// taskMissedAfter is how late a repeating task's turn may be and still
	// run. Later - the server was down, or the switch off - it is skipped for
	// the next one rather than run at a time nobody asked for. A one-off runs
	// however late: it is the only turn it has.
	taskMissedAfter = time.Hour
)

// StartTaskScheduler starts due tasks, in the background until ctx ends.
//
// Safe with several servers on one database: a pass takes its tasks with FOR
// UPDATE SKIP LOCKED and moves each one's next time on in the same
// transaction, so a turn is taken by one server, once. The run itself starts
// after that commits; a crash in between loses that one turn, never repeats it.
func (s *Server) StartTaskScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(taskCheckEvery)
		defer ticker.Stop()
		for {
			s.RunDueTasks(ctx, time.Now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RunDueTasks is one pass of the scheduler: every task due at now.
func (s *Server) RunDueTasks(ctx context.Context, now time.Time) {
	on, err := s.tasksOn(ctx)
	if err != nil {
		slog.WarnContext(ctx, "read scheduled tasks setting", "error", err)
		return
	}
	if !on {
		return
	}
	for {
		due, err := s.claimDueTasks(ctx, now)
		if err != nil {
			slog.WarnContext(ctx, "claim due tasks", "error", err)
			return
		}
		for _, task := range due {
			s.runDueTask(ctx, task)
		}
		if len(due) < taskBatch {
			return
		}
	}
}

// claimDueTasks takes the tasks due at now and moves each on to its next time.
// What it hands back are the ones to run; a repeating task's turn missed by
// too long is only moved on, and marked missed.
func (s *Server) claimDueTasks(ctx context.Context, now time.Time) ([]sqlc.ScheduledTask, error) {
	var run []sqlc.ScheduledTask
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		due, err := q.ClaimDueTasks(ctx, sqlc.ClaimDueTasksParams{Now: timestamptz(now, true), RowLimit: taskBatch})
		if err != nil {
			return err
		}
		for _, task := range due {
			// From now, not from the turn being taken: a task that was due
			// several times over while nothing ran runs once, not once for each.
			next, ok := specOfRow(task).Next(now)
			if err := q.AdvanceTask(ctx, sqlc.AdvanceTaskParams{ID: task.ID, NextRunAt: timestamptz(next, ok), RanAt: timestamptz(now, true)}); err != nil {
				return err
			}
			if task.Kind != "once" && now.Sub(task.NextRunAt.Time) > taskMissedAfter {
				missed := "missed"
				if err := q.TaskFailed(ctx, sqlc.TaskFailedParams{ID: task.ID, LastError: &missed}); err != nil {
					return err
				}
				continue
			}
			run = append(run, task)
		}
		return nil
	})
	return run, err
}

// runDueTask starts one task's run, and records why when it cannot.
func (s *Server) runDueTask(ctx context.Context, task sqlc.ScheduledTask) {
	log := slog.With("task_id", task.ID, "user_id", task.UserID, "conversation_id", task.ConversationID)
	user, err := s.q.GetUser(ctx, task.UserID)
	if err == nil && (user.DisabledAt.Valid || user.Pending) {
		err = errAccountUnavailable
	}
	if err == nil {
		_, err = s.startTaskRun(ctx, user, task)
	}
	if err == nil {
		log.InfoContext(ctx, "scheduled task started")
		return
	}
	code := taskErrorCode(err)
	if code == "failed" {
		log.WarnContext(ctx, "scheduled task did not start", "error", err)
	} else {
		log.InfoContext(ctx, "scheduled task did not start", "reason", code)
	}
	// A prompt moderation refuses would be refused at every turn.
	pause := isContentBlocked(err)
	if err := s.q.TaskFailed(ctx, sqlc.TaskFailedParams{ID: task.ID, LastError: &code, Pause: pause}); err != nil {
		log.WarnContext(ctx, "record scheduled task failure", "error", err)
	}
}
