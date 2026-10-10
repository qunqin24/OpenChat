-- name: GetModelOverride :one
SELECT *
FROM model_overrides
WHERE model_id = sqlc.arg('model_id');

-- name: ListModelOverrides :many
SELECT *
FROM model_overrides
WHERE model_id = ANY (sqlc.arg('model_ids')::text[]);

-- name: SaveModelOverride :one
-- Replaces the whole override: the settings panel always sends every field,
-- and a field it sends as null goes back to following the catalogue. A cap's
-- period and exemption left out (NULL) are their defaults, total and exempt.
INSERT INTO model_overrides (model_id, kind, reasoning, tool_call, input_modalities,
                             context_limit, input_limit, output_limit, reasoning_efforts, admin_only,
                             capacity, capacity_period, capacity_admin_exempt)
VALUES (sqlc.arg('model_id'), sqlc.narg('kind'), sqlc.narg('reasoning'), sqlc.narg('tool_call'),
        sqlc.narg('input_modalities')::text[], sqlc.narg('context_limit'), sqlc.narg('input_limit'),
        sqlc.narg('output_limit'), sqlc.narg('reasoning_efforts')::text[], sqlc.arg('admin_only'),
        sqlc.narg('capacity')::numeric, coalesce(sqlc.narg('capacity_period')::text, 'total'),
        coalesce(sqlc.narg('capacity_admin_exempt')::boolean, true))
ON CONFLICT (model_id) DO UPDATE
    SET kind              = excluded.kind,
        reasoning         = excluded.reasoning,
        tool_call         = excluded.tool_call,
        input_modalities  = excluded.input_modalities,
        context_limit     = excluded.context_limit,
        input_limit       = excluded.input_limit,
        output_limit      = excluded.output_limit,
        reasoning_efforts = excluded.reasoning_efforts,
        admin_only        = excluded.admin_only,
        -- capacity_since is not here on purpose: it is the operator's
        -- "recalculate", has its own endpoint, and survives saving the rest.
        capacity              = excluded.capacity,
        capacity_period       = excluded.capacity_period,
        capacity_admin_exempt = excluded.capacity_admin_exempt,
        updated_at        = now()
RETURNING *;

-- name: ResetModelCapacity :one
-- Starts a total cap's count again from `since`. Makes the row if the model
-- has none (every other column then at its default: no cap, nothing changed).
INSERT INTO model_overrides (model_id, capacity_since)
VALUES (sqlc.arg('model_id'), sqlc.arg('since'))
ON CONFLICT (model_id) DO UPDATE
    SET capacity_since = excluded.capacity_since,
        updated_at     = now()
RETURNING *;

-- name: ModelSpends :many
-- What each model has cost since the start of its window, and whether that has
-- reached its capacity, for a set of models at once. The three arrays go
-- together by position. A NULL `since` counts from the beginning (a total cap
-- never recalculated). Requests that were not priced have no cost (NULL) and
-- add nothing, so an unpriced model reads 0 forever.
--
-- The comparison is in numeric, in the database, so the sum and the capacity
-- are never rounded through a float on the way. The join is a range scan of
-- usage_logs_model_idx, which carries the cost: no table pages are read.
--
-- (sqlc cannot read a multi-argument unnest, hence the three joined by their
-- position.)
SELECT m.model_id::text                       AS model_id,
       coalesce(sum(l.cost), 0)::numeric      AS spent,
       coalesce(sum(l.cost), 0) >= c.capacity AS reached
FROM unnest(sqlc.arg('model_ids')::text[]) WITH ORDINALITY AS m(model_id, n)
         JOIN unnest(sqlc.arg('since')::timestamptz[]) WITH ORDINALITY AS s(since, n) USING (n)
         JOIN unnest(sqlc.arg('capacity')::numeric[]) WITH ORDINALITY AS c(capacity, n) USING (n)
         LEFT JOIN usage_logs l ON l.model_id = m.model_id AND l.created_at >= coalesce(s.since, '-infinity'::timestamptz)
GROUP BY m.n, m.model_id, c.capacity;

-- name: DeleteModelOverride :exec
DELETE
FROM model_overrides
WHERE model_id = sqlc.arg('model_id');

-- name: ModelWouldReach :one
-- Whether a model's spend since the start of its window, plus money held back
-- for requests in flight and the one about to go, reaches its capacity. The
-- same sum as ModelSpends and the same index-only scan, with the held amount
-- added in numeric: never through a float.
SELECT (coalesce(sum(l.cost), 0) + sqlc.arg('held')::numeric) >= sqlc.arg('capacity')::numeric AS reached
FROM usage_logs l
WHERE l.model_id = sqlc.arg('model_id')
  AND l.created_at >= coalesce(sqlc.narg('since')::timestamptz, '-infinity'::timestamptz);
