-- name: InsertUsageLog :one
-- Prices and cost arrive as double precision and are stored as numeric rounded
-- to ten places, so a float's tail (0.30000000000000004) never reaches the
-- ledger.
--
-- The row is written when the request ends, and what it names may have gone
-- meanwhile: an account or a channel an administrator deleted, a conversation
-- or a run that was removed. The request still cost what it cost, so a name
-- that is gone is written as NULL, as the foreign key would have set it, and
-- the row keeps the account's name (deleted_user) and the channel's, both
-- taken when the request began. deleted_user is read only where user_id is
-- NULL, so writing it for every account's request is what labels the one whose
-- account is deleted while it is out.
INSERT INTO usage_logs (created_at, kind, status, error_code, error_message,
                        user_id, conversation_id, run_id, channel_id, channel_name, deleted_user,
                        model_id, served_model,
                        input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_write_tokens,
                        input_price, output_price, cache_read_price, cache_write_price, cost,
                        streamed, first_token_ms, duration_ms, estimated)
VALUES (sqlc.arg('created_at'), sqlc.arg('kind'), sqlc.arg('status'), sqlc.narg('error_code'), sqlc.narg('error_message'),
        (SELECT id FROM users WHERE id = sqlc.narg('user_id')::uuid),
        (SELECT id FROM conversations WHERE id = sqlc.narg('conversation_id')::uuid),
        (SELECT id FROM runs WHERE id = sqlc.narg('run_id')::uuid),
        (SELECT id FROM channels WHERE id = sqlc.narg('channel_id')::uuid),
        sqlc.arg('channel_name'), sqlc.narg('deleted_user'),
        sqlc.arg('model_id'), sqlc.arg('served_model'),
        sqlc.arg('input_tokens'), sqlc.arg('output_tokens'), sqlc.arg('reasoning_tokens'),
        sqlc.arg('cache_read_tokens'), sqlc.arg('cache_write_tokens'),
        round((sqlc.narg('input_price')::double precision)::numeric, 10), round((sqlc.narg('output_price')::double precision)::numeric, 10),
        round((sqlc.narg('cache_read_price')::double precision)::numeric, 10), round((sqlc.narg('cache_write_price')::double precision)::numeric, 10),
        round((sqlc.narg('cost')::double precision)::numeric, 10),
        sqlc.arg('streamed'), sqlc.narg('first_token_ms'), sqlc.arg('duration_ms'), sqlc.arg('estimated'))
RETURNING id;

-- name: UsagePricing :one
-- What a request needs to be priced and labelled: the channel's current name,
-- and the catalogue's per-million-token prices for the model.
--
-- The catalogue row is chosen like ModelProfile chooses it, except that the
-- request's own channel decides which provider's entry wins -- the same model
-- id is priced differently by different providers -- and an entry that
-- actually carries a price beats one that does not. An operator's overrides
-- (model_overrides) have no price columns, so the catalogue is the only source.
-- Always one row: a request whose channel or model is unknown gets NULLs.
-- Also the name of the account it is for, kept with the row in
-- case the account is deleted before the request ends.
SELECT c.name                                                     AS channel_name,
       u.name                                                     AS user_name,
       (m.raw -> 'cost' ->> 'input')::numeric       AS input_price,
       (m.raw -> 'cost' ->> 'output')::numeric      AS output_price,
       (m.raw -> 'cost' ->> 'cache_read')::numeric  AS cache_read_price,
       (m.raw -> 'cost' ->> 'cache_write')::numeric AS cache_write_price
FROM (SELECT 1) one
         LEFT JOIN channels c ON c.id = sqlc.narg('channel_id')::uuid
         LEFT JOIN users u ON u.id = sqlc.narg('user_id')::uuid
         LEFT JOIN LATERAL (SELECT cm.raw
                            FROM catalog_models cm
                            WHERE cm.model_id = sqlc.arg('model_id')
                            ORDER BY (cm.raw -> 'cost' ->> 'input') IS NOT NULL DESC,
                                     (cm.provider_id = c.type::text) DESC,
                                     cm.provider_id
                            LIMIT 1) m ON true;

-- name: ListUsageLogs :many
-- Newest first, by keyset on (created_at, id): the order of usage_logs_created_idx,
-- so a page deep into a ledger of millions of rows costs what the first one
-- does, and a row written meanwhile cannot shift the pages. The total is the
-- summary's, not a count of this query.
--
-- `to` is exclusive, so a day's end is the next day's start and nothing falls
-- between two ranges.
SELECT l.id,
       l.created_at,
       l.kind,
       l.status,
       l.error_code,
       l.error_message,
       l.user_id,
       u.name                          AS user_name,
       u.email                         AS user_email,
       u.avatar_key                    AS user_avatar_key,
       l.deleted_user,
       l.conversation_id,
       l.run_id,
       l.channel_id,
       l.channel_name,
       l.model_id,
       l.served_model,
       l.input_tokens,
       l.output_tokens,
       l.reasoning_tokens,
       l.cache_read_tokens,
       l.cache_write_tokens,
       l.input_price,
       l.output_price,
       l.cache_read_price,
       l.cache_write_price,
       l.cost,
       l.first_token_ms,
       l.duration_ms,
       l.estimated
FROM usage_logs l
         LEFT JOIN users u ON u.id = l.user_id
WHERE (sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (l.created_at, l.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
  AND (sqlc.narg('user_id')::uuid IS NULL OR l.user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('model_id')::text IS NULL OR l.model_id = sqlc.narg('model_id')::text)
  AND (sqlc.narg('channel_id')::uuid IS NULL OR l.channel_id = sqlc.narg('channel_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR l.status = sqlc.narg('status')::text)
  AND (sqlc.narg('kind')::text IS NULL OR l.kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('from_time')::timestamptz IS NULL OR l.created_at >= sqlc.narg('from_time')::timestamptz)
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR l.created_at < sqlc.narg('to_time')::timestamptz)
ORDER BY l.created_at DESC, l.id DESC
LIMIT sqlc.arg('row_limit');

-- name: SummarizeUsageLogs :one
-- The total behind ListUsageLogs, with what an operator wants above the table:
-- how many requests, how many failed, how many tokens and how much money. The
-- same filters, in the same words -- if they drift the total stops matching
-- the rows.
--
-- The cache figures count the successful requests only. The hit rate is
-- token-weighted: cache_read / (input + cache_read + cache_write), where input
-- excludes the cached part (see llm.Usage); the two sums are returned and the
-- caller divides. The saving is what the cached reads would have cost at the
-- full input price, less what they did cost, over the requests priced with
-- both -- 0 when none were.
SELECT count(*)::bigint                                          AS requests,
       count(*) FILTER (WHERE status = 'error')::bigint          AS errors,
       coalesce(sum(input_tokens), 0)::bigint                    AS input_tokens,
       coalesce(sum(output_tokens), 0)::bigint                   AS output_tokens,
       coalesce(sum(cost), 0)::double precision                  AS cost,
       coalesce(sum(cache_read_tokens) FILTER (WHERE status = 'ok'), 0)::bigint  AS cache_read_tokens,
       coalesce(sum(cache_write_tokens) FILTER (WHERE status = 'ok'), 0)::bigint AS cache_write_tokens,
       coalesce(sum(input_tokens + cache_read_tokens + cache_write_tokens) FILTER (WHERE status = 'ok'), 0)::bigint AS cache_prompt_tokens,
       (coalesce(sum(cache_read_tokens * (input_price - cache_read_price)) FILTER (WHERE status = 'ok'), 0)
           / 1000000)::double precision                          AS cache_savings
FROM usage_logs l
WHERE (sqlc.narg('user_id')::uuid IS NULL OR l.user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('model_id')::text IS NULL OR l.model_id = sqlc.narg('model_id')::text)
  AND (sqlc.narg('channel_id')::uuid IS NULL OR l.channel_id = sqlc.narg('channel_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR l.status = sqlc.narg('status')::text)
  AND (sqlc.narg('kind')::text IS NULL OR l.kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('from_time')::timestamptz IS NULL OR l.created_at >= sqlc.narg('from_time')::timestamptz)
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR l.created_at < sqlc.narg('to_time')::timestamptz);

-- name: LatestContextTokens :one
-- How much of the model's window the conversation's latest answered request
-- took: everything it was sent, cached or not, plus what it wrote, which the
-- next request carries back.
SELECT (input_tokens + cache_read_tokens + cache_write_tokens + output_tokens)::bigint AS tokens,
       model_id,
       created_at
FROM usage_logs
WHERE conversation_id = $1
  AND kind = 'chat'
  AND status = 'ok'
ORDER BY created_at DESC
LIMIT 1;
