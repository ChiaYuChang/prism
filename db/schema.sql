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
    'CONTENT',
    'TITLE'
);


ALTER TYPE public.embedding_category OWNER TO postgres;

--
-- Name: fingerprint_status; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.fingerprint_status AS ENUM (
    'DISCOVERED',
    'ARCHIVED',
    'PROCESSED'
);


ALTER TYPE public.fingerprint_status OWNER TO postgres;

--
-- Name: source_type; Type: TYPE; Schema: public; Owner: postgres
--

CREATE TYPE public.source_type AS ENUM (
    'PARTY',
    'MEDIA'
);


ALTER TYPE public.source_type OWNER TO postgres;

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
-- Name: content_keywords; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.content_keywords (
    content_id uuid NOT NULL,
    keyword_id integer NOT NULL
);


ALTER TABLE public.content_keywords OWNER TO postgres;

--
-- Name: contents; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.contents (
    id uuid DEFAULT uuidv7() NOT NULL,
    type public.content_type NOT NULL,
    source_id integer,
    fingerprint_id integer NOT NULL,
    url text NOT NULL,
    title text NOT NULL,
    content text NOT NULL,
    author character varying(64),
    trace_id character varying(100) NOT NULL,
    published_at timestamp with time zone NOT NULL,
    fetched_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    deleted_at timestamp with time zone,
    metadata jsonb
);


ALTER TABLE public.contents OWNER TO postgres;

--
-- Name: embeddings_gemma_2025; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.embeddings_gemma_2025 (
    id bigint NOT NULL,
    content_id uuid NOT NULL,
    model_id smallint NOT NULL,
    category public.embedding_category NOT NULL,
    vector public.vector(768) NOT NULL,
    trace_id character varying(100) NOT NULL,
    created_at timestamp with time zone DEFAULT now()
);


ALTER TABLE public.embeddings_gemma_2025 OWNER TO postgres;

--
-- Name: embeddings_gemma_2025_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.embeddings_gemma_2025_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.embeddings_gemma_2025_id_seq OWNER TO postgres;

--
-- Name: embeddings_gemma_2025_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.embeddings_gemma_2025_id_seq OWNED BY public.embeddings_gemma_2025.id;


--
-- Name: fingerprints; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.fingerprints (
    id integer NOT NULL,
    fingerprint character(64) NOT NULL,
    source_id integer,
    url text NOT NULL,
    title text,
    description text,
    status public.fingerprint_status DEFAULT 'DISCOVERED'::public.fingerprint_status,
    trace_id character varying(100),
    discovered_at timestamp with time zone DEFAULT now()
);


ALTER TABLE public.fingerprints OWNER TO postgres;

--
-- Name: fingerprints_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.fingerprints_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.fingerprints_id_seq OWNER TO postgres;

--
-- Name: fingerprints_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.fingerprints_id_seq OWNED BY public.fingerprints.id;


--
-- Name: keywords; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.keywords (
    id integer NOT NULL,
    text text NOT NULL,
    created_at timestamp with time zone DEFAULT now()
);


ALTER TABLE public.keywords OWNER TO postgres;

--
-- Name: keywords_id_seq; Type: SEQUENCE; Schema: public; Owner: postgres
--

CREATE SEQUENCE public.keywords_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.keywords_id_seq OWNER TO postgres;

--
-- Name: keywords_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: postgres
--

ALTER SEQUENCE public.keywords_id_seq OWNED BY public.keywords.id;


--
-- Name: models; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.models (
    id smallint NOT NULL,
    name character varying(32) NOT NULL,
    publisher character varying(32) NOT NULL,
    publish_date date,
    url text,
    tag character varying(16),
    created_at timestamp with time zone DEFAULT now(),
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
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


ALTER TABLE public.schema_migrations OWNER TO postgres;

--
-- Name: search_tasks; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.search_tasks (
    id uuid DEFAULT uuidv7() NOT NULL,
    content_id uuid,
    phrases text[] NOT NULL,
    trace_id character varying(100) NOT NULL,
    frequency interval DEFAULT '06:00:00'::interval NOT NULL,
    next_run_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    status public.task_status DEFAULT 'PENDING'::public.task_status,
    retry_count integer DEFAULT 0,
    last_run_at timestamp with time zone,
    CONSTRAINT search_tasks_frequency_check CHECK ((frequency >= '00:30:00'::interval))
)
WITH (fillfactor='80');


ALTER TABLE public.search_tasks OWNER TO postgres;

--
-- Name: sources; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.sources (
    id integer NOT NULL,
    abbr character varying(8) NOT NULL,
    name character varying(64) NOT NULL,
    type public.source_type NOT NULL,
    base_url text NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
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
-- Name: embeddings_gemma_2025 id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.embeddings_gemma_2025 ALTER COLUMN id SET DEFAULT nextval('public.embeddings_gemma_2025_id_seq'::regclass);


--
-- Name: fingerprints id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.fingerprints ALTER COLUMN id SET DEFAULT nextval('public.fingerprints_id_seq'::regclass);


--
-- Name: keywords id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.keywords ALTER COLUMN id SET DEFAULT nextval('public.keywords_id_seq'::regclass);


--
-- Name: models id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.models ALTER COLUMN id SET DEFAULT nextval('public.models_id_seq'::regclass);


--
-- Name: sources id; Type: DEFAULT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.sources ALTER COLUMN id SET DEFAULT nextval('public.sources_id_seq'::regclass);


--
-- Name: content_keywords content_keywords_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_keywords
    ADD CONSTRAINT content_keywords_pkey PRIMARY KEY (content_id, keyword_id);


--
-- Name: contents contents_fingerprint_id_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_fingerprint_id_key UNIQUE (fingerprint_id);


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
-- Name: embeddings_gemma_2025 embeddings_gemma_2025_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.embeddings_gemma_2025
    ADD CONSTRAINT embeddings_gemma_2025_pkey PRIMARY KEY (id);


--
-- Name: fingerprints fingerprints_fingerprint_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.fingerprints
    ADD CONSTRAINT fingerprints_fingerprint_key UNIQUE (fingerprint);


--
-- Name: fingerprints fingerprints_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.fingerprints
    ADD CONSTRAINT fingerprints_pkey PRIMARY KEY (id);


--
-- Name: keywords keywords_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.keywords
    ADD CONSTRAINT keywords_pkey PRIMARY KEY (id);


--
-- Name: keywords keywords_text_key; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.keywords
    ADD CONSTRAINT keywords_text_key UNIQUE (text);


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
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: search_tasks search_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.search_tasks
    ADD CONSTRAINT search_tasks_pkey PRIMARY KEY (id);


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
-- Name: idx_contents_deleted_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_deleted_at ON public.contents USING btree (deleted_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_contents_published_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_contents_published_at ON public.contents USING btree (published_at);


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

CREATE INDEX idx_emb_g25_category ON public.embeddings_gemma_2025 USING btree (category);


--
-- Name: idx_emb_g25_content; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_content ON public.embeddings_gemma_2025 USING btree (content_id);


--
-- Name: idx_emb_g25_model; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_model ON public.embeddings_gemma_2025 USING btree (model_id);


--
-- Name: idx_emb_g25_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_trace_id ON public.embeddings_gemma_2025 USING btree (trace_id);


--
-- Name: idx_emb_g25_vec; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_emb_g25_vec ON public.embeddings_gemma_2025 USING hnsw (vector public.vector_cosine_ops);


--
-- Name: idx_fp_fingerprint_lookup; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_fp_fingerprint_lookup ON public.fingerprints USING btree (fingerprint);


--
-- Name: idx_fp_status; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_fp_status ON public.fingerprints USING btree (status);


--
-- Name: idx_fp_trace_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_fp_trace_id ON public.fingerprints USING btree (trace_id);


--
-- Name: idx_search_tasks_expiry; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_search_tasks_expiry ON public.search_tasks USING btree (expires_at);


--
-- Name: idx_search_tasks_schedule; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_search_tasks_schedule ON public.search_tasks USING btree (next_run_at, frequency) WITH (fillfactor='80');


--
-- Name: idx_search_tasks_status; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_search_tasks_status ON public.search_tasks USING btree (status);


--
-- Name: idx_sources_deleted_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_sources_deleted_at ON public.sources USING btree (deleted_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_sources_lookup; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_sources_lookup ON public.sources USING btree (abbr, base_url);


--
-- Name: content_keywords content_keywords_content_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_keywords
    ADD CONSTRAINT content_keywords_content_id_fkey FOREIGN KEY (content_id) REFERENCES public.contents(id) ON DELETE CASCADE;


--
-- Name: content_keywords content_keywords_keyword_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.content_keywords
    ADD CONSTRAINT content_keywords_keyword_id_fkey FOREIGN KEY (keyword_id) REFERENCES public.keywords(id) ON DELETE CASCADE;


--
-- Name: contents contents_fingerprint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_fingerprint_id_fkey FOREIGN KEY (fingerprint_id) REFERENCES public.fingerprints(id) ON DELETE CASCADE;


--
-- Name: contents contents_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.sources(id);


--
-- Name: embeddings_gemma_2025 embeddings_gemma_2025_content_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.embeddings_gemma_2025
    ADD CONSTRAINT embeddings_gemma_2025_content_id_fkey FOREIGN KEY (content_id) REFERENCES public.contents(id) ON DELETE CASCADE;


--
-- Name: embeddings_gemma_2025 embeddings_gemma_2025_model_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.embeddings_gemma_2025
    ADD CONSTRAINT embeddings_gemma_2025_model_id_fkey FOREIGN KEY (model_id) REFERENCES public.models(id);


--
-- Name: fingerprints fingerprints_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.fingerprints
    ADD CONSTRAINT fingerprints_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.sources(id);


--
-- Name: search_tasks search_tasks_content_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.search_tasks
    ADD CONSTRAINT search_tasks_content_id_fkey FOREIGN KEY (content_id) REFERENCES public.contents(id) ON DELETE CASCADE;


--
-- Name: SCHEMA public; Type: ACL; Schema: -; Owner: pg_database_owner
--

GRANT USAGE ON SCHEMA public TO prism;


--
-- Name: TABLE content_keywords; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.content_keywords TO prism;


--
-- Name: TABLE contents; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.contents TO prism;


--
-- Name: TABLE embeddings_gemma_2025; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.embeddings_gemma_2025 TO prism;


--
-- Name: SEQUENCE embeddings_gemma_2025_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.embeddings_gemma_2025_id_seq TO prism;


--
-- Name: TABLE fingerprints; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.fingerprints TO prism;


--
-- Name: SEQUENCE fingerprints_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.fingerprints_id_seq TO prism;


--
-- Name: TABLE keywords; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.keywords TO prism;


--
-- Name: SEQUENCE keywords_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.keywords_id_seq TO prism;


--
-- Name: TABLE models; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.models TO prism;


--
-- Name: SEQUENCE models_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.models_id_seq TO prism;


--
-- Name: TABLE schema_migrations; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.schema_migrations TO prism;


--
-- Name: TABLE search_tasks; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.search_tasks TO prism;


--
-- Name: TABLE sources; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON TABLE public.sources TO prism;


--
-- Name: SEQUENCE sources_id_seq; Type: ACL; Schema: public; Owner: postgres
--

GRANT ALL ON SEQUENCE public.sources_id_seq TO prism;


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


