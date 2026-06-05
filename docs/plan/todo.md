# Project Prism — TODO

Current sprint and pending checklist. Items move to `done.md` as they complete. Long-lived design lives in `spec.md`; deferred refactors in `future.md`.

## Phase 1 — Discovery Loop Execution & Pipeline Validation (Core)

* [ ] 1.1 Start Scheduler and Workers to process PENDING tasks.
* [ ] 1.2 Verify `DIRECTORY_FETCH` execution and `candidates` persistence.
* [ ] 1.3 Verify automatic `PAGE_FETCH` (PARTY) task creation and execution.
* [ ] 1.4 Validate full content extraction and archiving for party press releases.

## Phase 2.7 — User-Fetch Model (shipped)

Stages 1–4 complete; see `done.md` §"Phase 2.7 — User-Fetch Model" for the breakdown. Design rationale stays in `spec.md` §6 (`fetches` / `fetch_items`, three-status `POST /page_fetch` response, cross-user privacy). Unblocks Phase 2.8 (Operator TUI fetch monitor view) and `prism-mcp` bootstrap.

* [ ] **Notification (v2):** `fetches.completed_at` column persisted but unused in v1; v2 sets via sweeper or compute-on-write transition and fires webhook/email.

## Phase 2.8 — Operator TUI (`cmd/tui`)

* [ ] Bubble Tea app with four views — candidates list / submit-confirmation modal / batch monitor / content viewer.
* [ ] **List view:** filter bar (`q`, `source_abbr`, `since/until`), paginated table, multi-select (`space`), `f` to submit `POST /page_fetch`, `Enter` to view content, `b` to open fetch-request monitor by ID.
* [ ] **Submit modal:** show returned `fetch_id` and a per-candidate status table from `items[]` (`created` / `already_complete` / `not_found`); each row is keyed by `candidate_id` so the underlying list view can mark rows accordingly. Offer `[m] monitor` (jump to fetch view) / `[c] copy id` / `[↵] dismiss`. No `task_id` is ever displayed.
* [ ] **Fetch monitor view:** input fetch_id (or arrive from modal), client-pull `GET /fetches/{id}` on a fixed interval (default 5s; configurable via `--fetch-poll-interval`). Render progress bar + counters (pending / running / completed / failed / already_complete). Stop polling when `terminal=true`; `r` forces immediate refresh; `esc` back.
* [ ] **Content view:** render fetched content; if `GET /contents/{id}` returns 404, poll with backoff and show "waiting for collector" state; `y` copy URL, `o` open in browser.
* [ ] HTTP client reuses DTOs from `internal/http/api/` (no schema drift).
* [ ] `--api-url` flag (default `http://localhost:8090`), `--page-size` flag.
* [ ] Read-only + PAGE_FETCH trigger only; admin ops (pause/resume/replay) stay in Phase 4.2.

## Phase 2.9 — Layer 1 Tail (mostly shipped)

Phase A (`ArticleParser` removal + tests for kept components), the 2026-05 layer-1 tail closure (`parser/config` + `message` publisher path), and the SeaweedFS S3 test readiness fix are in `done.md`. The remaining open thread is the LLM-fallback parser (Immediate Next Steps #11), tracked in §"Immediate Next Steps".

## Phase 3 — Analysis Assets

* [ ] 3.1 Structured Extraction Persistence:
  * [ ] Persist `prompts`.
  * [ ] Persist `content_extractions`.
  * [ ] Persist extracted entities, topics, and phrases.
* [ ] 3.2 Vectorization (remaining):
  * [ ] Candidate embedding worker.
  * [ ] Content embedding worker.
* [ ] 3.3 Analysis:
  * [ ] Summarization over selected contents.
  * [ ] Semantic distance and clustering over fetched contents.

## Phase 4 — Monitoring and Operations

* [ ] 4.1 Operational Monitoring:
  * [ ] **Victoria OTLP observability foundation:** Prism emits OTLP logs, traces, and metrics to an OpenTelemetry Collector; collector exports logs to VictoriaLogs, traces to VictoriaTraces, and metrics to VictoriaMetrics; Grafana visualizes all three.
  * [ ] **Shared observability config in `internal/obs`:** every production/operator command reads and saves the same redaction-safe OTEL config (`RegisterTelemetryFlags`, `BindTelemetryFlags`, `LoadTelemetryConfig`, `SaveTelemetryConfig`), instead of duplicating config loading in each `cmd` package.
  * [ ] **Secret-safe observability logging:** never log plain passwords, API keys, tokens, secret keys, or OTLP headers; startup/config logs must use `SecretMask` or config types with `LogValue()` redaction.
  * [ ] **Command telemetry bootstrap:** initialize telemetry in scheduler, API, discovery, collector, planner, batch detector/publisher, backfiller, recover, and RSS; dev-only tools can stay noop unless explicitly opted in.
  * [ ] **Trace conventions:** root span per API request, scheduler tick, worker message, and recover run; child spans for fetch, parse, LLM/search provider calls, DB-heavy operations, and publish steps. Fix planner to inject propagated `trace_id` before starting its span.
  * [ ] **Metrics conventions:** use real-world OTEL instrument types: counters for monotonic totals (`tasks_dispatched_total`, `http_requests_total`), histograms for latency/size distributions (`worker_handle_duration`, `http_request_duration`, fetch bytes), up/down counters for in-flight work, and observable gauges for sampled state (`queue_lag`, cache size, open connections). Avoid high-cardinality URL/query/error labels.
  * [ ] **Compose/Grafana wiring:** add `otel-collector`, VictoriaMetrics, VictoriaTraces, Grafana datasource provisioning, starter dashboards, and worker container healthchecks.
* [ ] 4.2 Admin Operations:
  * [ ] Pause/resume discovery.
  * [ ] Replay failed tasks.
  * [ ] Inspect candidate and content ingestion state.

## Immediate Next Steps (items 11–15)

11. **LLM fallback parser** (Phase A + Phase B both shipped; see `done.md`):
    * [ ] **Per-input-type fallback (deferred plus):** `fallback.html` / `fallback.json` / `fallback.xml` with per-type prompt files. Requires content-type propagation through the F→M→T→P pipeline. v1's global `fallback.enable + fallback.llm` block is a foundation; the schema can be extended without breaking the global `enable` flag.
    * [ ] **Save-on-parse-failure path (PR2 from the original split):** when an assigned host's parser returns empty or errors AND fallback is disabled, archive the minified+transformed bytes with `recover_from: parse` metadata so `cmd/recover` can replay after a parsers.yaml fix. Touches `internal/collector/dispatcher.go` (empty-Article detection) + `cmd/recover` (new `recover_from` arm).
    * [ ] **Live-stack verification:** stand up `task test:e2e:page-fetch` against a candidate URL with no `parsers.yaml` entry and `fallback.enable=true` + a real provider key. Confirms end-to-end LLM extraction beyond the stub-generator unit tests.

12. **End-to-end smoke run incl. recover:** drive scheduler → discovery → collector → archiver → recover via `cmd/dev/fixture-server`; broad assertions only (`len(contents) > 0 && title != ""`). Replay-only path verified 2026-05-04 (item A); recover invocation not yet covered. (Containerize workers — formerly #12 — closed in Phase 4 / `done.md`.)

13. **Promote `S3Archiver` for production:** only after #11–#12 land. Deploy SeaweedFS or AWS S3, wire `--archive=s3://…`, configure lifecycle policy on `archives/` prefix per the retention plan in `future.md`.

14. After collector intake and configurable search providers are stable: candidate/content embedding workers; optional search-provider hardening (Brave / Google CSE named params maps, tracked replay fixtures, Google CSE credential validation).

15. **Secret-handling tail (per `docs/security.md` §5):** move `BRAVE_SEARCH_API` + `PGADMIN_PASSWORD` from env vars into `.secrets/`; resolve `migrate` / `psql` argv leak (dev-only, low priority); patch `secrets-bake.sh` to accept `SECRETS_DIR` env override (lets rotation-procedure validation use `.secrets-test/` without editing the script).

## Deferred until pipeline prototype is end-to-end working

These bundle for one cutover at the cloud-promotion phase, not piecemeal during prototype. Full design lives in `future.md`.

- Valkey-backed rate limiter
- Archive metadata catalog refactor
- `--mode={worker,lambda}` dispatch
- Scheduler `--once` short-lived migration
