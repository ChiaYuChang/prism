-- name: CreateUserFetchRequest :one
INSERT INTO user_fetch_requests (user_id)
VALUES (sqlc.narg(user_id))
RETURNING *;

-- name: GetUserFetchRequest :one
SELECT *
FROM user_fetch_requests
WHERE id = $1
LIMIT 1;

-- name: CreateUserFetchRequestItem :one
INSERT INTO user_fetch_request_items (
    request_id,
    candidate_id,
    task_id,
    snapshot_status
) VALUES (
    sqlc.arg(request_id),
    sqlc.arg(candidate_id),
    sqlc.narg(task_id),
    sqlc.narg(snapshot_status)
)
RETURNING *;

-- name: ListUserFetchRequestItems :many
SELECT
    i.request_id,
    i.candidate_id,
    i.task_id,
    i.snapshot_status,
    i.created_at,
    t.status AS task_status
FROM user_fetch_request_items i
LEFT JOIN tasks t ON t.id = i.task_id
WHERE i.request_id = $1
ORDER BY i.created_at ASC;

-- name: GetUserFetchRequestProgress :one
-- Aggregates item status using COALESCE(snapshot_status, tasks.status).
-- Returns counters plus a derived `terminal` flag (all items in COMPLETED /
-- FAILED / ALREADY_COMPLETE).
WITH resolved AS (
    SELECT
        COALESCE(i.snapshot_status, t.status::text) AS status
    FROM user_fetch_request_items i
    LEFT JOIN tasks t ON t.id = i.task_id
    WHERE i.request_id = $1
)
SELECT
    COUNT(*)                                                                   AS total,
    COUNT(*) FILTER (WHERE status = 'PENDING')                                 AS pending,
    COUNT(*) FILTER (WHERE status = 'RUNNING')                                 AS running,
    COUNT(*) FILTER (WHERE status = 'COMPLETED')                               AS completed,
    COUNT(*) FILTER (WHERE status = 'FAILED')                                  AS failed,
    COUNT(*) FILTER (WHERE status = 'ALREADY_COMPLETE')                        AS already_complete,
    (COUNT(*) > 0 AND COUNT(*) FILTER (
        WHERE status IN ('COMPLETED', 'FAILED', 'ALREADY_COMPLETE')
    ) = COUNT(*))                                                              AS terminal
FROM resolved;

-- name: MarkUserFetchRequestCompleted :exec
-- Sets completed_at on transition to terminal. Idempotent (WHERE clause
-- guards against double-set). v1 callers may skip this — progress endpoint
-- computes terminal on-the-fly. Reserved for v2 notification dispatcher.
UPDATE user_fetch_requests
SET completed_at = NOW()
WHERE id = $1
  AND completed_at IS NULL;

-- name: GetActivePageFetchTaskByURL :one
-- Companion to CreateTask's ON CONFLICT path. When CreateTask reports the
-- duplicate-active conflict, callers use this to recover the existing task's
-- id without an extra round-trip. Returns the row that owns the
-- uq_tasks_active_page_fetch index slot.
SELECT *
FROM tasks
WHERE kind = 'PAGE_FETCH'
  AND url = $1
  AND status IN ('PENDING', 'RUNNING')
LIMIT 1;
