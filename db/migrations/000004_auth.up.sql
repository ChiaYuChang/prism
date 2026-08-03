BEGIN;

CREATE TABLE tokens (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    type TEXT NOT NULL CHECK (type IN ('root', 'admin', 'user', 'worker')),
    name TEXT NOT NULL,
    hash_algorithm TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    permissions SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    renewed_at TIMESTAMPTZ,
    rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT tokens_permissions_range_check CHECK (permissions BETWEEN 0 AND 255),
    CONSTRAINT tokens_type_permissions_check CHECK (
        (type = 'root' AND permissions = 128)
        OR (type = 'admin' AND permissions <> 0 AND (permissions & 32) = 32 AND (permissions & ~97) = 0)
        OR (type = 'user' AND permissions = 1)
        OR (type = 'worker' AND permissions = 0 AND revoked_at IS NOT NULL)
    )
);

CREATE INDEX idx_tokens_active_type_expires_at ON tokens (type, expires_at)
WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX idx_tokens_one_root ON tokens (type) WHERE type = 'root';
CREATE INDEX idx_tokens_active_permissions_expires_at ON tokens (permissions, expires_at)
WHERE revoked_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'prism_rootctl') THEN
        CREATE ROLE prism_rootctl NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION prism_root_init(
    p_id UUID, p_name TEXT, p_hash_algorithm TEXT, p_token_hash TEXT, p_expires_at TIMESTAMPTZ
) RETURNS TABLE (
    id UUID, type TEXT, name TEXT, permissions SMALLINT, hash_algorithm TEXT,
    created_at TIMESTAMPTZ, expires_at TIMESTAMPTZ, last_used_at TIMESTAMPTZ,
    renewed_at TIMESTAMPTZ, rotated_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM tokens WHERE tokens.type = 'root') THEN
        RAISE EXCEPTION 'root already exists' USING ERRCODE = '23505';
    END IF;
    RETURN QUERY
    INSERT INTO tokens (id, type, name, permissions, hash_algorithm, token_hash, expires_at)
    VALUES (p_id, 'root', p_name, 128, p_hash_algorithm, p_token_hash, p_expires_at)
    RETURNING tokens.id, tokens.type, tokens.name, tokens.permissions, tokens.hash_algorithm,
        tokens.created_at, tokens.expires_at, tokens.last_used_at, tokens.renewed_at,
        tokens.rotated_at, tokens.revoked_at;
END
$$;

CREATE OR REPLACE FUNCTION prism_root_check(p_hash_algorithm TEXT, p_token_hash TEXT)
RETURNS BOOLEAN LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
    SELECT EXISTS (
        SELECT 1 FROM tokens
        WHERE type = 'root' AND revoked_at IS NULL AND expires_at > NOW()
          AND hash_algorithm = p_hash_algorithm AND token_hash = p_token_hash
    )
$$;

CREATE OR REPLACE FUNCTION prism_root_create_admin(
    p_root_hash_algorithm TEXT, p_root_token_hash TEXT, p_id UUID, p_name TEXT,
    p_hash_algorithm TEXT, p_token_hash TEXT, p_expires_at TIMESTAMPTZ
) RETURNS TABLE (
    id UUID, type TEXT, name TEXT, permissions SMALLINT, hash_algorithm TEXT,
    created_at TIMESTAMPTZ, expires_at TIMESTAMPTZ, last_used_at TIMESTAMPTZ,
    renewed_at TIMESTAMPTZ, rotated_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
BEGIN
    IF NOT prism_root_check(p_root_hash_algorithm, p_root_token_hash) THEN
        RAISE EXCEPTION 'root authentication failed' USING ERRCODE = '28000';
    END IF;
    RETURN QUERY
    INSERT INTO tokens (id, type, name, permissions, hash_algorithm, token_hash, expires_at)
    VALUES (p_id, 'admin', p_name, 97, p_hash_algorithm, p_token_hash, p_expires_at)
    RETURNING tokens.id, tokens.type, tokens.name, tokens.permissions, tokens.hash_algorithm,
        tokens.created_at, tokens.expires_at, tokens.last_used_at, tokens.renewed_at,
        tokens.rotated_at, tokens.revoked_at;
END
$$;

CREATE OR REPLACE FUNCTION prism_root_revoke_all(p_hash_algorithm TEXT, p_token_hash TEXT)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE revoked_count BIGINT;
BEGIN
    IF NOT prism_root_check(p_hash_algorithm, p_token_hash) THEN
        RAISE EXCEPTION 'root authentication failed' USING ERRCODE = '28000';
    END IF;
    WITH revoked AS (
        UPDATE tokens SET revoked_at = NOW()
        WHERE revoked_at IS NULL AND type <> 'root'
        RETURNING id
    ) SELECT COUNT(*) INTO revoked_count FROM revoked;
    RETURN revoked_count;
END
$$;

REVOKE ALL ON TABLE tokens FROM prism_rootctl;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM prism_rootctl;
REVOKE ALL ON FUNCTION prism_root_init(UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) FROM PUBLIC;
REVOKE ALL ON FUNCTION prism_root_check(TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION prism_root_create_admin(TEXT, TEXT, UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) FROM PUBLIC;
REVOKE ALL ON FUNCTION prism_root_revoke_all(TEXT, TEXT) FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO prism_rootctl;
GRANT EXECUTE ON FUNCTION prism_root_init(UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) TO prism_rootctl;
GRANT EXECUTE ON FUNCTION prism_root_check(TEXT, TEXT) TO prism_rootctl;
GRANT EXECUTE ON FUNCTION prism_root_create_admin(TEXT, TEXT, UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) TO prism_rootctl;
GRANT EXECUTE ON FUNCTION prism_root_revoke_all(TEXT, TEXT) TO prism_rootctl;

COMMIT;
