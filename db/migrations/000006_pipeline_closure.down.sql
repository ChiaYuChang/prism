BEGIN;

DROP TABLE IF EXISTS pipeline_input_contents;
DROP TABLE IF EXISTS pipeline_input_candidates;
DROP INDEX IF EXISTS idx_batches_purpose_completed;
DROP INDEX IF EXISTS uq_pipeline_root_idempotency;

ALTER TABLE batches
    DROP COLUMN IF EXISTS pipeline_input_snapshot_at,
    DROP COLUMN IF EXISTS pipeline_request_fingerprint,
    DROP COLUMN IF EXISTS pipeline_idempotency_key,
    DROP COLUMN IF EXISTS pipeline_definition_hash,
    DROP COLUMN IF EXISTS purpose;

DROP TYPE IF EXISTS batch_purpose;

COMMIT;
