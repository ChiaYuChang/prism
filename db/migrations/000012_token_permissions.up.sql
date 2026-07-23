BEGIN;

ALTER TABLE tokens ADD COLUMN permissions SMALLINT;

UPDATE tokens
SET permissions = CASE type
    WHEN 'root' THEN 128
    WHEN 'admin' THEN 97
    WHEN 'user' THEN 1
    WHEN 'worker' THEN 0
END,
    revoked_at = CASE WHEN type = 'worker' THEN COALESCE(revoked_at, NOW()) ELSE revoked_at END;

ALTER TABLE tokens ALTER COLUMN permissions SET NOT NULL;

ALTER TABLE tokens DROP CONSTRAINT tokens_type_check;
ALTER TABLE tokens ADD CONSTRAINT tokens_permissions_range_check
    CHECK (permissions BETWEEN 0 AND 255);
ALTER TABLE tokens ADD CONSTRAINT tokens_type_permissions_check CHECK (
    (type = 'root' AND permissions = 128)
    OR (type = 'admin' AND permissions <> 0 AND (permissions & 32) = 32 AND (permissions & ~97) = 0)
    OR (type = 'user' AND permissions = 1)
    OR (type = 'worker' AND permissions = 0 AND revoked_at IS NOT NULL)
);

CREATE INDEX idx_tokens_active_permissions_expires_at
    ON tokens (permissions, expires_at)
    WHERE revoked_at IS NULL;

COMMIT;
