#!/bin/bash
# Exit immediately if a command exits with a non-zero status
set -e

# Define the path for the Secret mounted within the container
INTERNAL_SECRET_PATH="${POSTGRES_APP_PASSWORD_FILE}"

# Retrieve the application user password from the Secret file
if [ -f "$INTERNAL_SECRET_PATH" ]; then
    # Primary: Read from Docker Secrets mount point
    APP_PASSWORD=$(cat "$INTERNAL_SECRET_PATH")
elif [ -n "$POSTGRES_APP_PASSWORD_FILE" ] && [ -f "$POSTGRES_APP_PASSWORD_FILE" ]; then
    # Fallback: Read from the path provided by the environment variable
    APP_PASSWORD=$(cat "$POSTGRES_APP_PASSWORD_FILE")
else
    echo "Error: Application database password not found in $INTERNAL_SECRET_PATH"
    exit 1
fi

# Execute database initialization using the PostgreSQL superuser
# Variables are expanded by the shell here because the HEREDOC delimiter is not quoted.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- Create the application user if it does not already exist (idempotency check)
    DO \$$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '$POSTGRES_APP_USER') THEN
            CREATE ROLE $POSTGRES_APP_USER WITH LOGIN PASSWORD '$APP_PASSWORD';
        END IF;
    END
    \$$;

    -- Grant database connection and usage permissions
    GRANT ALL PRIVILEGES ON DATABASE $POSTGRES_DB TO $POSTGRES_APP_USER;

    -- Grant usage on the public schema
    GRANT USAGE ON SCHEMA public TO $POSTGRES_APP_USER;

    -- Set default privileges to ensure the user has access to tables/sequences created in the future
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO $POSTGRES_APP_USER;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO $POSTGRES_APP_USER;
EOSQL

echo "Application user '$POSTGRES_APP_USER' initialized successfully."