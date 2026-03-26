# Database Tables

This document explains the meaning of each database table in Prism after the shift from the old `fingerprints + keywords` model to the current `candidates + task-driven discovery + structured analysis assets` model.

## Design Goals

The current schema is shaped by four constraints:

1. Prism does not crawl all news. It starts from high-signal seed texts, mainly party press releases.
2. Search results should first enter a lightweight candidate pool before full-page fetching.
3. Scheduler and workers should operate on explicit request-oriented tasks.
4. Discovery should optimize for broad recall using short keyword sets, while semantic extraction and clustering remain optional analysis tools.

## High-Level Flow

1. A cron job creates one `PARTY + DIRECTORY_FETCH` batch in `tasks`.
2. The scheduler claims runnable tasks and publishes them to MQ.
3. `Scout` consumes the tasks, downloads the directory pages, and writes article briefs into `candidates`.
4. `Scout` emits `PAGE_FETCH` messages for discovered article URLs.
5. The data pipeline fetches full press release pages and writes them to `contents`.
6. Candidate insertion triggers candidate embedding generation, including party press release briefs.
7. Content insertion triggers full-content embedding generation.
8. `Planner` waits until the current PARTY batch finishes, loads the batch contents, and asks the LLM to produce short keyword groups.
9. `Planner` creates one or more `MEDIA + DIRECTORY_FETCH` tasks whose request details are stored in task `payload`.
10. The scheduler dispatches those MEDIA tasks to `Scout`.
11. `Scout` calls search APIs or downloads result pages and writes the returned article briefs into `candidates`.
12. User-selected candidates are later promoted into full `contents`.
13. Structured extraction and clustering may run later as analysis steps, but they are not part of the minimal discovery critical path.

## Table Groups

The tables fall into six groups:

- registries: `models`, `sources`, `prompts`
- discovery intake: `candidates`
- full content storage: `contents`
- discovery execution: `tasks`
- analysis assets: `content_extractions`, `entities`, `content_extraction_entities`, `content_extraction_topics`, `content_extraction_phrases`
- vector assets: `candidate_embeddings_gemma_2025`, `content_embeddings_gemma_2025`

## Table Definitions

### `models`

Purpose:

- Registry for models used by Prism.

Why it exists:

- Extraction, embedding, and future summarization should remain auditable.

Important columns:

- `name`
- `provider`
- `type`
- `tag`

### `sources`

Purpose:

- Registry for party sites and media sites.

Why it exists:

- Both `candidates` and `contents` need stable source attribution.

Important columns:

- `abbr`
- `type`
- `base_url`

### `prompts`

Purpose:

- Registry for prompt assets used by structured analysis.

Why it exists:

- Prompt identity should be persisted once and referenced from extraction results.

Important columns:

- `hash`
- `path`

### `candidates`

Purpose:

- The candidate pool for article briefs before full fetching.

Why it exists:

- Search and subscription outputs usually begin as title, description, URL, and date only.
- Party press release directory crawls also first produce the same brief-shaped records.
- Users or downstream workers should be able to search and select from this pool before downloading full pages.
- `batch_id` can be copied into candidates for later traceability and batch-level diagnostics.

Important columns:

- `fingerprint`
- `batch_id`
- `ingestion_method`
- `title`
- `description`
- `url`
- `published_at`

Notes:

- `fingerprint` is a deduplication key, not a separate table.

### `candidate_embeddings_gemma_2025`

Purpose:

- Vector index for candidate-level retrieval.

Why it exists:

- Candidate search should support both text search and vector search.

Important columns:

- `candidate_id`
- `model_id`
- `category`

### `contents`

Purpose:

- Full fetched article content.

Why it exists:

- `contents` are the durable parsed assets used by downstream analysis.
- Party press release contents are usually fetched immediately after their directory-page briefs are inserted into `candidates`.
- `batch_id` can be used to load one cron-triggered party intake batch for planner work.

Important columns:

- `candidate_id`
- `batch_id`
- `type`
- `content`

Notes:

- Party press releases can be inserted directly into `contents` even if they are first observed through a candidate-like directory listing.

### `content_embeddings_gemma_2025`

Purpose:

- Full-content embeddings.

Why it exists:

- Later analysis operates on the full fetched article, not only on brief metadata.

Important columns:

- `content_id`
- `model_id`
- `category`

### `tasks`

Purpose:

- Runnable request-oriented tasks claimed by the scheduler and processed by workers.

Why it exists:

- Scheduler and workers should operate on explicit tasks rather than ad hoc business logic.
- One task row should contain enough information to construct a fetch or search request.
- `batch_id` groups one cron- or trigger-generated batch so that planner logic can reason about completion.

Important columns:

- `batch_id`
- `kind`
- `source_type`
- `source_id`
- `url`
- `payload`
- `frequency`
- `next_run_at`
- `expires_at`
- `status`
- `retry_count`
- `last_run_at`

Notes:

- `DIRECTORY_FETCH` is the current primary task kind.
- `PARTY + DIRECTORY_FETCH` tasks point to party press release directory pages.
- `MEDIA + DIRECTORY_FETCH` tasks point to search providers or media result pages.
- Search keywords belong in task `payload`, not as first-class task columns.
- `PAGE_FETCH` is reserved for future persistence if page-fetch jobs are later moved into the same task table.

### `content_extractions`

Purpose:

- One persisted structured extraction result for one content item under a specific prompt, model, and schema version.

Why it exists:

- Extractor output is a first-class analysis asset.

Important columns:

- `content_id`
- `model_id`
- `prompt_id`
- `schema_name`
- `schema_version`
- `title`
- `summary`
- `raw_result`

### `entities`

Purpose:

- Dictionary of normalized entities.

Why it exists:

- Entity canonical names should be deduplicated for reuse and later analysis.

Important columns:

- `canonical`
- `type`

### `content_extraction_entities`

Purpose:

- Links one extraction result to its entities.

Why it exists:

- Keeps the canonical entity separate from the observed wording in the article.

Important columns:

- `extraction_id`
- `entity_id`
- `surface`
- `ordinal`

### `content_extraction_topics`

Purpose:

- Stores extraction topics as text labels per extraction result.

Why it exists:

- Topics are currently kept simple and do not yet use a shared dictionary table.

Important columns:

- `extraction_id`
- `topic_text`
- `ordinal`

### `content_extraction_phrases`

Purpose:

- Stores search-oriented phrases extracted from a single content item.

Why it exists:

- These phrases are useful analysis outputs even if they are no longer the primary discovery mechanism.

Important columns:

- `extraction_id`
- `phrase`
- `ordinal`

## Why The Schema Changed

Compared with the old design, the current schema makes the following shifts:

### From `fingerprints` to `candidates`

Old idea:

- search results were buffered in a lightweight fingerprint table

Current idea:

- discovery output is a candidate article brief
- fingerprint remains a dedup key, but the persisted asset is the candidate

### From flat `keywords` to analysis assets

Old idea:

- extraction output was approximated as keywords

Current idea:

- extraction output is preserved as structured analysis data with title, entities, topics, phrases, and summary

### From cluster-driven discovery to task-driven discovery

Old idea:

- discovery tasks were derived from clustered summaries

Current idea:

- discovery is driven by explicit request-oriented tasks
- keyword groups are part of MEDIA task payloads
- clustering is still useful, but it belongs to the analysis side of the system

## Current Limits

The current schema intentionally keeps discovery simple and does not yet model:

- cluster lineage edges
- cross-day cluster DAGs
- advanced search-intent graphs

These remain future work and are documented in `plan.md`.
