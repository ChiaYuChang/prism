-- name: UpsertPromptVersion :one
WITH prompt_key AS (
    INSERT INTO prompt_keys (key)
    VALUES (sqlc.arg(key))
    ON CONFLICT (key) DO UPDATE SET key = EXCLUDED.key
    RETURNING id, key
), existing AS (
    SELECT
        pv.id,
        pk.id AS key_id,
        pk.key,
        pv.version,
        pv.hash,
        pv.path,
        pv.size_bytes,
        pv.created_at
    FROM prompt_versions pv
    JOIN prompt_key pk ON pk.id = pv.key_id
    WHERE pv.hash = sqlc.arg(hash)
), next_version AS (
    SELECT COALESCE(MAX(pv.version), 0) + 1 AS version
    FROM prompt_versions pv
    JOIN prompt_key pk ON pk.id = pv.key_id
), inserted AS (
    INSERT INTO prompt_versions (key_id, version, hash, path, size_bytes)
    SELECT pk.id, nv.version, sqlc.arg(hash), sqlc.arg(path), sqlc.arg(size_bytes)
    FROM prompt_key pk
    CROSS JOIN next_version nv
    WHERE NOT EXISTS (SELECT 1 FROM existing)
    RETURNING id, key_id, version, hash, path, size_bytes, created_at
)
SELECT
    i.id,
    i.key_id,
    pk.key,
    i.version,
    i.hash,
    i.path,
    i.size_bytes,
    i.created_at
FROM inserted i
JOIN prompt_key pk ON pk.id = i.key_id
UNION ALL
SELECT * FROM existing
LIMIT 1;

-- name: GetPromptVersionByID :one
SELECT
    pv.id,
    pk.id AS key_id,
    pk.key,
    pv.version,
    pv.hash,
    pv.path,
    pv.size_bytes,
    pv.created_at
FROM prompt_versions pv
JOIN prompt_keys pk ON pk.id = pv.key_id
WHERE pv.id = sqlc.arg(id)
LIMIT 1;

-- name: ListPromptVersions :many
SELECT
    pv.id,
    pk.id AS key_id,
    pk.key,
    pv.version,
    pv.hash,
    pv.path,
    pv.size_bytes,
    pv.created_at
FROM prompt_versions pv
JOIN prompt_keys pk ON pk.id = pv.key_id
ORDER BY pk.key ASC, pv.version DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: ListPromptVersionsByKey :many
SELECT
    pv.id,
    pk.id AS key_id,
    pk.key,
    pv.version,
    pv.hash,
    pv.path,
    pv.size_bytes,
    pv.created_at
FROM prompt_versions pv
JOIN prompt_keys pk ON pk.id = pv.key_id
WHERE pk.key = sqlc.arg(key)
ORDER BY pv.version DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);
