-- name: GetTaskByID :one
SELECT *
FROM tasks
WHERE id = $1
LIMIT 1;

-- name: IsTaskRunning :one
SELECT status = 'RUNNING'::task_status AS is_running
FROM tasks
WHERE id = $1;

-- name: ListTasksByBatchID :many
SELECT *
FROM tasks
WHERE batch_id = $1
ORDER BY created_at ASC, next_run_at ASC;

-- name: ListTaskStatusSummary :many
SELECT kind, status, COUNT(*)::bigint AS count
FROM tasks
GROUP BY kind, status
ORDER BY kind ASC, status ASC;

-- name: ListRecentFailedTasks :many
SELECT id, kind, source_abbr, url, failure_message, updated_at
FROM tasks
WHERE status = 'FAILED'
ORDER BY updated_at DESC
LIMIT $1;

-- name: GetActiveTaskByPayloadDedup :one
SELECT *
FROM tasks
WHERE source_abbr = sqlc.arg(source_abbr)
  AND kind = sqlc.arg(kind)
  AND payload_hash = sqlc.arg(payload_hash)
  AND status IN ('PENDING', 'RUNNING')
LIMIT 1;

-- name: EnsureBatchExists :exec
INSERT INTO batches (id, source_type, trace_id, parent_id)
VALUES ($1, $2, $3, sqlc.narg(parent_id))
ON CONFLICT (id) DO UPDATE
SET parent_id = COALESCE(batches.parent_id, EXCLUDED.parent_id);

-- name: CreateTask :one
-- Single-round-trip insert-or-recover. On unique-violation against either
-- uq_tasks_active_payload or uq_tasks_active_page_fetch, returns the
-- existing PENDING/RUNNING row with inserted=false. Adapter maps
-- inserted=false to repo.ErrTaskAlreadyActive while still surfacing the
-- recovered task fields, so callers that need the existing task_id (e.g.
-- the user-fetch handler) avoid a second SELECT.
WITH ins AS (
    INSERT INTO tasks (
        batch_id,
        kind,
        source_type,
        source_abbr,
        url,
        payload,
        payload_hash,
        meta,
        trace_id,
        previous_task_id,
        next_task_id,
        logical_key,
        frequency,
        next_run_at,
        expires_at
    ) VALUES (
        sqlc.arg(batch_id), sqlc.arg(kind), sqlc.arg(source_type), sqlc.arg(source_abbr), sqlc.arg(url),
        COALESCE(sqlc.narg(payload), '{}'::jsonb), sqlc.narg(payload_hash), sqlc.narg(meta), sqlc.arg(trace_id),
        sqlc.narg(previous_task_id), sqlc.narg(next_task_id), sqlc.narg(logical_key), sqlc.narg(frequency),
        COALESCE(sqlc.narg(next_run_at), NOW()), sqlc.narg(expires_at)
    )
    ON CONFLICT DO NOTHING
    RETURNING tasks.*
)
SELECT i.*, TRUE AS inserted FROM ins i
UNION ALL
SELECT t.*, FALSE AS inserted
FROM tasks t
WHERE NOT EXISTS (SELECT 1 FROM ins)
  AND t.status IN ('PENDING', 'RUNNING')
  AND t.kind = sqlc.arg(kind)
  AND (
        (t.kind = 'PAGE_FETCH' AND t.url = sqlc.arg(url))
     OR (
            t.source_abbr  = sqlc.arg(source_abbr)
        AND t.payload_hash IS NOT NULL
        AND t.payload_hash = sqlc.narg(payload_hash)
        )
  )
LIMIT 1;

-- name: ClaimTasks :many
UPDATE tasks
SET status = 'RUNNING',
    retry_count = retry_count + 1,
    failure_message = NULL,
    last_run_at = NOW(),
    updated_at = NOW()
WHERE id IN (
    SELECT id
    FROM tasks
    WHERE (
            status = 'PENDING'
        AND next_run_at <= NOW()
        AND (expires_at IS NULL OR expires_at > NOW())
        AND kind = ANY(sqlc.arg(kinds)::task_kind[])
        AND (
            COALESCE(array_length(sqlc.arg(source_types)::source_type[], 1), 0) = 0
            OR source_type = ANY(sqlc.arg(source_types)::source_type[])
        )
        AND (
            previous_task_id IS NULL
            OR EXISTS (
                SELECT 1 FROM tasks previous
                WHERE previous.id = tasks.previous_task_id
                  AND previous.status = 'COMPLETED'
            )
        )
    ) OR (
            status = 'RUNNING'
        AND last_run_at < NOW() - INTERVAL '30 minutes'
        AND (expires_at IS NULL OR expires_at > NOW())
        AND kind = ANY(sqlc.arg(kinds)::task_kind[])
        AND (
            COALESCE(array_length(sqlc.arg(source_types)::source_type[], 1), 0) = 0
            OR source_type = ANY(sqlc.arg(source_types)::source_type[])
        )
        AND (
            previous_task_id IS NULL
            OR EXISTS (
                SELECT 1 FROM tasks previous
                WHERE previous.id = tasks.previous_task_id
                  AND previous.status = 'COMPLETED'
            )
        )
    )
    ORDER BY next_run_at ASC
    LIMIT sqlc.arg(max_tasks)
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
     failure_message = NULL,
     last_run_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND status = 'RUNNING';

-- name: FailTask :exec
-- A claim increments retry_count before execution, so retry_count is the total
-- number of attempts. Failed attempts below retry_max are made runnable again.
UPDATE tasks
SET status = CASE
        WHEN retry_count < sqlc.arg(retry_max) THEN 'PENDING'::task_status
        ELSE 'FAILED'::task_status
    END,
     next_run_at = CASE
         WHEN retry_count < sqlc.arg(retry_max) THEN NOW()
         ELSE next_run_at
     END,
     failure_message = LEFT(sqlc.arg(failure_message), 2048),
     updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND status = 'RUNNING';

-- name: RetryFailedTask :one
-- Atomically reschedules a failed task while retaining its retry_count and
-- last_run_at history. Non-failed existing tasks are returned with retried=false.
WITH retried AS (
    UPDATE tasks
    SET status = 'PENDING',
        failure_message = NULL,
        next_run_at = NOW(),
        updated_at = NOW()
    WHERE tasks.id = sqlc.arg(id)
      AND status = 'FAILED'
    RETURNING tasks.*
)
SELECT r.*, TRUE AS retried
FROM retried r
UNION ALL
SELECT t.*, FALSE AS retried
FROM tasks t
WHERE t.id = sqlc.arg(id)
  AND NOT EXISTS (SELECT 1 FROM retried)
LIMIT 1;

-- name: ExtendActiveTaskExpiry :exec
-- Updates expires_at on an existing PENDING/RUNNING task identified by its dedup key.
-- Used when CreateTask returns ErrTaskAlreadyActive to refresh the task's lifetime.
UPDATE tasks
SET expires_at = sqlc.narg(expires_at),
    updated_at = NOW()
WHERE source_abbr    = sqlc.arg(source_abbr)
  AND kind         = sqlc.arg(kind)
  AND payload_hash = sqlc.arg(payload_hash)
  AND status IN ('PENDING', 'RUNNING');

-- name: ReleaseTasks :exec
-- Resets RUNNING tasks back to PENDING in bulk, undoing the ClaimTasks
-- retry_count increment. Used when dispatch is skipped (e.g. rate-limited)
-- so tasks are retried on the next scheduler tick without consuming retry slots.
UPDATE tasks
SET status      = 'PENDING',
    retry_count = GREATEST(retry_count - 1, 0),
    next_run_at = NOW() + INTERVAL '3 seconds',
    updated_at  = NOW()
WHERE id = ANY(sqlc.arg(ids)::uuid[])
  AND status = 'RUNNING';

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
