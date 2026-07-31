BEGIN;

CREATE TABLE schedules (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    config_present BOOLEAN NOT NULL DEFAULT TRUE,
    config_hash CHAR(64) NOT NULL,
    kind task_kind NOT NULL,
    source_type source_type NOT NULL,
    source_abbr VARCHAR(16) NOT NULL REFERENCES sources(abbr),
    url TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    meta JSONB,
    frequency INTERVAL SECOND(0) NOT NULL,
    run_on_insert BOOLEAN NOT NULL DEFAULT FALSE,
    next_fire_at TIMESTAMPTZ NOT NULL,
    last_fire_at TIMESTAMPTZ,
    last_materialized_at TIMESTAMPTZ,
    last_materialized_task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX schedules_due_idx ON schedules (next_fire_at, id)
WHERE enabled = TRUE AND config_present = TRUE;
CREATE INDEX schedules_source_idx ON schedules (source_type, source_abbr);

COMMIT;
