-- name: LockSkillOwner :exec
-- Held while a skill is created or renamed, so two requests at once cannot both
-- find room under the limit or both take the same name. The site's skills share
-- one lock (the nil uuid).
SELECT pg_advisory_xact_lock(hashtextextended('skills:' || sqlc.arg('owner_id')::uuid::text, 0));

-- name: CreateSkill :one
INSERT INTO skills (owner_id, name, enabled)
VALUES (sqlc.narg('owner_id'), sqlc.arg('name'), sqlc.arg('enabled'))
RETURNING *;

-- name: CreateSkillVersion :one
INSERT INTO skill_versions (skill_id, description, content_hash, checked, created_by)
VALUES (sqlc.arg('skill_id'), sqlc.arg('description'), sqlc.arg('content_hash'), sqlc.narg('checked'), sqlc.narg('created_by'))
RETURNING *;

-- name: InsertSkillFiles :exec
INSERT INTO skill_files (version_id, path, content)
SELECT sqlc.arg('version_id'), unnest(sqlc.arg('paths')::text[]), unnest(sqlc.arg('contents')::text[]);

-- name: SetSkillVersion :exec
-- A save: the new version becomes the skill's, under the name its SKILL.md
-- gives now.
UPDATE skills
SET current_version_id = sqlc.arg('version_id'),
    name               = sqlc.arg('name'),
    updated_at         = now()
WHERE id = sqlc.arg('id');

-- name: PruneSkillVersions :exec
-- Keeps the newest versions of a skill, the current one among them; a
-- conversation that started with an older one finds it gone and is told so.
DELETE FROM skill_versions v
WHERE v.skill_id = sqlc.arg('skill_id')
  AND v.id <> (SELECT current_version_id FROM skills WHERE id = sqlc.arg('skill_id'))
  AND v.id NOT IN (
      SELECT k.id FROM skill_versions k
      WHERE k.skill_id = sqlc.arg('skill_id')
      ORDER BY k.created_at DESC
      LIMIT sqlc.arg('keep')::int);

-- name: SkillNameTaken :one
-- Whether another skill of the same owner (or of the site) has this name.
SELECT EXISTS (
    SELECT 1 FROM skills
    WHERE name = sqlc.arg('name')
      AND owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid
      AND id IS DISTINCT FROM sqlc.narg('except')::uuid
) AS taken;

-- name: CountOwnedSkills :one
SELECT count(*) FROM skills WHERE owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid;

-- name: ListOwnedSkills :many
-- A person's skills (owner_id), or the site's (NULL), with their current
-- version, newest first.
SELECT s.id, s.owner_id, s.name, s.enabled, s.created_at, s.updated_at,
       v.id AS version_id, v.description, v.content_hash, v.checked, v.blocked
FROM skills s
JOIN skill_versions v ON v.id = s.current_version_id
WHERE s.owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid
ORDER BY s.updated_at DESC, s.id;

-- name: GetOwnedSkill :one
-- One skill of a person (owner_id) or of the site (NULL): anyone else's is not
-- found.
SELECT s.id, s.owner_id, s.name, s.enabled, s.created_at, s.updated_at,
       v.id AS version_id, v.description, v.content_hash, v.checked, v.blocked
FROM skills s
JOIN skill_versions v ON v.id = s.current_version_id
WHERE s.id = sqlc.arg('id')
  AND s.owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid;

-- name: ListSkillFiles :many
SELECT path, content FROM skill_files WHERE version_id = $1 ORDER BY path COLLATE "C";

-- name: SetSkillEnabled :one
UPDATE skills
SET enabled = sqlc.arg('enabled'), updated_at = now()
WHERE id = sqlc.arg('id')
  AND owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid
RETURNING id;

-- name: DeleteOwnedSkill :one
DELETE FROM skills
WHERE id = sqlc.arg('id')
  AND owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid
RETURNING id;

-- name: ListSnapshotSkills :many
-- What a new conversation of this person may use: the site's skills that are
-- on, then their own that are on, each at its current version.
SELECT s.id, s.owner_id, s.name, v.id AS version_id, v.description, v.content_hash, v.checked, v.blocked
FROM skills s
JOIN skill_versions v ON v.id = s.current_version_id
WHERE s.enabled
  AND (s.owner_id IS NULL OR s.owner_id = sqlc.arg('user_id'))
ORDER BY s.owner_id NULLS FIRST, s.updated_at DESC, s.id;

-- name: GetSkillVersionFor :one
-- A version a conversation of this person may read: of a site skill, or of
-- their own. A skill switched off since stays readable to the conversations
-- that started with it, as the rest of their snapshot does.
SELECT v.id, v.description, s.name
FROM skill_versions v
JOIN skills s ON s.id = v.skill_id
WHERE v.id = sqlc.arg('version_id')
  AND (s.owner_id IS NULL OR s.owner_id = sqlc.arg('user_id'));

-- name: MarkSkillVersionModerated :exec
-- What a snapshot-time check found, recorded only while the version still has
-- the hash that was checked.
UPDATE skill_versions
SET checked = CASE WHEN sqlc.arg('verdict')::text = 'ok' THEN sqlc.arg('hash')::text ELSE checked END,
    blocked = CASE WHEN sqlc.arg('verdict')::text = 'blocked' THEN sqlc.arg('hash')::text ELSE blocked END
WHERE id = sqlc.arg('id')
  AND content_hash = sqlc.arg('hash')::text;

-- name: ListSkillVersions :many
-- A skill's kept versions, newest first, if the skill is this owner's.
SELECT v.id, v.description, v.created_at, (v.id = s.current_version_id)::boolean AS current
FROM skill_versions v
JOIN skills s ON s.id = v.skill_id
WHERE s.id = sqlc.arg('skill_id')
  AND s.owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid
ORDER BY v.created_at DESC, v.id;

-- name: GetOwnedSkillVersion :one
-- One version of a skill of this owner.
SELECT v.id, v.description, v.created_at, s.name
FROM skill_versions v
JOIN skills s ON s.id = v.skill_id
WHERE v.id = sqlc.arg('version_id')
  AND s.id = sqlc.arg('skill_id')
  AND s.owner_id IS NOT DISTINCT FROM sqlc.narg('owner_id')::uuid;
