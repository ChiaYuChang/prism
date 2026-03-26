BEGIN;

CREATE TABLE IF NOT EXISTS candidate_embeddings_gemma_2025 (
    id           BIGSERIAL PRIMARY KEY,
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    model_id     SMALLINT NOT NULL REFERENCES models(id),
    category     embedding_category NOT NULL,
    vector       VECTOR(768) NOT NULL,
    trace_id     VARCHAR(100) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cemb_g25_candidate ON candidate_embeddings_gemma_2025(candidate_id);
CREATE INDEX IF NOT EXISTS idx_cemb_g25_model ON candidate_embeddings_gemma_2025(model_id);
CREATE INDEX IF NOT EXISTS idx_cemb_g25_category ON candidate_embeddings_gemma_2025(category);
CREATE INDEX IF NOT EXISTS idx_cemb_g25_trace_id ON candidate_embeddings_gemma_2025(trace_id);
CREATE INDEX IF NOT EXISTS idx_cemb_g25_vec
    ON candidate_embeddings_gemma_2025 USING hnsw (vector vector_cosine_ops);

CREATE TABLE IF NOT EXISTS content_embeddings_gemma_2025 (
    id         BIGSERIAL PRIMARY KEY,
    content_id UUID NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    model_id   SMALLINT NOT NULL REFERENCES models(id),
    category   embedding_category NOT NULL,
    vector     VECTOR(768) NOT NULL,
    trace_id   VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_emb_g25_content ON content_embeddings_gemma_2025(content_id);
CREATE INDEX IF NOT EXISTS idx_emb_g25_model ON content_embeddings_gemma_2025(model_id);
CREATE INDEX IF NOT EXISTS idx_emb_g25_category ON content_embeddings_gemma_2025(category);
CREATE INDEX IF NOT EXISTS idx_emb_g25_trace_id ON content_embeddings_gemma_2025(trace_id);
CREATE INDEX IF NOT EXISTS idx_emb_g25_vec
    ON content_embeddings_gemma_2025 USING hnsw (vector vector_cosine_ops);

COMMIT;
