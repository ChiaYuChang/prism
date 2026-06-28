BEGIN;

CREATE TABLE IF NOT EXISTS prompt_keys (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    key        TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS prompt_versions (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    key_id     UUID NOT NULL REFERENCES prompt_keys(id) ON DELETE RESTRICT,
    version    INT NOT NULL,
    hash       VARCHAR(71) NOT NULL,
    path       TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_id, version),
    UNIQUE (key_id, hash)
);

CREATE INDEX IF NOT EXISTS idx_prompt_versions_key_created_at ON prompt_versions(key_id, created_at DESC);

COMMIT;
