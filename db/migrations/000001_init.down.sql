BEGIN;

-- Drop tables with CASCADE to ensure associated SERIAL sequences are removed and reset
DROP TABLE IF EXISTS search_tasks CASCADE;
DROP TABLE IF EXISTS content_keywords CASCADE;
DROP TABLE IF EXISTS keywords CASCADE;
DROP TABLE IF EXISTS contents CASCADE;
DROP TABLE IF EXISTS fingerprints CASCADE;
DROP TABLE IF EXISTS sources CASCADE;
DROP TABLE IF EXISTS models CASCADE;

-- Drop custom enum types
DROP TYPE IF EXISTS embedding_category;
DROP TYPE IF EXISTS content_type;
DROP TYPE IF EXISTS fingerprint_status;
DROP TYPE IF EXISTS source_type;

-- Dorp extensions
DROP EXTENSION IF EXISTS "vector";

COMMIT;