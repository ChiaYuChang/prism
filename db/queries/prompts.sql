-- name: CreatePromptVersion :one
WITH locked AS (
    SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(name), 0))
)
INSERT INTO prompts (name, version, hash, size_bytes)
SELECT sqlc.arg(name), COALESCE(MAX(p.version), 0) + 1, sqlc.arg(hash), sqlc.arg(size_bytes)
FROM prompts AS p, locked
WHERE p.name = sqlc.arg(name)
RETURNING id, name AS key, version, hash, size_bytes, created_at;

-- name: GetPromptVersionByID :one
SELECT
    id,
    name AS key,
    version,
    hash,
    size_bytes,
    created_at
FROM prompts
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: GetLatestPromptVersionByName :one
SELECT id, name AS key, version, hash, size_bytes, created_at
FROM prompts
WHERE name = sqlc.arg(name)
ORDER BY version DESC
LIMIT 1;

-- name: GetPromptVersionByNameAndVersion :one
SELECT id, name AS key, version, hash, size_bytes, created_at
FROM prompts
WHERE name = sqlc.arg(name)
  AND version = sqlc.arg(version)
LIMIT 1;

-- name: ListPromptVersions :many
SELECT
    id,
    name AS key,
    version,
    hash,
    size_bytes,
    created_at
FROM prompts
ORDER BY name ASC, version DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: ListPromptVersionsByKey :many
SELECT
    id,
    name AS key,
    version,
    hash,
    size_bytes,
    created_at
FROM prompts
WHERE name = sqlc.arg(key)
ORDER BY version DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);
