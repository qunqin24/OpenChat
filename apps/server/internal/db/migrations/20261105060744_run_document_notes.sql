-- +goose Up
-- +goose StatementBegin
-- The versions of the person's writing blocks a run's turn note told the
-- model about (api.DocumentNote). Turn notes are kept and sent again with
-- every later request (runs.turn_note), so a later note tells only what came
-- since the last version told, rather than the same changes again. Gone with
-- the run, and with it its note.
CREATE TABLE run_document_notes (
    run_id      uuid    NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    document_id uuid    NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    version     integer NOT NULL,
    PRIMARY KEY (run_id, document_id)
);
CREATE INDEX run_document_notes_document_idx ON run_document_notes (document_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE run_document_notes;
-- +goose StatementEnd
