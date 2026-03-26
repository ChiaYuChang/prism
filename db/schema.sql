--
-- PostgreSQL database dump
--


-- Dumped from database version 18.2 (Debian 18.2-1.pgdg12+1)
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: vector; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;


--
-- Name: EXTENSION vector; Type: COMMENT; Schema: -; Owner: 
--

COMMENT ON EXTENSION vector IS 'vector data type and ivfflat and hnsw access methods';


--
-- Name: candidate_ingestion_method; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.candidate_ingestion_method AS ENUM (
    'DIRECTORY',
    'SEARCH',
    'SUBSCRIPTION',
    'MANUAL'
);


ALTER TYPE public.candidate_ingestion_method OWNER TO postgres;

--
-- Name: content_type; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.content_type AS ENUM (
    'PARTY_RELEASE',
    'ARTICLE'
);


ALTER TYPE public.content_type OWNER TO postgres;

--
-- Name: embedding_category; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.embedding_category AS ENUM (
    'TITLE',
    'CONTENT',
    'BRIEF'
);


ALTER TYPE public.embedding_category OWNER TO postgres;

--
-- Name: entity_type; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.entity_type AS ENUM (
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


ALTER TYPE public.entity_type OWNER TO postgres;

--
-- Name: model_type; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.model_type AS ENUM (
    'EXTRACTOR',
    'EMBEDDER',
    'ANALYZER'
);


ALTER TYPE public.model_type OWNER TO postgres;

--
-- Name: source_type; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.source_type AS ENUM (
    'PARTY',
    'MEDIA'
);


ALTER TYPE public.source_type OWNER TO postgres;

--
-- Name: task_kind; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.task_kind AS ENUM (
    'DIRECTORY_FETCH',
    'PAGE_FETCH'
);


ALTER TYPE public.task_kind OWNER TO postgres;

--
-- Name: task_status; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.task_status AS ENUM (
    'PENDING',
    'RUNNING',
    'FAILED',
    'COMPLETED'
);


ALTER TYPE public.task_status OWNER TO postgres;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: candidate_embeddings_gemma_2025; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.candidate_embeddings_gemma_2025 (
    id bigint NOT NULL,
    candidate_id uuid NOT NULL,
    model_id smallint NOT NULL,
    category public.embedding_category NOT NULL,
    vector public.vector(768) NOT NULL,
    trace_id character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.candidate_embeddings_gemma_2025 OWNER TO postgres;

--
-- Name: candidate_embeddings_gemma_2025_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.candidate_embeddings_gemma_2025_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.candidate_embeddings_gemma_2025_id_seq OWNER TO postgres;

--
-- Name: candidate_embeddings_gemma_2025_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.candidate_embeddings_gemma_2025_id_seq OWNED BY public.candidate_embeddings_gemma_2025.id;


--
-- Name: candidates; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.candidates (
    id uuid DEFAULT uuidv7() NOT NULL,
    batch_id uuid,
    source_id integer NOT NULL,
    trace_id character varying(100) NOT NULL,
    fingerprint character(32) NOT NULL,
    url text NOT NULL,
    title text NOT NULL,
    description text,
    ingestion_method public.candidate_ingestion_method NOT NULL,
    metadata jsonb,
    published_at timestamp with time zone,
    discovered_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.candidates OWNER TO postgres;

--
-- Name: content_embeddings_gemma_2025; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.content_embeddings_gemma_2025 (
    id bigint NOT NULL,
    content_id uuid NOT NULL,
    model_id smallint NOT NULL,
    category public.embedding_category NOT NULL,
    vector public.vector(768) NOT NULL,
    trace_id character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.content_embeddings_gemma_2025 OWNER TO postgres;

--
-- Name: content_embeddings_gemma_2025_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.content_embeddings_gemma_2025_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.content_embeddings_gemma_2025_id_seq OWNER TO postgres;

--
-- Name: content_embeddings_gemma_2025_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.content_embeddings_gemma_2025_id_seq OWNED BY public.content_embeddings_gemma_2025.id;


--
-- Name: content_extraction_entities; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.content_extraction_entities (
    extraction_id uuid NOT NULL,
    entity_id integer NOT NULL,
    surface text NOT NULL,
    ordinal smallint
);


ALTER TABLE public.content_extraction_entities OWNER TO postgres;

--
-- Name: content_extraction_phrases; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.content_extraction_phrases (
    id bigint NOT NULL,
    extraction_id uuid NOT NULL,
    phrase text NOT NULL,
    ordinal smallint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.content_extraction_phrases OWNER TO postgres;

--
-- Name: content_extraction_phrases_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.content_extraction_phrases_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.content_extraction_phrases_id_seq OWNER TO postgres;

--
-- Name: content_extraction_phrases_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.content_extraction_phrases_id_seq OWNED BY public.content_extraction_phrases.id;


--
-- Name: content_extraction_topics; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.content_extraction_topics (
    extraction_id uuid NOT NULL,
    topic_text text NOT NULL,
    ordinal smallint
);


ALTER TABLE public.content_extraction_topics OWNER TO postgres;

--
-- Name: content_extractions; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.content_extractions (
    id uuid DEFAULT uuidv7() NOT NULL,
    content_id uuid NOT NULL,
    model_id smallint NOT NULL,
    prompt_id uuid NOT NULL,
    schema_name text DEFAULT 'extraction_result'::text NOT NULL,
    schema_version integer DEFAULT 1 NOT NULL,
    title text NOT NULL,
    summary text NOT NULL,
    raw_result jsonb NOT NULL,
    trace_id character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.content_extractions OWNER TO postgres;

--
-- Name: contents; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.contents (
    id uuid DEFAULT uuidv7() NOT NULL,
    batch_id uuid,
    type public.content_type NOT NULL,
    source_id integer NOT NULL,
    candidate_id uuid,
    url text NOT NULL,
    title text NOT NULL,
    content text NOT NULL,
    author character varying(64),
    trace_id character varying(100) NOT NULL,
    published_at timestamp with time zone NOT NULL,
    fetched_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    metadata jsonb
);


ALTER TABLE public.contents OWNER TO postgres;

--
-- Name: entities; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.entities (
    id integer NOT NULL,
    canonical text NOT NULL,
    type public.entity_type NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.entities OWNER TO postgres;

--
-- Name: entities_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.entities_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.entities_id_seq OWNER TO postgres;

--
-- Name: entities_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.entities_id_seq OWNED BY public.entities.id;


--
-- Name: models; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.models (
    id smallint NOT NULL,
    name character varying(64) NOT NULL,
    provider character varying(32) NOT NULL,
    type public.model_type NOT NULL,
    publish_date date,
    url text,
    tag character varying(32),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


ALTER TABLE public.models OWNER TO postgres;

--
-- Name: models_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.models_id_seq
    AS smallint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.models_id_seq OWNER TO postgres;

--
-- Name: models_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.models_id_seq OWNED BY public.models.id;


--
-- Name: prompts; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.prompts (
    id uuid DEFAULT uuidv7() NOT NULL,
    hash character(64) NOT NULL,
    path text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.prompts OWNER TO postgres;

--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


ALTER TABLE public.schema_migrations OWNER TO postgres;

--
-- Name: sources; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.sources (
    id integer NOT NULL,
    abbr character varying(16) NOT NULL,
    name character varying(128) NOT NULL,
    type public.source_type NOT NULL,
    base_url text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


ALTER TABLE public.sources OWNER TO postgres;

--
-- Name: sources_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.sources_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.sources_id_seq OWNER TO postgres;

--
-- Name: sources_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.sources_id_seq OWNED BY public.sources.id;


--
-- Name: tasks; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.tasks (
    id uuid DEFAULT uuidv7() NOT NULL,
    batch_id uuid NOT NULL,
    kind public.task_kind NOT NULL,
    source_type public.source_type NOT NULL,
    source_id integer NOT NULL,
    url text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    trace_id character varying(100) NOT NULL,
    frequency interval second(0),
    next_run_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    status public.task_status DEFAULT 'PENDING'::public.task_status NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    last_run_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='80');


ALTER TABLE public.tasks OWNER TO postgres;

--
-- Name: candidate_embeddings_gemma_2025 id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidate_embeddings_gemma_2025 ALTER COLUMN id SET DEFAULT nextval('public.candidate_embeddings_gemma_2025_id_seq'::regclass);


--
-- Name: content_embeddings_gemma_2025 id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_embeddings_gemma_2025 ALTER COLUMN id SET DEFAULT nextval('public.content_embeddings_gemma_2025_id_seq'::regclass);


--
-- Name: content_extraction_phrases id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_phrases ALTER COLUMN id SET DEFAULT nextval('public.content_extraction_phrases_id_seq'::regclass);


--
-- Name: entities id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.entities ALTER COLUMN id SET DEFAULT nextval('public.entities_id_seq'::regclass);


--
-- Name: models id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.models ALTER COLUMN id SET DEFAULT nextval('public.models_id_seq'::regclass);


--
-- Name: sources id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.sources ALTER COLUMN id SET DEFAULT nextval('public.sources_id_seq'::regclass);


--
-- Name: candidate_embeddings_gemma_2025 candidate_embeddings_gemma_2025_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidate_embeddings_gemma_2025
    ADD CONSTRAINT candidate_embeddings_gemma_2025_pkey PRIMARY KEY (id);


--
-- Name: candidates candidates_fingerprint_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidates
    ADD CONSTRAINT candidates_fingerprint_key UNIQUE (fingerprint);


--
-- Name: candidates candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidates
    ADD CONSTRAINT candidates_pkey PRIMARY KEY (id);


--
-- Name: content_embeddings_gemma_2025 content_embeddings_gemma_2025_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_embeddings_gemma_2025
    ADD CONSTRAINT content_embeddings_gemma_2025_pkey PRIMARY KEY (id);


--
-- Name: content_extraction_entities content_extraction_entities_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_entities
    ADD CONSTRAINT content_extraction_entities_pkey PRIMARY KEY (extraction_id, entity_id);


--
-- Name: content_extraction_phrases content_extraction_phrases_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_phrases
    ADD CONSTRAINT content_extraction_phrases_pkey PRIMARY KEY (id);


--
-- Name: content_extraction_topics content_extraction_topics_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_topics
    ADD CONSTRAINT content_extraction_topics_pkey PRIMARY KEY (extraction_id, topic_text);


--
-- Name: content_extractions content_extractions_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extractions
    ADD CONSTRAINT content_extractions_pkey PRIMARY KEY (id);


--
-- Name: contents contents_candidate_id_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_candidate_id_key UNIQUE (candidate_id);


--
-- Name: contents contents_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_pkey PRIMARY KEY (id);


--
-- Name: contents contents_url_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_url_key UNIQUE (url);


--
-- Name: entities entities_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.entities
    ADD CONSTRAINT entities_pkey PRIMARY KEY (id);


--
-- Name: models models_name_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_name_key UNIQUE (name);


--
-- Name: models models_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_pkey PRIMARY KEY (id);


--
-- Name: prompts prompts_hash_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.prompts
    ADD CONSTRAINT prompts_hash_key UNIQUE (hash);


--
-- Name: prompts prompts_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.prompts
    ADD CONSTRAINT prompts_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: sources sources_abbr_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.sources
    ADD CONSTRAINT sources_abbr_key UNIQUE (abbr);


--
-- Name: sources sources_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.sources
    ADD CONSTRAINT sources_pkey PRIMARY KEY (id);


--
-- Name: tasks tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);


--
-- Name: idx_candidates_batch_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_candidates_batch_id ON public.candidates USING btree (batch_id);


--
-- Name: idx_candidates_discovered_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_candidates_discovered_at ON public.candidates USING btree (discovered_at);


--
-- Name: idx_candidates_source_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_candidates_source_id ON public.candidates USING btree (source_id);


--
-- Name: idx_candidates_source_published_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_candidates_source_published_at ON public.candidates USING btree (source_id, published_at);


--
-- Name: idx_candidates_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_candidates_trace_id ON public.candidates USING btree (trace_id);


--
-- Name: idx_candidates_url; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_candidates_url ON public.candidates USING btree (url);


--
-- Name: idx_cee_extraction_ordinal; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cee_extraction_ordinal ON public.content_extraction_entities USING btree (extraction_id, ordinal);


--
-- Name: idx_cemb_g25_candidate; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cemb_g25_candidate ON public.candidate_embeddings_gemma_2025 USING btree (candidate_id);


--
-- Name: idx_cemb_g25_category; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cemb_g25_category ON public.candidate_embeddings_gemma_2025 USING btree (category);


--
-- Name: idx_cemb_g25_model; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cemb_g25_model ON public.candidate_embeddings_gemma_2025 USING btree (model_id);


--
-- Name: idx_cemb_g25_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cemb_g25_trace_id ON public.candidate_embeddings_gemma_2025 USING btree (trace_id);


--
-- Name: idx_cemb_g25_vec; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cemb_g25_vec ON public.candidate_embeddings_gemma_2025 USING hnsw (vector public.vector_cosine_ops);


--
-- Name: idx_cet_extraction_ordinal; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_cet_extraction_ordinal ON public.content_extraction_topics USING btree (extraction_id, ordinal);


--
-- Name: idx_content_extraction_phrases_ordinal; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_content_extraction_phrases_ordinal ON public.content_extraction_phrases USING btree (extraction_id, ordinal);


--
-- Name: idx_content_extractions_content_created_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_content_extractions_content_created_at ON public.content_extractions USING btree (content_id, created_at);


--
-- Name: idx_content_extractions_content_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_content_extractions_content_id ON public.content_extractions USING btree (content_id);


--
-- Name: idx_content_extractions_model_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_content_extractions_model_id ON public.content_extractions USING btree (model_id);


--
-- Name: idx_content_extractions_prompt_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_content_extractions_prompt_id ON public.content_extractions USING btree (prompt_id);


--
-- Name: idx_content_extractions_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_content_extractions_trace_id ON public.content_extractions USING btree (trace_id);


--
-- Name: idx_contents_batch_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_batch_id ON public.contents USING btree (batch_id);


--
-- Name: idx_contents_candidate_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_candidate_id ON public.contents USING btree (candidate_id);


--
-- Name: idx_contents_deleted_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_deleted_at ON public.contents USING btree (deleted_at);


--
-- Name: idx_contents_published_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_published_at ON public.contents USING btree (published_at);


--
-- Name: idx_contents_source_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_source_id ON public.contents USING btree (source_id);


--
-- Name: idx_contents_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_trace_id ON public.contents USING btree (trace_id);


--
-- Name: idx_contents_type; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_type ON public.contents USING btree (type);


--
-- Name: idx_emb_g25_category; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_category ON public.content_embeddings_gemma_2025 USING btree (category);


--
-- Name: idx_emb_g25_content; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_content ON public.content_embeddings_gemma_2025 USING btree (content_id);


--
-- Name: idx_emb_g25_model; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_model ON public.content_embeddings_gemma_2025 USING btree (model_id);


--
-- Name: idx_emb_g25_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_trace_id ON public.content_embeddings_gemma_2025 USING btree (trace_id);


--
-- Name: idx_emb_g25_vec; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_vec ON public.content_embeddings_gemma_2025 USING hnsw (vector public.vector_cosine_ops);


--
-- Name: idx_entities_type; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_entities_type ON public.entities USING btree (type);


--
-- Name: idx_models_type_name; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_models_type_name ON public.models USING btree (type, name);


--
-- Name: idx_prompts_path; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_prompts_path ON public.prompts USING btree (path);


--
-- Name: idx_sources_deleted_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_sources_deleted_at ON public.sources USING btree (deleted_at);


--
-- Name: idx_sources_lookup; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_sources_lookup ON public.sources USING btree (abbr, base_url);


--
-- Name: idx_tasks_batch_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_batch_id ON public.tasks USING btree (batch_id);


--
-- Name: idx_tasks_expiry; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_expiry ON public.tasks USING btree (expires_at);


--
-- Name: idx_tasks_kind_source_type; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_kind_source_type ON public.tasks USING btree (kind, source_type);


--
-- Name: idx_tasks_schedule; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_schedule ON public.tasks USING btree (next_run_at, frequency);


--
-- Name: idx_tasks_source_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_source_id ON public.tasks USING btree (source_id);


--
-- Name: idx_tasks_status; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_status ON public.tasks USING btree (status);


--
-- Name: idx_tasks_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_trace_id ON public.tasks USING btree (trace_id);


--
-- Name: idx_tasks_url; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_tasks_url ON public.tasks USING btree (url);


--
-- Name: uq_content_extraction_phrases; Type: INDEX; Schema: public; Owner: postgres
--

CREATE UNIQUE INDEX uq_content_extraction_phrases ON public.content_extraction_phrases USING btree (extraction_id, phrase);


--
-- Name: uq_content_extractions_snapshot; Type: INDEX; Schema: public; Owner: postgres
--

CREATE UNIQUE INDEX uq_content_extractions_snapshot ON public.content_extractions USING btree (content_id, model_id, prompt_id, schema_version);


--
-- Name: uq_entities_canonical_type; Type: INDEX; Schema: public; Owner: postgres
--

CREATE UNIQUE INDEX uq_entities_canonical_type ON public.entities USING btree (canonical, type);


--
-- Name: candidate_embeddings_gemma_2025 candidate_embeddings_gemma_2025_candidate_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidate_embeddings_gemma_2025
    ADD CONSTRAINT candidate_embeddings_gemma_2025_candidate_id_fkey FOREIGN KEY (candidate_id) REFERENCES public.candidates(id) ON DELETE CASCADE;


--
-- Name: candidate_embeddings_gemma_2025 candidate_embeddings_gemma_2025_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidate_embeddings_gemma_2025
    ADD CONSTRAINT candidate_embeddings_gemma_2025_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id);


--
-- Name: candidates candidates_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.candidates
    ADD CONSTRAINT candidates_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.sources(id);


--
-- Name: content_embeddings_gemma_2025 content_embeddings_gemma_2025_content_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_embeddings_gemma_2025
    ADD CONSTRAINT content_embeddings_gemma_2025_content_id_fkey FOREIGN KEY (content_id) REFERENCES public.contents(id) ON DELETE CASCADE;


--
-- Name: content_embeddings_gemma_2025 content_embeddings_gemma_2025_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_embeddings_gemma_2025
    ADD CONSTRAINT content_embeddings_gemma_2025_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id);


--
-- Name: content_extraction_entities content_extraction_entities_entity_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_entities
    ADD CONSTRAINT content_extraction_entities_entity_id_fkey FOREIGN KEY (entity_id) REFERENCES public.entities(id) ON DELETE RESTRICT;


--
-- Name: content_extraction_entities content_extraction_entities_extraction_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_entities
    ADD CONSTRAINT content_extraction_entities_extraction_id_fkey FOREIGN KEY (extraction_id) REFERENCES public.content_extractions(id) ON DELETE CASCADE;


--
-- Name: content_extraction_phrases content_extraction_phrases_extraction_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_phrases
    ADD CONSTRAINT content_extraction_phrases_extraction_id_fkey FOREIGN KEY (extraction_id) REFERENCES public.content_extractions(id) ON DELETE CASCADE;


--
-- Name: content_extraction_topics content_extraction_topics_extraction_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extraction_topics
    ADD CONSTRAINT content_extraction_topics_extraction_id_fkey FOREIGN KEY (extraction_id) REFERENCES public.content_extractions(id) ON DELETE CASCADE;


--
-- Name: content_extractions content_extractions_content_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extractions
    ADD CONSTRAINT content_extractions_content_id_fkey FOREIGN KEY (content_id) REFERENCES public.contents(id) ON DELETE CASCADE;


--
-- Name: content_extractions content_extractions_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extractions
    ADD CONSTRAINT content_extractions_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id);


--
-- Name: content_extractions content_extractions_prompt_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_extractions
    ADD CONSTRAINT content_extractions_prompt_id_fkey FOREIGN KEY (prompt_id) REFERENCES public.prompts(id);


--
-- Name: contents contents_candidate_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_candidate_id_fkey FOREIGN KEY (candidate_id) REFERENCES public.candidates(id) ON DELETE SET NULL;


--
-- Name: contents contents_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.sources(id);


--
-- Name: tasks tasks_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.sources(id);


--
-- Name: SCHEMA public; Type: ACL; Schema: -; Owner: pg_database_owner
--

GRANT USAGE ON SCHEMA public TO prism;


--
-- Name: TABLE candidate_embeddings_gemma_2025; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.candidate_embeddings_gemma_2025 TO prism;


--
-- Name: SEQUENCE candidate_embeddings_gemma_2025_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.candidate_embeddings_gemma_2025_id_seq TO prism;


--
-- Name: TABLE candidates; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.candidates TO prism;


--
-- Name: TABLE content_embeddings_gemma_2025; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.content_embeddings_gemma_2025 TO prism;


--
-- Name: SEQUENCE content_embeddings_gemma_2025_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.content_embeddings_gemma_2025_id_seq TO prism;


--
-- Name: TABLE content_extraction_entities; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.content_extraction_entities TO prism;


--
-- Name: TABLE content_extraction_phrases; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.content_extraction_phrases TO prism;


--
-- Name: SEQUENCE content_extraction_phrases_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.content_extraction_phrases_id_seq TO prism;


--
-- Name: TABLE content_extraction_topics; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.content_extraction_topics TO prism;


--
-- Name: TABLE content_extractions; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.content_extractions TO prism;


--
-- Name: TABLE contents; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.contents TO prism;


--
-- Name: TABLE entities; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.entities TO prism;


--
-- Name: SEQUENCE entities_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.entities_id_seq TO prism;


--
-- Name: TABLE models; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.models TO prism;


--
-- Name: SEQUENCE models_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.models_id_seq TO prism;


--
-- Name: TABLE prompts; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.prompts TO prism;


--
-- Name: TABLE schema_migrations; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.schema_migrations TO prism;


--
-- Name: TABLE sources; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.sources TO prism;


--
-- Name: SEQUENCE sources_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.sources_id_seq TO prism;


--
-- Name: TABLE tasks; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.tasks TO prism;


--
-- Name: DEFAULT PRIVILEGES FOR SEQUENCES; Type: DEFAULT ACL; Schema: public; Owner: postgres
--

ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT ALL ON SEQUENCES TO prism;


--
-- Name: DEFAULT PRIVILEGES FOR TABLES; Type: DEFAULT ACL; Schema: public; Owner: postgres
--

ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT ALL ON TABLES TO prism;


--
-- PostgreSQL database dump complete
--


