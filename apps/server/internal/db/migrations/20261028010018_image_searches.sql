-- +goose Up
-- +goose StatementBegin
-- The pictures answers describe (api/images.go). An answer never gives an
-- image address: it describes a picture in a few words, and a search service
-- (Openverse, or Pixabay with a key) finds one. Each picture found is kept in
-- the file store - Pixabay asks for that rather than links to it, and it keeps
-- a picture from vanishing with the page it came from - with what its licence
-- asks to be shown.
CREATE TABLE pictures
(
    id           uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    provider     text        NOT NULL,
    provider_id  text        NOT NULL,
    storage_key  text        NOT NULL,
    content_type text        NOT NULL,
    width        integer     NOT NULL,
    height       integer     NOT NULL,
    title        text        NOT NULL DEFAULT '',
    creator      text        NOT NULL DEFAULT '',
    creator_url  text        NOT NULL DEFAULT '',
    license      text        NOT NULL DEFAULT '',
    license_url  text        NOT NULL DEFAULT '',
    landing_url  text        NOT NULL DEFAULT '',
    source       text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- The same picture found by other words is kept once.
    UNIQUE (provider, provider_id)
);

-- What each description found, so the same words show the same picture on
-- every reload and for every reader, and a service - free, and limited per
-- server - is asked once per description. Nothing here says who asked: a
-- description is a few words such as "laptop keyboard", shared by everyone. A
-- row without a picture is a search that found nothing, asked again once it is
-- a day old.
CREATE TABLE image_searches
(
    query       text PRIMARY KEY,
    picture_id  uuid REFERENCES pictures (id) ON DELETE SET NULL,
    searched_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE image_searches;
DROP TABLE pictures;
-- +goose StatementEnd
