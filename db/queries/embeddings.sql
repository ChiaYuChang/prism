-- name: UpsertCandidateEmbeddingGemma2025 :one
INSERT INTO candidate_embeddings_gemma_2025 (
    candidate_id,
    model_id,
    category,
    vector,
    input_hash,
    trace_id
) VALUES (
    sqlc.arg(candidate_id),
    sqlc.arg(model_id),
    sqlc.arg(category),
    sqlc.arg(vector),
    sqlc.arg(input_hash),
    sqlc.arg(trace_id)
)
ON CONFLICT (candidate_id, model_id, category) DO UPDATE
SET vector = EXCLUDED.vector,
    input_hash = EXCLUDED.input_hash,
    trace_id = EXCLUDED.trace_id,
    created_at = NOW()
RETURNING *;

-- name: UpsertContentEmbeddingGemma2025 :one
INSERT INTO content_embeddings_gemma_2025 (
    content_id,
    model_id,
    vector,
    input_hash,
    trace_id
) VALUES (
    sqlc.arg(content_id),
    sqlc.arg(model_id),
    sqlc.arg(vector),
    sqlc.arg(input_hash),
    sqlc.arg(trace_id)
)
ON CONFLICT (content_id, model_id) DO UPDATE
SET vector = EXCLUDED.vector,
    input_hash = EXCLUDED.input_hash,
    trace_id = EXCLUDED.trace_id,
    created_at = NOW(),
    deleted_at = NULL
RETURNING *;

-- name: ListCandidateEmbeddingsByCandidateID :many
SELECT id, candidate_id, model_id, category, vector, input_hash, trace_id, created_at
FROM candidate_embeddings_gemma_2025
WHERE candidate_id = $1
ORDER BY created_at DESC, id DESC;

-- name: GetCandidateEmbeddingInputHash :one
SELECT input_hash
FROM candidate_embeddings_gemma_2025
WHERE candidate_id = $1
  AND model_id = $2
  AND category = $3;

-- name: ListCandidateEmbeddingsGemma2025 :many
SELECT id, candidate_id, model_id, category, input_hash, trace_id, created_at
FROM candidate_embeddings_gemma_2025
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: ListContentEmbeddingsByContentID :many
SELECT id, content_id, model_id, vector, input_hash, trace_id, created_at, deleted_at
FROM content_embeddings_gemma_2025
WHERE content_id = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: GetContentEmbeddingInputHash :one
SELECT input_hash
FROM content_embeddings_gemma_2025
WHERE content_id = $1
  AND model_id = $2
  AND deleted_at IS NULL;

-- name: ListContentEmbeddingsGemma2025 :many
SELECT id, content_id, model_id, input_hash, trace_id, created_at, deleted_at
FROM content_embeddings_gemma_2025
WHERE deleted_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: SearchCandidatesByVector :many
SELECT
    c.*,
    e.vector <=> $2 AS distance
FROM candidate_embeddings_gemma_2025 AS e
JOIN candidates AS c ON c.id = e.candidate_id
WHERE e.model_id = $1
ORDER BY e.vector <=> $2
LIMIT $3;

-- name: SearchContentsByVector :many
SELECT
    c.*,
    e.vector <=> $2 AS distance
FROM content_embeddings_gemma_2025 AS e
JOIN contents AS c ON c.id = e.content_id
    WHERE e.model_id = $1
  AND e.deleted_at IS NULL
  AND c.deleted_at IS NULL
ORDER BY e.vector <=> $2
LIMIT $3;

-- name: SoftDeleteContentEmbeddings :exec
UPDATE content_embeddings_gemma_2025
SET deleted_at = NOW()
WHERE content_id = $1
  AND deleted_at IS NULL;

-- name: RestoreContentEmbeddings :exec
UPDATE content_embeddings_gemma_2025
SET deleted_at = NULL
WHERE content_id = $1
  AND deleted_at IS NOT NULL;
