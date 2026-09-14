-- name: DeleteCatalogModels :exec
DELETE
FROM catalog_models;

-- name: DeleteCatalogProviders :exec
DELETE
FROM catalog_providers;

-- name: InsertCatalogProviders :copyfrom
INSERT INTO catalog_providers (id, name, doc, env)
VALUES ($1, $2, $3, $4);

-- name: InsertCatalogModels :copyfrom
INSERT INTO catalog_models (provider_id, model_id, name, description, family,
                            attachment, reasoning, tool_call, structured_output,
                            context_limit, output_limit, input_cost, output_cost,
                            release_date, knowledge, input_modalities, output_modalities, raw)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: CountCatalogProviders :one
SELECT count(*)
FROM catalog_providers;

-- name: CountCatalogModels :one
SELECT count(*)
FROM catalog_models;

-- name: ListCatalogProviders :many
-- The model count comes from the same read as the provider: the picker shows
-- both, and a provider with no models is worth showing as empty rather than
-- hiding.
SELECT p.id,
       p.name,
       p.doc,
       p.env,
       count(m.model_id) AS model_count
FROM catalog_providers p
         LEFT JOIN catalog_models m ON m.provider_id = p.id
GROUP BY p.id
ORDER BY p.name;

-- name: ListCatalogModels :many
-- The full-catalogue scope: everything models.dev knows about. Not an
-- inventory -- it is the pool you read when deciding which channel to add
-- next, which is why membership here owes nothing to the channels.
--
-- Offset paging, as in ListUsers: the table wants page numbers and a total.
--
-- `enabled` is a left join rather than a column because offering a model is
-- this installation's decision and the catalogue is a cache of somebody else's
-- file. What decides it is the channels' own model lists: a model is offered
-- when some channel that serves it has it switched on. The channels know a
-- model by its id alone, so a model id served anywhere counts for every
-- catalogue entry that spells it the same -- `claude-sonnet-4` reads as offered
-- under both `anthropic` and `amazon-bedrock`. `raw` is deliberately not
-- selected, being several times the size of everything else here.
--
-- Each capability narrows rather than widens, so asking for two gets models
-- that have both.
SELECT m.provider_id,
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
       m.input_cost::double precision  AS input_cost,
       m.output_cost::double precision AS output_cost,
       m.release_date,
       m.knowledge,
       m.input_modalities,
       m.output_modalities,
       p.name                          AS provider_name,
       coalesce(o.enabled, false)      AS enabled
FROM catalog_models m
         JOIN catalog_providers p ON p.id = m.provider_id
         LEFT JOIN (SELECT cm.model_id, bool_or(cm.enabled) AS enabled
                    FROM channel_models cm
                    GROUP BY cm.model_id) o ON o.model_id = m.model_id
WHERE (sqlc.narg('search')::text IS NULL
    OR m.model_id ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.family ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('provider_id')::text IS NULL OR m.provider_id = sqlc.narg('provider_id')::text)
  AND (NOT sqlc.arg('require_reasoning')::boolean OR m.reasoning)
  AND (NOT sqlc.arg('require_tool_call')::boolean OR m.tool_call)
  AND (NOT sqlc.arg('require_attachment')::boolean OR m.attachment)
  AND (NOT sqlc.arg('require_structured_output')::boolean OR m.structured_output)
  AND (sqlc.narg('enabled')::boolean IS NULL OR coalesce(o.enabled, false) = sqlc.narg('enabled')::boolean)
ORDER BY p.name, m.name, m.model_id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountCatalogModelsFiltered :one
SELECT count(*)
FROM catalog_models m
         JOIN catalog_providers p ON p.id = m.provider_id
         LEFT JOIN (SELECT cm.model_id, bool_or(cm.enabled) AS enabled
                    FROM channel_models cm
                    GROUP BY cm.model_id) o ON o.model_id = m.model_id
WHERE (sqlc.narg('search')::text IS NULL
    OR m.model_id ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.family ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('provider_id')::text IS NULL OR m.provider_id = sqlc.narg('provider_id')::text)
  AND (NOT sqlc.arg('require_reasoning')::boolean OR m.reasoning)
  AND (NOT sqlc.arg('require_tool_call')::boolean OR m.tool_call)
  AND (NOT sqlc.arg('require_attachment')::boolean OR m.attachment)
  AND (NOT sqlc.arg('require_structured_output')::boolean OR m.structured_output)
  AND (sqlc.narg('enabled')::boolean IS NULL OR coalesce(o.enabled, false) = sqlc.narg('enabled')::boolean);

-- name: ListServedModels :many
-- The channel scope: the models this installation actually serves.
--
-- A channel's own model list is the authority on which models exist. A relay
-- serving Claude through an OpenAI-shaped API serves Claude; a provider id
-- says nothing about it either way, and deriving reach from one credited every
-- OpenAI model to that relay and none of the models it actually answers with.
-- So the rows come from channel_models and models.dev is a LEFT JOIN that only
-- decorates them: a model the catalogue has never heard of keeps its row and
-- arrives with its metadata columns at their zero values, which the handler
-- reports as absent rather than as a confident zero.
--
-- One row per MODEL, never per (model, channel). Three channels serving the
-- same model are one row carrying three channels, so the channels are folded
-- into the aggregate here and named separately, one query per page, by
-- ListChannelsServingModels. Joining them in would multiply the model by three
-- and break both the paging and the total.
--
-- A capability filter necessarily excludes an uncatalogued model: nothing is
-- known about what it can do, and answering "it might" is not a filter.
WITH served AS (SELECT cm.model_id,
                       bool_or(cm.enabled)                                  AS enabled,
                       min(nullif(cm.display_name, ''))                     AS display_name,
                       -- The brands the operator put on the channels that serve
                       -- it. `provider_id` on a channel is a hint, never how the
                       -- endpoint is called, and all it does here is pick which
                       -- catalogue row decorates the model -- it has no say in
                       -- which models exist.
                       array_remove(array_agg(DISTINCT c.provider_id), NULL) AS brands
                FROM channel_models cm
                         JOIN channels c ON c.id = cm.channel_id
                GROUP BY cm.model_id)
-- An uncatalogued model has no brand, and the serving channel's is not a
-- substitute: a relay branded `openai` is exactly the thing that serves models
-- OpenAI never made, so claiming its brand here would put the wrong mark on
-- them. Empty is the honest answer, and the card falls back to the model's own
-- name for its icon.
SELECT coalesce(m.provider_id, '')::text                            AS provider_id,
       s.model_id,
       -- The endpoint's own spelling is the last resort, and a perfectly good
       -- name: it is what an operator typed in and what goes back on the wire.
       coalesce(nullif(m.name, ''), s.display_name, s.model_id)::text AS name,
       coalesce(m.description, '')::text                            AS description,
       coalesce(m.family, '')::text                                 AS family,
       coalesce(m.attachment, false)::boolean                       AS attachment,
       coalesce(m.reasoning, false)::boolean                        AS reasoning,
       coalesce(m.tool_call, false)::boolean                        AS tool_call,
       coalesce(m.structured_output, false)::boolean                AS structured_output,
       coalesce(m.context_limit, 0)::bigint                         AS context_limit,
       coalesce(m.output_limit, 0)::bigint                          AS output_limit,
       coalesce(m.input_cost, 0)::double precision                  AS input_cost,
       coalesce(m.output_cost, 0)::double precision                 AS output_cost,
       coalesce(m.release_date, '')::text                           AS release_date,
       coalesce(m.knowledge, '')::text                              AS knowledge,
       coalesce(m.input_modalities, '{}')::text[]                   AS input_modalities,
       coalesce(m.output_modalities, '{}')::text[]                  AS output_modalities,
       coalesce(m.provider_name, '')::text                          AS provider_name,
       coalesce(s.enabled, false)::boolean                          AS enabled
FROM served s
         -- One catalogue row at most, chosen rather than joined: the same model
         -- id appears under several providers, and joining them all would
         -- multiply the row again. A provider one of the serving channels
         -- declares wins; failing that, the first by id, so the choice is
         -- stable from one page to the next.
         LEFT JOIN LATERAL (SELECT c2.provider_id,
                                   c2.name,
                                   c2.description,
                                   c2.family,
                                   c2.attachment,
                                   c2.reasoning,
                                   c2.tool_call,
                                   c2.structured_output,
                                   c2.context_limit,
                                   c2.output_limit,
                                   c2.input_cost,
                                   c2.output_cost,
                                   c2.release_date,
                                   c2.knowledge,
                                   c2.input_modalities,
                                   c2.output_modalities,
                                   p.name AS provider_name
                            FROM catalog_models c2
                                     JOIN catalog_providers p ON p.id = c2.provider_id
                            WHERE c2.model_id = s.model_id
                            ORDER BY (c2.provider_id = ANY (s.brands)) DESC, c2.provider_id
                            LIMIT 1) m ON true
WHERE (sqlc.narg('search')::text IS NULL
    OR s.model_id ILIKE '%' || sqlc.narg('search')::text || '%'
    OR s.display_name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.family ILIKE '%' || sqlc.narg('search')::text || '%')
  -- A model the catalogue does not list has no brand to filter on, so asking
  -- for one excludes it, exactly as a capability filter does.
  AND (sqlc.narg('provider_id')::text IS NULL OR m.provider_id = sqlc.narg('provider_id')::text)
  AND (NOT sqlc.arg('require_reasoning')::boolean OR coalesce(m.reasoning, false))
  AND (NOT sqlc.arg('require_tool_call')::boolean OR coalesce(m.tool_call, false))
  AND (NOT sqlc.arg('require_attachment')::boolean OR coalesce(m.attachment, false))
  AND (NOT sqlc.arg('require_structured_output')::boolean OR coalesce(m.structured_output, false))
  AND (sqlc.narg('enabled')::boolean IS NULL OR coalesce(s.enabled, false) = sqlc.narg('enabled')::boolean)
-- By name rather than by brand: half these rows can have no brand to group
-- under, and sorting on an empty one would herd every uncatalogued model into a
-- block at the top that looks like a fault.
ORDER BY coalesce(nullif(m.name, ''), s.display_name, s.model_id), s.model_id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountServedModels :one
-- The total behind ListServedModels; see the comments there. Counting `served`
-- rather than channel_models is what keeps a model served by three channels
-- from counting three times.
WITH served AS (SELECT cm.model_id,
                       bool_or(cm.enabled)                                  AS enabled,
                       min(nullif(cm.display_name, ''))                     AS display_name,
                       array_remove(array_agg(DISTINCT c.provider_id), NULL) AS brands
                FROM channel_models cm
                         JOIN channels c ON c.id = cm.channel_id
                GROUP BY cm.model_id)
SELECT count(*)
FROM served s
         LEFT JOIN LATERAL (SELECT c2.provider_id,
                                   c2.name,
                                   c2.family,
                                   c2.attachment,
                                   c2.reasoning,
                                   c2.tool_call,
                                   c2.structured_output
                            FROM catalog_models c2
                            WHERE c2.model_id = s.model_id
                            ORDER BY (c2.provider_id = ANY (s.brands)) DESC, c2.provider_id
                            LIMIT 1) m ON true
WHERE (sqlc.narg('search')::text IS NULL
    OR s.model_id ILIKE '%' || sqlc.narg('search')::text || '%'
    OR s.display_name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.family ILIKE '%' || sqlc.narg('search')::text || '%')
  -- A model the catalogue does not list has no brand to filter on, so asking
  -- for one excludes it, exactly as a capability filter does.
  AND (sqlc.narg('provider_id')::text IS NULL OR m.provider_id = sqlc.narg('provider_id')::text)
  AND (NOT sqlc.arg('require_reasoning')::boolean OR coalesce(m.reasoning, false))
  AND (NOT sqlc.arg('require_tool_call')::boolean OR coalesce(m.tool_call, false))
  AND (NOT sqlc.arg('require_attachment')::boolean OR coalesce(m.attachment, false))
  AND (NOT sqlc.arg('require_structured_output')::boolean OR coalesce(m.structured_output, false))
  AND (sqlc.narg('enabled')::boolean IS NULL OR coalesce(s.enabled, false) = sqlc.narg('enabled')::boolean);

-- name: GetCatalogModel :one
SELECT m.provider_id,
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
       m.input_cost::double precision  AS input_cost,
       m.output_cost::double precision AS output_cost,
       m.release_date,
       m.knowledge,
       m.input_modalities,
       m.output_modalities,
       p.name                          AS provider_name,
       coalesce(s.enabled, false)      AS enabled
FROM catalog_models m
         JOIN catalog_providers p ON p.id = m.provider_id
         LEFT JOIN LATERAL (SELECT bool_or(cm.enabled) AS enabled
                            FROM channel_models cm
                            WHERE cm.model_id = m.model_id) s ON true
WHERE m.provider_id = sqlc.arg('provider_id')
  AND m.model_id = sqlc.arg('model_id');



-- name: ListChannelsServingModels :many
-- Which configured channels serve each of these models -- asked of the
-- channels' own model lists, which is the only place that knows. A channel's
-- provider id is branding and cannot answer it: it would hand a relay every
-- model of the brand it names and none of the ones it actually serves.
--
-- Fetched for one page of models rather than joined into the list, because a
-- model with three channels would otherwise appear three times and break both
-- the paging and the count.
SELECT cm.model_id, c.id, c.name, c.enabled
FROM channel_models cm
         JOIN channels c ON c.id = cm.channel_id
WHERE cm.model_id = ANY (sqlc.arg('model_ids')::text[])
ORDER BY c.sort_order, c.created_at;
