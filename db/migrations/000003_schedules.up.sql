CREATE TABLE schedules (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    config_present boolean NOT NULL DEFAULT true,
    config_hash char(64) NOT NULL,
    kind task_kind NOT NULL,
    source_type source_type NOT NULL,
    source_abbr varchar(16) NOT NULL REFERENCES sources(abbr),
    url text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    meta jsonb,
    frequency interval second(0) NOT NULL,
    run_on_insert boolean NOT NULL DEFAULT false,
    next_fire_at timestamptz NOT NULL,
    last_fire_at timestamptz,
    last_materialized_at timestamptz,
    last_materialized_task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE schedules IS 'Recurring schedule intent that materializes concrete task rows.';
COMMENT ON COLUMN schedules.id IS 'Stable operator-provided UUIDv7 identity. Names and source_abbr are not durable identity.';
COMMENT ON COLUMN schedules.config_present IS 'False when a previously synced YAML schedule is absent from the latest config load; absent schedules do not fire.';
COMMENT ON COLUMN schedules.next_fire_at IS 'Next time the schedule trigger should materialize a concrete task.';
COMMENT ON COLUMN schedules.last_materialized_task_id IS 'Latest task inserted or recovered by the schedule trigger.';

CREATE INDEX schedules_due_idx ON schedules (next_fire_at, id)
WHERE enabled = true AND config_present = true;

CREATE INDEX schedules_source_idx ON schedules (source_type, source_abbr);
