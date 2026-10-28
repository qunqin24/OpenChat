-- +goose Up
-- +goose StatementBegin

-- Guest mode is gone: only accounts use the chat. What it left in the
-- database goes with it.

-- A guest's user row lived for one run; any still here are leftovers of a
-- process that died mid-run. Deleting them takes their conversations, runs,
-- messages and attachment rows by cascade; their files under uploads/ then
-- have no row and the orphan sweep removes them. Their requests stay in the
-- usage ledger (the foreign key sets the owner to NULL): spend is counted from
-- it.
DELETE FROM users WHERE guest;
DROP INDEX users_guest_idx;
ALTER TABLE users DROP COLUMN guest;

DROP TABLE guest_requests;

-- How many runs one address had going, for the guests' concurrency limit.
DROP INDEX runs_running_ip_idx;
ALTER TABLE runs DROP COLUMN client_ip;

-- A guest's blocked messages were logged against their address, with no
-- account to show or sanction; without guest mode nothing can act on them.
DROP TABLE guest_bans;
DELETE FROM moderation_events WHERE guest_ip IS NOT NULL OR source = 'guest' OR sanction = 'banned';
ALTER TABLE moderation_events DROP COLUMN guest_ip;
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document'));
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_sanction;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_sanction CHECK (sanction IS NULL OR sanction IN ('disabled'));

-- The switch, the limits, the ban length and the key guest receipts were
-- signed with.
DELETE FROM app_settings
WHERE key IN ('guest_mode', 'guest_hourly_limit', 'guest_daily_limit', 'guest_max_concurrent_runs',
              'guest_ban_hours', 'guest_receipt_key');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- The structure comes back empty: the guests' rows, bans and settings are not
-- restored, so guest mode is off with its defaults.
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_sanction;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_sanction CHECK (sanction IS NULL OR sanction IN ('disabled', 'banned'));
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document'));
ALTER TABLE moderation_events ADD COLUMN guest_ip text;
CREATE INDEX moderation_events_ip_idx ON moderation_events (guest_ip, created_at DESC) WHERE guest_ip IS NOT NULL;

CREATE TABLE guest_bans
(
    ip         text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    reason     text        NOT NULL DEFAULT '',
    event_id   uuid REFERENCES moderation_events (id) ON DELETE SET NULL
);
CREATE INDEX guest_bans_expires_idx ON guest_bans (expires_at) WHERE expires_at IS NOT NULL;

ALTER TABLE runs ADD COLUMN client_ip text;
CREATE INDEX runs_running_ip_idx ON runs (client_ip) WHERE status = 'running' AND client_ip IS NOT NULL;

CREATE TABLE guest_requests
(
    ip         text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX guest_requests_ip_idx ON guest_requests (ip, created_at DESC);
CREATE INDEX guest_requests_created_idx ON guest_requests (created_at DESC);

ALTER TABLE users ADD COLUMN guest boolean NOT NULL DEFAULT false;
CREATE INDEX users_guest_idx ON users (created_at) WHERE guest;

-- +goose StatementEnd
