BEGIN;

CREATE TABLE analysis_executions (
    id                  UUID PRIMARY KEY,
    report_fingerprint  CHAR(64) NOT NULL UNIQUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE batches
    ADD COLUMN analysis_execution_id UUID REFERENCES analysis_executions(id),
    ADD COLUMN failure_kind TEXT,
    ADD COLUMN failure_task_id UUID,
    ADD COLUMN failure_recorded_at TIMESTAMPTZ;

ALTER TABLE batches
    ADD CONSTRAINT batches_analysis_execution_purpose_check
        CHECK (analysis_execution_id IS NULL OR purpose = 'ANALYZER_PIPELINE_ROOT'),
    ADD CONSTRAINT batches_execution_id_key
        UNIQUE (analysis_execution_id, id);

CREATE TABLE reports (
    id                          UUID PRIMARY KEY DEFAULT uuidv7(),
    analysis_execution_id      UUID NOT NULL UNIQUE REFERENCES analysis_executions(id),
    root_batch_id               UUID NOT NULL,
    storage_uri                 TEXT NOT NULL UNIQUE,
    byte_size                   BIGINT NOT NULL CHECK (byte_size BETWEEN 0 AND 1048576),
    sha256                      CHAR(64) NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    expires_at                  TIMESTAMPTZ NOT NULL,
    artifact_missing_at         TIMESTAMPTZ,
    artifact_corrupt_at         TIMESTAMPTZ,
    artifact_corruption_reason  TEXT,
    artifact_removed_at         TIMESTAMPTZ,
    artifact_removed_by         UUID REFERENCES tokens(id),
    artifact_removal_reason     TEXT,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT reports_execution_id_key UNIQUE (analysis_execution_id, id),
    CONSTRAINT reports_execution_root_key UNIQUE (analysis_execution_id, root_batch_id),
    CONSTRAINT reports_root_key UNIQUE (root_batch_id),
    CONSTRAINT reports_root_fk
        FOREIGN KEY (analysis_execution_id, root_batch_id)
        REFERENCES batches (analysis_execution_id, id),
    CONSTRAINT reports_uri_check
        CHECK (storage_uri = 'reports/' || analysis_execution_id::text || '.md'),
    CONSTRAINT reports_expiry_check CHECK (expires_at > created_at),
    CONSTRAINT reports_corrupt_reason_check CHECK (
        (artifact_corrupt_at IS NULL AND artifact_corruption_reason IS NULL)
        OR (artifact_corrupt_at IS NOT NULL
            AND length(btrim(artifact_corruption_reason)) BETWEEN 1 AND 1024)
    ),
    CONSTRAINT reports_removed_fields_check CHECK (
        (artifact_removed_at IS NULL
            AND artifact_removed_by IS NULL
            AND artifact_removal_reason IS NULL)
        OR (artifact_removed_at IS NOT NULL
            AND artifact_removed_by IS NOT NULL
            AND length(btrim(artifact_removal_reason)) BETWEEN 1 AND 1024)
    ),
    CONSTRAINT reports_missing_corrupt_exclusive_check CHECK (
        NOT (artifact_missing_at IS NOT NULL AND artifact_corrupt_at IS NOT NULL)
    )
);

CREATE INDEX idx_reports_expires_at ON reports (expires_at);
CREATE INDEX idx_reports_missing_at ON reports (artifact_missing_at)
    WHERE artifact_missing_at IS NOT NULL;
CREATE INDEX idx_reports_removed_at ON reports (artifact_removed_at)
    WHERE artifact_removed_at IS NOT NULL;

ALTER TABLE analysis_runs
    ADD COLUMN execution_id UUID,
    ADD COLUMN report_id UUID;

ALTER TABLE analysis_runs
    ADD CONSTRAINT analysis_runs_execution_fk
        FOREIGN KEY (execution_id) REFERENCES analysis_executions(id),
    ADD CONSTRAINT analysis_runs_root_execution_fk
        FOREIGN KEY (execution_id, root_batch_id)
        REFERENCES batches (analysis_execution_id, id)
        MATCH FULL,
    ADD CONSTRAINT analysis_runs_report_execution_fk
        FOREIGN KEY (execution_id, report_id)
        REFERENCES reports (analysis_execution_id, id)
        MATCH FULL,
    ADD CONSTRAINT analysis_runs_root_execution_pair_check
        CHECK (root_batch_id IS NULL OR execution_id IS NOT NULL),
    ADD CONSTRAINT analysis_runs_report_execution_pair_check
        CHECK (report_id IS NULL OR execution_id IS NOT NULL);

CREATE TABLE report_audit_events (
    id                    UUID PRIMARY KEY DEFAULT uuidv7(),
    report_id             UUID NOT NULL,
    analysis_execution_id UUID NOT NULL,
    event_type            TEXT NOT NULL CHECK (event_type IN (
        'ADMIN_REMOVE', 'ADMIN_REMOVE_ATTEMPT', 'ARTIFACT_MISSING',
        'ARTIFACT_CORRUPT', 'ARTIFACT_REPAIRED'
    )),
    actor_token_id        UUID REFERENCES tokens(id),
    actor_component       TEXT NOT NULL CHECK (actor_component IN (
        'operator', 'report-read', 'report-recovery'
    )),
    actor_name            TEXT,
    reason                TEXT,
    request_id            TEXT,
    storage_uri           TEXT NOT NULL,
    storage_outcome       TEXT NOT NULL CHECK (storage_outcome IN (
        'NOT_ATTEMPTED', 'PENDING', 'DELETED', 'NOT_FOUND', 'FAILED'
    )),
    storage_error         TEXT,
    occurred_at           TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT report_audit_report_fk
        FOREIGN KEY (analysis_execution_id, report_id)
        REFERENCES reports (analysis_execution_id, id),
    CONSTRAINT report_audit_reason_check CHECK (
        event_type NOT IN ('ADMIN_REMOVE', 'ARTIFACT_REPAIRED')
        OR length(btrim(reason)) BETWEEN 1 AND 1024
    ),
    CONSTRAINT report_audit_actor_check CHECK (
        event_type <> 'ADMIN_REMOVE'
        OR (actor_token_id IS NOT NULL AND actor_component = 'operator')
    )
);

CREATE UNIQUE INDEX uq_report_admin_remove_event
    ON report_audit_events (report_id)
    WHERE event_type = 'ADMIN_REMOVE';
CREATE UNIQUE INDEX uq_report_availability_observation_event
    ON report_audit_events (report_id, event_type)
    WHERE event_type IN ('ARTIFACT_MISSING', 'ARTIFACT_CORRUPT');
CREATE INDEX idx_report_audit_events_report_time
    ON report_audit_events (report_id, occurred_at DESC);

COMMIT;
