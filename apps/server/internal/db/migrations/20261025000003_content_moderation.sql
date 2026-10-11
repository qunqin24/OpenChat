-- +goose Up
-- +goose StatementBegin

-- A request that content moderation turned away (api/moderation.go). Only the
-- blocked ones are kept: a message that passed leaves no trace here. The row is
-- the operator's own record of what was sent through their upstream accounts,
-- which is why it holds an excerpt of the text, and why it is dropped after 90
-- days.
CREATE TABLE moderation_events
(
    id              uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- Whose message it was: an account, or for a guest the network address
    -- (clientIP: an IPv6 address as its /64) their requests came from. A guest
    -- has no user row that outlives the request.
    user_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    guest_ip        text,
    conversation_id uuid REFERENCES conversations (id) ON DELETE SET NULL,
    source          text        NOT NULL,
    mode            text        NOT NULL,
    categories      text[]      NOT NULL DEFAULT '{}',
    -- The first 500 characters of the blocked text.
    excerpt         text        NOT NULL DEFAULT '',
    action          text        NOT NULL DEFAULT 'blocked',
    -- What this event set off, when it was the one that reached the strike
    -- threshold: the account was disabled, or the address banned.
    sanction        text,
    CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest')),
    CONSTRAINT moderation_events_mode CHECK (mode IN ('openai', 'model')),
    CONSTRAINT moderation_events_action CHECK (action IN ('blocked')),
    CONSTRAINT moderation_events_sanction CHECK (sanction IS NULL OR sanction IN ('disabled', 'banned'))
);

-- The log page pages newest first by keyset on (created_at, id).
CREATE INDEX moderation_events_created_idx ON moderation_events (created_at DESC, id DESC);
-- Counting a subject's strikes in a window, and filtering the log by one.
CREATE INDEX moderation_events_user_idx ON moderation_events (user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX moderation_events_ip_idx ON moderation_events (guest_ip, created_at DESC) WHERE guest_ip IS NOT NULL;
CREATE INDEX moderation_events_categories_idx ON moderation_events USING gin (categories);

-- Guest addresses that may not use guest mode. A guest has no account to
-- disable, so the address is what is banned. A NULL expiry is permanent.
CREATE TABLE guest_bans
(
    ip         text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    reason     text        NOT NULL DEFAULT '',
    event_id   uuid REFERENCES moderation_events (id) ON DELETE SET NULL
);

CREATE INDEX guest_bans_expires_idx ON guest_bans (expires_at) WHERE expires_at IS NOT NULL;

-- Moderation requests to a model are metered like any other.
ALTER TABLE usage_logs
    DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM usage_logs WHERE kind = 'moderation';
ALTER TABLE usage_logs
    DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression'));
DROP TABLE guest_bans;
DROP TABLE moderation_events;

-- +goose StatementEnd
