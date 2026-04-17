-- name: ListPendingCompletionBatches :many
SELECT *
FROM batches
WHERE completed_at IS NULL
  AND source_type = $1
ORDER BY created_at ASC
LIMIT $2;

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

-- name: MarkBatchCompleted :exec
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
