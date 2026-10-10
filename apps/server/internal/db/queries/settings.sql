-- name: GetSetting :one
SELECT value
FROM app_settings
WHERE key = $1;

-- name: SetSetting :exec
-- Upsert, so a setting added after this database was created still writes.
INSERT INTO app_settings (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key) DO UPDATE SET value      = excluded.value,
                                updated_at = now();

-- name: InsertSettingIfMissing :exec
-- Writes a setting only when there is none yet, so two processes that both find
-- it missing cannot overwrite each other's value. Read it back afterwards.
INSERT INTO app_settings (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key) DO NOTHING;
