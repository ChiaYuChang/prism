BEGIN;

ALTER TABLE tokens DROP CONSTRAINT tokens_type_permissions_check;
ALTER TABLE tokens DROP CONSTRAINT tokens_permissions_range_check;
ALTER TABLE tokens ADD CONSTRAINT tokens_type_check
    CHECK (type IN ('root', 'admin', 'user', 'worker'));
DROP INDEX IF EXISTS idx_tokens_active_permissions_expires_at;
ALTER TABLE tokens DROP COLUMN permissions;

COMMIT;
