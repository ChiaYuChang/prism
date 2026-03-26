-- name: GetContentByID :one
SELECT *
FROM contents
WHERE id = $1
LIMIT 1;

-- name: GetContentByURL :one
SELECT *
FROM contents
WHERE url = $1
LIMIT 1;

-- name: GetContentByCandidateID :one
SELECT *
FROM contents
WHERE candidate_id = $1
LIMIT 1;

-- name: CreateContent :one
INSERT INTO contents (
    batch_id,
    type,
    source_id,
    candidate_id,
    url,
    title,
    content,
    author,
    trace_id,
    published_at,
    fetched_at,
    metadata
) VALUES (
    sqlc.narg(batch_id),
    sqlc.arg(type),
    sqlc.arg(source_id),
    sqlc.narg(candidate_id),
    sqlc.arg(url),
    sqlc.arg(title),
    sqlc.arg(content),
    sqlc.narg(author),
    sqlc.arg(trace_id),
    sqlc.arg(published_at),
    sqlc.arg(fetched_at),
    sqlc.narg(metadata)
)
RETURNING *;

-- name: UpdateContentMetadata :one
UPDATE contents
SET author = COALESCE(sqlc.narg(author), author),
    published_at = COALESCE(sqlc.narg(published_at), published_at),
    metadata = COALESCE(sqlc.narg(metadata), metadata)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListRecentSeedContents :many
SELECT *
FROM contents
WHERE type = 'PARTY_RELEASE'
  AND deleted_at IS NULL
ORDER BY published_at DESC, created_at DESC
LIMIT $1;

-- name: ListContentsByBatchID :many
SELECT *
FROM contents
WHERE batch_id = $1
  AND deleted_at IS NULL
ORDER BY published_at ASC, created_at ASC;
