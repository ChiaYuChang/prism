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
        CREATE TYPE content_type AS ENUM ('PARTY_RELEASE', 'ARTICLE');
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
        CREATE TYPE task_kind AS ENUM ('DIRECTORY_FETCH', 'KEYWORD_SEARCH');
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
    id          SERIAL PRIMARY KEY,
    abbr        VARCHAR(16) UNIQUE NOT NULL,
    name        VARCHAR(128) NOT NULL,
    type        source_type NOT NULL,
    base_url    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sources_lookup ON sources(abbr, base_url);
CREATE INDEX IF NOT EXISTS idx_sources_deleted_at ON sources(deleted_at);

CREATE TABLE IF NOT EXISTS candidates (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    batch_id         UUID,
    source_id        INT NOT NULL REFERENCES sources(id),
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

CREATE INDEX IF NOT EXISTS idx_candidates_source_id ON candidates(source_id);
CREATE INDEX IF NOT EXISTS idx_candidates_batch_id ON candidates(batch_id);
CREATE INDEX IF NOT EXISTS idx_candidates_source_published_at ON candidates(source_id, published_at);
CREATE INDEX IF NOT EXISTS idx_candidates_discovered_at ON candidates(discovered_at);
CREATE INDEX IF NOT EXISTS idx_candidates_trace_id ON candidates(trace_id);
CREATE INDEX IF NOT EXISTS idx_candidates_url ON candidates(url);

CREATE TABLE IF NOT EXISTS contents (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    batch_id     UUID,
    type         content_type NOT NULL,
    source_id    INT NOT NULL REFERENCES sources(id),
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
CREATE INDEX IF NOT EXISTS idx_contents_source_id ON contents(source_id);
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
    source_id      INT NOT NULL REFERENCES sources(id),
    url            TEXT NOT NULL,
    payload        JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_hash   CHAR(64),
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
CREATE INDEX IF NOT EXISTS idx_tasks_source_id ON tasks(source_id);
CREATE INDEX IF NOT EXISTS idx_tasks_kind_source_type ON tasks(kind, source_type);
CREATE INDEX IF NOT EXISTS idx_tasks_url ON tasks(url);

-- Prevent duplicate active KEYWORD_SEARCH tasks for the same (source, phrase).
-- payload_hash = hex(SHA-256(canonical JSON payload)), computed in application code.
CREATE UNIQUE INDEX IF NOT EXISTS uq_tasks_active_payload
    ON tasks(source_id, kind, payload_hash)
    WHERE status IN ('PENDING', 'RUNNING') AND payload_hash IS NOT NULL;

ALTER TABLE tasks SET (fillfactor = 80);

COMMIT;
