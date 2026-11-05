-- +goose Up
-- +goose StatementBegin
-- Image generation (docs/design/image-generation-plan.md).

-- A picture the image tool made is the person's file, like an upload: an
-- attachment row on the tool result's message, in storage, on their quota,
-- in their library, and gone by the same paths that collect storage keys.
-- `origin` tells it from an upload (NULL); `run_id` is the run that made it,
-- which is what the claim on the result's message checks. `sha256` is the
-- receipt the server took when it wrote the bytes, compared when the picture
-- is read again to be edited.
ALTER TABLE attachments ADD COLUMN origin text CHECK (origin IN ('generated'));
ALTER TABLE attachments ADD COLUMN run_id uuid REFERENCES runs (id) ON DELETE SET NULL;
ALTER TABLE attachments ADD COLUMN sha256 text;
ALTER TABLE attachments ADD COLUMN width integer;
ALTER TABLE attachments ADD COLUMN height integer;
CREATE INDEX attachments_run ON attachments (run_id) WHERE run_id IS NOT NULL;

-- Each image request against the day's allowance, taken under a per-person
-- lock before the request goes out. A failed request gives its pictures back
-- (`failed` is not counted); a deleted run does not (`run_id` SET NULL).
CREATE TABLE image_requests (
    id         uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    run_id     uuid        REFERENCES runs (id) ON DELETE SET NULL,
    count      integer     NOT NULL CHECK (count > 0),
    status     text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX image_requests_user ON image_requests (user_id, created_at);

-- One person's daily allowance of pictures, over the site's default
-- (app_settings.image_daily): NULL is the default, 0 is no limit.
ALTER TABLE users ADD COLUMN image_daily integer CHECK (image_daily >= 0);

-- USD per picture, set by an administrator for an image model the catalogue
-- has no price for, or to bill pictures by the piece. Wins over token prices.
ALTER TABLE model_overrides ADD COLUMN image_price numeric CHECK (image_price >= 0);

-- An image request in the ledger: how many pictures it stored and the price
-- per picture it was billed at, when it was billed by the piece.
ALTER TABLE usage_logs ADD COLUMN images integer NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN image_price numeric;
ALTER TABLE usage_logs DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation', 'subagent', 'image'));

-- The prompt the model wrote for a picture is checked before it goes out;
-- refused, it is logged under its own source and is not a strike.
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document', 'skill', 'image_prompt'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM moderation_events WHERE source = 'image_prompt';
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document', 'skill'));
DELETE FROM usage_logs WHERE kind = 'image';
ALTER TABLE usage_logs DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation', 'subagent'));
ALTER TABLE usage_logs DROP COLUMN image_price;
ALTER TABLE usage_logs DROP COLUMN images;
ALTER TABLE model_overrides DROP COLUMN image_price;
ALTER TABLE users DROP COLUMN image_daily;
DROP TABLE image_requests;
DROP INDEX attachments_run;
ALTER TABLE attachments DROP COLUMN height;
ALTER TABLE attachments DROP COLUMN width;
ALTER TABLE attachments DROP COLUMN sha256;
ALTER TABLE attachments DROP COLUMN run_id;
ALTER TABLE attachments DROP COLUMN origin;
-- +goose StatementEnd
