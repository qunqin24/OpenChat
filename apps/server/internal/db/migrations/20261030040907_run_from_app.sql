-- +goose Up
-- +goose StatementBegin
-- Whether the run was started from the phone app (a request signed in with a
-- bearer token, which only the app is given). The run is carried out in the
-- background, after the request is gone, and its system prompt then tells the
-- model the answer is read on a narrow screen. Runs the server starts itself -
-- scheduled tasks, delivered sub-agent reports - are not.
ALTER TABLE runs ADD COLUMN from_app boolean NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN from_app;
-- +goose StatementEnd
