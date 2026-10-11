-- +goose Up
-- +goose StatementBegin

-- 20261025000005 marked every memory whose `source` is not 'user' as already
-- checked by content moderation, on the reasoning that the model wrote them. But
-- before it a person editing a memory the model had written did not change its
-- `source`: such a memory holds the person's words under the model's name, and
-- the backfill vouched for them without anyone having looked. The text of a
-- snapshot is not moderated again once it carries a mark, so those words would
-- reach the model unchecked.
--
-- A mark made by that backfill cannot be told from one made since by the code
-- (a model's write is recorded as checked as it is made, and so is a person's
-- edit that passed): all are the SHA-256 of the current text. So all marks on
-- memories that are not the person's own are cleared, which costs one batched
-- re-check of them when the person's next conversation takes its snapshot (and
-- nothing at all while moderation is off). A memory the model writes from now
-- on is recorded as checked when it is written (memory.By.vouched), as before.
--
-- Memories whose source is 'user' were never backfilled and keep their marks, as
-- do the custom instructions, which the earlier migration did not mark. A
-- `blocked_hash` is a refusal and stays.
UPDATE user_memories
SET checked_hash = NULL
WHERE source <> 'user'
  AND checked_hash IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- The marks cleared above are not restored: they were not trustworthy, and a
-- memory without one is simply checked again when it is next put in a snapshot.
SELECT 1;

-- +goose StatementEnd
