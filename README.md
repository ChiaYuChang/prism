# Project Prism

Project Prism is a headless data pipeline for tracking how Taiwan political party positions propagate into media coverage. It does not try to crawl all news. It starts from bounded seed texts, mainly party press releases, discovers broad candidate coverage, and only fetches full articles when needed.

## Purpose

Prism is designed to:

- use party press releases as high-signal discovery seeds
- generate broad, cheap discovery requests from those seeds
- store article briefs in `candidates`
- promote selected briefs into full `contents`
- preserve full auditability with TraceID-linked logs, records, and archives
- support later extraction, embedding, and semantic analysis

## Core Workflow

### 1. Party Press Release Intake

Background jobs periodically crawl party press release directory pages, store directory-page briefs in `candidates`, and then fetch the full press release pages into `contents`.

### 2. Planner

After the current batch of party press release fetches completes, `Planner` loads today's press release contents and asks the LLM to produce a bounded number of short keyword groups. The goal is recall, not precision.

### 3. Discovery Loop

The discovery loop follows a trigger / executor model:

- the scheduler runs periodically
- it claims runnable `tasks` from PostgreSQL
- it publishes task messages through Watermill
- downstream workers execute the actual directory-fetch work

The scheduler is a task dispatcher, not a crawler.

### 4. Candidate Buffering

`Scout` handles directory-fetch tasks, downloads party directory pages or media search results, and stores discovered briefs in the `candidates` pool.

### 5. Candidate Promotion

Selected candidates are fetched into full `contents` for parsing, archiving, extraction, and later analysis.

### 6. Embeddings

Candidate insertion and content insertion each trigger their own embedding workers so that both `candidates` and `contents` can later be searched semantically. This includes party press release briefs as well as fetched full contents.

### 7. Analysis

Structured extraction, embeddings, clustering, and summarization are analysis capabilities built on top of fetched `contents`. They are not mandatory steps in the minimal discovery loop.

## Design Principles

### Recall Over Precision

Discovery should optimize for broad coverage. Missing a relevant article is more costly than briefly collecting noise.

### Seed-Driven, Not Crawl-Everything

Prism intentionally starts from limited, high-signal political texts and expands outward in a controlled way.

### Candidates Before Contents

Article briefs and full fetched contents are different lifecycle stages and should stay separate in both code and storage.

### Analysis Decoupled From Discovery

Semantic extraction and clustering remain important, but discovery should not depend on them in order to stay simple and quota-aware. Discovery is driven by request-oriented tasks, not by cluster summaries.

## Normalized Pipeline

Prism uses a normalization-first workflow:

1. Fetch (F): retrieve raw HTML or JSON
2. Transform (T): clean DOM and generate a canonical string
3. Save (S): archive canonical content for auditability
4. Parse (P): produce structured data and embeddings

This is commonly described as `F-T-(S||P)`.

## Major Components

| Component | Technology | Role |
| :--- | :--- | :--- |
| Language | Go 1.24+ | High-concurrency services and workers |
| Message Broker | Watermill over NATS / SQS | Task dispatch and async workflows |
| Database | PostgreSQL 18 + pgvector | Structured data, tasks, and embeddings |
| Cache / Lock | Valkey | Future short-term rate limiting and deduplication |
| Object Storage | SeaweedFS / S3 | Canonical archive storage |
| Telemetry | OpenTelemetry + VictoriaLogs | Trace-linked logging and auditability |

## Repository Layout

- `cmd/`: service entry points such as the scheduler and workers
- `internal/collector/`: fetch, transform, save, and parse interfaces
- `internal/discovery/`: discovery interfaces and extractor implementation
- `internal/message/`: message contracts for worker dispatch
- `internal/model/`: domain data structures
- `internal/repo/`: repository abstractions
- `internal/repo/pg/`: PostgreSQL implementations
- `assets/prompts/`: prompt assets used by analysis components
- `pkg/schema/`: structured output schema contract helpers
- `db/migrations/`: database schema history
- `docs/`: design and query planning documents

## Status

- Phase 1 foundation is complete
- Scheduler and LLM infrastructure are in place
- Discovery worker, candidate embedding worker, and content embedding worker are the next major milestones
