DROP INDEX IF EXISTS idx_batches_parent_id;

ALTER TABLE batches
DROP COLUMN IF EXISTS parent_id;
