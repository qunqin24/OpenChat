-- +goose Up
-- +goose StatementBegin
-- Why a disabled account was disabled, which the person is shown instead of
-- being signed out: an administrator ('admin', with what they wrote, if
-- anything) or content moderation's strikes ('moderation'). Both NULL while
-- the account is enabled.
ALTER TABLE users
    ADD COLUMN disabled_reason text,
    ADD COLUMN disabled_note   text,
    ADD CONSTRAINT users_disabled_reason CHECK (disabled_reason IN ('admin', 'moderation')),
    ADD CONSTRAINT users_disabled_note CHECK (char_length(disabled_note) <= 500);

-- Accounts already disabled: by moderation where its sanction is on record
-- at the moment the account was cut off, by an administrator otherwise.
UPDATE users u
SET disabled_reason = CASE
                          WHEN EXISTS (SELECT 1
                                       FROM moderation_events e
                                       WHERE e.user_id = u.id
                                         AND e.sanction = 'disabled'
                                         AND e.created_at BETWEEN u.disabled_at - interval '1 minute'
                                                              AND u.disabled_at + interval '1 minute')
                              THEN 'moderation'
                          ELSE 'admin'
                          END
WHERE u.disabled_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_disabled_note,
    DROP CONSTRAINT IF EXISTS users_disabled_reason,
    DROP COLUMN IF EXISTS disabled_note,
    DROP COLUMN IF EXISTS disabled_reason;
-- +goose StatementEnd
