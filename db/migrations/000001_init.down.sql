BEGIN;

DROP TABLE IF EXISTS fetch_items CASCADE;
DROP TABLE IF EXISTS fetches CASCADE;
-- legacy names from pre-rename Phase 2.7 dev iterations; safe no-op on fresh DBs
DROP TABLE IF EXISTS user_fetch_request_items CASCADE;
DROP TABLE IF EXISTS user_fetch_requests CASCADE;
DROP TABLE IF EXISTS tasks CASCADE;
DROP TABLE IF EXISTS batches CASCADE;
DROP TABLE IF EXISTS content_extraction_phrases CASCADE;
DROP TABLE IF EXISTS content_extraction_topics CASCADE;
DROP TABLE IF EXISTS content_extraction_entities CASCADE;
DROP TABLE IF EXISTS entities CASCADE;
DROP TABLE IF EXISTS content_extractions CASCADE;
DROP TABLE IF EXISTS prompts CASCADE;
DROP TABLE IF EXISTS contents CASCADE;
DROP TABLE IF EXISTS candidates CASCADE;
DROP TABLE IF EXISTS sources CASCADE;
DROP TABLE IF EXISTS models CASCADE;

DROP TYPE IF EXISTS task_status;
DROP TYPE IF EXISTS task_kind;
DROP TYPE IF EXISTS candidate_ingestion_method;
DROP TYPE IF EXISTS entity_type;
DROP TYPE IF EXISTS embedding_category;
DROP TYPE IF EXISTS content_type;
DROP TYPE IF EXISTS model_type;
DROP TYPE IF EXISTS source_type;

DROP EXTENSION IF EXISTS "vector";

COMMIT;
