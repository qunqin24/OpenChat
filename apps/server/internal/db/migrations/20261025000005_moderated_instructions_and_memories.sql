-- +goose Up
-- +goose StatementBegin

-- Custom instructions and memories a person writes reach the model through the
-- conversation's snapshot, so they are content moderation's business like a
-- message is (api/moderation_text.go): checked when they are saved, and again
-- when a conversation takes its snapshot if they were never checked - saved
-- before moderation was turned on, or while it was off.
--
-- What was checked is remembered as the SHA-256 (hex, of the UTF-8 text) of the
-- text that passed: the text is "checked" while its hash equals the stored one,
-- and an edit makes it unchecked again without anything having to clear a flag.
-- `blocked_hash` is the same for text a snapshot-time check turned away, so that
-- the next conversation does not ask again about words already refused.
--
-- NULL is "never checked". The memories the model wrote are vouched for - they
-- came out of conversations that were moderated as they were sent, and model
-- output is not moderated - so they start out checked; a person's are not.
ALTER TABLE users
    ADD COLUMN instructions_about_checked_hash text,
    ADD COLUMN instructions_about_blocked_hash text,
    ADD COLUMN instructions_style_checked_hash text,
    ADD COLUMN instructions_style_blocked_hash text;

ALTER TABLE user_memories
    ADD COLUMN checked_hash text,
    ADD COLUMN blocked_hash text;

UPDATE user_memories
SET checked_hash = encode(sha256(convert_to(content, 'UTF8')), 'hex')
WHERE source <> 'user';

-- Where a refused text came from: the custom instructions or a memory as it was
-- saved, or either as a conversation's snapshot was taken (`snapshot`, which is
-- logged but is not a strike: nothing was being sent).
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source
        CHECK (source IN ('send', 'edit', 'answer', 'guest', 'instructions', 'memory', 'snapshot'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM moderation_events WHERE source IN ('instructions', 'memory', 'snapshot');
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest'));

ALTER TABLE user_memories DROP COLUMN blocked_hash, DROP COLUMN checked_hash;
ALTER TABLE users
    DROP COLUMN instructions_style_blocked_hash,
    DROP COLUMN instructions_style_checked_hash,
    DROP COLUMN instructions_about_blocked_hash,
    DROP COLUMN instructions_about_checked_hash;

-- +goose StatementEnd
