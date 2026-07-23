BEGIN;

DROP TABLE IF EXISTS content_embeddings_gemma_2025 CASCADE;
DROP TABLE IF EXISTS candidate_embeddings_gemma_2025 CASCADE;
DROP TYPE IF EXISTS embedding_category;

CREATE TYPE embedding_category AS ENUM ('TITLE', 'CONTENT', 'BRIEF');

CREATE TABLE candidate_embeddings_gemma_2025 (
    id           BIGSERIAL PRIMARY KEY,
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    model_id     SMALLINT NOT NULL REFERENCES models(id),
    category     embedding_category NOT NULL,
    vector       VECTOR(768) NOT NULL,
    trace_id     VARCHAR(100) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE content_embeddings_gemma_2025 (
    id         BIGSERIAL PRIMARY KEY,
    content_id UUID NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    model_id   SMALLINT NOT NULL REFERENCES models(id),
    category   embedding_category NOT NULL,
    vector     VECTOR(768) NOT NULL,
    trace_id   VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMIT;
