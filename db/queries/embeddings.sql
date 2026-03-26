-- name: CreateCandidateEmbeddingGemma2025 :one
INSERT INTO candidate_embeddings_gemma_2025 (
    candidate_id,
    model_id,
    category,
    vector,
    trace_id
) VALUES (
    sqlc.arg(candidate_id),
    sqlc.arg(model_id),
    sqlc.arg(category),
    sqlc.arg(vector),
    sqlc.arg(trace_id)
)
RETURNING *;

-- name: CreateContentEmbeddingGemma2025 :one
INSERT INTO content_embeddings_gemma_2025 (
    content_id,
    model_id,
    category,
    vector,
    trace_id
) VALUES (
    sqlc.arg(content_id),
    sqlc.arg(model_id),
    sqlc.arg(category),
    sqlc.arg(vector),
    sqlc.arg(trace_id)
)
RETURNING *;

-- name: ListCandidateEmbeddingsByCandidateID :many
SELECT *
FROM candidate_embeddings_gemma_2025
WHERE candidate_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListContentEmbeddingsByContentID :many
SELECT *
FROM content_embeddings_gemma_2025
WHERE content_id = $1
ORDER BY created_at DESC, id DESC;

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
ORDER BY e.vector <=> $2
LIMIT $3;
