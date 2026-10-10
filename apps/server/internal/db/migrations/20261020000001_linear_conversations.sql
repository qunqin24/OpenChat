-- +goose Up
-- +goose StatementBegin

-- A conversation is a line again. Branches (20261015000001) and the versions
-- before them (answers: 20261011000003, questions: 20261011000004) are gone:
-- what is replaced is deleted, not set aside. Editing a question deletes it and
-- everything after it; regenerating an answer deletes the old one. Nothing is
-- off the path any more, so nothing needs to say which path is shown, and the
-- transcript is every row of the conversation in the order it was written.
--
-- This migration brings existing data to that shape. What stays in every
-- conversation is exactly what its reader sees today: the rows of
-- conversation_transcript, which this migration then drops with the columns it
-- read. Everything else - the other versions of a question and what followed
-- them, and every answer that was regenerated - is deleted.
CREATE TEMPORARY TABLE linear_keep ON COMMIT DROP AS
SELECT DISTINCT t.id
FROM conversations c
         CROSS JOIN LATERAL conversation_transcript(c.id) AS t (id);
ALTER TABLE linear_keep ADD PRIMARY KEY (id);

-- Files. A question that was edited keeps its files on the first version of
-- the question (edit_of), and every later version shows them by reference. If
-- the first version is not the one shown, the files move to the one that is,
-- or they would go with the version they were left on.
UPDATE attachments a
SET message_id = shown.id
FROM messages shown
         JOIN messages root ON root.id = shown.edit_of
WHERE a.message_id = root.id
  AND shown.role = 'user'
  AND shown.id IN (SELECT id FROM linear_keep)
  AND root.id NOT IN (SELECT id FROM linear_keep);

-- The files of the rows that are about to go. A migration cannot delete files,
-- and the rows being deleted are the only record of where they are, so they are
-- not deleted with the rows: they are let go of. A file no message claims is an
-- upload that was never sent, which the cleanup loop already removes together
-- with its stored object (local disk or bucket) once it is older than the
-- unsent-upload window; the date is set back to the epoch so that the first
-- pass after this migration finds them, whatever the window is.
UPDATE attachments
SET message_id = NULL,
    created_at = 'epoch'
WHERE message_id IN (SELECT m.id FROM messages m WHERE m.id NOT IN (SELECT id FROM linear_keep));

-- The tree. These go before the rows do, so that deleting a question does not
-- cascade (parent_input_id, edit_of) to the version that is kept.
DROP FUNCTION conversation_transcript(uuid);
DROP FUNCTION conversation_path(uuid);
ALTER TABLE conversations DROP COLUMN current_input_id;
DROP INDEX messages_parent_input_idx;
DROP INDEX messages_edit_of_idx;
ALTER TABLE messages DROP COLUMN parent_input_id, DROP COLUMN shown_at, DROP COLUMN edit_of;

-- A deleted question takes its runs with it (runs.input_message_id); the rows
-- of a run only lose their run_id when it goes, which is why they are deleted
-- here by what is not kept rather than by what cascades.
DELETE FROM messages WHERE id NOT IN (SELECT id FROM linear_keep);

-- A regenerated answer's runs stay with the question that is kept, superseded;
-- their rows are gone above.
DELETE FROM runs WHERE superseded;
ALTER TABLE runs DROP COLUMN superseded;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- The tree again, as a line: each question follows the one before it, the
-- reader is on the last. What was deleted does not come back.
ALTER TABLE messages
    ADD COLUMN edit_of         uuid REFERENCES messages (id) ON DELETE CASCADE,
    ADD COLUMN parent_input_id uuid REFERENCES messages (id) ON DELETE CASCADE,
    ADD COLUMN shown_at        timestamptz;
CREATE INDEX messages_edit_of_idx ON messages (edit_of) WHERE edit_of IS NOT NULL;
CREATE INDEX messages_parent_input_idx ON messages (parent_input_id) WHERE parent_input_id IS NOT NULL;
ALTER TABLE conversations
    ADD COLUMN current_input_id uuid REFERENCES messages (id) ON DELETE SET NULL;
ALTER TABLE runs
    ADD COLUMN superseded boolean NOT NULL DEFAULT false;

UPDATE messages m
SET parent_input_id = prev.id
FROM (SELECT u.id, lag(u.id) OVER (PARTITION BY u.conversation_id ORDER BY u.created_at, u.id) AS parent
      FROM messages u
      WHERE u.role = 'user') ordered
         JOIN messages prev ON prev.id = ordered.parent
WHERE m.id = ordered.id;

UPDATE conversations c
SET current_input_id = last.id
FROM (SELECT DISTINCT ON (u.conversation_id) u.conversation_id, u.id
      FROM messages u
      WHERE u.role = 'user'
      ORDER BY u.conversation_id, u.created_at DESC, u.id DESC) last
WHERE last.conversation_id = c.id;

UPDATE messages m
SET shown_at = m.created_at
WHERE m.role = 'user';

CREATE FUNCTION conversation_path(conv uuid) RETURNS SETOF uuid
    LANGUAGE sql
    STABLE
AS
$$
WITH RECURSIVE path (id) AS (SELECT c.current_input_id
                             FROM conversations c
                             WHERE c.id = conv
                               AND c.current_input_id IS NOT NULL
                             UNION
                             SELECT m.parent_input_id
                             FROM messages m
                                      JOIN path ON m.id = path.id
                             WHERE m.parent_input_id IS NOT NULL)
SELECT id
FROM path
$$;

CREATE FUNCTION conversation_transcript(conv uuid) RETURNS SETOF uuid
    LANGUAGE sql
    STABLE
AS
$$
WITH path AS MATERIALIZED (SELECT conversation_path(conv) AS id)
SELECT id
FROM path
UNION ALL
SELECT m.id
FROM runs r
         JOIN messages m ON m.run_id = r.id
WHERE r.input_message_id IN (SELECT id FROM path)
  AND NOT r.superseded
UNION ALL
SELECT m.id
FROM messages m
WHERE m.conversation_id = conv
  AND m.run_id IS NULL
  AND m.role <> 'user'
  AND (SELECT u.id
       FROM messages u
       WHERE u.conversation_id = conv
         AND u.role = 'user'
         AND (u.created_at, u.id) < (m.created_at, m.id)
       ORDER BY u.created_at DESC, u.id DESC
       LIMIT 1) IN (SELECT id FROM path)
$$;

-- +goose StatementEnd
