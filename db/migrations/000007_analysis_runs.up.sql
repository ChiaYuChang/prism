BEGIN;

CREATE TABLE analysis_runs (
    id                              UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id                         UUID,
    fetch_id                        UUID NOT NULL UNIQUE REFERENCES fetches(id) ON DELETE CASCADE,
    topic                           TEXT NOT NULL DEFAULT '',
    brief                           TEXT NOT NULL DEFAULT '',
    fetch_failure_policy            TEXT NOT NULL CHECK (fetch_failure_policy IN ('STOP', 'IGNORE_FAILED')),
    status                          TEXT NOT NULL CHECK (status IN (
        'DRAFT', 'PREFLIGHT', 'WAITING_FOR_CONFIRMATION', 'FETCHING',
        'AWAITING_RESOLUTION', 'READY_TO_ANALYZE', 'ANALYZING', 'COMPLETED',
        'FAILED', 'CANCELLED'
    )),
    original_selected_candidate_ids UUID[] NOT NULL,
    unavailable_candidate_ids       UUID[] NOT NULL DEFAULT '{}'::uuid[],
    ready_candidate_ids              UUID[] NOT NULL DEFAULT '{}'::uuid[],
    ready_content_ids                UUID[] NOT NULL DEFAULT '{}'::uuid[],
    failed_candidate_ids             UUID[] NOT NULL DEFAULT '{}'::uuid[],
    root_batch_id                    UUID,
    failure_code                     TEXT,
    confirmed_at                     TIMESTAMPTZ,
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_analysis_runs_user_created
    ON analysis_runs(user_id, created_at DESC);
CREATE INDEX idx_analysis_runs_status
    ON analysis_runs(status, updated_at);

COMMIT;
