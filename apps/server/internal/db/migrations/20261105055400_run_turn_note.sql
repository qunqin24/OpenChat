-- +goose Up
-- +goose StatementBegin
-- A run's turn note (agent.TurnNote): what its model was told with its input
-- that the transcript does not say - the writing blocks the person edited,
-- the sub-agents, the plugins, that the message came from the phone app. Kept
-- so that every later request tells the same thing at the same place: a
-- history whose earlier messages change from one run to the next invalidates
-- the thinking blocks sent back and the provider's cache. NULL until the run
-- has asked for it; empty when there was nothing to tell.
ALTER TABLE runs ADD COLUMN turn_note text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN turn_note;
-- +goose StatementEnd
