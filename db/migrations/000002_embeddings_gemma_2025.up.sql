BEGIN;

-- 1. Vector Storage Table (Gemma 2025 Version)
CREATE TABLE IF NOT EXISTS embeddings_gemma_2025 (
    id          BIGSERIAL PRIMARY KEY,
    content_id  UUID NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    
    -- Reference to model ID from the core models table
    model_id    SMALLINT NOT NULL REFERENCES models(id), 
    
    -- Use the globally defined category enum (CONTENT or TITLE)
    category    embedding_category NOT NULL,
    vector      vector(768) NOT NULL,         -- Fixed dimension for Gemma model
    
    trace_id    VARCHAR(100) NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 2. Performance Indexes
CREATE INDEX IF NOT EXISTS idx_emb_g25_content ON embeddings_gemma_2025(content_id);
CREATE INDEX IF NOT EXISTS idx_emb_g25_model ON embeddings_gemma_2025(model_id);
CREATE INDEX IF NOT EXISTS idx_emb_g25_category ON embeddings_gemma_2025(category);
CREATE INDEX IF NOT EXISTS idx_emb_g25_trace_id ON embeddings_gemma_2025(trace_id);

-- HNSW Index: Semantic search optimization
CREATE INDEX IF NOT EXISTS idx_emb_g25_vec ON embeddings_gemma_2025 USING hnsw (vector vector_cosine_ops);

COMMIT;