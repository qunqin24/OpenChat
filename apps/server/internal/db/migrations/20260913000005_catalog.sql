-- +goose Up
-- +goose StatementBegin

-- A cache of models.dev, not a source of truth. Every row is replaceable by the
-- next refresh, which is why nothing references it by foreign key.
CREATE TABLE catalog_providers
(
    id         text PRIMARY KEY,
    name       text        NOT NULL,
    doc        text        NOT NULL DEFAULT '',
    env        text[]      NOT NULL DEFAULT '{}',
    synced_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE catalog_models
(
    provider_id       text        NOT NULL,
    model_id          text        NOT NULL,
    name              text        NOT NULL,
    description       text        NOT NULL DEFAULT '',
    family            text        NOT NULL DEFAULT '',
    attachment        boolean     NOT NULL DEFAULT false,
    reasoning         boolean     NOT NULL DEFAULT false,
    tool_call         boolean     NOT NULL DEFAULT false,
    structured_output boolean     NOT NULL DEFAULT false,
    context_limit     bigint      NOT NULL DEFAULT 0,
    output_limit      bigint      NOT NULL DEFAULT 0,
    input_cost        numeric     NOT NULL DEFAULT 0,
    output_cost       numeric     NOT NULL DEFAULT 0,
    release_date      text        NOT NULL DEFAULT '',
    knowledge         text        NOT NULL DEFAULT '',
    input_modalities  text[]      NOT NULL DEFAULT '{}',
    output_modalities text[]      NOT NULL DEFAULT '{}',
    -- The upstream entry verbatim. Columns above are what we query on; this is
    -- what lets a later feature read a field we did not think to extract,
    -- without waiting for a refresh.
    raw               jsonb       NOT NULL,
    synced_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, model_id)
);

CREATE INDEX catalog_models_provider_idx ON catalog_models (provider_id);

-- No index for search. The contract's search is `ILIKE '%x%'` across id, name
-- and family, and a leading wildcard cannot use a btree — an index on
-- lower(name) would sit there looking useful and never be chosen. A sequential
-- scan over a few thousand rows is cheap; when it stops being cheap the answer
-- is a pg_trgm GIN index, which is a different decision with a dependency
-- attached.

-- Which models this installation has switched on. Separate from the cache
-- because it is *our* decision: a refresh replaces the catalogue and must not
-- silently turn anything on or off.
CREATE TABLE model_settings
(
    provider_id text        NOT NULL,
    model_id    text        NOT NULL,
    enabled     boolean     NOT NULL DEFAULT false,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, model_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS model_settings;
DROP TABLE IF EXISTS catalog_models;
DROP TABLE IF EXISTS catalog_providers;
-- +goose StatementEnd
