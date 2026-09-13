-- +goose Up
-- +goose StatementBegin

-- A channel is one configured connection to a model provider: which provider,
-- where, and with what credential. Models are discovered from the catalogue
-- rather than stored here, so this table stays about connections.
CREATE TABLE channels
(
    id          uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    name        text        NOT NULL CHECK (length(btrim(name)) > 0),
    -- A models.dev provider id. Not a foreign key: the catalogue is a cache of
    -- an upstream file, and a channel must survive that cache being empty or
    -- refreshed.
    provider_id text        NOT NULL CHECK (length(btrim(provider_id)) > 0),
    -- Empty means "use the provider's default endpoint", which is knowledge the
    -- adapter has and this table does not.
    base_url    text        NOT NULL DEFAULT '',
    -- Stored as given. The API returns only whether a key exists and its last
    -- four characters, never the value. Anything that can read this column --
    -- backups, a dump, a log of one -- holds every credential, so treat them as
    -- secret material.
    api_key     text        NOT NULL DEFAULT '',
    enabled     boolean     NOT NULL DEFAULT true,
    sort_order  integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- The list is ordered by hand first, then by age, so the ordering is stable
-- when several channels share a sort_order.
CREATE INDEX channels_order_idx ON channels (sort_order, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS channels;
-- +goose StatementEnd
