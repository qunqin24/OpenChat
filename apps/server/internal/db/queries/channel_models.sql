-- name: ListChannelModels :many
-- Ordered by id rather than by name: the id is what gets sent to the endpoint
-- and what the operator recognises, and a display name is often absent.
SELECT *
FROM channel_models
WHERE channel_id = $1
ORDER BY model_id;

-- name: GetChannelModel :one
SELECT *
FROM channel_models
WHERE channel_id = $1
  AND model_id = $2;

-- name: AddManualChannelModel :one
-- Typed in by an operator, for an endpoint that publishes no list or omits a
-- model it actually serves. Enabled from the start: naming one model by hand is
-- already the explicit act, and leaving it off would make the operator say the
-- same thing twice.
--
-- DO NOTHING rather than an upsert, so a model that is already here reports the
-- conflict instead of quietly changing source and enabled under a refresh's
-- feet. No rows back therefore means "already listed".
INSERT INTO channel_models (channel_id, model_id, display_name, source, enabled)
VALUES (sqlc.arg('channel_id'), sqlc.arg('model_id'), sqlc.arg('display_name'), 'manual', true)
ON CONFLICT (channel_id, model_id) DO NOTHING
RETURNING *;

-- name: CreateSelectedChannelModels :exec
-- These are the operator's selections at creation, not the endpoint's entire
-- list. Insert them together with the channel in one transaction.
INSERT INTO channel_models (channel_id, model_id, display_name, source, enabled, fetched_at)
SELECT sqlc.arg('channel_id'), ids.model_id, names.display_name, sources.source, true,
       CASE WHEN sources.source = 'fetched' THEN now() END
FROM unnest(sqlc.arg('model_ids')::text[]) WITH ORDINALITY AS ids(model_id, n)
         JOIN unnest(sqlc.arg('display_names')::text[]) WITH ORDINALITY AS names(display_name, n)
              ON names.n = ids.n
         JOIN unnest(sqlc.arg('sources')::text[]) WITH ORDINALITY AS sources(source, n)
              ON sources.n = ids.n;

-- name: SetChannelModelEnabled :one
-- The only statement in this file that changes `enabled` on a row that already
-- exists. The inserts set it once, when the row is created and there is no
-- decision to preserve; a refresh never touches it. That is the whole point of
-- the column: whether this installation offers a model is our decision, and a
-- refresh of somebody else's list is not an occasion to revisit it.
UPDATE channel_models
SET enabled = sqlc.arg('enabled')
WHERE channel_id = sqlc.arg('channel_id')
  AND model_id = sqlc.arg('model_id')
RETURNING *;

-- name: DeleteManualChannelModel :execrows
-- Only a manual row can be deleted by hand. A fetched row is removed by the
-- endpoint no longer listing it; deleting one here would just bring it back on
-- the next refresh, which reads as the button not working.
DELETE
FROM channel_models
WHERE channel_id = sqlc.arg('channel_id')
  AND model_id = sqlc.arg('model_id')
  AND source = 'manual';

-- name: UpsertFetchedChannelModels :exec
-- One statement for the whole list, so a refresh cannot half-apply.
--
-- Two rules are enforced by what this statement does *not* say:
--
--   * `enabled` appears nowhere. A new row takes the column default (off), and
--     a row that already exists keeps whatever the operator set. A refresh
--     never turns a model on or off.
--   * The DO UPDATE is fenced to `source = 'fetched'`. A manual row whose id
--     the endpoint happens to list is left exactly as it was — still manual,
--     still with the operator's own display name, and still out of reach of the
--     prune below.
--
-- The two arrays are zipped by ordinality rather than with the two-argument
-- `unnest(ids, names)`, which says the same thing in one line but which sqlc's
-- analyser cannot type.
INSERT INTO channel_models (channel_id, model_id, display_name, source, fetched_at)
SELECT sqlc.arg('channel_id'),
       ids.model_id,
       names.display_name,
       'fetched',
       now()
FROM unnest(sqlc.arg('model_ids')::text[]) WITH ORDINALITY AS ids(model_id, n)
         JOIN unnest(sqlc.arg('display_names')::text[]) WITH ORDINALITY AS names(display_name, n)
              ON names.n = ids.n
ON CONFLICT (channel_id, model_id)
    DO UPDATE SET display_name = excluded.display_name,
                  fetched_at   = now()
    WHERE channel_models.source = 'fetched';

-- name: DeleteStaleChannelModels :execrows
-- Models the endpoint stopped listing. Scoped to `source = 'fetched'`: a manual
-- row was never the endpoint's to mention, so its absence says nothing about
-- it.
DELETE
FROM channel_models
WHERE channel_id = sqlc.arg('channel_id')
  AND source = 'fetched'
  AND NOT (model_id = ANY (sqlc.arg('model_ids')::text[]));

-- name: CountEnabledChannelModels :many
-- One grouped read for the whole channel list rather than a count per channel.
-- Channels with nothing enabled are absent rather than zero, which is the same
-- answer a caller reading a map gets for free.
SELECT channel_id, count(*) AS model_count
FROM channel_models
WHERE enabled
GROUP BY channel_id;

-- name: ChannelSpend :many
-- What each channel's requests have cost, all time, from the usage ledger: one
-- grouped read for the whole channel list. Requests with no price (a model
-- the catalogue does not price) add nothing. A channel never used is absent.
SELECT channel_id::uuid AS channel_id, COALESCE(sum(cost), 0)::float8 AS spent
FROM usage_logs
WHERE channel_id IS NOT NULL
GROUP BY channel_id;

-- name: CountEnabledChannelModelsFor :one
SELECT count(*)
FROM channel_models
WHERE channel_id = $1
  AND enabled;

-- name: ListCatalogModelsForChannel :many
-- What models.dev knows about the models one channel lists.
--
-- Deliberately the same column list as ListServedModels, so the row converts to
-- that one and both go through a single mapper. If the two ever diverge this
-- stops compiling, which is when you want to hear about it.
--
-- The channel is the authority on which models exist; this only decorates them,
-- so a model the catalogue has never heard of simply arrives undecorated.
SELECT DISTINCT ON (m.model_id) m.provider_id,
       m.model_id,
       m.name,
       m.description,
       m.family,
       m.attachment,
       m.reasoning,
       m.tool_call,
       m.structured_output,
       m.context_limit,
       m.output_limit,
       coalesce((m.raw -> 'limit' ->> 'input')::numeric::bigint, 0)::bigint AS input_limit,
       m.input_cost::double precision  AS input_cost,
       m.output_cost::double precision AS output_cost,
       m.release_date,
       m.knowledge,
       m.input_modalities,
       m.output_modalities,
       p.name                          AS provider_name,
       coalesce(s.enabled, false)      AS enabled,
       coalesce(m.raw -> 'reasoning_options', '[]'::jsonb)::jsonb AS reasoning_options
FROM catalog_models m
         JOIN catalog_providers p ON p.id = m.provider_id
         LEFT JOIN LATERAL (SELECT bool_or(cm.enabled) AS enabled
                            FROM channel_models cm
                            WHERE cm.model_id = m.model_id) s ON true
-- Keyed on the model id alone. The channel no longer carries a brand, and one
-- model id can sit under several catalogue providers (`claude-sonnet-4` under
-- both `anthropic` and `amazon-bedrock`), so DISTINCT ON picks one and picks
-- the same one every time. Preferring the provider that matches the channel's
-- protocol describes a direct connection correctly; for a relay any row is
-- equally arbitrary, and an arbitrary context limit beats none.
WHERE m.model_id = ANY (sqlc.arg('model_ids')::text[])
ORDER BY m.model_id, (m.provider_id = sqlc.arg('wire_api')::text) DESC, m.provider_id;

-- name: PickServingChannel :one
-- The channel a new run of this model is answered from: the first enabled one
-- that offers it, in the operator's order. Pinned on the run, so a later change
-- here affects the next run, never one already going.
SELECT c.id
FROM channel_models cm
         JOIN channels c ON c.id = cm.channel_id
WHERE cm.model_id = sqlc.arg('model_id')
  AND cm.enabled
  AND c.enabled
ORDER BY c.sort_order, c.created_at
LIMIT 1;

-- name: FirstServedModel :one
-- What a conversation that never picked a model is answered with.
SELECT cm.model_id
FROM channel_models cm
         JOIN channels c ON c.id = cm.channel_id
WHERE cm.enabled
  AND c.enabled
ORDER BY c.sort_order, c.created_at, cm.model_id
LIMIT 1;

-- name: GetServingChannel :one
-- A run's pinned channel, as long as it still serves the model. Anything else
-- — deleted, disabled, the model switched off — reads as no rows, which is
-- the run's `no_model`.
SELECT c.*
FROM channels c
         JOIN channel_models cm ON cm.channel_id = c.id
WHERE c.id = sqlc.arg('channel_id')
  AND cm.model_id = sqlc.arg('model_id')
  AND c.enabled
  AND cm.enabled;
