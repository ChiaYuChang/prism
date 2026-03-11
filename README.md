# Project Prism

**Project Prism** is a headless, high-precision data pipeline designed to analyze media bias across various platforms regarding political issues in Taiwan. It standardizes the ingestion and analysis of media reports using a specialized pipeline architecture and AI-driven semantic analysis.

## 1. Core Philosophy: The Normalized Pipeline (F-T-(S||P))

Project Prism operates on a standardized **Normalization First** workflow to ensure data consistency and auditability:

1. **Fetch (F)**: Retrieve raw "dirty" data (HTML/JSON) from remote sources.
2. **Transform (T)**: The normalization core. Clean the DOM, minify content, and produce a **Canonical String**.
3. **Parallel Fork**:
    * **Save (S)**: Background physical archiving. The canonical string is compressed (Gzip+Base64) and written to **SeaweedFS/S3** for permanent auditing.
    * **Parse (P)**: Immediate structured extraction into structured objects for immediate analysis and **vectorization**.

## 2. System Architecture & Data Flow

The system consists of decoupled components communicating via **Watermill** (supporting NATS JetStream locally and AWS SQS/SNS in cloud environments).

### A. The Discovery Loop (Background Job)

The Discovery Loop follows a **Trigger + Executor** pattern to identify and buffer news reports.

* **Scheduler (Trigger & Executor)**:
  * **Trigger**: Sends a signal message (e.g., `prism.cron.discovery_refresh`) to the executor periodically.
  * **Executor**:
    * **Task Claiming**: Pulls pending `search_tasks` from **PostgreSQL 18** using `FOR UPDATE SKIP LOCKED` for thread-safe batching.
    * **Keyword Deduplication**: Uses **Valkey (Redis)** to lock keyword combinations (`prism:search_lock:<hash>`).
    * **Dispatching**: If the keyword combination is not locked, it publishes search signals to the MQ (NATS/SQS).
  * **Deployment Mapping**:
    * **Local**: A standalone Go service (`cmd/scheduler`).
    * **Cloud**: AWS EventBridge (Trigger) + AWS Lambda (Executor).
* **Discovery Worker**: Subscribes to search signals, interacts with Search Engine APIs (Google/Serper) to find new URLs, and persists them as `DISCOVERED` in the `fingerprints` table (SHA256 deduplication).

### B. The Analysis Pipeline (Background Job)

* **Pipeline Worker**: Subscribes to discovered URL signals and executes the **F-T-(S||P)** flow using the `BaseCollector`.
* **Semantic Analysis**: Uses LLMs to generate structured metadata and **Gemma 2025** embeddings.
* **Vector Retrieval**: Stores **768-dimensional** embeddings in **pgvector (HNSW index)** to calculate semantic distance.

### C. Data Access (API & Interface)

* **API Server**: A headless RESTful server providing endpoints to query analysis results, media preference scores, and semantic clusters.
* **Traceability**: Every operation is tagged with an **OpenTelemetry TraceID**, correlating logs and physical archives.

## 3. Infrastructure Stack

| Component | Technology | Role |
| :--- | :--- | :--- |
| **Language** | Go (Golang 1.24+) | High-concurrency processing core |
| **Message Broker** | Watermill (NATS / SQS) | Abstraction layer for task dispatching & DLQ |
| **Primary Database** | PostgreSQL 18 + pgvector | Structured data with native **uuidv7** support |
| **Distributed Lock** | Valkey (Redis) | Deduplication guard and task concurrency control |
| **Object Storage** | SeaweedFS / S3 | Data Lake for Gzip-compressed canonical archives |
| **Telemetry** | OpenTelemetry + VictoriaLogs | Full-link auditability and structured JSON logging |

## 4. Database Schema Highlights (PostgreSQL 18)

* **uuidv7**: Used as the Primary Key for contents and search_tasks for time-ordered indexing.
* **Fingerprints**: SHA256 hashes of URLs used as the primary deduplication key.
* **Embeddings**: Partitioned storage for `TITLE` and `CONTENT` vectors.

## 5. Internal Project Structure

* `cmd/`: Entry points for services (Scheduler, Workers, API).
* `internal/model/`: Pure domain entities (no dependencies).
* `internal/message/`: Wire protocols and messaging contracts.
* `internal/repo/`: Abstract repository interfaces for persistence.
* `internal/repo/pg/`: Concrete PostgreSQL implementation (Adapter).
* `internal/collector/`: The core F-T-S-P logic and interfaces.
* `pkg/`: Generic utility packages (archive, telemetry, hash).

## 6. Roadmap

* [x] Phase 1: Foundation & Data Contracts
* [ ] Phase 2: Discovery Loop & Scheduler (In Progress)
* [ ] Phase 3: Parallel Pipeline Execution
* [ ] Phase 4: Semantic Bias Analysis
* [ ] Future: Admin API (Pause/Resume Scheduler), Prism TUI & Web Dashboard
