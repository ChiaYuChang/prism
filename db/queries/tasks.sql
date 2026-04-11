-- name: GetTaskByID :one
SELECT *
FROM tasks
WHERE id = $1
LIMIT 1;

-- name: ListTasksByBatchID :many
SELECT *
FROM tasks
WHERE batch_id = $1
ORDER BY created_at ASC, next_run_at ASC;

-- name: CreateTask :one
INSERT INTO tasks (
    batch_id,
    kind,
    source_type,
    source_id,
    url,
    payload,
    payload_hash,
    trace_id,
    frequency,
    next_run_at,
    expires_at
) VALUES (
    sqlc.arg(batch_id),
    sqlc.arg(kind),
    sqlc.arg(source_type),
    sqlc.arg(source_id),
    sqlc.arg(url),
    sqlc.narg(payload),
    sqlc.narg(payload_hash),
    sqlc.arg(trace_id),
    sqlc.narg(frequency),
    COALESCE(sqlc.narg(next_run_at), NOW()),
    sqlc.narg(expires_at)
)
RETURNING *;

-- name: ClaimTasks :many
UPDATE tasks
SET status = 'RUNNING',
    retry_count = retry_count + 1,
    last_run_at = NOW(),
    updated_at = NOW()
WHERE id IN (
    SELECT id
    FROM tasks
    WHERE (
            status = 'PENDING'
        AND next_run_at <= NOW()
        AND (expires_at IS NULL OR expires_at > NOW())
    ) OR (
            status = 'RUNNING'
        AND last_run_at < NOW() - INTERVAL '30 minutes'
        AND (expires_at IS NULL OR expires_at > NOW())
    )
    ORDER BY next_run_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteTask :exec
UPDATE tasks
SET status = CASE
        WHEN frequency IS NOT NULL
         AND (expires_at IS NULL OR NOW() + frequency <= expires_at)
            THEN 'PENDING'::task_status
        ELSE 'COMPLETED'::task_status
    END,
    next_run_at = CASE
        WHEN frequency IS NOT NULL
         AND (expires_at IS NULL OR NOW() + frequency <= expires_at)
            THEN NOW() + frequency
        ELSE next_run_at
    END,
    last_run_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: FailTask :exec
UPDATE tasks
SET status = 'FAILED',
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: ExtendActiveTaskExpiry :exec
-- Updates expires_at on an existing PENDING/RUNNING task identified by its dedup key.
-- Used when CreateTask returns ErrTaskAlreadyActive to refresh the task's lifetime.
UPDATE tasks
SET expires_at = sqlc.narg(expires_at),
    updated_at = NOW()
WHERE source_id    = sqlc.arg(source_id)
  AND kind         = sqlc.arg(kind)
  AND payload_hash = sqlc.arg(payload_hash)
  AND status IN ('PENDING', 'RUNNING');

-- name: ListRunnableTasks :many
SELECT *
FROM tasks
WHERE (
        status = 'PENDING'
    AND next_run_at <= NOW()
    AND (expires_at IS NULL OR expires_at > NOW())
) OR (
        status = 'RUNNING'
    AND last_run_at < NOW() - INTERVAL '30 minutes'
    AND (expires_at IS NULL OR expires_at > NOW())
)
ORDER BY next_run_at ASC, created_at ASC
LIMIT $1;
