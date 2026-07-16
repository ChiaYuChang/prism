ALTER TABLE prompts ADD COLUMN IF NOT EXISTS path TEXT;

UPDATE prompts
SET path = name
WHERE path IS NULL;

DROP INDEX IF EXISTS idx_prompts_hash;
DROP INDEX IF EXISTS idx_prompts_name_version;
DROP INDEX IF EXISTS prompts_name_version_key;

ALTER TABLE prompts DROP COLUMN IF EXISTS size_bytes;
ALTER TABLE prompts DROP COLUMN IF EXISTS version;
ALTER TABLE prompts DROP COLUMN IF EXISTS name;
