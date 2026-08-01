BEGIN;

CREATE TYPE batch_purpose AS ENUM (
    'COLLECTION',
    'ANALYZER_PIPELINE_ROOT',
    'ANALYZER_PIPELINE_STAGE'
);

ALTER TABLE batches
    ADD COLUMN purpose batch_purpose NOT NULL DEFAULT 'COLLECTION',
    ADD COLUMN pipeline_definition_hash CHAR(64),
    ADD COLUMN pipeline_idempotency_key TEXT,
    ADD COLUMN pipeline_request_fingerprint CHAR(64),
    ADD COLUMN pipeline_input_snapshot_at TIMESTAMPTZ;

CREATE UNIQUE INDEX uq_pipeline_root_idempotency
    ON batches(parent_id, pipeline_idempotency_key)
    WHERE purpose = 'ANALYZER_PIPELINE_ROOT'
      AND pipeline_idempotency_key IS NOT NULL;

CREATE INDEX idx_batches_purpose_completed
    ON batches(purpose, completed_at, created_at);

CREATE TABLE pipeline_input_candidates (
    root_batch_id UUID NOT NULL,
    candidate_id UUID NOT NULL,
    batch_id UUID NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    source_abbr VARCHAR(16) NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT,
    published_at TIMESTAMPTZ,
    discovered_at TIMESTAMPTZ NOT NULL,
    trace_id VARCHAR(100) NOT NULL,
    ingestion_method candidate_ingestion_method NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (root_batch_id, candidate_id)
);

CREATE TABLE pipeline_input_contents (
    root_batch_id UUID NOT NULL,
    content_id UUID NOT NULL,
    batch_id UUID NOT NULL,
    type content_type NOT NULL,
    source_abbr VARCHAR(16) NOT NULL,
    candidate_id UUID,
    url TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    author TEXT,
    trace_id VARCHAR(100) NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    metadata JSONB,
    PRIMARY KEY (root_batch_id, content_id)
);

COMMENT ON COLUMN batches.purpose IS 'Explicit lifecycle owner for collection and analyzer pipeline batches.';
COMMENT ON COLUMN batches.pipeline_definition_hash IS 'SHA-256 hash of the YAML definition used by this pipeline Root.';
COMMENT ON COLUMN batches.pipeline_idempotency_key IS 'Client idempotency key scoped to the source batch.';
COMMENT ON COLUMN batches.pipeline_request_fingerprint IS 'SHA-256 fingerprint of the semantic pipeline creation request.';
COMMENT ON COLUMN batches.pipeline_input_snapshot_at IS 'Timestamp at which immutable pipeline input membership was captured.';

COMMIT;
