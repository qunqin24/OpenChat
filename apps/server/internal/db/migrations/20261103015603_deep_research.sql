-- +goose Up
-- +goose StatementBegin
-- Deep research (docs/design/deep-research-plan.md). A research run is the one
-- the person started from a confirmed plan: it coordinates researchers and
-- writes the report. The plan run before it is an ordinary run.
ALTER TABLE runs ADD COLUMN research boolean NOT NULL DEFAULT false;

-- One row per research the person started, counted per calendar month against
-- the monthly allowance. Kept when the run is deleted: regenerating an answer
-- does not give the research back.
CREATE TABLE research_starts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    run_id     uuid        REFERENCES runs (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX research_starts_user ON research_starts (user_id, created_at);

-- Results of the keyless search services, shared by every user (the services
-- are called without cookies, so a result is nobody's in particular) and by
-- every server process: the free tiers are rate-limited per server address.
CREATE TABLE search_cache (
    key        text PRIMARY KEY,
    results    jsonb       NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX search_cache_expires ON search_cache (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE search_cache;
DROP TABLE research_starts;
ALTER TABLE runs DROP COLUMN research;
-- +goose StatementEnd
