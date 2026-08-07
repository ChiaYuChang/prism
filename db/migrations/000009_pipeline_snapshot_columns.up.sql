BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pipeline_input_candidates)
       OR EXISTS (SELECT 1 FROM pipeline_input_contents) THEN
        RAISE EXCEPTION 'cannot repair legacy pipeline snapshot tables with existing rows';
    END IF;
END
$$;

ALTER TABLE pipeline_input_candidates
    ADD COLUMN IF NOT EXISTS batch_id UUID,
    ADD COLUMN IF NOT EXISTS fingerprint VARCHAR(64),
    ADD COLUMN IF NOT EXISTS title TEXT,
    ADD COLUMN IF NOT EXISTS url TEXT,
    ADD COLUMN IF NOT EXISTS description TEXT,
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS discovered_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS trace_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS ingestion_method candidate_ingestion_method,
    ADD COLUMN IF NOT EXISTS metadata JSONB,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ;

ALTER TABLE pipeline_input_candidates
    ALTER COLUMN batch_id SET NOT NULL,
    ALTER COLUMN fingerprint SET NOT NULL,
    ALTER COLUMN title SET NOT NULL,
    ALTER COLUMN url SET NOT NULL,
    ALTER COLUMN discovered_at SET NOT NULL,
    ALTER COLUMN trace_id SET NOT NULL,
    ALTER COLUMN ingestion_method SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL;

ALTER TABLE pipeline_input_contents
    ADD COLUMN IF NOT EXISTS batch_id UUID,
    ADD COLUMN IF NOT EXISTS type content_type,
    ADD COLUMN IF NOT EXISTS candidate_id UUID,
    ADD COLUMN IF NOT EXISTS url TEXT,
    ADD COLUMN IF NOT EXISTS title TEXT,
    ADD COLUMN IF NOT EXISTS content TEXT,
    ADD COLUMN IF NOT EXISTS author TEXT,
    ADD COLUMN IF NOT EXISTS trace_id VARCHAR(100),
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS fetched_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS metadata JSONB;

ALTER TABLE pipeline_input_contents
    ALTER COLUMN batch_id SET NOT NULL,
    ALTER COLUMN type SET NOT NULL,
    ALTER COLUMN url SET NOT NULL,
    ALTER COLUMN title SET NOT NULL,
    ALTER COLUMN content SET NOT NULL,
    ALTER COLUMN trace_id SET NOT NULL,
    ALTER COLUMN published_at SET NOT NULL,
    ALTER COLUMN fetched_at SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL;

COMMIT;
