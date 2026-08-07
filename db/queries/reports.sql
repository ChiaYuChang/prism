-- name: GetReportByID :one
SELECT * FROM reports WHERE id = $1;

-- name: EnsureAnalysisExecution :exec
INSERT INTO analysis_executions (id, report_fingerprint)
VALUES ($1, $2)
ON CONFLICT (id) DO UPDATE
SET report_fingerprint = EXCLUDED.report_fingerprint;

-- name: GetReportByExecutionID :one
SELECT * FROM reports WHERE analysis_execution_id = $1;

-- name: FindReportCacheHit :one
SELECT *
FROM reports
WHERE analysis_execution_id = $1
  AND expires_at > clock_timestamp()
  AND artifact_missing_at IS NULL
  AND artifact_corrupt_at IS NULL
  AND artifact_removed_at IS NULL;

-- name: InsertReport :one
INSERT INTO reports (
    analysis_execution_id,
    root_batch_id,
    storage_uri,
    byte_size,
    sha256,
    expires_at
) VALUES (
    sqlc.arg(analysis_execution_id),
    sqlc.arg(root_batch_id),
    sqlc.arg(storage_uri),
    sqlc.arg(byte_size),
    sqlc.arg(sha256),
    sqlc.arg(expires_at)
)
ON CONFLICT (analysis_execution_id) DO NOTHING
RETURNING *;

-- name: LockReportByExecutionID :one
SELECT * FROM reports
WHERE analysis_execution_id = $1
FOR UPDATE;

-- name: LockReportTask :one
SELECT * FROM tasks WHERE id = $1 FOR UPDATE;

-- name: LockReportRoot :one
SELECT * FROM batches
WHERE id = $1
  AND analysis_execution_id = $2
  AND completed_at IS NULL
FOR UPDATE;

-- name: LockAnalysisExecution :one
SELECT id FROM analysis_executions WHERE id = $1 FOR UPDATE;

-- name: MarkReportMissing :execrows
UPDATE reports
SET artifact_missing_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = $1
  AND expires_at > clock_timestamp()
  AND artifact_missing_at IS NULL
  AND artifact_corrupt_at IS NULL
  AND artifact_removed_at IS NULL;

-- name: MarkReportCorrupt :execrows
UPDATE reports
SET artifact_corrupt_at = clock_timestamp(),
    artifact_corruption_reason = $2,
    updated_at = clock_timestamp()
WHERE id = $1
  AND artifact_corrupt_at IS NULL
  AND artifact_missing_at IS NULL
  AND artifact_removed_at IS NULL;

-- name: MarkReportRemoved :execrows
UPDATE reports
SET artifact_removed_at = clock_timestamp(),
    artifact_removed_by = sqlc.arg(actor_token_id),
    artifact_removal_reason = sqlc.arg(reason),
    updated_at = clock_timestamp()
WHERE id = sqlc.arg(id)
  AND artifact_removed_at IS NULL;

-- name: LockReportForRemoval :one
SELECT * FROM reports
WHERE analysis_execution_id = $1
FOR UPDATE;

-- name: InsertReportAuditEvent :exec
INSERT INTO report_audit_events (
    report_id,
    analysis_execution_id,
    event_type,
    actor_token_id,
    actor_component,
    actor_name,
    reason,
    request_id,
    storage_uri,
    storage_outcome,
    storage_error
) VALUES (
    sqlc.arg(report_id),
    sqlc.arg(analysis_execution_id),
    sqlc.arg(event_type),
    sqlc.narg(actor_token_id),
    sqlc.arg(actor_component),
    sqlc.narg(actor_name),
    sqlc.narg(reason),
    sqlc.narg(request_id),
    sqlc.arg(storage_uri),
    sqlc.arg(storage_outcome),
    sqlc.narg(storage_error)
);

-- name: CompleteReportTask :execrows
UPDATE tasks AS t
SET status = 'COMPLETED',
    failure_message = NULL,
    updated_at = clock_timestamp()
FROM batches AS b
WHERE t.id = sqlc.arg(task_id)
  AND t.batch_id = sqlc.arg(root_batch_id)
  AND t.kind = 'PIPELINE_STAGE'
  AND t.url = 'pipeline://stage/generate_report'
  AND t.status = 'RUNNING'
  AND b.id = t.batch_id
  AND b.analysis_execution_id = sqlc.arg(analysis_execution_id)
  AND b.completed_at IS NULL
  AND EXISTS (
      SELECT 1 FROM reports r
      WHERE r.analysis_execution_id = sqlc.arg(analysis_execution_id)
        AND r.root_batch_id = t.batch_id
        AND r.storage_uri = sqlc.arg(storage_uri)
        AND r.byte_size = sqlc.arg(byte_size)
        AND r.sha256 = sqlc.arg(sha256)
  );
