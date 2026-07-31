-- name: ListPendingCompletionBatches :many
SELECT *
FROM batches
WHERE completed_at IS NULL
  AND source_type = $1
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
  AND EXISTS (SELECT 1 FROM tasks t WHERE t.batch_id = b.id)
  AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.batch_id = b.id AND t.status != 'COMPLETED')
  AND (SELECT COUNT(*) FROM candidates c WHERE c.batch_id = b.id) > 0
  AND (SELECT COUNT(*) FROM candidates c WHERE c.batch_id = b.id) <= (SELECT COUNT(*) FROM contents ct WHERE ct.batch_id = b.id)
ORDER BY b.created_at ASC
LIMIT $2;

-- name: MarkBatchCompleted :execrows
-- Optimistic-concurrency claim: returns rows-affected so the caller can
-- distinguish the winner (1) from a loser racing against another instance
-- (0). Only the winner should publish the batch.completed signal.
UPDATE batches
SET completed_at = NOW(),
    updated_at = NOW(),
    trace_id = COALESCE(NULLIF(trace_id, ''), NULLIF(sqlc.arg(trace_id), ''))
WHERE id = $1
  AND completed_at IS NULL;

-- name: ListReadyToPublishBatches :many
SELECT *
FROM batches
WHERE completed_at IS NOT NULL
  AND published_at IS NULL
  AND source_type = $1
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
SELECT id, n_subtasks, completed_at
FROM batches
WHERE id = $1
FOR UPDATE;

-- name: EnsurePipelineChildBatch :one
INSERT INTO batches (id, source_type, trace_id, parent_id, parent_task_id)
VALUES (sqlc.arg(id), sqlc.arg(source_type), sqlc.arg(trace_id), sqlc.arg(parent_id), sqlc.arg(parent_task_id))
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
WHERE id = sqlc.arg(batch_id)
  AND completed_at IS NULL;

-- name: MarkPipelineRootFinished :execrows
UPDATE batches
SET completed_at = NOW(),
    succeeded = sqlc.arg(succeeded),
    updated_at = NOW(),
    trace_id = COALESCE(NULLIF(trace_id, ''), NULLIF(sqlc.arg(trace_id), ''))
WHERE id = sqlc.arg(batch_id)
  AND parent_task_id IS NULL
  AND completed_at IS NULL;

-- name: SetPipelineRootFailure :exec
UPDATE batches
SET n_subtasks = COALESCE(n_subtasks, 1),
    updated_at = NOW()
WHERE id = $1
  AND parent_task_id IS NULL;

-- name: ListReadyPipelineBatches :many
SELECT *
FROM batches
WHERE completed_at IS NOT NULL
  AND n_subtasks IS NOT NULL
  AND pipeline_published_at IS NULL
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
