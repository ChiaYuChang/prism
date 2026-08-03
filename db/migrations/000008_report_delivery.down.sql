BEGIN;

DROP TABLE IF EXISTS report_audit_events;

ALTER TABLE analysis_runs
    DROP CONSTRAINT IF EXISTS analysis_runs_report_execution_pair_check,
    DROP CONSTRAINT IF EXISTS analysis_runs_root_execution_pair_check,
    DROP CONSTRAINT IF EXISTS analysis_runs_report_execution_fk,
    DROP CONSTRAINT IF EXISTS analysis_runs_root_execution_fk,
    DROP CONSTRAINT IF EXISTS analysis_runs_execution_fk,
    DROP COLUMN IF EXISTS report_id,
    DROP COLUMN IF EXISTS execution_id;

DROP TABLE IF EXISTS reports;

ALTER TABLE batches
    DROP CONSTRAINT IF EXISTS batches_execution_id_key,
    DROP CONSTRAINT IF EXISTS batches_analysis_execution_purpose_check,
    DROP COLUMN IF EXISTS failure_recorded_at,
    DROP COLUMN IF EXISTS failure_task_id,
    DROP COLUMN IF EXISTS failure_kind,
    DROP COLUMN IF EXISTS analysis_execution_id;

DROP TABLE IF EXISTS analysis_executions;

COMMIT;
