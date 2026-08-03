-- name: ListPendingCompletionBatches :many
SELECT *
FROM batches
WHERE completed_at IS NULL
  AND source_type = $1
  AND purpose = 'COLLECTION'
ORDER BY created_at ASC
LIMIT $2;

-- name: ListBatches :many
SELECT *
FROM batches
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: FindNewlyCompletedBatches :many
-- Finds batches where all tasks are completed and all candidates are promoted to contents.
SELECT id, source_type, trace_id
FROM batches b
WHERE b.completed_at IS NULL 
  AND b.source_type = $1
  AND b.purpose = 'COLLECTION'
  AND EXISTS (SELECT 1 FROM tasks t WHERE t.batch_id = b.id)
  AND NOT EXISTS (
      SELECT 1 FROM tasks t
      WHERE t.batch_id = b.id
        AND t.status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED')
  )
   AND (
       EXISTS (
           SELECT 1 FROM tasks t
           WHERE t.batch_id = b.id
             AND t.status IN ('FAILED', 'CANCELLED')
       )
       OR (SELECT COUNT(*) FROM candidates c WHERE c.batch_id = b.id) = 0
       OR (SELECT COUNT(*) FROM candidates c WHERE c.batch_id = b.id)
           <= (SELECT COUNT(*) FROM contents ct WHERE ct.batch_id = b.id)
   )
ORDER BY b.created_at ASC
LIMIT $2;

-- name: MarkBatchCompleted :execrows
-- Optimistic-concurrency claim: returns rows-affected so the caller can
-- distinguish the winner (1) from a loser racing against another instance
-- (0). Only the winner should publish the batch.completed signal.
UPDATE batches
SET completed_at = NOW(),
    updated_at = NOW(),
    trace_id = COALESCE(NULLIF(trace_id, ''), NULLIF(sqlc.arg(trace_id), '')),
    succeeded = NOT EXISTS (
        SELECT 1 FROM tasks t
        WHERE t.batch_id = batches.id
          AND t.status IN ('FAILED', 'CANCELLED')
    )
WHERE batches.id = $1
  AND batches.purpose = 'COLLECTION'
  AND batches.completed_at IS NULL;

-- name: ListReadyToPublishBatches :many
SELECT *
FROM batches
WHERE completed_at IS NOT NULL
  AND published_at IS NULL
  AND source_type = $1
  AND purpose = 'COLLECTION'
ORDER BY completed_at ASC, created_at ASC
LIMIT $2;

-- name: MarkBatchPublished :exec
UPDATE batches
SET published_at = NOW(),
    updated_at = NOW(),
    publish_error = NULL
WHERE id = $1
  AND published_at IS NULL;

-- name: RecordBatchPublishFailure :exec
UPDATE batches
SET last_publish_attempt_at = NOW(),
    publish_retry_count = publish_retry_count + 1,
    publish_error = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: GetBatchByID :one
SELECT *
FROM batches
WHERE id = $1;

-- name: LockBatchForTaskInsert :one
SELECT id, n_subtasks, completed_at, succeeded
FROM batches
WHERE id = $1
FOR UPDATE;

-- name: GetPipelineRootByIdempotency :one
SELECT *
FROM batches
WHERE parent_id = sqlc.arg(parent_id)
  AND purpose = 'ANALYZER_PIPELINE_ROOT'
  AND pipeline_idempotency_key = sqlc.arg(idempotency_key)
FOR UPDATE;

-- name: EnsurePipelineRoot :exec
INSERT INTO batches (
    id, source_type, trace_id, parent_id, purpose,
    pipeline_definition_hash, pipeline_idempotency_key, pipeline_request_fingerprint,
    analysis_execution_id
)
VALUES (
    sqlc.arg(id), sqlc.arg(source_type), sqlc.arg(trace_id), sqlc.arg(parent_id),
    'ANALYZER_PIPELINE_ROOT', sqlc.arg(definition_hash),
    sqlc.narg(idempotency_key), sqlc.arg(request_fingerprint),
    sqlc.narg(analysis_execution_id)
)
ON CONFLICT (id) DO UPDATE
SET parent_id = COALESCE(batches.parent_id, EXCLUDED.parent_id),
    purpose = COALESCE(batches.purpose, EXCLUDED.purpose),
     pipeline_definition_hash = COALESCE(batches.pipeline_definition_hash, EXCLUDED.pipeline_definition_hash),
     pipeline_idempotency_key = COALESCE(batches.pipeline_idempotency_key, EXCLUDED.pipeline_idempotency_key),
     pipeline_request_fingerprint = COALESCE(batches.pipeline_request_fingerprint, EXCLUDED.pipeline_request_fingerprint),
     analysis_execution_id = COALESCE(batches.analysis_execution_id, EXCLUDED.analysis_execution_id);

-- name: MarkPipelineInputSnapshot :execrows
UPDATE batches
SET pipeline_input_snapshot_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg(root_batch_id)
  AND purpose = 'ANALYZER_PIPELINE_ROOT'
  AND pipeline_input_snapshot_at IS NULL;

-- name: SnapshotPipelineCandidates :exec
INSERT INTO pipeline_input_candidates (
    root_batch_id, candidate_id, batch_id, fingerprint, source_abbr, title, url,
    description, published_at, discovered_at, trace_id, ingestion_method, metadata, created_at
)
SELECT
    sqlc.arg(root_batch_id), c.id, c.batch_id, c.fingerprint, c.source_abbr, c.title, c.url,
    c.description, c.published_at, c.discovered_at, c.trace_id, c.ingestion_method, c.metadata, c.created_at
FROM candidates c
WHERE c.batch_id = sqlc.arg(input_batch_id)
ON CONFLICT (root_batch_id, candidate_id) DO NOTHING;

-- name: SnapshotPipelineCandidatesByIDs :exec
INSERT INTO pipeline_input_candidates (
    root_batch_id, candidate_id, batch_id, fingerprint, source_abbr, title, url,
    description, published_at, discovered_at, trace_id, ingestion_method, metadata, created_at
)
SELECT
    sqlc.arg(root_batch_id), c.id, c.batch_id, c.fingerprint, c.source_abbr, c.title, c.url,
    c.description, c.published_at, c.discovered_at, c.trace_id, c.ingestion_method, c.metadata, c.created_at
FROM candidates c
WHERE c.id = ANY(sqlc.arg(candidate_ids)::uuid[])
ON CONFLICT (root_batch_id, candidate_id) DO NOTHING;

-- name: SnapshotPipelineContents :exec
INSERT INTO pipeline_input_contents (
    root_batch_id, content_id, batch_id, type, source_abbr, candidate_id, url, title,
    content, author, trace_id, published_at, fetched_at, created_at, deleted_at, metadata
)
SELECT
    sqlc.arg(root_batch_id), c.id, c.batch_id, c.type, c.source_abbr, c.candidate_id, c.url, c.title,
    c.content, c.author, c.trace_id, c.published_at, c.fetched_at, c.created_at, c.deleted_at, c.metadata
FROM contents c
WHERE c.batch_id = sqlc.arg(input_batch_id)
  AND c.deleted_at IS NULL
ON CONFLICT (root_batch_id, content_id) DO NOTHING;

-- name: SnapshotPipelineContentsByIDs :exec
INSERT INTO pipeline_input_contents (
    root_batch_id, content_id, batch_id, type, source_abbr, candidate_id, url, title,
    content, author, trace_id, published_at, fetched_at, created_at, deleted_at, metadata
)
SELECT
    sqlc.arg(root_batch_id), c.id, c.batch_id, c.type, c.source_abbr, c.candidate_id, c.url, c.title,
    c.content, c.author, c.trace_id, c.published_at, c.fetched_at, c.created_at, c.deleted_at, c.metadata
FROM contents c
WHERE c.id = ANY(sqlc.arg(content_ids)::uuid[])
  AND c.deleted_at IS NULL
ON CONFLICT (root_batch_id, content_id) DO NOTHING;

-- name: ListPipelineInputCandidates :many
SELECT candidate_id, batch_id, fingerprint, source_abbr, title, url, description,
       published_at, discovered_at, trace_id, ingestion_method::text AS ingestion_method, metadata, created_at
FROM pipeline_input_candidates
WHERE root_batch_id = $1
ORDER BY candidate_id;

-- name: ListPipelineInputContents :many
SELECT content_id, batch_id, type, source_abbr, candidate_id, url, title, content,
       author, trace_id, published_at, fetched_at, created_at, deleted_at, metadata
FROM pipeline_input_contents
WHERE root_batch_id = $1
ORDER BY content_id;

-- name: EnsurePipelineChildBatch :one
INSERT INTO batches (id, source_type, trace_id, parent_id, parent_task_id, purpose)
VALUES (sqlc.arg(id), sqlc.arg(source_type), sqlc.arg(trace_id), sqlc.arg(parent_id), sqlc.arg(parent_task_id), 'ANALYZER_PIPELINE_STAGE')
ON CONFLICT (parent_task_id) WHERE parent_task_id IS NOT NULL DO UPDATE
SET updated_at = batches.updated_at
RETURNING id;

-- name: FindFinishedPipelineBatches :many
SELECT b.*,
       COUNT(t.id) FILTER (WHERE t.status IN ('FAILED', 'CANCELLED')) = 0 AS completion_succeeded
FROM batches b
LEFT JOIN tasks t ON t.batch_id = b.id
WHERE b.completed_at IS NULL
  AND b.n_subtasks IS NOT NULL
  AND b.purpose = 'ANALYZER_PIPELINE_STAGE'
  AND b.parent_task_id IS NOT NULL
GROUP BY b.id
HAVING COUNT(t.id) = b.n_subtasks
   AND COUNT(t.id) FILTER (WHERE t.status IN ('COMPLETED', 'FAILED', 'CANCELLED')) = b.n_subtasks
ORDER BY b.created_at ASC
LIMIT $1;

-- name: FindFinishedPipelineRootBatches :many
SELECT b.*,
       COUNT(t.id) FILTER (WHERE t.status IN ('FAILED', 'CANCELLED')) = 0 AS completion_succeeded
FROM batches b
LEFT JOIN tasks t ON t.batch_id = b.id
WHERE b.completed_at IS NULL
  AND b.n_subtasks IS NOT NULL
  AND b.purpose = 'ANALYZER_PIPELINE_ROOT'
  AND b.parent_id IS NOT NULL
  AND b.parent_task_id IS NULL
GROUP BY b.id
HAVING COUNT(t.id) = b.n_subtasks
   AND COUNT(t.id) FILTER (WHERE t.status IN ('COMPLETED', 'FAILED', 'CANCELLED')) = b.n_subtasks
ORDER BY b.created_at ASC
LIMIT $1;

-- name: SetBatchNSubtasks :one
WITH locked AS (
    SELECT id, n_subtasks, completed_at
    FROM batches
    WHERE batches.id = sqlc.arg(batch_id)
    FOR UPDATE
), updated AS (
    UPDATE batches b
    SET n_subtasks = sqlc.arg(n_subtasks), updated_at = NOW()
    FROM locked
    WHERE b.id = locked.id
      AND locked.completed_at IS NULL
      AND (locked.n_subtasks IS NULL OR locked.n_subtasks = sqlc.arg(n_subtasks))
      AND (SELECT COUNT(*) FROM tasks t WHERE t.batch_id = b.id) <= sqlc.arg(n_subtasks)
    RETURNING b.*
)
SELECT * FROM updated;

-- name: MarkPipelineBatchFinished :execrows
UPDATE batches
SET completed_at = NOW(),
    succeeded = sqlc.arg(succeeded),
    updated_at = NOW(),
    trace_id = COALESCE(NULLIF(trace_id, ''), NULLIF(sqlc.arg(trace_id), ''))
WHERE batches.id = sqlc.arg(batch_id)
  AND purpose = 'ANALYZER_PIPELINE_STAGE'
  AND completed_at IS NULL
  AND n_subtasks IS NOT NULL
  AND (SELECT COUNT(*) FROM tasks t WHERE t.batch_id = batches.id) = n_subtasks
  AND (SELECT COUNT(*) FROM tasks t WHERE t.batch_id = batches.id AND t.status IN ('COMPLETED', 'FAILED', 'CANCELLED')) = n_subtasks;

-- name: MarkPipelineRootFinished :execrows
UPDATE batches
SET completed_at = NOW(),
    succeeded = sqlc.arg(succeeded),
    updated_at = NOW(),
    trace_id = COALESCE(NULLIF(trace_id, ''), NULLIF(sqlc.arg(trace_id), ''))
WHERE batches.id = sqlc.arg(batch_id)
  AND purpose = 'ANALYZER_PIPELINE_ROOT'
  AND parent_task_id IS NULL
  AND completed_at IS NULL
  AND n_subtasks IS NOT NULL
  AND (SELECT COUNT(*) FROM tasks t WHERE t.batch_id = batches.id) = n_subtasks
  AND (SELECT COUNT(*) FROM tasks t WHERE t.batch_id = batches.id AND t.status IN ('COMPLETED', 'FAILED', 'CANCELLED')) = n_subtasks;

-- name: SetPipelineRootFailure :exec
UPDATE batches
SET n_subtasks = COALESCE(n_subtasks, 1),
    updated_at = NOW()
WHERE id = $1
  AND purpose = 'ANALYZER_PIPELINE_ROOT'
  AND parent_task_id IS NULL;

-- name: ListReadyPipelineBatches :many
SELECT *
FROM batches
WHERE completed_at IS NOT NULL
  AND n_subtasks IS NOT NULL
  AND pipeline_published_at IS NULL
  AND purpose = 'ANALYZER_PIPELINE_STAGE'
  AND parent_task_id IS NOT NULL
ORDER BY completed_at ASC, created_at ASC
LIMIT $1;

-- name: MarkPipelinePublished :exec
UPDATE batches
SET pipeline_published_at = NOW(),
    pipeline_publish_error = NULL,
    updated_at = NOW()
WHERE id = $1
  AND pipeline_published_at IS NULL;

-- name: RecordPipelinePublishFailure :exec
UPDATE batches
SET pipeline_publish_retry_count = pipeline_publish_retry_count + 1,
    pipeline_publish_error = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: ListChildBatchesByParentID :many
SELECT *
FROM batches
WHERE parent_id = $1
ORDER BY created_at ASC;
