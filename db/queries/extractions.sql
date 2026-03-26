-- name: GetContentExtractionByID :one
SELECT *
FROM content_extractions
WHERE id = $1
LIMIT 1;

-- name: GetContentExtractionSnapshot :one
SELECT *
FROM content_extractions
WHERE content_id = $1
  AND model_id = $2
  AND prompt_id = $3
  AND schema_version = $4
LIMIT 1;

-- name: CreateContentExtraction :one
INSERT INTO content_extractions (
    content_id,
    model_id,
    prompt_id,
    schema_name,
    schema_version,
    title,
    summary,
    raw_result,
    trace_id
) VALUES (
    sqlc.arg(content_id),
    sqlc.arg(model_id),
    sqlc.arg(prompt_id),
    sqlc.arg(schema_name),
    sqlc.arg(schema_version),
    sqlc.arg(title),
    sqlc.arg(summary),
    sqlc.arg(raw_result),
    sqlc.arg(trace_id)
)
RETURNING *;

-- name: GetEntityByCanonicalAndType :one
SELECT *
FROM entities
WHERE canonical = $1
  AND type = $2
LIMIT 1;

-- name: UpsertEntity :one
INSERT INTO entities (
    canonical,
    type
) VALUES (
    sqlc.arg(canonical),
    sqlc.arg(type)
)
ON CONFLICT (canonical, type) DO UPDATE
SET canonical = EXCLUDED.canonical
RETURNING *;

-- name: CreateContentExtractionEntity :exec
INSERT INTO content_extraction_entities (
    extraction_id,
    entity_id,
    surface,
    ordinal
) VALUES (
    sqlc.arg(extraction_id),
    sqlc.arg(entity_id),
    sqlc.arg(surface),
    sqlc.narg(ordinal)
);

-- name: ReplaceContentExtractionTopics :exec
WITH deleted AS (
    DELETE FROM content_extraction_topics
    WHERE extraction_id = $1
)
INSERT INTO content_extraction_topics (
    extraction_id,
    topic_text,
    ordinal
)
SELECT
    $1,
    topic_text,
    ordinality::smallint
FROM unnest($2::text[]) WITH ORDINALITY AS t(topic_text, ordinality);

-- name: ReplaceContentExtractionPhrases :exec
WITH deleted AS (
    DELETE FROM content_extraction_phrases
    WHERE extraction_id = $1
)
INSERT INTO content_extraction_phrases (
    extraction_id,
    phrase,
    ordinal
)
SELECT
    $1,
    phrase,
    ordinality::smallint
FROM unnest($2::text[]) WITH ORDINALITY AS t(phrase, ordinality);
