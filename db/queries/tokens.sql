-- name: CreateToken :one
INSERT INTO tokens (
    id,
    type,
    name,
    hash_algorithm,
    token_hash,
    expires_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(type),
    sqlc.arg(name),
    sqlc.arg(hash_algorithm),
    sqlc.arg(token_hash),
    sqlc.arg(expires_at)
)
RETURNING *;

-- name: GetRootToken :one
SELECT *
FROM tokens
WHERE type = 'root'
LIMIT 1;

-- name: GetTokenByID :one
SELECT *
FROM tokens
WHERE id = sqlc.arg(id)
LIMIT 1;

-- name: ListTokens :many
SELECT *
FROM tokens
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: RenewToken :one
UPDATE tokens
SET expires_at = sqlc.arg(expires_at),
    renewed_at = NOW()
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
RETURNING *;

-- name: RotateToken :one
UPDATE tokens
SET hash_algorithm = sqlc.arg(hash_algorithm),
    token_hash = sqlc.arg(token_hash),
    expires_at = sqlc.arg(expires_at),
    rotated_at = NOW()
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeToken :one
UPDATE tokens
SET revoked_at = NOW()
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAllTokens :one
WITH revoked AS (
    UPDATE tokens
    SET revoked_at = NOW()
    WHERE revoked_at IS NULL
      AND type <> 'root'
    RETURNING id
)
SELECT COUNT(*)::BIGINT FROM revoked;

-- name: CountActiveAdminTokensExcluding :one
SELECT COUNT(*)::BIGINT
FROM tokens
WHERE type = 'admin'
  AND revoked_at IS NULL
  AND expires_at > NOW()
  AND id <> sqlc.arg(id);
