-- name: GetSourceByAbbr :one
SELECT *
FROM sources
WHERE abbr = $1
  AND deleted_at IS NULL
LIMIT 1;

-- name: ListSourcesByType :many
SELECT *
FROM sources
WHERE type = $1
  AND deleted_at IS NULL
ORDER BY abbr ASC;

-- name: ListSources :many
SELECT *
FROM sources
ORDER BY type ASC, abbr ASC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: CreateSource :one
INSERT INTO sources (abbr, name, type, base_url)
VALUES (sqlc.arg(abbr), sqlc.arg(name), sqlc.arg(type), sqlc.arg(base_url))
RETURNING *;

-- name: UpdateSource :one
UPDATE sources
SET name = sqlc.arg(name),
    type = sqlc.arg(type),
    base_url = sqlc.arg(base_url),
    deleted_at = NULL
WHERE abbr = sqlc.arg(abbr)
RETURNING *;

-- name: DeleteSource :one
UPDATE sources
SET deleted_at = COALESCE(deleted_at, NOW())
WHERE abbr = sqlc.arg(abbr)
RETURNING *;

-- name: RestoreSource :one
UPDATE sources
SET deleted_at = NULL
WHERE abbr = sqlc.arg(abbr)
RETURNING *;

-- name: GetModelByID :one
SELECT *
FROM models
WHERE id = $1
LIMIT 1;

-- name: ListModels :many
SELECT *
FROM models
ORDER BY type ASC, provider ASC, name ASC, id ASC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: CreateModel :one
INSERT INTO models (
    name,
    provider,
    type,
    publish_date,
    url,
    tag
) VALUES (
    sqlc.arg(name),
    sqlc.arg(provider),
    sqlc.arg(type),
    sqlc.narg(publish_date),
    sqlc.narg(url),
    sqlc.narg(tag)
)
ON CONFLICT (name, provider, type) DO UPDATE
SET deleted_at = NULL,
    publish_date = EXCLUDED.publish_date,
    url = EXCLUDED.url,
    tag = EXCLUDED.tag
RETURNING *;

-- name: GetModelByNameAndType :one
SELECT *
FROM models
WHERE name = $1
  AND type = $2
  AND deleted_at IS NULL
LIMIT 1;
