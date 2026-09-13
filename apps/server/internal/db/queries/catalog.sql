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
-- Offset paging, as in ListUsers: the table wants page numbers and a total.
--
-- `enabled` is a left join rather than a column because model_settings is this
-- installation's decision and the catalogue is a cache of somebody else's file;
-- a model nobody has touched has no row at all. `raw` is deliberately not
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
       coalesce(s.enabled, false)      AS enabled
FROM catalog_models m
         JOIN catalog_providers p ON p.id = m.provider_id
         LEFT JOIN model_settings s ON s.provider_id = m.provider_id AND s.model_id = m.model_id
WHERE (sqlc.narg('search')::text IS NULL
    OR m.model_id ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.family ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('provider_id')::text IS NULL OR m.provider_id = sqlc.narg('provider_id')::text)
  AND (NOT sqlc.arg('require_reasoning')::boolean OR m.reasoning)
  AND (NOT sqlc.arg('require_tool_call')::boolean OR m.tool_call)
  AND (NOT sqlc.arg('require_attachment')::boolean OR m.attachment)
  AND (NOT sqlc.arg('require_structured_output')::boolean OR m.structured_output)
  AND (sqlc.narg('enabled')::boolean IS NULL OR coalesce(s.enabled, false) = sqlc.narg('enabled')::boolean)
  AND (NOT sqlc.arg('channels_only')::boolean
    OR EXISTS (SELECT 1 FROM channels c WHERE c.provider_id = m.provider_id))
ORDER BY p.name, m.name, m.model_id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountCatalogModelsFiltered :one
SELECT count(*)
FROM catalog_models m
         JOIN catalog_providers p ON p.id = m.provider_id
         LEFT JOIN model_settings s ON s.provider_id = m.provider_id AND s.model_id = m.model_id
WHERE (sqlc.narg('search')::text IS NULL
    OR m.model_id ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR m.family ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('provider_id')::text IS NULL OR m.provider_id = sqlc.narg('provider_id')::text)
  AND (NOT sqlc.arg('require_reasoning')::boolean OR m.reasoning)
  AND (NOT sqlc.arg('require_tool_call')::boolean OR m.tool_call)
  AND (NOT sqlc.arg('require_attachment')::boolean OR m.attachment)
  AND (NOT sqlc.arg('require_structured_output')::boolean OR m.structured_output)
  AND (sqlc.narg('enabled')::boolean IS NULL OR coalesce(s.enabled, false) = sqlc.narg('enabled')::boolean)
  AND (NOT sqlc.arg('channels_only')::boolean
    OR EXISTS (SELECT 1 FROM channels c WHERE c.provider_id = m.provider_id));

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
         LEFT JOIN model_settings s ON s.provider_id = m.provider_id AND s.model_id = m.model_id
WHERE m.provider_id = sqlc.arg('provider_id')
  AND m.model_id = sqlc.arg('model_id');

-- name: SetModelEnabled :execrows
-- The only statement in this file that writes model_settings. A sync must not
-- reach it: replacing the catalogue is not a reason to switch a model on or
-- off.
--
-- The SELECT is the existence check rather than a query before it: a settings
-- row for a model no provider offers is a row nothing would ever read again.
-- No rows affected therefore means the model is unknown.
INSERT INTO model_settings (provider_id, model_id, enabled, updated_at)
SELECT m.provider_id, m.model_id, sqlc.arg('enabled'), now()
FROM catalog_models m
WHERE m.provider_id = sqlc.arg('provider_id')
  AND m.model_id = sqlc.arg('model_id')
ON CONFLICT (provider_id, model_id)
    DO UPDATE SET enabled    = excluded.enabled,
                  updated_at = now();


-- name: ListChannelsForProviders :many
-- Which configured channels can serve each of these providers. Fetched for one
-- page of models rather than joined into the list, because a provider with
-- three channels would otherwise multiply its models by three and break both
-- the paging and the count.
SELECT id, name, enabled, provider_id
FROM channels
WHERE provider_id = ANY (sqlc.arg('provider_ids')::text[])
ORDER BY sort_order, created_at;
