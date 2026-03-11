# Project Prism: Master Implementation Plan

## 1. Project Objective

Project Prism is a headless data pipeline designed to analyze media bias across various platforms regarding political issues in Taiwan. Its primary objectives include:

* Standardizing data ingestion through a "Discovery Loop" to pre-fill buffers.  
* Executing high-precision analysis using a parallelized pipeline architecture (F-T-(S||P)).  
* Ensuring data auditability and traceability using OpenTelemetry TraceIDs.

## 2. Core Architecture: Normalized Pipeline (F-T-(S||P))

The system follows a standardized "Normalization First, Parallel Execution" workflow:

1. Fetch (F): Synchronously retrieve raw "dirty" data (HTML/JSON).  
2. Transform (T): The normalization core. Clean the DOM, minify content, and produce a "Canonical String".  
3. Parallel Fork (Fork):  
   * Save (S): Background physical archiving. Compress the canonical string (Gzip+Base64) and write directly to SeaweedFS/S3.  
   * Parse (P): Immediate structured extraction and vectorization.

## 3. Infrastructure Mapping

| Component | Local Environment | Cloud Environment (AWS) | Transfer Strategy |
| :---- | :---- | :---- | :---- |
| Scheduling | Go Cron (Trigger) | EventBridge | Trigger Signals |
| Message Broker | Watermill (NATS) | Watermill (SQS/SNS) | Abstracted MQ Contract |
| Primary Database | PostgreSQL 18 (uuidv7) | RDS Aurora (Postgres 18) | Native Time-ordered PK |
| Data Lake | SeaweedFS | AWS S3 | Gzip Encapsulated Data |
| Deduplication | Valkey 9 | ElastiCache | SHA256 Fingerprint & Keyword Locks |

## 4. Actionable Checklist

### Phase 1: Foundation and Data Contracts

* [x] 1.1 Infrastructure Deployment: Docker Compose with Postgres 18, NATS, and Valkey.  
* [x] 1.2 Interface Definitions: Fetcher, Transformer, Saver, and Parser in `internal/collector`.  
* [x] 1.3 Utility Implementation: SHA256 Fingerprinting and Gzip/Base64 helpers.  
* [x] 1.4 Database Management: Repository abstraction (`internal/repo`) and Postgres adapter (`internal/repo/pg`).
* [x] 1.5 Database Management: Initial migration with **uuidv7** support.
* [x] 1.6 Utilities Refinement & Traceability:
  * [x] Implement OpenTelemetry context propagation helpers.
  * [x] Add basic health-check endpoints with Level/Message monitor.
* [x] 1.7 Generic Logging System:
  * [x] Implement `pkg/logger` with `slog` Hook mechanism (Decorator pattern).
  * [x] Integrate `internal/obs` with `TraceIDHook` for distributed tracing.
  * [x] Implement dynamic logger enrichment using `WithHook` and `SinceHook`.
* [x] 1.8 Pluggable Messenger Infrastructure:
  * [x] Implement `GoChannel` backend in `internal/infra/messenger.go` for in-memory testing.
  * [x] Refactor `cmd/scheduler` with `MessengerConfig` interface for polymorphic backends (NATS/GoChannel).

### Phase 2: Discovery Loop (Trigger + Executor)

* [x] 2.1 Signal Dispatching (Scheduler):
  * [x] Implement `cmd/scheduler` with Viper/Pflag/Env and structured `PostgresConfig`.
  * [x] Integrate Valkey `SETNX` for **Keyword-level deduplication** (`prism:search_lock:<hash>`).
  * [x] Trigger Logic: Periodically publish `prism.cron.discovery_refresh` to MQ.
  * [x] Executor Logic:
    * [x] Postgres repository for claiming `search_tasks` using `FOR UPDATE SKIP LOCKED` (with zombie recovery).
    * [x] Watermill publisher for sending `SearchTaskSignal` (UUID v7, TraceID propagation).
    * [x] Comprehensive logging using PID, Uptime, and Task-specific Hooks.
    * [x] Unit tests for configuration and validation.
* [ ] 2.2 LLM Analysis (Keyword & Phrase Extraction):
  * [ ] Implement Gemini client for extracting "Composite Search Phrases".
  * [ ] Design prompts for political text analysis.
* [ ] 2.3 Discovery Execution (Worker):
  * [ ] Implement `cmd/discovery-worker` subscribing to search signals.
  * [ ] Integrate Search Engine API (Google/Serper) for URL discovery.
  * [ ] URL normalization and SHA256 "Fingerprint" deduplication.
* [ ] 2.4 Async Persistence:
  * [ ] Persistence logic for writing results to `fingerprints` table with `DISCOVERED` status.

### Phase 3: Pipeline Execution and Vectorization

* [ ] 3.1 Smart Dispatcher: Automatic routing and `BaseCollector` assembly logic.  
* [ ] 3.2 Parallel Pipeline: Concurrent Save (S3) and Parse (Structured Data).  
* [x] 3.3 Vectorization Integration:
  * [x] Schema for **768-dimensional** embeddings with **pgvector (HNSW index)**.
  * [ ] Implement **Partitioned storage** for TITLE and CONTENT vectors.
* [ ] 3.4 Vector Execution: Implementation of Embedding client (Gemma 2025) and worker logic.

### Phase 4: Analysis and Monitoring

* [ ] 4.1 Issue Summarization: Cross-media reporting summaries using LLM.  
* [ ] 4.2 Semantic Distance: Calculation of cosine similarity between media and party stances.  
* [ ] 4.3 Operational Monitoring:
  * [ ] **VictoriaLogs** integration for full-link auditability.
  * [ ] Grafana Dashboard for tracking throughput and error rates.

## 5. Future Roadmap

* [ ] Admin API: Endpoints to Pause/Resume Scheduler and update keyword TTL.
* [ ] User Interfaces: Prism TUI (Terminal UI) and Web Dashboard.  
* [ ] Expansion: Direct Media API connectors and JS-rendered scraping via Playwright.
