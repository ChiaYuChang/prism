BEGIN;

-- 0. Enable required extensions
CREATE EXTENSION IF NOT EXISTS "vector";

-- 1. Define core enums
DO $$
BEGIN
    -- Source types: Political party or media
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'source_type') THEN
        CREATE TYPE source_type AS ENUM ('PARTY', 'MEDIA');
    END IF;
    
    -- Fingerprint status: Discovery pipeline states
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'fingerprint_status') THEN
        CREATE TYPE fingerprint_status AS ENUM ('DISCOVERED', 'ARCHIVED', 'PROCESSED');
    END IF;
    
    -- Content type: Origin of the document
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'content_type') THEN
        CREATE TYPE content_type AS ENUM ('PARTY_RELEASE', 'ARTICLE');
    END IF;

    -- Embedding category: Distinguished by focus area
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'embedding_category') THEN
        CREATE TYPE embedding_category AS ENUM ('CONTENT', 'TITLE');
    END IF;
END
$$;

-- 2. AI Models Registry
-- Retain Soft Delete: Models define the vector space and should be auditable.
CREATE TABLE IF NOT EXISTS models (
    id           SMALLSERIAL PRIMARY KEY,    
    name         VARCHAR(32) UNIQUE NOT NULL,  -- e.g., 'embedding-gemma'
    publisher    VARCHAR(32) NOT NULL,         -- e.g., 'Google'
    publish_date DATE,                         -- Release date of the model
    url          TEXT,                         -- Documentation URL
    tag          VARCHAR(16),                  -- Version or variant tag (e.g., '300m', 'v2')
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at   TIMESTAMP WITH TIME ZONE      -- Support for soft delete
);


-- 3. Sources Registry (Parties or Media outlets)
-- Retain Soft Delete: Sources are critical for content attribution.
CREATE TABLE IF NOT EXISTS sources (
    id          SERIAL PRIMARY KEY,
    abbr        VARCHAR(8) UNIQUE NOT NULL,    -- e.g., 'KMT', 'DPP', 'CNA'
    name        VARCHAR(64) NOT NULL,          -- Full display name
    type        source_type NOT NULL,
    base_url    TEXT NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at  TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_sources_lookup ON sources(abbr, base_url);
CREATE INDEX IF NOT EXISTS idx_sources_deleted_at ON sources(deleted_at) WHERE deleted_at IS NULL;

-- 4. Fingerprints Buffer (Discovery Layer)
-- Removed Soft Delete: This is a high-volume buffer; historical traces are in contents.
CREATE TABLE IF NOT EXISTS fingerprints (
    id           SERIAL PRIMARY KEY,
    fingerprint  CHAR(64) UNIQUE NOT NULL,
    source_id    INT REFERENCES sources(id),
    url          TEXT NOT NULL,
    title        TEXT,
    description  TEXT,                    
    status       fingerprint_status DEFAULT 'DISCOVERED',
    trace_id     VARCHAR(100),                 
    discovered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fp_fingerprint_lookup ON fingerprints(fingerprint);
CREATE INDEX IF NOT EXISTS idx_fp_status ON fingerprints(status);
CREATE INDEX IF NOT EXISTS idx_fp_trace_id ON fingerprints(trace_id);

-- 5. Unified Contents (Parsed and Standardized Data)
-- Retain Soft Delete: This is the primary data asset.
CREATE TABLE IF NOT EXISTS contents (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    type           content_type NOT NULL,
    source_id      INT REFERENCES sources(id),
    fingerprint_id INT UNIQUE NOT NULL REFERENCES fingerprints(id) ON DELETE CASCADE,
    url            TEXT UNIQUE NOT NULL,
    title          TEXT NOT NULL,
    content        TEXT NOT NULL,
    author         VARCHAR(64),
    trace_id       VARCHAR(100) NOT NULL,      
    publish_date   TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at     TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at     TIMESTAMP WITH TIME ZONE,   
    metadata       JSONB
);

CREATE INDEX IF NOT EXISTS idx_contents_type ON contents(type);
CREATE INDEX IF NOT EXISTS idx_contents_trace_id ON contents(trace_id);
CREATE INDEX IF NOT EXISTS idx_contents_publish_date ON contents(publish_date);
CREATE INDEX IF NOT EXISTS idx_contents_deleted_at ON contents(deleted_at) WHERE deleted_at IS NULL;

-- 6. Keywords System (Normalized Dictionary)
-- Removed Soft Delete: Dictionary items are shared across content.
CREATE TABLE IF NOT EXISTS keywords (
    id          SERIAL PRIMARY KEY,
    text        TEXT UNIQUE NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS content_keywords (
    content_id  UUID REFERENCES contents(id) ON DELETE CASCADE,
    keyword_id  INT REFERENCES keywords(id) ON DELETE CASCADE,
    PRIMARY KEY (content_id, keyword_id)
);

-- 7. Search Tasks Buffer (Active Queue)
-- Removed Soft Delete: Tasks are transient and have an explicit expires_at.
CREATE TABLE IF NOT EXISTS search_tasks (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    content_id     UUID REFERENCES contents(id) ON DELETE CASCADE, 
    phrases        TEXT[] NOT NULL,              -- Composite search phrases
    trace_id       VARCHAR(100) NOT NULL,
    expires_at     TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at     TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at     TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_search_tasks_expiry ON search_tasks(expires_at);

COMMIT;