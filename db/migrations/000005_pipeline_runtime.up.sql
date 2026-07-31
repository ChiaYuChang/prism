BEGIN;

ALTER TABLE batches
    ADD COLUMN n_subtasks INTEGER CHECK (n_subtasks >= 0),
    ADD COLUMN parent_id UUID REFERENCES batches(id) ON DELETE SET NULL,
    ADD COLUMN parent_task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    ADD COLUMN succeeded BOOLEAN,
    ADD COLUMN pipeline_published_at TIMESTAMPTZ,
    ADD COLUMN pipeline_publish_retry_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN pipeline_publish_error TEXT;

ALTER TABLE tasks
    ADD COLUMN previous_task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    ADD COLUMN next_task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    ADD COLUMN logical_key TEXT;

CREATE INDEX idx_batches_parent_task_id ON batches(parent_task_id);
CREATE INDEX idx_batches_parent_id ON batches(parent_id);
CREATE UNIQUE INDEX uq_batches_parent_task_id ON batches(parent_task_id)
WHERE parent_task_id IS NOT NULL;
CREATE INDEX idx_tasks_previous_task_id ON tasks(previous_task_id);
CREATE INDEX idx_tasks_next_task_id ON tasks(next_task_id);
CREATE UNIQUE INDEX uq_tasks_batch_logical_key
    ON tasks(batch_id, logical_key)
    WHERE logical_key IS NOT NULL;

COMMENT ON COLUMN batches.n_subtasks IS 'Expected number of direct tasks; NULL means expansion is not complete.';
COMMENT ON COLUMN batches.parent_task_id IS 'Stage-control task that owns this child batch.';
COMMENT ON COLUMN batches.succeeded IS 'Whether a finished batch completed without failed or cancelled direct tasks.';
COMMENT ON COLUMN batches.pipeline_published_at IS 'Pipeline completion notification publish timestamp.';
COMMENT ON COLUMN tasks.logical_key IS 'Stable idempotency key scoped to the owning batch.';
COMMENT ON COLUMN tasks.previous_task_id IS 'Successful predecessor required before this task can be claimed.';
COMMENT ON COLUMN tasks.next_task_id IS 'Successor stage-control task awakened after this task succeeds.';

COMMIT;
