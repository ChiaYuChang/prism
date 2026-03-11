-- name: ClaimSearchTasks :many
-- ClaimSearchTasks selects and locks pending search tasks or zombie tasks (stuck in RUNNING for > 30 mins).
-- It atomically updates their status to 'RUNNING' and returns the task details.
UPDATE search_tasks
SET 
    status = 'RUNNING',
    retry_count = retry_count + 1,
    last_run_at = NOW(),
    updated_at = NOW()
WHERE id IN (
    SELECT id FROM search_tasks
    WHERE (status = 'PENDING' AND next_run_at <= NOW())
       OR (status = 'RUNNING' AND last_run_at < NOW() - INTERVAL '30 minutes')
    ORDER BY next_run_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, content_id, phrases, trace_id, frequency, next_run_at, retry_count;

-- name: CompleteSearchTask :exec
-- CompleteSearchTask marks a task as COMPLETED and updates its next scheduled run time.
UPDATE search_tasks
SET 
    status = 'PENDING',
    last_run_at = NOW(),
    next_run_at = NOW() + frequency,
    updated_at = NOW()
WHERE id = $1;

-- name: FailSearchTask :exec
-- FailSearchTask marks a task as FAILED.
UPDATE search_tasks
SET 
    status = 'FAILED',
    updated_at = NOW()
WHERE id = $1;
