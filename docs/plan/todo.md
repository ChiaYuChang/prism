# Project Prism — TODO

Current sprint and pending checklist. Items move to `done.md` as they complete. Long-lived design lives in `spec.md`; deferred refactors in `future.md`.

## Phase 1 — Discovery Loop Execution & Pipeline Validation (Core)

* [ ] 1.1 Start Scheduler and Workers to process PENDING tasks.
* [ ] 1.2 Verify `DIRECTORY_FETCH` execution and `candidates` persistence.
* [ ] 1.3 Verify automatic `PAGE_FETCH` (PARTY) task creation and execution.
* [ ] 1.4 Validate full content extraction and archiving for party press releases.

## Phase 2.7 — Async Batch Model for User-Triggered Fetches

* **Why:** `POST /page_fetch` is inherently async (collector may take seconds–minutes per article, rate-limited per source). A synchronous per-candidate response invites users to wait on the HTTP call and treats each candidate as independent. Grouping user-selected candidates into one `batch_id` matches the existing batch primitive, gives one thing to monitor / notify on, and lets the UI (TUI or future web) track progress without iterating individual task IDs.
* [ ] **Revise `POST /page_fetch` response:** accept `candidate_ids`, create one fresh `batch_id` for the user request (distinct from each candidate's original discovery batch), insert MEDIA + PAGE_FETCH tasks under that batch, return `{batch_id, task_count, skipped_count}` — no per-candidate results.
* [ ] **New endpoint `GET /batches/{batch_id}`:** returns progress — `{batch_id, total, pending, running, completed, failed, contents_ready, terminal}`. Plain request/response, no long-polling — client-pull model:
  * Client drives the cadence (hardcoded in TUI, e.g. 5s fixed, or `2s → 5s → 10s` backoff). Avoid high-frequency polling on the client side.
  * Server-side **Valkey cache** on `batch:progress:{batch_id}` with ~1–2s TTL; N concurrent watchers of the same batch collapse to one DB hit per TTL window. Terminal batches cache for 60s+ (they won't change).
  * Server-side **rate limit** on this endpoint (reuse existing Valkey token bucket infra) — per-IP or per-batch_id — to cap runaway clients. Same `infra.RateLimiter` interface used by the scheduler.
  * Rationale: long-polling is hostile to short-lived / serverless deployments (it holds connections) and adds fragility; client-pull + short TTL cache is simpler, cheaper, and deploy-agnostic.
* [ ] **Batch completion signal:** reuse existing `BatchTrigger` / `batch.completed` infra; user-triggered MEDIA batches participate in the same lifecycle as PARTY batches.
* [ ] **Optional completion notification (future):** email / webhook fired when a user-triggered batch reaches completion. Not in v1; design the batch progress endpoint so notification is an add-on, not a rewrite.

## Phase 2.8 — Operator TUI (`cmd/tui`)

* [ ] Bubble Tea app with four views — candidates list / submit-confirmation modal / batch monitor / content viewer.
* [ ] **List view:** filter bar (`q`, `source_abbr`, `since/until`), paginated table, multi-select (`space`), `f` to submit `POST /page_fetch`, `Enter` to view content, `b` to open batch monitor by ID.
* [ ] **Submit modal:** minimal — show returned `batch_id`, `task_count`, `skipped_count`, offer `[m] monitor` (jump to batch view) / `[c] copy id` / `[↵] dismiss`. No per-candidate status enumeration.
* [ ] **Batch monitor view:** input batch_id (or arrive from modal), client-pull `GET /batches/{batch_id}` on a fixed interval (default 5s; configurable via `--batch-poll-interval`). Render progress bar + counters (pending / running / completed / failed). Stop polling when `terminal=true`; `r` forces immediate refresh; `esc` back.
* [ ] **Content view:** render fetched content; if `GET /contents/{id}` returns 404, poll with backoff and show "waiting for collector" state; `y` copy URL, `o` open in browser.
* [ ] HTTP client reuses DTOs from `internal/http/api/` (no schema drift).
* [ ] `--api-url` flag (default `http://localhost:8090`), `--page-size` flag.
* [ ] Read-only + PAGE_FETCH trigger only; admin ops (pause/resume/replay) stay in Phase 4.2.

## Phase 2.9 — Unit Test Coverage Consolidation (Layer 1)

* **Why:** industry-standard four-layer test strategy — (1) unit tests with mocked boundaries, (2) component/contract tests with real dependencies via testcontainers, (3) small number of E2E happy paths on local fixtures, (4) scheduled real-site smoke. Layer 1 is the widest net: each component asserts its own input→output correctness against fixed fixtures; E2E then only has to prove the wiring, not per-component correctness. Current layer 1 has holes in exactly the packages where bugs bite hardest — pipeline orchestration and cross-service contracts.
* [ ] **`internal/collector` (Dispatcher)** — 0% coverage. Test F→M→T→(S‖P) orchestration with mocked Fetcher / Minifier / Transformer / Parser / Saver. Assert stage error propagation (`StageError.Stage`, `.Intermediate`), avoid-refetch branches, PARTY vs MEDIA routing.
* [ ] **`internal/collector/parser/{html,jsonld}`** — 0% coverage. Fixture-driven tests feeding real HTML from `testdata/fixtures/` (DPP / TPP / Yahoo / CNA) through each parser; assert title/author/date/content extraction. Catches per-source DOM drift.
* [ ] **`internal/collector/minifier`** — 0% coverage. Small HTML fixtures; assert idempotency (minify(minify(x)) == minify(x)) and that noise (script/style/nav) is stripped while article body survives.
* [ ] **`internal/message`** — 0% coverage. JSON round-trip tests for every signal (`TaskSignal`, `BatchCompletedSignal`). JSON tag typos here silently break cross-service delivery and nothing else catches them.
* [ ] **`internal/model`** — 0% coverage. `Candidates.Fingerprint()` determinism + URL-normalization edge cases (trailing slash, query order, fragment).
* [ ] **Fix `internal/collector/archiver/s3_test.go` flake** — currently fails intermittently with `StatusCode: 0, connection reset` on `CreateBucket`. Likely SeaweedFS 4.05 container readiness race (`wait.ForHTTP("/cluster/status")` returns before S3 listener accepts). Switch wait strategy or add retry on bucket creation.
* **Out of scope for layer 1:** `internal/llm/{gemini,ollama,openai}` provider adapters (real API calls cheaper to verify manually); `internal/repo/pg` (layer 2 / testcontainers); `internal/appconfig`, `infra`, `obs`, `pkg/{logger,utils,functional}` (plumbing / trivial).

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
  * [ ] VictoriaLogs integration.
  * [ ] Grafana dashboards for throughput and failure visibility.
* [ ] 4.2 Admin Operations:
  * [ ] Pause/resume discovery.
  * [ ] Replay failed tasks.
  * [ ] Inspect candidate and content ingestion state.

## Immediate Next Steps (items 11–17)

11. **Fill layer 1 unit test gaps (2.9), Phase B — config-opt-in LLM fallback parser** (Phase A done; see `done.md`):
    * [ ] Design fallback config schema: per-host empty config entry + `fallback: llm` flag (NOT a global "fallback all unmatched hosts" — too expensive, accidental coverage). Mechanism: to enable LLM extraction for a new host, operator adds an empty entry to `parsers.yaml` with `fallback: llm`. This makes LLM activation explicit, host-by-host, and reviewable.
    * [ ] Wire LLM provider into `config.BuildRegistry`: when entry has `fallback: llm`, build an LLM parser bound to that host and register it in the map; no global fallback path.
    * [ ] `cmd/dev/parse-probe`: when `--all-parsers` is set, include `__llm__` in the result map only if at least one host has `fallback: llm` configured (uses the configured LLM provider; no implicit baseline).
    * [ ] Add `Registry`/`config.BuildRegistry` tests for LLM fallback path (use a stub LLM provider; do not call real API in unit tests).
    * [ ] Add §6 Design Clarification (in `spec.md`): LLM has two roles — (a) parse-time extractor when host opts in via `fallback: llm`, (b) config-snippet generator for human review (`ToConfigSnippet`). Both are explicitly opt-in; there is no automatic promotion path from LLM output to formalized parser rules. The intended workflow for adopting a new site: operator adds empty config + `fallback: llm` → site is parseable via LLM → operator reviews extracted samples + LLM-generated snippet → if quality acceptable, operator promotes the snippet to a real `html`/`jsonld` rule and removes the `fallback` flag.
    * [ ] Update todo.md / done.md after each item completes.
    * Fix `s3_test.go` flake separately. Defer layer 2 (`internal/repo/pg` testcontainers) until layer 1 lands.

12. **Wire archive publisher end-to-end:** flip `archivePublisher` from `nil` to the real `msgr` instance in `cmd/worker/collector/main.go`; bring up `cmd/worker/archiver` to consume `prism.archive`; verify a PARTY page fetch produces a stored archive object.

13. **Extend `cmd/recover` to dual-path replay:** support both `recover_from=Minify` (raw → M+T+P) and `recover_from=Transform/Parse` (minified/canonical → T+P or P only). Required to unblock the deferred Transform/Parse archive cases in `handler.go` (currently TODO).

14. **Annotate deprecated archiver APIs in code:** add doc comments on `Archiver.Scan`, `Archiver.Remove`, `meta.go`, and the `YYYY/MM/DD/<traceID>` path layout pointing to the relevant Future Roadmap items. Goal: any new contributor reading the code sees the direction without needing plan.md context.

15. **End-to-end smoke run:** drive a full happy path through scheduler → discovery → collector → archiver → recover via `cmd/dev/fixture-server`; broad assertions only (`len(contents) > 0 && title != ""`). Do not add per-source matrices.

16. **Promote `S3Archiver` for production:** only after #11–#15 land. Deploy SeaweedFS or AWS S3, wire `--archive=s3://…`, configure lifecycle policy on `archives/` prefix per the retention plan in `future.md`.

17. After collector intake is stable: candidate/content embedding workers; planner KEYWORD_SEARCH wiring.

## Deferred until pipeline prototype is end-to-end working

These bundle for one cutover at the cloud-promotion phase, not piecemeal during prototype. Full design lives in `future.md`.

- Valkey-backed rate limiter
- Archive metadata catalog refactor
- `--mode={worker,lambda}` dispatch
- Scheduler `--once` short-lived migration
