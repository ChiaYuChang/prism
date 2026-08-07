BEGIN;

ALTER TABLE pipeline_input_contents
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS fetched_at,
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS trace_id,
    DROP COLUMN IF EXISTS author,
    DROP COLUMN IF EXISTS content,
    DROP COLUMN IF EXISTS title,
    DROP COLUMN IF EXISTS url,
    DROP COLUMN IF EXISTS candidate_id,
    DROP COLUMN IF EXISTS type,
    DROP COLUMN IF EXISTS batch_id;

ALTER TABLE pipeline_input_candidates
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS ingestion_method,
    DROP COLUMN IF EXISTS trace_id,
    DROP COLUMN IF EXISTS discovered_at,
    DROP COLUMN IF EXISTS published_at,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS url,
    DROP COLUMN IF EXISTS title,
    DROP COLUMN IF EXISTS fingerprint,
    DROP COLUMN IF EXISTS batch_id;

COMMIT;
