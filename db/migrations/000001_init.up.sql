BEGIN;

CREATE EXTENSION IF NOT EXISTS "vector";

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'source_type') THEN
        CREATE TYPE source_type AS ENUM ('PARTY', 'MEDIA');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'model_type') THEN
        CREATE TYPE model_type AS ENUM ('EXTRACTOR', 'EMBEDDER', 'ANALYZER');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'content_type') THEN
        CREATE TYPE content_type AS ENUM ('PARTY_RELEASE', 'ARTICLE', 'SOCIAL');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'embedding_category') THEN
        CREATE TYPE embedding_category AS ENUM ('TITLE', 'CONTENT', 'BRIEF');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'entity_type') THEN
        CREATE TYPE entity_type AS ENUM (
            'person',
            'party',
            'government_agency',
            'legislative_body',
            'judicial_body',
            'military',
            'foreign_government',
            'organization',
            'media',
            'civic_group',
            'location',
            'other'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'candidate_ingestion_method') THEN
        CREATE TYPE candidate_ingestion_method AS ENUM ('DIRECTORY', 'SEARCH', 'SUBSCRIPTION', 'MANUAL');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'task_status') THEN
        CREATE TYPE task_status AS ENUM ('PENDING', 'RUNNING', 'FAILED', 'COMPLETED');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'task_kind') THEN
        CREATE TYPE task_kind AS ENUM ('DIRECTORY_FETCH', 'KEYWORD_SEARCH', 'PAGE_FETCH');
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS models (
    id           SMALLSERIAL PRIMARY KEY,
    name         VARCHAR(64) UNIQUE NOT NULL,
    provider     VARCHAR(32) NOT NULL,
    type         model_type NOT NULL,
    publish_date DATE,
    url          TEXT,
    tag          VARCHAR(32),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_models_type_name ON models(type, name);

CREATE TABLE IF NOT EXISTS sources (
    abbr        VARCHAR(16) PRIMARY KEY,
    name        VARCHAR(128) NOT NULL,
    type        source_type NOT NULL,
    base_url    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sources_base_url ON sources(base_url);
CREATE INDEX IF NOT EXISTS idx_sources_deleted_at ON sources(deleted_at);

CREATE TABLE IF NOT EXISTS batches (
    id                       UUID PRIMARY KEY,
    source_type              source_type NOT NULL,
    trace_id                 VARCHAR(100),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at             TIMESTAMPTZ,
    published_at             TIMESTAMPTZ,
    last_publish_attempt_at  TIMESTAMPTZ,
    publish_retry_count      INT NOT NULL DEFAULT 0,
    publish_error            TEXT,
    stalled_at               TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_batches_source_type_created_at ON batches(source_type, created_at);
CREATE INDEX IF NOT EXISTS idx_batches_ready_to_publish
    ON batches(completed_at)
    WHERE completed_at IS NOT NULL AND published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_batches_open_created_at
    ON batches(created_at)
    WHERE completed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_batches_stalled_at ON batches(stalled_at);

CREATE TABLE IF NOT EXISTS candidates (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    batch_id         UUID,
    source_abbr      VARCHAR(16) NOT NULL REFERENCES sources(abbr),
    trace_id         VARCHAR(100) NOT NULL,
    fingerprint      CHAR(32) UNIQUE NOT NULL,
    url              TEXT NOT NULL,
    title            TEXT NOT NULL,
    description      TEXT,
    ingestion_method candidate_ingestion_method NOT NULL,
    metadata         JSONB,
    published_at     TIMESTAMPTZ,
    discovered_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_candidates_source_abbr ON candidates(source_abbr);
CREATE INDEX IF NOT EXISTS idx_candidates_batch_id ON candidates(batch_id);
CREATE INDEX IF NOT EXISTS idx_candidates_source_published_at ON candidates(source_abbr, published_at);
CREATE INDEX IF NOT EXISTS idx_candidates_discovered_at ON candidates(discovered_at);
CREATE INDEX IF NOT EXISTS idx_candidates_trace_id ON candidates(trace_id);
CREATE INDEX IF NOT EXISTS idx_candidates_url ON candidates(url);

CREATE TABLE IF NOT EXISTS contents (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    batch_id     UUID,
    type         content_type NOT NULL,
    source_abbr  VARCHAR(16) NOT NULL REFERENCES sources(abbr),
    candidate_id UUID UNIQUE REFERENCES candidates(id) ON DELETE SET NULL,
    url          TEXT UNIQUE NOT NULL,
    title        TEXT NOT NULL,
    content      TEXT NOT NULL,
    author       VARCHAR(64),
    trace_id     VARCHAR(100) NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    fetched_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ,
    metadata     JSONB
);

CREATE INDEX IF NOT EXISTS idx_contents_type ON contents(type);
CREATE INDEX IF NOT EXISTS idx_contents_batch_id ON contents(batch_id);
CREATE INDEX IF NOT EXISTS idx_contents_source_abbr ON contents(source_abbr);
CREATE INDEX IF NOT EXISTS idx_contents_candidate_id ON contents(candidate_id);
CREATE INDEX IF NOT EXISTS idx_contents_trace_id ON contents(trace_id);
CREATE INDEX IF NOT EXISTS idx_contents_published_at ON contents(published_at);
CREATE INDEX IF NOT EXISTS idx_contents_deleted_at ON contents(deleted_at);

CREATE TABLE IF NOT EXISTS prompts (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    hash        CHAR(64) UNIQUE NOT NULL,
    path        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_prompts_path ON prompts(path);

CREATE TABLE IF NOT EXISTS content_extractions (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    content_id     UUID NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    model_id       SMALLINT NOT NULL REFERENCES models(id),
    prompt_id      UUID NOT NULL REFERENCES prompts(id),
    schema_name    TEXT NOT NULL DEFAULT 'extraction_result',
    schema_version INT NOT NULL DEFAULT 1,
    title          TEXT NOT NULL,
    summary        TEXT NOT NULL,
    raw_result     JSONB NOT NULL,
    trace_id       VARCHAR(100) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_extractions_content_id ON content_extractions(content_id);
CREATE INDEX IF NOT EXISTS idx_content_extractions_model_id ON content_extractions(model_id);
CREATE INDEX IF NOT EXISTS idx_content_extractions_prompt_id ON content_extractions(prompt_id);
CREATE INDEX IF NOT EXISTS idx_content_extractions_trace_id ON content_extractions(trace_id);
CREATE INDEX IF NOT EXISTS idx_content_extractions_content_created_at ON content_extractions(content_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_content_extractions_snapshot
    ON content_extractions(content_id, model_id, prompt_id, schema_version);

CREATE TABLE IF NOT EXISTS entities (
    id          SERIAL PRIMARY KEY,
    canonical   TEXT NOT NULL,
    type        entity_type NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_entities_canonical_type ON entities(canonical, type);
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);

CREATE TABLE IF NOT EXISTS content_extraction_entities (
    extraction_id UUID NOT NULL REFERENCES content_extractions(id) ON DELETE CASCADE,
    entity_id     INT NOT NULL REFERENCES entities(id) ON DELETE RESTRICT,
    surface       TEXT NOT NULL,
    ordinal       SMALLINT,
    PRIMARY KEY (extraction_id, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_cee_extraction_ordinal ON content_extraction_entities(extraction_id, ordinal);

CREATE TABLE IF NOT EXISTS content_extraction_topics (
    extraction_id UUID NOT NULL REFERENCES content_extractions(id) ON DELETE CASCADE,
    topic_text    TEXT NOT NULL,
    ordinal       SMALLINT,
    PRIMARY KEY (extraction_id, topic_text)
);

CREATE INDEX IF NOT EXISTS idx_cet_extraction_ordinal ON content_extraction_topics(extraction_id, ordinal);

CREATE TABLE IF NOT EXISTS content_extraction_phrases (
    id            BIGSERIAL PRIMARY KEY,
    extraction_id UUID NOT NULL REFERENCES content_extractions(id) ON DELETE CASCADE,
    phrase        TEXT NOT NULL,
    ordinal       SMALLINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_content_extraction_phrases
    ON content_extraction_phrases(extraction_id, phrase);
CREATE INDEX IF NOT EXISTS idx_content_extraction_phrases_ordinal
    ON content_extraction_phrases(extraction_id, ordinal);

CREATE TABLE IF NOT EXISTS tasks (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    batch_id       UUID NOT NULL,
    kind           task_kind NOT NULL,
    source_type    source_type NOT NULL,
    source_abbr    VARCHAR(16) NOT NULL REFERENCES sources(abbr),
    url            TEXT NOT NULL,
    payload        JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_hash   CHAR(64),
    meta           JSONB,
    trace_id       VARCHAR(100) NOT NULL,
    frequency      INTERVAL SECOND(0),
    next_run_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ,
    status         task_status NOT NULL DEFAULT 'PENDING',
    retry_count    INT NOT NULL DEFAULT 0,
    last_run_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tasks_batch_id ON tasks(batch_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_expiry ON tasks(expires_at);
CREATE INDEX IF NOT EXISTS idx_tasks_schedule ON tasks(next_run_at, frequency);
CREATE INDEX IF NOT EXISTS idx_tasks_trace_id ON tasks(trace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_source_abbr ON tasks(source_abbr);
CREATE INDEX IF NOT EXISTS idx_tasks_kind_source_type ON tasks(kind, source_type);
CREATE INDEX IF NOT EXISTS idx_tasks_url ON tasks(url);

-- Prevent duplicate active KEYWORD_SEARCH tasks for the same (source, phrase).
-- payload_hash = hex(SHA-256(canonical JSON payload)), computed in application code.
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_active_payload
    ON tasks(source_abbr, kind, payload_hash)
    WHERE status IN ('PENDING', 'RUNNING') AND payload_hash IS NOT NULL;

-- Prevent duplicate active PAGE_FETCH tasks for the same URL.
-- URL is the natural identity for page fetch; payload_hash is not used.
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_active_page_fetch
    ON tasks(kind, url)
    WHERE kind = 'PAGE_FETCH' AND status IN ('PENDING', 'RUNNING');

ALTER TABLE tasks SET (fillfactor = 80);

COMMENT ON TABLE candidates IS 'Article briefs (title/url/desc) before full-page fetch. Discovery terminal asset.';
COMMENT ON COLUMN candidates.fingerprint IS 'Dedup key (SHA-256[:16] hex of URL+title+published_at). Not a separate table.';
COMMENT ON TABLE contents IS 'Full fetched article. 1:1 with candidates via UNIQUE candidate_id.';
COMMENT ON TABLE batches IS 'Groups one cron/trigger run so planner can detect completion. id used in tasks.batch_id and copied into candidates/contents.';
COMMENT ON TABLE tasks IS 'Runnable request-oriented work unit. Scheduler claims with FOR UPDATE SKIP LOCKED.';
COMMENT ON COLUMN tasks.payload_hash IS 'SHA-256(canonical JSON payload), hex. KEYWORD_SEARCH dedup via uq_tasks_active_payload. PAGE_FETCH dedups on url instead.';
COMMENT ON COLUMN tasks.payload IS 'Request details (e.g. {query, site} for KEYWORD_SEARCH). Search keywords belong here, not as columns.';
COMMENT ON TABLE prompts IS 'Prompt asset registry. hash = SHA-256(body), used to pin extraction provenance.';
COMMENT ON TABLE content_extractions IS 'One structured extraction per (content, model, prompt, schema_version). Append-only snapshot.';

-- ----------------------------------------------------------------------------
-- User-facing fetch layer (Phase 2.7)
--
-- Parallel to `batches` but serves a different purpose. `batches` is system-
-- internal (groups one cron / discovery / planner trigger; gates Planner's
-- KEYWORD_SEARCH emission). `fetches` is user-facing (groups the candidates
-- one user selected in one POST /page_fetch call; gates user notification,
-- not system work). Multiple fetches can share the same underlying active
-- task — items snapshot which task is doing the work and aggregate status
-- via COALESCE(snapshot_status, tasks.status). No fetch observes another's
-- existence.
--
-- Names are unprefixed (`fetches`, `fetch_items`) so a future migration can
-- move them into a dedicated `user` schema (`user.fetches`, `user.fetch_items`,
-- `user.users`) via `ALTER TABLE ... SET SCHEMA "user"` without renaming.
--
-- See docs/plan/spec.md §6 for the full design clarification.
-- ----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS fetches (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id       UUID,                                   -- nullable: reserved for multi-user RBAC
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ                             -- nullable: persisted hook for v2 notifications
);

CREATE INDEX IF NOT EXISTS idx_fetches_user_id
    ON fetches(user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_fetches_open
    ON fetches(created_at)
    WHERE completed_at IS NULL;

CREATE TABLE IF NOT EXISTS fetch_items (
    fetch_id          UUID NOT NULL REFERENCES fetches(id) ON DELETE CASCADE,
    candidate_id      UUID NOT NULL REFERENCES candidates(id),
    task_id           UUID REFERENCES tasks(id) ON DELETE SET NULL,
    snapshot_status   TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (fetch_id, candidate_id)
);

-- Reverse lookup ("which fetches reference this task") for the aggregator
-- and for live progress fan-out.
CREATE INDEX IF NOT EXISTS idx_fetch_items_task_id
    ON fetch_items(task_id)
    WHERE task_id IS NOT NULL;

COMMENT ON TABLE fetches IS
    'User-facing observation layer for POST /page_fetch. Groups one user submission. Parallel to batches; see docs/plan/spec.md §6.';
COMMENT ON COLUMN fetches.user_id IS
    'Nullable in v1 (single-user dev). Filter target for multi-user RBAC.';
COMMENT ON COLUMN fetches.completed_at IS
    'Persisted in v1 but unused; v2 notification dispatcher will set on transition.';
COMMENT ON TABLE fetch_items IS
    'One row per (fetch, candidate). task_id may point at a shared active task created by another fetch — task fan-out is internal and never user-visible.';
COMMENT ON COLUMN fetch_items.snapshot_status IS
    'NULL for live items (status comes from tasks.status). Set to ALREADY_COMPLETE when the candidate already had contents at submit time.';

COMMIT;
