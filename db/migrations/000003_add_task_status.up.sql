-- Up migration: Add status and lifecycle tracking to search_tasks

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'task_status') THEN
        CREATE TYPE task_status AS ENUM ('PENDING', 'RUNNING', 'FAILED', 'COMPLETED');
    END IF;
END
$$;

ALTER TABLE search_tasks 
ADD COLUMN IF NOT EXISTS status task_status DEFAULT 'PENDING',
ADD COLUMN IF NOT EXISTS retry_count INT DEFAULT 0,
ADD COLUMN IF NOT EXISTS last_run_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_search_tasks_status ON search_tasks(status);
