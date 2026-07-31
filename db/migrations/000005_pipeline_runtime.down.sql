BEGIN;

DROP INDEX IF EXISTS uq_tasks_batch_logical_key;
DROP INDEX IF EXISTS idx_tasks_next_task_id;
DROP INDEX IF EXISTS idx_tasks_previous_task_id;
DROP INDEX IF EXISTS idx_batches_parent_task_id;
DROP INDEX IF EXISTS idx_batches_parent_id;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS logical_key,
    DROP COLUMN IF EXISTS next_task_id,
    DROP COLUMN IF EXISTS previous_task_id;

ALTER TABLE batches
    DROP COLUMN IF EXISTS parent_task_id,
    DROP COLUMN IF EXISTS parent_id,
    DROP COLUMN IF EXISTS n_subtasks;

COMMIT;
