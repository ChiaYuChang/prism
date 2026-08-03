-- name: CreateAnalysisRun :one
INSERT INTO analysis_runs (
    id,
    user_id,
    fetch_id,
    topic,
    brief,
    fetch_failure_policy,
    status,
    original_selected_candidate_ids,
    unavailable_candidate_ids
) VALUES (
    sqlc.arg(id),
    sqlc.narg(user_id),
    sqlc.arg(fetch_id),
    sqlc.arg(topic),
    sqlc.arg(brief),
    sqlc.arg(fetch_failure_policy),
    sqlc.arg(status),
    sqlc.arg(original_selected_candidate_ids),
    sqlc.arg(unavailable_candidate_ids)
)
RETURNING *;

-- name: GetAnalysisRunByID :one
SELECT * FROM analysis_runs WHERE id = $1;

-- name: GetAnalysisRunByFetchID :one
SELECT * FROM analysis_runs WHERE fetch_id = $1;

-- name: ListAnalysisRunItems :many
SELECT
    i.candidate_id,
    i.task_id,
    i.snapshot_status,
    COALESCE(t.status::text, '') AS task_status,
    c.id AS content_id
FROM fetch_items i
LEFT JOIN tasks t ON t.id = i.task_id
LEFT JOIN contents c ON c.candidate_id = i.candidate_id AND c.deleted_at IS NULL
WHERE i.fetch_id = $1
  AND i.snapshot_status IS DISTINCT FROM 'CANCELLED'
ORDER BY i.created_at ASC;

-- name: SetAnalysisRunManifest :one
UPDATE analysis_runs
SET status = 'READY_TO_ANALYZE',
    ready_candidate_ids = sqlc.arg(ready_candidate_ids),
    ready_content_ids = sqlc.arg(ready_content_ids),
    failed_candidate_ids = sqlc.arg(failed_candidate_ids),
    confirmed_at = NOW(),
    failure_code = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND status IN ('FETCHING', 'AWAITING_RESOLUTION')
RETURNING *;

-- name: SetAnalysisRunStatus :one
UPDATE analysis_runs
SET status = sqlc.arg(status),
    failed_candidate_ids = sqlc.arg(failed_candidate_ids),
    failure_code = sqlc.narg(failure_code),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetAnalysisRunRoot :one
UPDATE analysis_runs
SET root_batch_id = sqlc.arg(root_batch_id),
    status = 'ANALYZING',
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetAnalysisRunExecution :one
UPDATE analysis_runs
SET execution_id = COALESCE(execution_id, $1),
    updated_at = clock_timestamp()
WHERE id = $2
  AND (execution_id IS NULL OR execution_id = $1)
RETURNING *;

-- name: CancelAnalysisRunItems :exec
UPDATE fetch_items
SET snapshot_status = 'CANCELLED'
WHERE fetch_id = $1
  AND snapshot_status IS DISTINCT FROM 'CANCELLED';
