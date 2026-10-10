-- The dashboard: operational figures from the usage ledger over a window, each
-- a single pass over the rows from `since` on.
--
-- Shared definitions. Errors are status = 'error'. The cache hit rate is
-- token-weighted, cache_read / (input + cache_read + cache_write) -- input
-- excludes the cached part, see llm.Usage -- over successful requests only; the
-- query returns the two sums (cache_read_tokens, cache_prompt_tokens) and the
-- caller divides, so "nothing was sent" stays a missing rate rather than a 0%.
-- Latency percentiles look at successful requests that have the time, and are
-- -1 where none did (sqlc cannot return a NULL double).

-- name: GetDashboardWindow :one
-- Where a window starts. `bucket` is 'hour' or 'day' and `buckets` how many of
-- them the window spans, counting the current, partial one -- so a 7-day window
-- is today and the six days before it. `prev_since` opens the equal-length
-- window before, the one the figures are compared with. Worked out here, in
-- the database's time zone, so the buckets of the trend line up with it.
SELECT (date_trunc(sqlc.arg('bucket')::text, now())
        - (sqlc.arg('buckets')::int - 1) * CASE sqlc.arg('bucket')::text WHEN 'hour' THEN interval '1 hour' ELSE interval '1 day' END
       )::timestamptz AS since,
       (date_trunc(sqlc.arg('bucket')::text, now())
        - (2 * sqlc.arg('buckets')::int - 1) * CASE sqlc.arg('bucket')::text WHEN 'hour' THEN interval '1 hour' ELSE interval '1 day' END
       )::timestamptz AS prev_since,
       (SELECT count(*) FROM users WHERE disabled_at IS NULL AND NOT guest)::bigint AS total_users;

-- name: GetDashboardKpis :one
-- The headline figures for [from_time, to_time); an open `to_time` runs up to now.
SELECT count(*)::bigint                                          AS requests,
       count(*) FILTER (WHERE status = 'error')::bigint          AS errors,
       coalesce(sum(input_tokens), 0)::bigint                    AS input_tokens,
       coalesce(sum(output_tokens), 0)::bigint                   AS output_tokens,
       coalesce(sum(cache_read_tokens) FILTER (WHERE status = 'ok'), 0)::bigint  AS cache_read_tokens,
       coalesce(sum(cache_write_tokens) FILTER (WHERE status = 'ok'), 0)::bigint AS cache_write_tokens,
       coalesce(sum(input_tokens + cache_read_tokens + cache_write_tokens) FILTER (WHERE status = 'ok'), 0)::bigint AS cache_prompt_tokens,
       (coalesce(sum(cache_read_tokens * (input_price - cache_read_price)) FILTER (WHERE status = 'ok'), 0)
           / 1000000)::double precision                          AS cache_savings,
       coalesce(sum(cost), 0)::double precision                  AS cost,
       count(DISTINCT user_id) FILTER (WHERE user_id IN (SELECT id FROM users WHERE NOT guest))::bigint AS active_users,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_ms)
           FILTER (WHERE status = 'ok' AND first_token_ms IS NOT NULL), -1)::double precision  AS first_token_p50_ms,
       coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms)
           FILTER (WHERE status = 'ok' AND first_token_ms IS NOT NULL), -1)::double precision  AS first_token_p95_ms
FROM usage_logs
WHERE created_at >= sqlc.arg('from_time')::timestamptz
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR created_at < sqlc.narg('to_time')::timestamptz);

-- name: GetDashboardTrend :many
-- One row per bucket from `since` to now, empty ones included: a gap in the
-- series would draw as a straight line across the chart.
SELECT b.at::timestamptz                                         AS at,
       coalesce(u.requests, 0)::bigint                           AS requests,
       coalesce(u.errors, 0)::bigint                             AS errors,
       coalesce(u.cost, 0)::double precision                     AS cost
FROM generate_series(
             date_trunc(sqlc.arg('bucket')::text, sqlc.arg('since')::timestamptz),
             now(),
             CASE sqlc.arg('bucket')::text WHEN 'hour' THEN interval '1 hour' ELSE interval '1 day' END
     ) AS b(at)
         LEFT JOIN (SELECT date_trunc(sqlc.arg('bucket')::text, created_at)  AS at,
                           count(*)                                          AS requests,
                           count(*) FILTER (WHERE status = 'error')          AS errors,
                           sum(cost)                                         AS cost
                    FROM usage_logs
                    WHERE created_at >= sqlc.arg('since')::timestamptz
                    GROUP BY 1) u ON u.at = b.at
ORDER BY b.at;

-- name: GetDashboardModels :many
-- The busiest models. Ties break on the name so the order does not shuffle
-- between two refreshes.
SELECT model_id,
       count(*)::bigint                                          AS requests,
       count(*) FILTER (WHERE status = 'error')::bigint          AS errors,
       coalesce(sum(input_tokens), 0)::bigint                    AS input_tokens,
       coalesce(sum(output_tokens), 0)::bigint                   AS output_tokens,
       coalesce(sum(cache_read_tokens) FILTER (WHERE status = 'ok'), 0)::bigint  AS cache_read_tokens,
       coalesce(sum(input_tokens + cache_read_tokens + cache_write_tokens) FILTER (WHERE status = 'ok'), 0)::bigint AS cache_prompt_tokens,
       coalesce(sum(cost), 0)::double precision                  AS cost,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_ms)
           FILTER (WHERE status = 'ok' AND first_token_ms IS NOT NULL), -1)::double precision  AS first_token_p50_ms
FROM usage_logs
WHERE created_at >= sqlc.arg('since')::timestamptz
GROUP BY model_id
ORDER BY count(*) DESC, model_id
LIMIT sqlc.arg('row_limit');

-- name: GetDashboardChannels :many
-- Per upstream, to show which one is failing or slow. A channel is its id and
-- the name it had: deleting one keeps its history under the name, with no id.
SELECT channel_id,
       channel_name,
       count(*)::bigint                                          AS requests,
       count(*) FILTER (WHERE status = 'error')::bigint          AS errors,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_ms)
           FILTER (WHERE status = 'ok' AND first_token_ms IS NOT NULL), -1)::double precision  AS first_token_p50_ms,
       coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms)
           FILTER (WHERE status = 'ok' AND first_token_ms IS NOT NULL), -1)::double precision  AS first_token_p95_ms,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms)
           FILTER (WHERE status = 'ok'), -1)::double precision       AS duration_p50_ms,
       coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms)
           FILTER (WHERE status = 'ok'), -1)::double precision       AS duration_p95_ms,
       max(created_at) FILTER (WHERE status = 'error')::timestamptz AS last_error_at
FROM usage_logs
WHERE created_at >= sqlc.arg('since')::timestamptz
GROUP BY channel_id, channel_name
ORDER BY count(*) DESC, channel_name
LIMIT sqlc.arg('row_limit');

-- name: GetDashboardUsers :many
-- Who spends the most. Requests with no owner group as one: they are guests'
-- (an account is never deleted, and a guest is deleted when its run ends).
SELECT u.id                                                      AS user_id,
       u.name                                                    AS user_name,
       u.email                                                   AS user_email,
       u.avatar_key                                              AS user_avatar_key,
       count(*)::bigint                                          AS requests,
       coalesce(sum(l.cost), 0)::double precision                AS cost
FROM usage_logs l
         LEFT JOIN users u ON u.id = l.user_id AND NOT u.guest
WHERE l.created_at >= sqlc.arg('since')::timestamptz
GROUP BY u.id, u.name, u.email, u.avatar_key
ORDER BY coalesce(sum(l.cost), 0) DESC, count(*) DESC, u.id
LIMIT sqlc.arg('row_limit');

-- name: GetDashboardErrors :many
-- The latest failures; the partial index on errors serves it.
SELECT id, created_at, model_id, channel_name, error_code, error_message
FROM usage_logs
WHERE status = 'error' AND created_at >= sqlc.arg('since')::timestamptz
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');
