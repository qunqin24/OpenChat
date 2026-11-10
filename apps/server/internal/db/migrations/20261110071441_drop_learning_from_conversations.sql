-- +goose Up
-- +goose StatementBegin

-- Learning memories from conversations by itself (the planned second phase of
-- personal memory) is not going to be built: referring to earlier
-- conversations (chat-history-plan.md) covers picking up what was said before,
-- verbatim. Its switch, never shown, and the source it would have written go.
ALTER TABLE users DROP COLUMN memory_learn;

-- Nothing ever wrote `auto`; were a row to have it, it was a model's.
UPDATE user_memories SET source = 'model' WHERE source = 'auto';
UPDATE memory_events SET source = 'model' WHERE source = 'auto';

ALTER TYPE memory_source RENAME TO memory_source_old;
CREATE TYPE memory_source AS ENUM ('user', 'model');
ALTER TABLE user_memories ALTER COLUMN source TYPE memory_source USING source::text::memory_source;
ALTER TABLE memory_events ALTER COLUMN source TYPE memory_source USING source::text::memory_source;
DROP TYPE memory_source_old;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TYPE memory_source ADD VALUE 'auto';
ALTER TABLE users ADD COLUMN memory_learn boolean NOT NULL DEFAULT false;

-- +goose StatementEnd
