-- name: MarkSchedulesConfigAbsent :exec
UPDATE schedules
SET config_present = false,
    updated_at = NOW()
WHERE config_present = true;

-- name: UpsertSchedule :one
INSERT INTO schedules (
    id,
    name,
    enabled,
    config_present,
    config_hash,
    kind,
    source_type,
    source_abbr,
    url,
    payload,
    meta,
    frequency,
    run_on_insert,
    next_fire_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(name),
    sqlc.arg(enabled),
    true,
    sqlc.arg(config_hash),
    sqlc.arg(kind),
    sqlc.arg(source_type),
    sqlc.arg(source_abbr),
    sqlc.arg(url),
    COALESCE(sqlc.narg(payload)::jsonb, '{}'::jsonb),
    sqlc.narg(meta)::jsonb,
    sqlc.arg(frequency),
    sqlc.arg(run_on_insert),
    CASE
        WHEN sqlc.arg(run_on_insert)::boolean THEN NOW()
        ELSE NOW() + sqlc.arg(frequency)
    END
)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    enabled = EXCLUDED.enabled,
    config_present = true,
    config_hash = EXCLUDED.config_hash,
    kind = EXCLUDED.kind,
    source_type = EXCLUDED.source_type,
    source_abbr = EXCLUDED.source_abbr,
    url = EXCLUDED.url,
    payload = EXCLUDED.payload,
    meta = EXCLUDED.meta,
    frequency = EXCLUDED.frequency,
    run_on_insert = EXCLUDED.run_on_insert,
    next_fire_at = CASE
        WHEN schedules.config_hash IS DISTINCT FROM EXCLUDED.config_hash THEN NOW()
        WHEN schedules.frequency IS DISTINCT FROM EXCLUDED.frequency
          OR schedules.run_on_insert IS DISTINCT FROM EXCLUDED.run_on_insert
        THEN CASE
            WHEN schedules.last_fire_at IS NOT NULL THEN schedules.last_fire_at + EXCLUDED.frequency
            WHEN EXCLUDED.run_on_insert THEN NOW()
            ELSE NOW() + EXCLUDED.frequency
        END
        ELSE schedules.next_fire_at
    END,
    updated_at = NOW()
RETURNING *;

-- name: ClaimDueSchedules :many
SELECT *
FROM schedules
WHERE enabled = true
  AND config_present = true
  AND next_fire_at <= NOW()
ORDER BY next_fire_at ASC, id ASC
LIMIT sqlc.arg(lim)
FOR UPDATE SKIP LOCKED;

-- name: MarkScheduleMaterialized :exec
UPDATE schedules
SET last_fire_at = NOW(),
    last_materialized_at = NOW(),
    last_materialized_task_id = sqlc.arg(task_id),
    last_error = NULL,
    next_fire_at = NOW() + frequency,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: MarkScheduleError :exec
UPDATE schedules
SET last_fire_at = NOW(),
    last_error = sqlc.arg(last_error),
    next_fire_at = NOW() + frequency,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: ListSchedules :many
SELECT *
FROM schedules
ORDER BY source_type ASC, source_abbr ASC, name ASC
LIMIT sqlc.arg(lim);
