-- +goose Up
-- +goose StatementBegin
-- A conversation pinned to the top of its owner's list, and when: pins are
-- shown newest first. NULL is not pinned.
ALTER TABLE conversations ADD COLUMN pinned_at TIMESTAMPTZ;

CREATE INDEX conversations_pinned_idx ON conversations (user_id, pinned_at DESC) WHERE pinned_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS conversations_pinned_idx;
ALTER TABLE conversations DROP COLUMN IF EXISTS pinned_at;
-- +goose StatementEnd
