-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE conversations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT        NOT NULL DEFAULT 'New chat',
    model      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

-- Keyset pagination: newest first, skipping soft-deleted rows.
CREATE INDEX conversations_user_updated_idx
    ON conversations (user_id, updated_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TYPE message_role AS ENUM ('system', 'user', 'assistant', 'tool');
CREATE TYPE message_status AS ENUM ('pending', 'streaming', 'complete', 'failed');

CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID           NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            message_role   NOT NULL,
    content         TEXT           NOT NULL DEFAULT '',
    status          message_status NOT NULL DEFAULT 'complete',
    metadata        JSONB          NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now()
);

CREATE INDEX messages_conversation_created_idx
    ON messages (conversation_id, created_at, id);

CREATE TABLE attachments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id   UUID REFERENCES messages (id) ON DELETE CASCADE,
    storage_key  TEXT        NOT NULL,
    filename     TEXT        NOT NULL,
    content_type TEXT        NOT NULL,
    size_bytes   BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX attachments_message_idx ON attachments (message_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS messages;
DROP TYPE IF EXISTS message_status;
DROP TYPE IF EXISTS message_role;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
