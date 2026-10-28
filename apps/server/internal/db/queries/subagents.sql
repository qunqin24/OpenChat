-- Sub-agents (docs/design/sub-agents.md): hidden child conversations whose
-- runs do work the main agent handed over, and the reports they hand back.

-- name: CreateSubagentConversation :one
INSERT INTO conversations (user_id, title, model, web_search, kind, parent_id, launched_by, launch_call_id, background)
VALUES (sqlc.arg('user_id'), sqlc.arg('title'), sqlc.arg('model'), sqlc.arg('web_search'), 'subagent',
        sqlc.arg('parent_id'), sqlc.arg('launched_by'), sqlc.arg('launch_call_id'), sqlc.arg('background'))
RETURNING *;

-- name: GetLaunchedSubagent :one
-- The sub-agent a tool call started, for the same call replayed after a restart.
SELECT *
FROM conversations
WHERE launched_by = sqlc.arg('launched_by')
  AND launch_call_id = sqlc.arg('launch_call_id');

-- name: GetSubagent :one
-- One of a conversation's sub-agents. The parent and its owner are part of the
-- lookup: anyone else's reads as not found.
SELECT *
FROM conversations
WHERE id = sqlc.arg('id')
  AND parent_id = sqlc.arg('parent_id')
  AND user_id = sqlc.arg('user_id')
  AND kind = 'subagent';

-- name: GetSubagentByID :one
-- A sub-agent by its own id, for the runner's hooks, which have no person.
SELECT *
FROM conversations
WHERE id = $1
  AND kind = 'subagent';

-- name: FindSubagentsByName :many
-- A conversation's sub-agents with this name, newest first: the main agent
-- addresses them by the names it gave.
SELECT *
FROM conversations
WHERE parent_id = sqlc.arg('parent_id')
  AND user_id = sqlc.arg('user_id')
  AND kind = 'subagent'
  AND lower(title) = lower(sqlc.arg('name')::text)
ORDER BY created_at DESC, id DESC;

-- name: ListSubagents :many
SELECT *
FROM conversations
WHERE parent_id = sqlc.arg('parent_id')
  AND user_id = sqlc.arg('user_id')
  AND kind = 'subagent'
ORDER BY created_at, id;

-- name: ListSubagentLatestRuns :many
-- The latest run of each of a conversation's sub-agents.
SELECT DISTINCT ON (runs.conversation_id) runs.*
FROM runs
         JOIN conversations c ON c.id = runs.conversation_id
WHERE c.parent_id = sqlc.arg('parent_id')
ORDER BY runs.conversation_id, runs.created_at DESC, runs.id DESC;

-- name: ListSubagentLatestReports :many
-- The latest report of each of a conversation's sub-agents.
SELECT DISTINCT ON (conversation_id) *
FROM subagent_reports
WHERE parent_id = sqlc.arg('parent_id')
ORDER BY conversation_id, created_at DESC, run_id DESC;

-- name: CountSubagentToolCalls :many
-- How many tool calls each of a conversation's sub-agents has made.
SELECT m.conversation_id, count(*)::bigint AS calls
FROM messages m
         JOIN conversations c ON c.id = m.conversation_id
WHERE c.parent_id = sqlc.arg('parent_id')
  AND m.role = 'tool'
GROUP BY m.conversation_id;

-- name: GetConversationKind :one
-- What the runner needs to know of a run's conversation: whether it is a
-- sub-agent's, and whose.
SELECT kind, parent_id, launched_by
FROM conversations
WHERE id = $1;

-- name: GetLatestRun :one
SELECT *
FROM runs
WHERE conversation_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: LockSubagentSlots :exec
-- Held until the transaction ends, like LockRunSlots: counting a person's
-- sub-agent runs and starting one is one step, on one server or several.
SELECT pg_advisory_xact_lock(hashtextextended('subagent_slots:' || sqlc.arg('user_id')::text, 23));

-- name: CountSubagentRuns :one
-- The person's sub-agent runs that are going: in one conversation, and in all.
SELECT (count(*) FILTER (WHERE c.parent_id = sqlc.arg('parent_id')))::bigint AS in_conversation,
       count(*)::bigint                                                     AS for_user
FROM runs
         JOIN conversations c ON c.id = runs.conversation_id
WHERE runs.user_id = sqlc.arg('user_id')
  AND runs.status = 'running'
  AND c.kind = 'subagent';

-- name: CountLaunchedSubagents :one
-- How many sub-agents one run has launched.
SELECT count(*)::bigint
FROM conversations
WHERE launched_by = $1;

-- name: ListSubagentRunningRuns :many
-- The runs a conversation's sub-agents have going: what a deletion that takes
-- them away must stop once it commits.
SELECT runs.id
FROM runs
         JOIN conversations c ON c.id = runs.conversation_id
WHERE c.parent_id = sqlc.arg('parent_id')
  AND runs.status = 'running';

-- name: ExistingRuns :many
SELECT id
FROM runs
WHERE id = ANY (sqlc.arg('ids')::uuid[]);

-- name: SetSubagentBackground :exec
UPDATE conversations
SET background = true
WHERE id = $1
  AND kind = 'subagent';

-- name: SetSubagentStopped :exec
UPDATE conversations
SET stopped_by_user = sqlc.arg('stopped')
WHERE id = sqlc.arg('id')
  AND kind = 'subagent';

-- name: ListRunAssistantRows :many
-- What a run of a sub-agent wrote as its answers, oldest first: its report.
SELECT *
FROM messages
WHERE run_id = $1
  AND role = 'assistant'
ORDER BY created_at, id;

-- name: InsertSubagentReport :exec
-- In the transaction that ends the run. Keyed by the run: written once.
INSERT INTO subagent_reports (run_id, conversation_id, parent_id, state, report, duration_ms)
VALUES (sqlc.arg('run_id'), sqlc.arg('conversation_id'), sqlc.arg('parent_id'), sqlc.arg('state'),
        sqlc.arg('report'), sqlc.arg('duration_ms'))
ON CONFLICT (run_id) DO NOTHING;

-- name: GetSubagentReport :one
SELECT *
FROM subagent_reports
WHERE run_id = $1;

-- name: ClaimPendingReports :many
-- The reports waiting for a conversation, oldest first, held until the
-- transaction that delivers them ends. Only a background sub-agent's: a
-- foreground one's is taken by the call waiting for it (TakeReportForTool).
SELECT rep.*
FROM subagent_reports rep
         JOIN conversations c ON c.id = rep.conversation_id
WHERE rep.parent_id = sqlc.arg('parent_id')
  AND rep.delivered_message_id IS NULL
  AND NOT rep.via_tool
  AND c.background
ORDER BY rep.created_at, rep.run_id
    FOR UPDATE OF rep;

-- name: MarkReportsDelivered :exec
UPDATE subagent_reports
SET delivered_message_id = sqlc.arg('message_id')
WHERE run_id = ANY (sqlc.arg('run_ids')::uuid[]);

-- name: TakeReportForTool :one
-- A foreground call takes its sub-agent's report as its result, unless it was
-- delivered some other way first. Taking it again is the same call replayed
-- (the claim is committed apart from the call's result, and a process can die
-- in between): only the call waiting on this run takes it, so it gets it back.
UPDATE subagent_reports
SET via_tool = true
WHERE run_id = $1
  AND delivered_message_id IS NULL
RETURNING *;

-- name: ListConversationsWithPendingReports :many
-- The sweep's work: conversations a report is waiting for.
SELECT DISTINCT rep.parent_id
FROM subagent_reports rep
         JOIN conversations c ON c.id = rep.conversation_id
         JOIN conversations p ON p.id = rep.parent_id
WHERE rep.delivered_message_id IS NULL
  AND NOT rep.via_tool
  AND c.background
  AND p.deleted_at IS NULL
LIMIT 100;

-- name: QueueSubagentMessage :exec
INSERT INTO subagent_inbox (conversation_id, content)
VALUES ($1, $2);

-- name: TakeSubagentInbox :many
-- The messages waiting for a sub-agent, taken: the caller writes them into its
-- transcript in the same transaction.
WITH taken AS (
    DELETE FROM subagent_inbox
        WHERE conversation_id = $1
        RETURNING *)
SELECT *
FROM taken
ORDER BY created_at, id;

-- name: HasSubagentInbox :one
SELECT EXISTS (SELECT 1 FROM subagent_inbox WHERE conversation_id = $1) AS waiting;

-- name: HasPendingReports :one
-- Whether a report waits for the conversation: asked before every request of
-- its runs, so cheap and outside any transaction.
SELECT EXISTS (SELECT 1
               FROM subagent_reports rep
                        JOIN conversations c ON c.id = rep.conversation_id
               WHERE rep.parent_id = $1
                 AND rep.delivered_message_id IS NULL
                 AND NOT rep.via_tool
                 AND c.background) AS waiting;

-- name: ListIdleSubagentInboxes :many
-- Sub-agents a message waits for with no run going to read it: the sweep
-- starts the run (a process that died between the two leaves this).
SELECT DISTINCT i.conversation_id
FROM subagent_inbox i
         JOIN conversations c ON c.id = i.conversation_id
WHERE NOT c.stopped_by_user
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.conversation_id = i.conversation_id AND r.status = 'running')
LIMIT 100;

-- name: GetChatConversationByID :one
-- A conversation by id alone, for the server's own work on it (delivering
-- reports), which has no person asking.
SELECT *
FROM conversations
WHERE id = $1
  AND kind = 'chat';

-- name: CountQueuedSubagentsBefore :one
-- A conversation's sub-agents launched before this one that are still
-- queued (no run yet): a queued sub-agent starts only when none ahead of it
-- waits, so they start in the order they were launched.
SELECT count(*)::bigint
FROM conversations c
WHERE c.parent_id = sqlc.arg('parent_id')
  AND c.kind = 'subagent'
  AND (c.created_at, c.id) < (sqlc.arg('created_at')::timestamptz, sqlc.arg('id')::uuid)
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.conversation_id = c.id);

-- name: GetSubagentInput :one
-- A queued sub-agent's task: the first row of its conversation.
SELECT *
FROM messages
WHERE conversation_id = $1
ORDER BY created_at, id
LIMIT 1;

-- name: DeleteQueuedSubagent :execrows
-- A queued sub-agent whose wait ended without a start - the reply that
-- launched it stopped, or the person cancelled it: it never did anything.
DELETE FROM conversations c
WHERE c.id = $1
  AND c.kind = 'subagent'
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.conversation_id = c.id);

-- name: DeleteOrphanedQueuedSubagents :execrows
-- Queued sub-agents nothing waits for any more: the run that launched them
-- is over (a process died while they waited, and the run was stopped before
-- it could take up the wait again). One moved to the background waits on the
-- queue alone (ListBackgroundQueuedSubagents), and is not one of these.
DELETE FROM conversations c
WHERE c.kind = 'subagent'
  AND NOT c.background
  AND c.created_at < now() - interval '1 minute'
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.conversation_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.id = c.launched_by AND r.status = 'running');

-- name: ListBackgroundQueuedSubagents :many
-- Queued sub-agents nothing waits for but the queue: moved to the background
-- while queued, so the call that launched them returned. Oldest first; for
-- one person, or everyone (the sweep). Not the ones the person cancelled, nor
-- those of a deleted conversation.
SELECT c.*
FROM conversations c
         JOIN conversations p ON p.id = c.parent_id
WHERE c.kind = 'subagent'
  AND c.background
  AND NOT c.stopped_by_user
  AND p.deleted_at IS NULL
  AND (sqlc.narg('user_id')::uuid IS NULL OR c.user_id = sqlc.narg('user_id')::uuid)
  AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.conversation_id = c.id)
ORDER BY c.created_at, c.id
LIMIT 100;
