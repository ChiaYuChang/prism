# Query Plan

This document defines the intended SQL query surface for Prism before final SQL implementation and `sqlc` code generation.

Reference:
- Table semantics are documented in [database-tables.md](/home/cychang/Documents/prism/docs/database-tables.md).

## Goals

The query layer should support:

1. seed intake from party release directories
2. candidate ingestion from search or subscription sources
3. content fetching and promotion from candidate to full content
4. task creation, scheduling, and execution
5. optional structured extraction persistence
6. optional candidate and content embeddings

## Design Principles

- Queries should reflect the current `tasks -> candidates -> contents` discovery flow.
- Semantic extraction queries should be optional and not required by the discovery path.
- Query names should describe domain intent, not implementation detail.
- Writes should be idempotent where possible.

## Query Groups

- registry queries
- candidate queries
- content queries
- task queries
- extraction queries
- embedding queries

## 1. Registry Queries

### `GetSourceByID :one`
Purpose:
- Load one source registry row by primary key.

### `GetSourceByAbbr :one`
Purpose:
- Look up a source by its stable abbreviation.

### `ListSourcesByType :many`
Purpose:
- Load all party or media sources for scheduled jobs.

### `GetModelByID :one`
Purpose:
- Load one model registry row by primary key.

### `GetModelByNameAndType :one`
Purpose:
- Resolve a model row for extractor, embedder, or analyzer usage.

### `GetPromptByID :one`
Purpose:
- Load one prompt registry row by primary key.

### `GetPromptByHash :one`
Purpose:
- Check whether a prompt is already registered.

### `UpsertPrompt :one`
Purpose:
- Insert a prompt row if missing and return its ID.

## 2. Candidate Queries

### `GetCandidateByID :one`
Purpose:
- Load one candidate row by primary key.

### `GetCandidateByFingerprint :one`
Purpose:
- Check whether a candidate already exists by dedup key.

### `CreateCandidate :one`
Purpose:
- Insert one new candidate brief.

### `UpsertCandidate :one`
Purpose:
- Create a candidate if new, otherwise reuse the existing row.

### `ListCandidatesForAnalysis :many`
Purpose:
- List candidate briefs for user selection or system recommendation.

### `SearchCandidatesByText :many`
Purpose:
- Search candidate title and description.

## 3. Content Queries

### `GetContentByID :one`
Purpose:
- Load one content row by primary key.

### `GetContentByURL :one`
Purpose:
- Check whether the full article already exists by URL.

### `GetContentByCandidateID :one`
Purpose:
- Check whether a candidate has already been promoted into content.

### `CreateContent :one`
Purpose:
- Insert one full content row.

### `UpdateContentMetadata :one`
Purpose:
- Update author, published time, or parsed metadata after fetch.

### `ListRecentSeedContents :many`
Purpose:
- Load recent party press releases for keyword generation.

### `ListContentsByBatchID :many`
Purpose:
- Load one completed PARTY batch for planner work.

## 4. Task Queries

### `GetTaskByID :one`
Purpose:
- Load one task by primary key.

### `ListTasksByBatchID :many`
Purpose:
- Load tasks that belong to one batch.

### `CreateTask :one`
Purpose:
- Insert one new task row.

### `ClaimTasks :many`
Purpose:
- Atomically claim runnable or zombie tasks and mark them as running.

### `CompleteTask :exec`
Purpose:
- Mark a task as handled and schedule its next run if needed.

### `FailTask :exec`
Purpose:
- Mark a task as failed.

### `ListRunnableTasks :many`
Purpose:
- Diagnostic query for observing pending or overdue tasks.

## 5. Extraction Queries

### `GetContentExtractionByID :one`
Purpose:
- Load one extraction row by primary key.

### `GetContentExtractionSnapshot :one`
Purpose:
- Check whether an extraction already exists for the same content, model, prompt, and schema version.

### `CreateContentExtraction :one`
Purpose:
- Insert one extraction result row.

### `GetEntityByCanonicalAndType :one`
Purpose:
- Resolve an entity dictionary row before relation insertion.

### `UpsertEntity :one`
Purpose:
- Insert or reuse an entity dictionary row.

### `CreateContentExtractionEntity :exec`
Purpose:
- Link one extraction to one entity.

### `ReplaceContentExtractionTopics :exec`
Purpose:
- Rewrite topic rows for one extraction.

### `ReplaceContentExtractionPhrases :exec`
Purpose:
- Rewrite phrase rows for one extraction.

## 6. Embedding Queries

### `CreateCandidateEmbeddingGemma2025 :one`
Purpose:
- Insert one candidate embedding row.

### `CreateContentEmbeddingGemma2025 :one`
Purpose:
- Insert one content embedding row.

### `ListCandidateEmbeddingsByCandidateID :many`
Purpose:
- Load candidate embeddings for one candidate.

### `ListContentEmbeddingsByContentID :many`
Purpose:
- Load content embeddings for one content row.

### `SearchCandidatesByVector :many`
Purpose:
- Retrieve semantically similar candidate briefs.

### `SearchContentsByVector :many`
Purpose:
- Retrieve semantically similar full articles.

## Suggested SQL File Layout

- `db/queries/registry.sql`
- `db/queries/candidates.sql`
- `db/queries/contents.sql`
- `db/queries/tasks.sql`
- `db/queries/extractions.sql`
- `db/queries/embeddings.sql`

## Immediate Next Step

Before writing SQL, the next discussion should settle:

1. whether `PAGE_FETCH` should remain MQ-only or also persist into `tasks`
2. how planner should determine one PARTY `batch_id` is fully complete
3. whether candidate upsert should update title/description when the same fingerprint is rediscovered
4. whether extraction writes should be transactional as one unit
