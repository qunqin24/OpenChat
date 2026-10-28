-- The pictures answers describe, as a search service found them (api/images.go).

-- name: GetImageSearch :one
-- What a description found before: the picture, or no picture (a search that
-- found nothing); no row for words never searched.
SELECT *
FROM image_searches
WHERE query = $1;

-- name: GetPicture :one
SELECT *
FROM pictures
WHERE id = $1;

-- name: GetPictureAt :one
-- A picture already kept, found again by other words.
SELECT *
FROM pictures
WHERE provider = $1
  AND provider_id = $2;

-- name: InsertPicture :one
-- A picture newly kept. Two searches that found it at once: the first kept
-- wins, and the second's stored copy is the caller's to remove.
INSERT INTO pictures (provider, provider_id, storage_key, content_type, width, height, title, creator,
                      creator_url, license, license_url, landing_url, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (provider, provider_id) DO UPDATE SET provider = excluded.provider
RETURNING *;

-- name: SaveImageSearch :exec
-- A search's outcome; a later search of the same words replaces it.
INSERT INTO image_searches (query, picture_id, searched_at)
VALUES ($1, $2, now())
ON CONFLICT (query) DO UPDATE
    SET picture_id  = excluded.picture_id,
        searched_at = now();
