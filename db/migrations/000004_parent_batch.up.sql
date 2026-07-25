ALTER TABLE batches
ADD COLUMN IF NOT EXISTS parent_id UUID REFERENCES batches(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_batches_parent_id ON batches(parent_id);
