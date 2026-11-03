-- +goose Up
-- +goose StatementBegin
-- What a plugin needs to reach its service and to offer its tools: the header
-- an administrator sets (its value sealed with OPENCHAT_SECRET_KEY, never in
-- the clear), and each tool as the service describes it.
ALTER TABLE plugins
    ADD COLUMN header_name   text  NOT NULL DEFAULT 'Authorization',
    ADD COLUMN header_secret bytea,
    ADD COLUMN tools_fetched_at timestamptz;

ALTER TABLE plugin_tools
    ADD COLUMN title        text    NOT NULL DEFAULT '',
    ADD COLUMN input_schema jsonb   NOT NULL DEFAULT '{}',
    ADD COLUMN destructive  boolean NOT NULL DEFAULT true;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plugin_tools DROP COLUMN destructive, DROP COLUMN input_schema, DROP COLUMN title;
ALTER TABLE plugins DROP COLUMN tools_fetched_at, DROP COLUMN header_secret, DROP COLUMN header_name;
-- +goose StatementEnd
