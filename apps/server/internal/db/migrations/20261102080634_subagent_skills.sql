-- +goose Up
-- +goose StatementBegin
-- The skills a sub-agent may open (docs/design/skills-plan.md): named by the
-- main agent when it launches one, from those its own conversation lists. A
-- sub-agent opens nothing else, and reads them at the versions the main
-- conversation listed. NULL, or for a conversation that is not a sub-agent,
-- none.
ALTER TABLE conversations ADD COLUMN skills text[];
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE conversations DROP COLUMN skills;
-- +goose StatementEnd
