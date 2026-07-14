BEGIN;

CREATE TABLE IF NOT EXISTS tokens (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    type           TEXT NOT NULL CHECK (type IN ('root', 'admin', 'user', 'worker')),
    name           TEXT NOT NULL,
    hash_algorithm TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL,
    last_used_at   TIMESTAMPTZ,
    renewed_at     TIMESTAMPTZ,
    rotated_at     TIMESTAMPTZ,
    revoked_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tokens_active_type_expires_at
    ON tokens (type, expires_at)
    WHERE revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tokens_one_root
    ON tokens (type)
    WHERE type = 'root';

COMMIT;
