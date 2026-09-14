-- +goose Up
-- +goose StatementBegin

-- What protocol this endpoint speaks, which is not the same question as which
-- brand it is. A relay speaks OpenAI while serving Anthropic and Google models,
-- so `provider_id` (branding, catalogue lookup) cannot decide how to call it.
CREATE TYPE channel_kind AS ENUM ('openai', 'anthropic', 'gemini');

ALTER TABLE channels
    ADD COLUMN kind channel_kind NOT NULL DEFAULT 'openai';

-- provider_id stops being required: an endpoint models.dev has never heard of
-- is still perfectly usable, it just arrives without an icon or a context limit.
ALTER TABLE channels
    ALTER COLUMN provider_id DROP NOT NULL;

-- The original constraint predates the column being nullable and now says
-- something confusing: NULL passes it and '' does not, so "no brand" had two
-- spellings, one of which the database rejected. Replace it with the rule that
-- was actually intended — absent is NULL, and a present value is not blank.
ALTER TABLE channels
    DROP CONSTRAINT IF EXISTS channels_provider_id_check;
ALTER TABLE channels
    ADD CONSTRAINT channels_provider_id_not_blank
        CHECK (provider_id IS NULL OR length(btrim(provider_id)) > 0);

-- The channel is the authority on which models exist. models.dev decorates
-- these rows; it does not decide their membership, which is why this table has
-- no foreign key into the catalogue and rows here can name models the catalogue
-- has never heard of.
CREATE TABLE channel_models
(
    channel_id   uuid        NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    model_id     text        NOT NULL,
    display_name text        NOT NULL DEFAULT '',
    -- `fetched` came from the endpoint's own list and a refresh may remove it;
    -- `manual` was typed in and a refresh must leave it alone.
    source       text        NOT NULL DEFAULT 'fetched'
        CONSTRAINT channel_models_source CHECK (source IN ('fetched', 'manual')),
    -- Ours, never the refresh's. A model that survives a refresh keeps this.
    enabled      boolean     NOT NULL DEFAULT false,
    fetched_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, model_id)
);

CREATE INDEX channel_models_enabled_idx ON channel_models (channel_id) WHERE enabled;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS channel_models;
ALTER TABLE channels DROP COLUMN IF EXISTS kind;
DROP TYPE IF EXISTS channel_kind;
-- +goose StatementEnd
