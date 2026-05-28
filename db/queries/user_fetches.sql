-- name: CreateUserFetch :one
INSERT INTO fetches (user_id)
VALUES (sqlc.narg(user_id))
RETURNING *;

-- name: GetUserFetch :one
SELECT *
FROM fetches
WHERE id = $1
LIMIT 1;

-- name: CreateUserFetchItem :one
INSERT INTO fetch_items (
    fetch_id,
    candidate_id,
    task_id,
    snapshot_status
) VALUES (
    sqlc.arg(fetch_id),
    sqlc.arg(candidate_id),
    sqlc.narg(task_id),
    sqlc.narg(snapshot_status)
)
RETURNING *;

-- name: ListUserFetchItems :many
SELECT
    i.fetch_id,
    i.candidate_id,
    i.task_id,
    i.snapshot_status,
    i.created_at,
    t.status AS task_status
FROM fetch_items i
LEFT JOIN tasks t ON t.id = i.task_id
WHERE i.fetch_id = $1
ORDER BY i.created_at ASC;

-- name: GetUserFetchProgress :one
-- Aggregates item status using COALESCE(snapshot_status, tasks.status).
-- Returns candidate IDs grouped by status plus a derived `terminal` flag (all
-- items in COMPLETED / FAILED / ALREADY_COMPLETE).
WITH resolved AS (
    SELECT
        i.candidate_id,
        COALESCE(i.snapshot_status, t.status::text) AS status
    FROM fetch_items i
    LEFT JOIN tasks t ON t.id = i.task_id
    WHERE i.fetch_id = $1
)
SELECT
    (SELECT COUNT(*) FROM resolved)                                            AS total,
    ARRAY(
        SELECT candidate_id FROM resolved
        WHERE status = 'PENDING'
        ORDER BY candidate_id
    )::uuid[]                                                                  AS pending_candidate_ids,
    ARRAY(
        SELECT candidate_id FROM resolved
        WHERE status = 'RUNNING'
        ORDER BY candidate_id
    )::uuid[]                                                                  AS running_candidate_ids,
    ARRAY(
        SELECT candidate_id FROM resolved
        WHERE status = 'COMPLETED'
        ORDER BY candidate_id
    )::uuid[]                                                                  AS completed_candidate_ids,
    ARRAY(
        SELECT candidate_id FROM resolved
        WHERE status = 'FAILED'
        ORDER BY candidate_id
    )::uuid[]                                                                  AS failed_candidate_ids,
    ARRAY(
        SELECT candidate_id FROM resolved
        WHERE status = 'ALREADY_COMPLETE'
        ORDER BY candidate_id
    )::uuid[]                                                                  AS already_complete_candidate_ids,
    ((SELECT COUNT(*) FROM resolved) > 0 AND (SELECT COUNT(*) FROM resolved
        WHERE status IN ('COMPLETED', 'FAILED', 'ALREADY_COMPLETE')
    ) = (SELECT COUNT(*) FROM resolved))                                        AS terminal;

-- name: MarkUserFetchCompleted :exec
-- Sets completed_at on transition to terminal. Idempotent (WHERE clause
-- guards against double-set). v1 callers may skip this — progress endpoint
-- computes terminal on-the-fly. Reserved for v2 notification dispatcher.
UPDATE fetches
SET completed_at = NOW()
WHERE id = $1
  AND completed_at IS NULL;
