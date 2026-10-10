-- +goose Up
-- +goose StatementBegin

-- A conversation is a tree of questions, and what the reader sees is one path
-- through it. Every user message (an "input") names the input it follows:
-- `parent_input_id`, null for a conversation's first. Asking a question again in
-- new words (any question, not only the last) adds a sibling under the same
-- parent and makes it the conversation's current leaf; what followed the
-- original stays in the tree, off the path, and comes back when the original is
-- switched to. The siblings of one parent are the versions of a question,
-- `edit_of` still naming the first of them.
--
-- The shown transcript is the path from the root to `current_input_id`, and for
-- each input on it the rows of its one answer that is not superseded
-- (20261011000003). Superseding is now only about an input's own answers
-- (regenerate); a question set aside by an edit is off the path, not hidden by
-- the state of its runs.
ALTER TABLE messages
    ADD COLUMN parent_input_id uuid REFERENCES messages (id) ON DELETE CASCADE,
    -- Inputs only: the last time this one was the end of the branch shown. Where
    -- the reader lands when they switch to a version of an earlier question is
    -- the end of the branch under it that they were last on, and this is that.
    ADD COLUMN shown_at        timestamptz;

CREATE INDEX messages_parent_input_idx ON messages (parent_input_id) WHERE parent_input_id IS NOT NULL;

-- Where the reader is. Always an input with no children (a new message, an edit,
-- or the end of the branch switched to). Set null only with the message itself.
ALTER TABLE conversations
    ADD COLUMN current_input_id uuid REFERENCES messages (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------- backfill
--
-- Until now there was one line of turns, and a turn was its first question plus
-- every edit of it (`edit_of`). Which member of a turn was shown was decided by
-- runs.superseded: an input all of whose runs are superseded was hidden. Only
-- the last turn could change, so each turn has one shown member, and the turns
-- line up by the time their first question was asked.
--
-- The path that was shown is therefore: the shown member of every turn in turn
-- order, each the parent of the next turn's members. The hidden members become
-- siblings of the shown one — they have the same parent, the turn's. A turn
-- with no shown member (it cannot happen, but a hand-edited database might)
-- is represented by its newest member rather than breaking the chain.
CREATE TEMPORARY TABLE branch_backfill ON COMMIT DROP AS
WITH inputs AS (
    SELECT m.id,
           m.conversation_id,
           COALESCE(m.edit_of, m.id) AS root,
           m.created_at,
           NOT (EXISTS (SELECT 1 FROM runs r WHERE r.input_message_id = m.id)
               AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.input_message_id = m.id AND NOT r.superseded)) AS shown
    FROM messages m
    WHERE m.role = 'user'
),
     turns AS (
         SELECT DISTINCT ON (i.conversation_id, i.root) i.conversation_id, i.root, i.id AS shown_id
         FROM inputs i
         ORDER BY i.conversation_id, i.root, i.shown DESC, i.created_at DESC, i.id DESC
     )
SELECT t.conversation_id,
       t.root,
       t.shown_id,
       lag(t.shown_id) OVER w AS parent_id,
       -- 1 for the newest turn.
       row_number() OVER (PARTITION BY t.conversation_id ORDER BY fm.created_at DESC, fm.id DESC) AS newest
FROM turns t
         JOIN messages fm ON fm.id = t.root
WINDOW w AS (PARTITION BY t.conversation_id ORDER BY fm.created_at, fm.id);

UPDATE messages m
SET parent_input_id = b.parent_id
FROM branch_backfill b
WHERE b.parent_id IS NOT NULL
  AND m.role = 'user'
  AND COALESCE(m.edit_of, m.id) = b.root;

UPDATE conversations c
SET current_input_id = b.shown_id
FROM branch_backfill b
WHERE b.conversation_id = c.id
  AND b.newest = 1;

-- A branch that was left, was left at its last row.
UPDATE messages m
SET shown_at = COALESCE((SELECT max(x.created_at)
                         FROM runs r
                                  JOIN messages x ON x.run_id = r.id
                         WHERE r.input_message_id = m.id), m.created_at)
WHERE m.role = 'user';

-- A version that was set aside by an edit had every answer superseded. Switching
-- back to it must show an answer, so the one it last had comes back: its newest
-- (with the runs that carried on after a question, which share its fate).
-- Inputs on the path are untouched — their one shown answer is already right.
UPDATE runs r
SET superseded = false
FROM (SELECT DISTINCT ON (x.input_message_id) x.input_message_id, x.id AS head
      FROM runs x
      WHERE x.continues IS NULL
        AND NOT EXISTS (SELECT 1 FROM runs y WHERE y.input_message_id = x.input_message_id AND NOT y.superseded)
      ORDER BY x.input_message_id, x.created_at DESC, x.id DESC) newest
WHERE r.input_message_id = newest.input_message_id
  AND COALESCE(r.continues, r.id) = newest.head;

-- ---------------------------------------------------------------- reading
--
-- The two ways of reading the tree, once, for every query that needs them.

-- The inputs on the shown path, from the current leaf up to the root.
-- UNION, not UNION ALL, so a parent chain that somehow looped would end.
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

-- Every row the reader sees, and the model is sent: the inputs on the path and
-- the rows of each one's answer that is not superseded. A row written before
-- runs existed (no run_id, never a user message) belongs to the question it
-- came after.
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

-- +goose Down
-- +goose StatementBegin

-- The old shape has one line of turns, and a turn's other versions hidden by
-- the state of their runs. What is on the path stays as it is; its siblings
-- stay as hidden versions (every run superseded); everything else in the tree —
-- what followed a version set aside — has no place in it and goes with its rows.
CREATE TEMPORARY TABLE branch_path ON COMMIT DROP AS
SELECT c.id AS conversation_id, p.id
FROM conversations c
         CROSS JOIN LATERAL conversation_path(c.id) AS p (id)
WHERE c.current_input_id IS NOT NULL;

CREATE TEMPORARY TABLE branch_keep ON COMMIT DROP AS
SELECT path.id, true AS on_path
FROM branch_path path
UNION
SELECT s.id, false
FROM branch_path path
         JOIN messages pm ON pm.id = path.id
         JOIN messages s ON s.conversation_id = path.conversation_id
    AND s.role = 'user'
    AND s.parent_input_id IS NOT DISTINCT FROM pm.parent_input_id;

-- A version is a path member's sibling, never also on the path itself.
DELETE FROM branch_keep k WHERE NOT k.on_path AND EXISTS (SELECT 1 FROM branch_keep p WHERE p.on_path AND p.id = k.id);

DELETE FROM messages
WHERE run_id IN (SELECT r.id
                 FROM runs r
                          JOIN messages i ON i.id = r.input_message_id
                 WHERE i.role = 'user'
                   AND i.id NOT IN (SELECT id FROM branch_keep)
                   AND i.conversation_id IN (SELECT id FROM conversations WHERE current_input_id IS NOT NULL));
DELETE FROM messages
WHERE role = 'user'
  AND id NOT IN (SELECT id FROM branch_keep)
  AND conversation_id IN (SELECT id FROM conversations WHERE current_input_id IS NOT NULL);

UPDATE runs
SET superseded = true
WHERE input_message_id IN (SELECT id FROM branch_keep WHERE NOT on_path);

DROP FUNCTION conversation_transcript(uuid);
DROP FUNCTION conversation_path(uuid);
ALTER TABLE conversations DROP COLUMN current_input_id;
DROP INDEX messages_parent_input_idx;
ALTER TABLE messages DROP COLUMN IF EXISTS shown_at, DROP COLUMN parent_input_id;

-- +goose StatementEnd
