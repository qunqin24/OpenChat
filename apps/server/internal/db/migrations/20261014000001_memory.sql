-- +goose Up
-- +goose StatementBegin

-- Personalisation lives on the user: the two custom-instruction fields, the
-- person's own switch for memory, and (phase two, not shown anywhere yet) the
-- switch for learning from conversations on its own. That one defaults to off:
-- nothing is learned behind anyone's back until they have been asked.
ALTER TABLE users
    ADD COLUMN instructions_about text    NOT NULL DEFAULT '',
    ADD COLUMN instructions_style text    NOT NULL DEFAULT '',
    ADD COLUMN memory_enabled     boolean NOT NULL DEFAULT true,
    ADD COLUMN memory_learn       boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT users_instructions_about_len CHECK (char_length(instructions_about) <= 1500),
    ADD CONSTRAINT users_instructions_style_len CHECK (char_length(instructions_style) <= 1500);

-- `user` is a person writing it down, `model` the memory tool, `auto` is
-- reserved for the phase that extracts memories after a conversation.
CREATE TYPE memory_source AS ENUM ('user', 'model', 'auto');
CREATE TYPE memory_op AS ENUM ('add', 'update', 'delete');

CREATE TABLE user_memories (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    content         text          NOT NULL CHECK (char_length(content) BETWEEN 1 AND 200),
    source          memory_source NOT NULL,
    -- Where it was learned, if from a conversation. The conversation going away
    -- does not take the memory with it.
    conversation_id uuid          REFERENCES conversations (id) ON DELETE SET NULL,
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now()
);

CREATE INDEX user_memories_user_idx ON user_memories (user_id, created_at, id);

-- What the model (or, later, the extractor) did to the list: the log behind
-- "undo" on the chat and the "recently learned" list. A person's own edits are
-- not logged — there is nothing to undo there, and a log of deleted text is
-- exactly what deleting a memory is meant to get rid of. `memory_id` is not a
-- foreign key for the same reason: it must outlive a memory that was deleted,
-- since undoing a deletion brings it back under the same id.
CREATE TABLE memory_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    memory_id       uuid          NOT NULL,
    op              memory_op     NOT NULL,
    before          text,
    after           text,
    source          memory_source NOT NULL,
    conversation_id uuid          REFERENCES conversations (id) ON DELETE SET NULL,
    undone_at       timestamptz,
    created_at      timestamptz   NOT NULL DEFAULT now()
);

CREATE INDEX memory_events_user_idx ON memory_events (user_id, created_at DESC);
CREATE INDEX memory_events_memory_idx ON memory_events (user_id, memory_id);

-- A conversation takes the person's instructions and memories once, at its
-- first run, and every request of the conversation is built from that copy: the
-- system prompt is then the same bytes every turn, which the provider's prompt
-- cache and the thinking blocks replayed against it both depend on. A memory
-- saved mid-conversation reaches the next conversation, not this one.
--
-- `temporary` conversations take no memories, are left out of the history, and
-- are deleted a day after they were made.
ALTER TABLE conversations
    ADD COLUMN memory_snapshot jsonb,
    ADD COLUMN temporary       boolean NOT NULL DEFAULT false;

CREATE INDEX conversations_temporary_idx ON conversations (created_at) WHERE temporary;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX conversations_temporary_idx;
ALTER TABLE conversations DROP COLUMN temporary, DROP COLUMN memory_snapshot;
DROP TABLE memory_events;
DROP TABLE user_memories;
DROP TYPE memory_op;
DROP TYPE memory_source;
ALTER TABLE users
    DROP CONSTRAINT users_instructions_style_len,
    DROP CONSTRAINT users_instructions_about_len,
    DROP COLUMN memory_learn,
    DROP COLUMN memory_enabled,
    DROP COLUMN instructions_style,
    DROP COLUMN instructions_about;

-- +goose StatementEnd
