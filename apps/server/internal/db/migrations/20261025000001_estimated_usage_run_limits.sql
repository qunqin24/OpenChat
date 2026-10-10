-- +goose Up
-- +goose StatementBegin

-- A request that was stopped, or broke, before the provider's usage frame still
-- cost what it cost. The ledger now prices it from an estimate of the tokens
-- sent and received, and says so: `estimated` marks a row whose token counts
-- (some or all) are the server's own figures and not the provider's. Rows from
-- before are what the provider reported, or nothing, so they stay false.
ALTER TABLE usage_logs
    ADD COLUMN estimated boolean NOT NULL DEFAULT false;

-- How many runs may be going at once is counted per account and, for guests,
-- per address: the address the guest's run was started from, which the router's
-- RealIP middleware reports (IPv6 as its /64). NULL for every account's run.
ALTER TABLE runs
    ADD COLUMN client_ip text;

-- The two counts the limit runs under an advisory lock, before a run is
-- inserted. Both read only the runs that are going.
CREATE INDEX runs_running_user_idx ON runs (user_id) WHERE status = 'running';
CREATE INDEX runs_running_ip_idx ON runs (client_ip) WHERE status = 'running' AND client_ip IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX runs_running_ip_idx;
DROP INDEX runs_running_user_idx;
ALTER TABLE runs
    DROP COLUMN client_ip;
ALTER TABLE usage_logs
    DROP COLUMN estimated;

-- +goose StatementEnd
