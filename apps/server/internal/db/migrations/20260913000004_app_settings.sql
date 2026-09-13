-- +goose Up
-- +goose StatementBegin

-- Server-wide settings an operator can change without a redeploy. One row per
-- setting rather than one row with a column each, so adding a setting is an
-- INSERT and not a migration.
CREATE TABLE app_settings
(
    key        text PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Debug logging is persisted rather than kept in memory because the moment you
-- most want it is across a restart: an intermittent fault you are trying to
-- catch should not need the switch flipped again every time the process comes
-- back.
INSERT INTO app_settings (key, value)
VALUES ('debug_logging', 'false'::jsonb);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS app_settings;
-- +goose StatementEnd
