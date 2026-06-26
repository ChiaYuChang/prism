-- name: GetSourceByAbbr :one
SELECT *
FROM sources
WHERE abbr = $1
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

-- name: GetModelByNameAndType :one
SELECT *
FROM models
WHERE name = $1
  AND type = $2
  AND deleted_at IS NULL
LIMIT 1;

-- name: GetPromptByID :one
SELECT *
FROM prompts
WHERE id = $1
LIMIT 1;

-- name: GetPromptByHash :one
SELECT *
FROM prompts
WHERE hash = $1
LIMIT 1;

-- name: UpsertPrompt :one
INSERT INTO prompts (
    hash,
    path
) VALUES (
    sqlc.arg(hash),
    sqlc.arg(path)
)
ON CONFLICT (hash) DO UPDATE
SET path = EXCLUDED.path
RETURNING *;
