-- +goose Up
-- +goose StatementBegin

-- A model kept back from everyone but administrators: hidden from the picker
-- and refused when asked for by anyone else, while administrators and the
-- server's own tasks (titles, say) keep using it. Not a catalogue figure, so
-- not nullable like the columns beside it: there is nothing to follow.
ALTER TABLE model_overrides
    ADD COLUMN admin_only boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE model_overrides
    DROP COLUMN IF EXISTS admin_only;
-- +goose StatementEnd
