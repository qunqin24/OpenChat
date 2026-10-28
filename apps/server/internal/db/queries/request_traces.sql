-- Request traces: what an administrator's model requests carried (the 轨迹
-- view), next to their usage_logs rows.

-- name: InsertRequestTrace :exec
INSERT INTO request_traces (usage_log_id, user_id, conversation_id, request, response)
VALUES (sqlc.arg('usage_log_id'), sqlc.arg('user_id'), sqlc.arg('conversation_id'), sqlc.arg('request'), sqlc.arg('response'));

-- name: ListConversationTrace :many
-- A conversation's recorded requests, oldest first, each with its figures.
-- Only the caller's own conversation: an administrator reads their own.
SELECT t.usage_log_id, t.request, t.response,
       u.created_at, u.kind, u.status, u.error_code, u.error_message, u.run_id,
       u.channel_name, u.model_id, u.served_model,
       u.input_tokens, u.output_tokens, u.reasoning_tokens, u.cache_read_tokens, u.cache_write_tokens,
       u.cost, u.first_token_ms, u.duration_ms, u.estimated
FROM request_traces t
         JOIN usage_logs u ON u.id = t.usage_log_id
         JOIN conversations c ON c.id = t.conversation_id
WHERE t.conversation_id = sqlc.arg('conversation_id')
  AND t.user_id = sqlc.arg('user_id')
  AND c.user_id = sqlc.arg('user_id')
ORDER BY u.created_at, t.usage_log_id;

-- name: DeleteOldRequestTraces :execrows
DELETE FROM request_traces
WHERE usage_log_id IN (SELECT r.usage_log_id
                       FROM request_traces r
                       WHERE r.created_at < sqlc.arg('before')
                       ORDER BY r.created_at
                       LIMIT sqlc.arg('row_limit'));

-- name: UserIsAdmin :one
SELECT role = 'admin' AND disabled_at IS NULL
FROM users
WHERE id = $1;
