-- Deep research (api/research.go, docs/design/deep-research-plan.md).

-- name: LockResearchStarts :exec
-- Held until the transaction ends, so that counting this month's researches
-- and recording one more is a single step.
SELECT pg_advisory_xact_lock(hashtextextended('research_starts:' || sqlc.arg('key')::text, 29));

-- name: CountResearchStarts :one
-- The researches a person started since the given moment (the month's start).
SELECT count(*)::bigint
FROM research_starts
WHERE user_id = $1
  AND created_at >= sqlc.arg('since');

-- name: RecordResearchStart :exec
INSERT INTO research_starts (user_id, run_id)
VALUES ($1, $2);

-- name: SubagentLaunchedByResearch :one
-- Whether a sub-agent's conversation was launched by a research run: it is
-- then a researcher.
SELECT coalesce(r.research, false)::boolean
FROM conversations c
         LEFT JOIN runs r ON r.id = c.launched_by
WHERE c.id = $1;

-- name: GetSearchCache :one
SELECT results
FROM search_cache
WHERE key = $1
  AND expires_at > now();

-- name: PutSearchCache :exec
INSERT INTO search_cache (key, results, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE
    SET results    = excluded.results,
        expires_at = excluded.expires_at;

-- name: DeleteExpiredSearchCache :execrows
DELETE
FROM search_cache
WHERE expires_at <= now();
