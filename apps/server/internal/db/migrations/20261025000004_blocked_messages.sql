-- +goose Up
-- +goose StatementBegin

-- A message that content moderation refused is kept in the conversation, so it
-- is still there after a refresh and on the person's other devices: a `user`
-- row with this status, holding the refused text. It is shown and never sent -
-- it is not part of what a model is given, what a summary covers, or what a
-- title is made from (api/moderation.go, agent/context.go). It starts no run
-- and claims no file.
--
-- ADD VALUE is allowed inside goose's transaction because nothing in this
-- migration uses the new value.
ALTER TYPE message_status ADD VALUE 'blocked';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Postgres cannot drop an enum value, so the type is rebuilt. The refused
-- messages go: the old code has no way to show or skip them.
DELETE FROM messages WHERE status = 'blocked';

ALTER TABLE messages ALTER COLUMN status DROP DEFAULT;
ALTER TYPE message_status RENAME TO message_status_new;
CREATE TYPE message_status AS ENUM ('pending', 'streaming', 'complete', 'failed');
ALTER TABLE messages
    ALTER COLUMN status TYPE message_status USING status::text::message_status;
ALTER TABLE messages ALTER COLUMN status SET DEFAULT 'complete';
DROP TYPE message_status_new;

-- +goose StatementEnd
