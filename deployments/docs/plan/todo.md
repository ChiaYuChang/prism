# Project Prism - TODO

Current server-side work and deployment validation. Completed work moves to
`done.md`; stable behavior belongs in `spec.md`; deliberately deferred design
belongs in `future.md`.

## Bootstrap And Deployment Validation

- [ ] Initialize root authentication and create the first admin token.
- [x] Register sources with `prismctl admin sources sync`.
- [ ] Register models with the admin API; names must match worker configuration.
- [x] Upload versioned prompts with `prismctl admin prompts upload`.
- [ ] Run `task test:bootstrap` after `task deploy:test` and verify source,
  model, and planner-prompt setup without calling an LLM or search provider.
- [ ] Deploy the local stack and verify discovery, PARTY collection, planner
  execution, analyzer pipeline execution, archiving, logs, metrics, and traces.

Use `prismctl admin` inspection commands first. Use direct Postgres queries only
as an independent audit when the CLI does not expose the required state.

## Pre-Analysis Fetch And Confirmation

Detailed server-side contract and acceptance tests: [`pre-analysis.md`](../../../pre-analysis.md).
Client implementation is outside this repository; the API remains defensive
against malformed client requests.

- [ ] Add `POST /analysis/preflight` to classify selected IDs as available or
  unavailable without creating work. A candidate is available only when it
  exists and its URL can be normalized into a canonical URL.
- [ ] Add analysis-run persistence for topic, brief, selected IDs, failure
  policy, acquisition session, state, and report provenance.
- [ ] Reject an empty candidate-ID list with `ANALYSIS_INPUT_EMPTY`; create no
  fetch session/item, analysis run, pipeline Root, or P0.
- [ ] Create/reuse shared `PAGE_FETCH` tasks by canonical URL while keeping
  each analysis run's fetch-item membership private and cancellable.
- [ ] Expand `GET /fetches/{id}` to return grouped candidate IDs in the
  user-facing `PENDING`, `FETCHING`, `READY`, and `FAILED` states.
- [ ] Implement terminal resolution: `STOP` -> `AWAITING_RESOLUTION` on
  failures; `IGNORE_FAILED` starts only with at least one READY input; zero
  READY inputs fails the run without Root/P0.
- [ ] Add retry/ignore/cancel resolution APIs. Cancelling a run detaches only
  its fetch items and never cancels globally shared `PAGE_FETCH` work.
- [ ] Persist and revalidate the confirmed READY manifest atomically with
  Root/P0 creation. If a confirmed input disappears, fail with
  `INPUT_CANDIDATE_MISSING`; never substitute a newer input.
- [ ] Add PostgreSQL-backed acceptance tests for preflight, shared work,
  progress grouping, failure policy, cancellation isolation, and manifest
  revalidation.

## Analysis Assets

The declarative analyzer runtime and embedding pipeline are shipped; see
`done.md` and `../../pipeline.md`. The deployed definition is currently limited
to `EMBED_CANDIDATE` and `EMBED_CONTENT`.

- [ ] Persist `content_extractions`, extracted entities, topics, and phrases.
- [ ] Define and implement corpus-dependent analysis stages after the
  pre-analysis confirmed-manifest gate exists.
- [ ] Add summarization, semantic distance, clustering, and report persistence
  over the confirmed corpus.

## Monitoring And Operations

- [ ] Verify deployment observability: `/healthz`, `/readyz`, `/metrics`,
  service logs, trace propagation, and `trace_id` correlation across API,
  scheduler, and workers.
- [ ] Decide whether `cmd/recover`, RSS/dev tools, and operator-only commands
  need full telemetry or intentionally lightweight/noop telemetry.
- [ ] Add queue/cache gauges only when deployment evidence shows an operational
  need.
- [ ] Create review-ready dashboards and alerts for scheduler, workers,
  providers, API health, Postgres, and Valkey.
- [ ] Add a separate pinned GitHub Actions lint job after confirming the local
  `golangci-lint` version and a clean full run. Keep short tests unchanged.
- [ ] Document the laptop deployment runbook: secrets/config/Compose bake,
  migrations, application and worker startup, health checks, and teardown.
- [ ] Run `cmd/recover status/list/run --dry-run` against local archives after
  the synthetic-fixture split.
- [ ] Document API/DB inspection for runnable and failed tasks, candidates,
  fetch progress, content ingestion, and analyzer runs.
- [ ] Validate `task deploy:test` with persistent volumes, one PAGE_FETCH,
  terminal fetch progress, content/archive visibility, and telemetry.
- [ ] Port the validated Docker path to Podman for the home server, then define
  Postgres and object-storage backup/restore before treating it as durable.

## Deferred Product Clients

Client applications are not implemented in this repository.

- [ ] Operator TUI or web UI consuming the API's candidate selection,
  preflight, analysis-run, fetch-progress, and content endpoints.
- [ ] Notification delivery after terminal acquisition or analysis completion.
