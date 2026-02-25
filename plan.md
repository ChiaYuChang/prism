# Project Prism: Master Implementation Plan

## 1. Project Objective

Project Prism is a headless data pipeline designed to analyze media bias across various platforms regarding political issues in Taiwan. Its primary objectives include:

* Standardizing data ingestion through a "Discovery Loop" to pre-fill buffers.  
* Executing high-precision analysis using a parallelized pipeline architecture (F-T-(S||P)).  
* Ensuring data auditability and traceability using OpenTelemetry TraceIDs.

## 2. Core Architecture: Normalized Pipeline (F-T-(S||P))

The system follows a standardized "Normalization First, Parallel Execution" workflow:

1. Fetch (F): Synchronously retrieve raw "dirty" data (HTML/JSON).  
2. Transform (T): The normalization core. Parse the DOM, perform minification, and output a "Canonical String".  
3. Parallel Fork (Fork):  
   * Save (S): Background physical archiving. Compress the canonical string (Gzip+Base64) and write directly to S3/SeaweedFS.  
   * Parse (P): Immediate structured extraction. Extract fields like Title, Author, and Content from the canonical string.

## 3. Infrastructure Mapping

| Component | Local Environment | Cloud Environment (AWS) | Transfer Strategy |
| :---- | :---- | :---- | :---- |
| Scheduling | Go Cron Service | EventBridge | Trigger Signals |
| Discovery Queue | NATS JetStream | AWS SQS | Direct JSON (\< 2KB) |
| Data Lake | SeaweedFS | AWS S3 | Gzip Encapsulated Data |
| Deduplication Cache | Valkey 9 | ElastiCache | URL Fingerprint & Keyword TTL Locks |

## 4. Actionable Checklist

### Phase 1: Foundation and Data Contracts

* [ ] 1.1 Infrastructure Deployment: Docker Compose configuration completed.  
* [x] 1.2 Interface Definitions: Fetcher, Transformer, Saver, and Parser interfaces defined.  
* [ ] 1.3 Utility Implementation: Gzip+Base64 tools, TraceID extraction, and Hash deduplication tools completed.  
* [ ] 1.4 Database Management: Migration folder created and initial schema (000001\_init.up.sql) established.

### Phase 2: Discovery Loop and Query Optimization

* [ ] 2.1 Signal Dispatching: cmd/scheduler implemented with Valkey SETNX deduplication.  
* [ ] 2.2 Keyword Phrase Extraction: Gemini integration for extracting 2-3 "Composite Search Phrases".  
* [ ] 2.3 Save Queue: NATS prism.discovery.detected async database persistence implemented.  
* [ ] 2.4 Keyword Persistence: Implementation of active\_search\_queries buffer with TTL-based iterative search.

### Phase 3: Pipeline Execution and Parsing

* [ ] 3.1 Smart Dispatcher: Automatic routing and component assembly logic implemented.  
* [ ] 3.2 Parallel Pipeline: BaseCollector implemented with background Save and foreground Parse.  
* [ ] 3.3 Vectorization Integration: Integration of Embedding models (e.g., BGE-M3) for semantic storage.

### Phase 4: Analysis Results

* [ ] 4.1 Issue Summarization: Cross-media reporting summaries using LLM.  
* [ ] 4.2 Semantic Distance: Calculation of cosine similarity between media reports and party releases.  
* [ ] 4.3 Bias Detection: Quantitative analysis of media preference and information filtering.

## 5. Future Roadmap

* [ ] User Interfaces: Prism TUI (Terminal UI) and Web Dashboard.  
* [ ] Operational Monitoring: VictoriaMetrics Dashboard for tracking throughput and error rates.  
* [ ] Expansion: Direct Media API connectors and JS-rendered scraping via Playwright.
