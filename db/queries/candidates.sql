-- name: GetCandidateByID :one
SELECT *
FROM candidates
WHERE id = $1
LIMIT 1;

-- name: GetCandidateByFingerprint :one
SELECT *
FROM candidates
WHERE fingerprint = $1
LIMIT 1;

-- name: CreateCandidate :one
INSERT INTO candidates (
    batch_id,
    fingerprint,
    source_abbr,
    title,
    url,
    description,
    published_at,
    trace_id,
    ingestion_method,
    metadata
) VALUES (
    sqlc.narg(batch_id),
    sqlc.arg(fingerprint),
    sqlc.arg(source_abbr),
    sqlc.arg(title),
    sqlc.arg(url),
    sqlc.narg(description),
    sqlc.narg(published_at),
    sqlc.arg(trace_id),
    sqlc.arg(ingestion_method),
    sqlc.narg(metadata)
)
RETURNING *;

-- name: UpsertCandidate :one
INSERT INTO candidates (
    batch_id,
    fingerprint,
    source_abbr,
    title,
    url,
    description,
    published_at,
    trace_id,
    ingestion_method,
    metadata
) VALUES (
    sqlc.narg(batch_id),
    sqlc.arg(fingerprint),
    sqlc.arg(source_abbr),
    sqlc.arg(title),
    sqlc.arg(url),
    sqlc.narg(description),
    sqlc.narg(published_at),
    sqlc.arg(trace_id),
    sqlc.arg(ingestion_method),
    sqlc.narg(metadata)
)
ON CONFLICT (fingerprint) DO UPDATE
SET discovered_at = NOW(),
    trace_id = EXCLUDED.trace_id
RETURNING *;

-- name: ListCandidatesForAnalysis :many
SELECT *
FROM candidates
ORDER BY published_at DESC NULLS LAST, discovered_at DESC, created_at DESC
LIMIT $1
OFFSET $2;

-- name: SearchCandidatesByText :many
SELECT *
FROM candidates
WHERE title ILIKE '%' || $1 || '%'
   OR COALESCE(description, '') ILIKE '%' || $1 || '%'
ORDER BY published_at DESC NULLS LAST, discovered_at DESC, created_at DESC
LIMIT $2
OFFSET $3;

-- name: CountCandidatesByBatchID :one
SELECT COUNT(*)
FROM candidates
WHERE batch_id = $1;

-- name: ListCandidates :many
SELECT *
FROM candidates
WHERE (sqlc.narg(query)::text IS NULL
       OR title ILIKE '%' || sqlc.narg(query)::text || '%'
       OR COALESCE(description, '') ILIKE '%' || sqlc.narg(query)::text || '%')
  AND (sqlc.narg(source_abbr)::varchar IS NULL OR source_abbr = sqlc.narg(source_abbr)::varchar)
  AND (sqlc.narg(since)::timestamptz IS NULL OR COALESCE(published_at, discovered_at) >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(until)::timestamptz IS NULL OR COALESCE(published_at, discovered_at) <= sqlc.narg(until)::timestamptz)
ORDER BY published_at DESC NULLS LAST, discovered_at DESC, created_at DESC
LIMIT sqlc.arg(lim)::int
OFFSET sqlc.arg(off)::int;

-- name: GetCandidatesByIDs :many
SELECT *
FROM candidates
WHERE id = ANY(sqlc.arg(ids)::uuid[]);
