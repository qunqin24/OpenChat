-- +goose Up
-- +goose StatementBegin

-- A run can stop to ask the user something (the `ask_user` tool): its calls are
-- on the transcript with no result, and nothing is running — no connection is
-- held open for the answer, which may come in a minute or in a week. The run is
-- finished, as far as the machinery is concerned, and the answer starts a new
-- one. `awaiting_input` is that ending; like the other three it is terminal, so
-- it does not hold the conversation's one active-run slot.
--
-- ADD VALUE is allowed inside goose's transaction because nothing in this
-- migration uses the new value.
ALTER TYPE run_status ADD VALUE 'awaiting_input';

-- The answer to a question is a run of its own, on the same input as the one
-- that asked, and the two are one answer to the reader: one set of rows in the
-- turn, one version, regenerated and edited together. `continues` is the first
-- run of that answer — the run that began it, never a middle one — and is null
-- for a run that begins its own.
ALTER TABLE runs
    ADD COLUMN continues uuid REFERENCES runs (id) ON DELETE CASCADE;

CREATE INDEX runs_continues_idx ON runs (continues) WHERE continues IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- A continuation without the column would read as another version of the turn;
-- it goes, with its rows.
DELETE FROM messages WHERE run_id IN (SELECT id FROM runs WHERE continues IS NOT NULL);
DELETE FROM runs WHERE continues IS NOT NULL;

DROP INDEX runs_continues_idx;
ALTER TABLE runs DROP COLUMN continues;

-- Postgres cannot drop an enum value, so the type is rebuilt. A run that was
-- waiting for an answer is finished as it stands.
UPDATE runs SET status = 'completed' WHERE status = 'awaiting_input';

ALTER TABLE runs DROP CONSTRAINT runs_finished_when_terminal;
DROP INDEX runs_one_active_per_conversation;
DROP INDEX runs_running;
ALTER TABLE runs ALTER COLUMN status DROP DEFAULT;
ALTER TYPE run_status RENAME TO run_status_new;
CREATE TYPE run_status AS ENUM ('running', 'completed', 'failed', 'cancelled');
ALTER TABLE runs
    ALTER COLUMN status TYPE run_status USING status::text::run_status;
ALTER TABLE runs ALTER COLUMN status SET DEFAULT 'running';
DROP TYPE run_status_new;
ALTER TABLE runs
    ADD CONSTRAINT runs_finished_when_terminal CHECK ((status = 'running') = (finished_at IS NULL));
CREATE UNIQUE INDEX runs_one_active_per_conversation ON runs (conversation_id) WHERE status = 'running';
CREATE INDEX runs_running ON runs (created_at) WHERE status = 'running';

-- +goose StatementEnd
