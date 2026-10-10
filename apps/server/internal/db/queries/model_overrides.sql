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
-- and a field it sends as null goes back to following the catalogue.
INSERT INTO model_overrides (model_id, kind, reasoning, tool_call, input_modalities,
                             context_limit, input_limit, output_limit, reasoning_efforts, admin_only)
VALUES (sqlc.arg('model_id'), sqlc.narg('kind'), sqlc.narg('reasoning'), sqlc.narg('tool_call'),
        sqlc.narg('input_modalities')::text[], sqlc.narg('context_limit'), sqlc.narg('input_limit'),
        sqlc.narg('output_limit'), sqlc.narg('reasoning_efforts')::text[], sqlc.arg('admin_only'))
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
        updated_at        = now()
RETURNING *;

-- name: DeleteModelOverride :exec
DELETE
FROM model_overrides
WHERE model_id = sqlc.arg('model_id');
