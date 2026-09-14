-- +goose Up
-- +goose StatementBegin

-- Whether a model is offered is a property of the channel that serves it, and
-- `channel_models.enabled` is where that now lives. This table held the same
-- answer keyed on (catalogue provider, model), which stopped being able to
-- express the truth the moment one model could arrive through several channels
-- — and worse, went on answering `false` next to a channel row saying `true`.
--
-- Nothing reads it. Dropping rather than leaving it is the point: a table with
-- no readers reads as one with readers not yet found.
DROP TABLE IF EXISTS model_settings;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE model_settings
(
    provider_id text        NOT NULL,
    model_id    text        NOT NULL,
    enabled     boolean     NOT NULL DEFAULT false,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, model_id)
);
-- +goose StatementEnd
