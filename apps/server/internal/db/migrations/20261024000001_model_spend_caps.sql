-- +goose Up
-- +goose StatementBegin

-- A spending cap per model: once what the model has cost in the cap's period
-- reaches `capacity`, it cannot start or go on with a request (unless the
-- requester is an administrator and the cap exempts them).
--
-- Not catalogue figures, so nothing follows models.dev here, like admin_only.
-- A row from before this migration has no capacity and is not capped. The
-- period and the exemption mean nothing without a capacity; the server writes
-- their defaults back whenever the capacity is cleared, so no state hides
-- behind an uncapped model.
ALTER TABLE model_overrides
    -- USD, the unit usage_logs.cost is in. numeric, and compared as numeric:
    -- a float's tail would let a cap trip a request early or late.
    ADD COLUMN capacity              numeric
        CONSTRAINT model_overrides_capacity CHECK (capacity > 0),
    -- `total` is spend since `capacity_since` (all of it when that is NULL);
    -- `monthly` and `daily` since the start of the calendar month or day in the
    -- server's time zone, which the server works out, so nothing here resets.
    ADD COLUMN capacity_period       text        NOT NULL DEFAULT 'total'
        CONSTRAINT model_overrides_capacity_period CHECK (capacity_period IN ('total', 'monthly', 'daily')),
    -- Administrators may go on using a capped model (their spend still counts).
    ADD COLUMN capacity_admin_exempt boolean     NOT NULL DEFAULT true,
    -- Where a `total` cap starts counting: the operator's "recalculate".
    -- Written by its own endpoint and left alone by saving the settings.
    ADD COLUMN capacity_since        timestamptz;

-- The cap's sum runs before every request of a capped model: the model, a
-- range of time, and the cost. Carrying the cost in the index lets that be an
-- index-only scan. It replaces usage_logs_model_idx, which it serves as well
-- (same keys, same order) for the log's model filter.
DROP INDEX usage_logs_model_idx;
CREATE INDEX usage_logs_model_idx ON usage_logs (model_id, created_at DESC) INCLUDE (cost);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX usage_logs_model_idx;
CREATE INDEX usage_logs_model_idx ON usage_logs (model_id, created_at DESC);

ALTER TABLE model_overrides
    DROP COLUMN IF EXISTS capacity_since,
    DROP COLUMN IF EXISTS capacity_admin_exempt,
    DROP COLUMN IF EXISTS capacity_period,
    DROP COLUMN IF EXISTS capacity;
-- +goose StatementEnd
