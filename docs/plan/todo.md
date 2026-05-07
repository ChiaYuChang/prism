# Project Prism — TODO

Current sprint and pending checklist. Items move to `done.md` as they complete. Long-lived design lives in `spec.md`; deferred refactors in `future.md`.

## Phase 1 — Discovery Loop Execution & Pipeline Validation (Core)

* [ ] 1.1 Start Scheduler and Workers to process PENDING tasks.
* [ ] 1.2 Verify `DIRECTORY_FETCH` execution and `candidates` persistence.
* [ ] 1.3 Verify automatic `PAGE_FETCH` (PARTY) task creation and execution.
* [ ] 1.4 Validate full content extraction and archiving for party press releases.

## Phase 2.7 — User-Fetch Request Model (replaces "Async Batch Model")

* **Why:** `POST /page_fetch` is inherently async. The current per-candidate response is hostile to UI work (must iterate every task_id) and, for multi-user / multi-query scenarios, risks side-channel leakage (telling user B "1 of your 5 is `already_active`" reveals that user A is fetching the same URL). Discovery `batches` cannot serve as the user-facing observation unit because their completion semantics (`count(candidates) <= count(contents)`) do not apply, and overloading them couples user privacy to system internals. See `spec.md` §6 for the design clarification.
* **Design (per `spec.md` §6):** introduce a parallel `user_fetch_requests` + `user_fetch_request_items` pair as a user-facing observation layer; keep `batches` and `tasks.batch_id` semantics unchanged. Items reference the underlying active task by `task_id` (nullable for `ALREADY_COMPLETE` snapshots). Multiple requests can share the same task without seeing each other.
* [ ] **Schema migration:** new tables.
  * `user_fetch_requests (id UUID PK, user_id UUID NULL, created_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ NULL)`. `user_id` nullable for v1 (single-user); reserved for multi-user RBAC.
  * `user_fetch_request_items (request_id UUID FK, candidate_id UUID FK, task_id UUID FK NULL, snapshot_status TEXT NULL, PRIMARY KEY (request_id, candidate_id))`. Index `idx_request_items_task_id ON (task_id) WHERE task_id IS NOT NULL` for reverse lookup.
  * Edit `db/migrations/000001_init.up.sql` directly per memory `feedback_migration_style.md` (DB is empty during pre-prod prototype).
  * `task sqlc` to regenerate.
* [ ] **Repo:** new SQLC queries + repo interface methods.
  * `CreateUserFetchRequest(user_id) → UserFetchRequest`.
  * `CreateUserFetchRequestItem(request_id, candidate_id, task_id, snapshot_status) → Item`.
  * `GetUserFetchRequest(id) → UserFetchRequest`.
  * `GetUserFetchRequestProgress(id) → {total, pending, running, completed, failed, already_complete, terminal}` — single SQL aggregating `COALESCE(snapshot_status, tasks.status)` over items.
  * Add interface `repo.UserFetches` to `internal/repo/repo.go` + `pg` impl + mock regen via `task mocks`.
* [ ] **`CreateTask` returns existing active task on conflict.** Modify `db/queries/tasks.sql` `CreateTask` to use `INSERT … ON CONFLICT (kind, url) WHERE status IN ('PENDING','RUNNING') DO NOTHING RETURNING id, …`; add a follow-up `SELECT … WHERE kind='PAGE_FETCH' AND url=$ AND status IN ('PENDING','RUNNING')` for the conflict path (single-statement ON CONFLICT DO NOTHING does not return existing rows). Wrap both in a CTE so callers get one round-trip and a populated `task` row regardless of insert vs conflict; preserve `ErrTaskAlreadyActive` sentinel for callers that still want it.
* [ ] **Revise `POST /page_fetch` handler** (`internal/http/api/page_fetch.go`):
  * Accept `{candidate_ids: []uuid}`; cap at 100 (existing).
  * Create one `user_fetch_request` (user_id from authn context, NULL for v1).
  * For each candidate (preserve input order in response `items[]`):
    * not in DB → echo `{candidate_id, status:"not_found"}`; do **not** insert a `user_fetch_request_items` row.
    * try `CreateTask` → on success, insert item with `task_id=new_task.id`, `snapshot_status=NULL`; echo `{candidate_id, status:"created"}`.
    * on `ErrTaskAlreadyActive` → fetch existing active task by URL via `Tasks.GetActivePageFetchTaskByURL`; insert item with `task_id=existing.id`, `snapshot_status=NULL`; echo `{candidate_id, status:"created"}`. **`created` collapses fresh-insert and shared-active-task** — never expose `already_active` (cross-user / cross-request side-channel).
    * if `ErrTaskAlreadyActive` + lookup miss (task terminated between conflict and lookup): query `contents` by URL. If present, insert item with `task_id=NULL`, `snapshot_status='ALREADY_COMPLETE'`; echo `{candidate_id, status:"already_complete"}`. If absent, return `500` for the whole request — collector ordering (`CreateContent` → `CompleteTask`) makes this race window non-existent, so a miss here is a design-invariant violation.
  * Return `202 Accepted` with `{request_id, items: [{candidate_id, status}]}`, `status ∈ {created, already_complete, not_found}`. **Never** expose `task_id` or `already_active`. `not_found` items are response-only (no row in `user_fetch_request_items`), so `GET /fetches/{request_id}` progress counts only reflect stored items.
* [ ] **New endpoint `GET /api/v1/fetches/{request_id}`:** returns `{request_id, total, pending, running, completed, failed, already_complete, terminal}`. URL named `/fetches/` rather than `/batches/` so user-facing language does not leak the internal `batches` term.
  * Server-side Valkey cache on `fetch:progress:{request_id}` with ~1–2s TTL for live; 60s+ for terminal.
  * Server-side rate limit (per-IP or per-request_id) reusing `infra.RateLimiter`.
  * Client-pull model (5s fixed or `2→5→10s` backoff). No long-polling.
  * Future: filter by `user_id` from authn context (v1: no filter, single-user).
* [ ] **Drop `PageFetchTaskResult` / `PageFetchResponse.Results` from API** + regenerate Swagger (`task swag`).
* [ ] **Tests:**
  * `api_test.go`: same URL submitted by two requests → both items reference same task_id, both reach terminal when task COMPLETED, neither response leaks the other's existence.
  * `api_test.go`: candidate already in `contents` → item snapshot_status=`ALREADY_COMPLETE`, task_id=NULL, GET /fetches/{id} reports `already_complete:1` and `terminal:true`.
  * `api_test.go`: not_found candidate echoed as `{candidate_id, status:"not_found"}` in `items[]`, not stored as `user_fetch_request_items` row, `GET /fetches/{request_id}` progress counts unaffected.
  * `api_test.go`: response items preserve input candidate_id order; no `task_id` field is ever present in the response.
  * `api_test.go`: aggregator status is `COALESCE(snapshot_status, tasks.status)` — verify a mix.
  * Repo: `CreateTask` ON CONFLICT path returns existing task_id without raising error to caller (or raises sentinel, depending on chosen contract).
* [ ] **Notification deferred:** `user_fetch_requests.completed_at` column persisted but unused in v1; v2 will set it via sweeper or compute-on-write transition and fire webhook/email. Document in commit message that the column is reserved.
* [ ] **Update done.md / spec.md cross-refs after merge.**

This unblocks Phase 2.8 (Operator TUI), which consumes `GET /fetches/{request_id}` for the batch monitor view.

## Phase 2.8 — Operator TUI (`cmd/tui`)

* [ ] Bubble Tea app with four views — candidates list / submit-confirmation modal / batch monitor / content viewer.
* [ ] **List view:** filter bar (`q`, `source_abbr`, `since/until`), paginated table, multi-select (`space`), `f` to submit `POST /page_fetch`, `Enter` to view content, `b` to open fetch-request monitor by ID.
* [ ] **Submit modal:** show returned `request_id` and a per-candidate status table from `items[]` (`created` / `already_complete` / `not_found`); each row is keyed by `candidate_id` so the underlying list view can mark rows accordingly. Offer `[m] monitor` (jump to fetch view) / `[c] copy id` / `[↵] dismiss`. No `task_id` is ever displayed.
* [ ] **Fetch monitor view:** input request_id (or arrive from modal), client-pull `GET /fetches/{request_id}` on a fixed interval (default 5s; configurable via `--fetch-poll-interval`). Render progress bar + counters (pending / running / completed / failed / already_complete). Stop polling when `terminal=true`; `r` forces immediate refresh; `esc` back.
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

## Immediate Next Steps (items 11–15)

11. **Fill layer 1 unit test gaps (2.9), Phase B — config-opt-in LLM fallback parser** (Phase A done; see `done.md`):
    * [ ] Design fallback config schema: per-host empty config entry + `fallback: llm` flag (NOT a global "fallback all unmatched hosts" — too expensive, accidental coverage). Mechanism: to enable LLM extraction for a new host, operator adds an empty entry to `parsers.yaml` with `fallback: llm`. This makes LLM activation explicit, host-by-host, and reviewable.
    * [ ] Wire LLM provider into `config.BuildRegistry`: when entry has `fallback: llm`, build an LLM parser bound to that host and register it in the map; no global fallback path.
    * [ ] `cmd/dev/parse-probe`: when `--all-parsers` is set, include `__llm__` in the result map only if at least one host has `fallback: llm` configured (uses the configured LLM provider; no implicit baseline).
    * [ ] Add `Registry`/`config.BuildRegistry` tests for LLM fallback path (use a stub LLM provider; do not call real API in unit tests).
    * [ ] Add §6 Design Clarification (in `spec.md`): LLM has two roles — (a) parse-time extractor when host opts in via `fallback: llm`, (b) config-snippet generator for human review (`ToConfigSnippet`). Both are explicitly opt-in; there is no automatic promotion path from LLM output to formalized parser rules. The intended workflow for adopting a new site: operator adds empty config + `fallback: llm` → site is parseable via LLM → operator reviews extracted samples + LLM-generated snippet → if quality acceptable, operator promotes the snippet to a real `html`/`jsonld` rule and removes the `fallback` flag.
    * [ ] Update todo.md / done.md after each item completes.
    * Fix `s3_test.go` flake separately. Defer layer 2 (`internal/repo/pg` testcontainers) until layer 1 lands.

12. **End-to-end smoke run incl. recover:** drive scheduler → discovery → collector → archiver → recover via `cmd/dev/fixture-server`; broad assertions only (`len(contents) > 0 && title != ""`). Replay-only path verified 2026-05-04 (item A); recover invocation not yet covered. (Containerize workers — formerly #12 — closed in Phase 4 / `done.md`.)

13. **Promote `S3Archiver` for production:** only after #11–#12 land. Deploy SeaweedFS or AWS S3, wire `--archive=s3://…`, configure lifecycle policy on `archives/` prefix per the retention plan in `future.md`.

14. After collector intake is stable: candidate/content embedding workers; planner KEYWORD_SEARCH wiring.

15. **Secret-handling tail (per `docs/security.md` §5):** move `BRAVE_SEARCH_API` + `PGADMIN_PASSWORD` from env vars into `.secrets/`; resolve `migrate` / `psql` argv leak (dev-only, low priority); patch `secrets-bake.sh` to accept `SECRETS_DIR` env override (lets rotation-procedure validation use `.secrets-test/` without editing the script).

## Deferred until pipeline prototype is end-to-end working

These bundle for one cutover at the cloud-promotion phase, not piecemeal during prototype. Full design lives in `future.md`.

- Valkey-backed rate limiter
- Archive metadata catalog refactor
- `--mode={worker,lambda}` dispatch
- Scheduler `--once` short-lived migration
