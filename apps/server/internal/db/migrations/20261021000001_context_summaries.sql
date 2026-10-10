-- +goose Up
-- +goose StatementBegin

-- A conversation's compressed context. The rows of a conversation are never
-- changed by compression: the transcript the person reads stays exactly as it
-- was written. What changes is what the model is sent - the summary in place of
-- the rows it covers.
--
-- A conversation has at most one summary; compressing again replaces it with one
-- that covers what the old one did and what came after. `through_message_id` is
-- the last row covered, and `kept_message_id` the user message that lies in
-- that range but is sent as it was written (the question being answered), or
-- NULL when none is.
--
-- Both are cascading references, and that is the whole consistency story with
-- editing: replacing a question deletes it and every row after it, so a summary
-- whose last row (or kept question) went with them goes too, and one that ends
-- before the edit stays. Regenerating deletes the answer after the last
-- question, which takes with it a summary that covered that answer.
CREATE TABLE context_summaries
(
    conversation_id    uuid PRIMARY KEY REFERENCES conversations (id) ON DELETE CASCADE,
    through_message_id uuid        NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    kept_message_id    uuid REFERENCES messages (id) ON DELETE CASCADE,
    content            text        NOT NULL,
    -- The model that wrote it, and what writing it took (all its requests).
    model_id           text        NOT NULL,
    input_tokens       integer     NOT NULL DEFAULT 0,
    output_tokens      integer     NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now()
);

-- A cascade from messages finds its rows by these.
CREATE INDEX context_summaries_through_idx ON context_summaries (through_message_id);
CREATE INDEX context_summaries_kept_idx ON context_summaries (kept_message_id) WHERE kept_message_id IS NOT NULL;

ALTER TABLE usage_logs
    DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM usage_logs WHERE kind = 'compression';
ALTER TABLE usage_logs
    DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title'));
DROP TABLE context_summaries;

-- +goose StatementEnd
