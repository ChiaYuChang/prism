-- Upgrade databases created before prompts became versioned assets.
ALTER TABLE prompts ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE prompts ADD COLUMN IF NOT EXISTS version INT;
ALTER TABLE prompts ADD COLUMN IF NOT EXISTS size_bytes BIGINT;
ALTER TABLE prompts ALTER COLUMN hash TYPE VARCHAR(71);
ALTER TABLE prompts DROP CONSTRAINT IF EXISTS prompts_hash_key;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'prompts'
          AND column_name = 'path'
    ) THEN
        UPDATE prompts
        SET name = COALESCE(NULLIF(path, ''), 'legacy/' || id::text),
            version = COALESCE(version, 1),
            size_bytes = COALESCE(size_bytes, 0)
        WHERE name IS NULL OR version IS NULL OR size_bytes IS NULL;

        ALTER TABLE prompts DROP COLUMN path;
    END IF;
END
$$;

UPDATE prompts
SET name = COALESCE(name, 'legacy/' || id::text),
    version = COALESCE(version, 1),
    size_bytes = COALESCE(size_bytes, 0);

ALTER TABLE prompts ALTER COLUMN name SET NOT NULL;
ALTER TABLE prompts ALTER COLUMN version SET NOT NULL;
ALTER TABLE prompts ALTER COLUMN size_bytes SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS prompts_name_version_key
    ON prompts (name, version);

CREATE INDEX IF NOT EXISTS idx_prompts_name_version
    ON prompts (name, version DESC);

CREATE INDEX IF NOT EXISTS idx_prompts_hash
    ON prompts (hash);
