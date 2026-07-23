#!/usr/bin/env bash
set -euo pipefail

ROOTCTL_USER="${POSTGRES_ROOTCTL_USER:-prism_rootctl}"
ROOTCTL_PASSWORD_FILE="${POSTGRES_ROOTCTL_PASSWORD_FILE:-.secrets/pg-rootctl}"
ADMIN_PASSWORD_FILE="${POSTGRES_ADMIN_PASSWORD_FILE:-.secrets/pg-admin}"
PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5432}"
PG_USER="${POSTGRES_ADMIN_USER:-postgres}"
PG_DB="${POSTGRES_DB:-prism}"

if [[ ! -f "$ROOTCTL_PASSWORD_FILE" ]]; then
    printf 'missing rootctl password file: %s\n' "$ROOTCTL_PASSWORD_FILE" >&2
    exit 1
fi
if [[ ! -f "$ADMIN_PASSWORD_FILE" ]]; then
    printf 'missing PostgreSQL admin password file: %s\n' "$ADMIN_PASSWORD_FILE" >&2
    exit 1
fi
if [[ ! "$ROOTCTL_USER" =~ ^[a-z_][a-z0-9_]*$ || ! "$PG_DB" =~ ^[a-z_][a-z0-9_]*$ ]]; then
    printf '%s\n' 'invalid PostgreSQL identifier in rootctl provisioning settings' >&2
    exit 1
fi

rootctl_password="$(<"$ROOTCTL_PASSWORD_FILE")"
admin_password="$(<"$ADMIN_PASSWORD_FILE")"
rootctl_password_escaped="${rootctl_password//\'/\'\'}"
export PGPASSWORD="$admin_password"

psql_args=(--no-password --host "$PG_HOST" --port "$PG_PORT" --username "$PG_USER" --dbname "$PG_DB")
psql "${psql_args[@]}" <<SQL
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '${ROOTCTL_USER}') THEN
        CREATE ROLE "${ROOTCTL_USER}" LOGIN PASSWORD '${rootctl_password_escaped}';
    ELSE
        ALTER ROLE "${ROOTCTL_USER}" LOGIN PASSWORD '${rootctl_password_escaped}';
    END IF;
    ALTER ROLE "${ROOTCTL_USER}" NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
END
\$\$;
REVOKE ALL ON DATABASE "${PG_DB}" FROM "${ROOTCTL_USER}";
GRANT CONNECT ON DATABASE "${PG_DB}" TO "${ROOTCTL_USER}";
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM "${ROOTCTL_USER}";
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM "${ROOTCTL_USER}";
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM "${ROOTCTL_USER}";
GRANT USAGE ON SCHEMA public TO "${ROOTCTL_USER}";
GRANT EXECUTE ON FUNCTION prism_root_init(UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) TO "${ROOTCTL_USER}";
GRANT EXECUTE ON FUNCTION prism_root_check(TEXT, TEXT) TO "${ROOTCTL_USER}";
GRANT EXECUTE ON FUNCTION prism_root_create_admin(TEXT, TEXT, UUID, TEXT, TEXT, TEXT, TIMESTAMPTZ) TO "${ROOTCTL_USER}";
GRANT EXECUTE ON FUNCTION prism_root_revoke_all(TEXT, TEXT) TO "${ROOTCTL_USER}";
SQL
