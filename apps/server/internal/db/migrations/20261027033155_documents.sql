-- +goose Up
-- +goose StatementBegin
-- Writing blocks: a text a model writes or revises for the person - an
-- article, an email, a post - kept apart from the transcript so it can be
-- edited in place, by the person in the side panel and by the model with
-- targeted edits (the `document` tool, api/document_tool.go). Every change is
-- a version; the document is its latest.
--
-- A document belongs to the run that made it: when that answer goes (an edit
-- of the question before it, a regeneration), the document goes too, as the
-- answer's rows do. A model's later change belongs to its own run the same
-- way; the person's changes belong to nobody's run and stay.
CREATE TABLE documents (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    run_id          uuid        NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX documents_conversation_idx ON documents (conversation_id, created_at);

CREATE TABLE document_versions (
    document_id uuid        NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    version     int         NOT NULL,
    title       text        NOT NULL,
    content     text        NOT NULL,
    -- `model` or `user`: who wrote this version.
    source      text        NOT NULL,
    run_id      uuid        REFERENCES runs (id) ON DELETE CASCADE,
    -- The hash (memory.Hash) of title and content that content moderation
    -- passed, or that a model wrote; NULL until the person's text is checked,
    -- which happens when a model is about to read it.
    checked     text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, version),
    CONSTRAINT document_versions_source CHECK (source IN ('model', 'user'))
);

-- The person's text of a document, checked as a model is about to read it:
-- something they are sending, so a strike like a message.
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM moderation_events WHERE source = 'document';
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest', 'instructions', 'memory', 'snapshot', 'task', 'task_run'));
DROP TABLE document_versions;
DROP TABLE documents;
-- +goose StatementEnd
