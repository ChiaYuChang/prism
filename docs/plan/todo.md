# Project Prism — TODO

Current sprint and pending checklist. Items move to `done.md` as they complete. Long-lived design lives in `spec.md`; deferred refactors in `future.md`.

## Phase 1 — Discovery Loop Execution & Pipeline Validation (Core)

* [ ] 1.1 Start Scheduler and Workers to process PENDING tasks.
* [ ] 1.2 Verify `DIRECTORY_FETCH` execution and `candidates` persistence.
* [ ] 1.3 Verify automatic `PAGE_FETCH` (PARTY) task creation and execution.
* [ ] 1.4 Validate full content extraction and archiving for party press releases.

## Phase 2.7 — User-Fetch Model (replaces "Async Batch Model")

* **Why:** `POST /page_fetch` is inherently async. The current per-candidate response is hostile to UI work (must iterate every task_id) and, for multi-user / multi-query scenarios, risks side-channel leakage (telling user B "1 of your 5 is `already_active`" reveals that user A is fetching the same URL). Discovery `batches` cannot serve as the user-facing observation unit because their completion semantics (`count(candidates) <= count(contents)`) do not apply, and overloading them couples user privacy to system internals. See `spec.md` §6 for the design clarification.
* **Design (per `spec.md` §6):** parallel `fetches` + `fetch_items` pair as user-facing observation layer; keep `batches` and `tasks.batch_id` semantics unchanged. Items reference the underlying active task by `task_id` (nullable for `ALREADY_COMPLETE` snapshots). Multiple fetches can share the same task without seeing each other. Tables sit in `public` schema today; future migration moves them to a dedicated `user` schema (`user.fetches`, `user.fetch_items`, `user.users`) via `ALTER TABLE ... SET SCHEMA "user"` — no rename needed.
* [x] **Schema migration:** new tables.
  * `fetches (id UUID PK, user_id UUID NULL, created_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ NULL)`. `user_id` nullable for v1 (single-user); reserved for multi-user RBAC.
  * `fetch_items (fetch_id UUID FK, candidate_id UUID FK, task_id UUID FK NULL, snapshot_status TEXT NULL, PRIMARY KEY (fetch_id, candidate_id))`. Index `idx_fetch_items_task_id ON (task_id) WHERE task_id IS NOT NULL` for reverse lookup.
  * Edited `db/migrations/000001_init.up.sql` directly per memory `feedback_migration_style.md` (DB empty during pre-prod prototype).
* [x] **Repo:** SQLC queries + repo interface methods.
  * `Create(user_id) → UserFetch`, `Get(id) → UserFetch`, `CreateItem(fetch_id, candidate_id, task_id, snapshot_status) → UserFetchItem`, `GetProgress(id) → {total, pending, running, completed, failed, already_complete, terminal}`, `MarkCompleted(id)` (v2 reserved).
  * Interface `repo.UserFetches` in `internal/repo/repo.go` + `pg.PGUserFetches` impl + mocks via `task mocks`.
* [x] **`CreateTask` returns existing active task on conflict.** `db/queries/tasks.sql` `CreateTask` is now a CTE: `INSERT … ON CONFLICT DO NOTHING RETURNING tasks.*` UNION ALL with a `SELECT tasks.*, FALSE AS inserted FROM tasks WHERE NOT EXISTS (SELECT 1 FROM ins) AND status IN ('PENDING','RUNNING')` recovery branch covering both `uq_tasks_active_page_fetch` (PAGE_FETCH + url) and `uq_tasks_active_payload` (source_abbr + kind + payload_hash). Adapter maps `inserted=false` → `(task, repo.ErrTaskAlreadyActive)` so the user-fetch handler reads `task.ID` directly with no second round-trip; the standalone `GetActivePageFetchTaskByURL` helper is removed. Race window (conflict at insert + colliding row no longer PENDING/RUNNING by recovery-SELECT) returns `pgx.ErrNoRows`, which the adapter still maps to `ErrTaskAlreadyActive` with a zero `task.ID`; the handler detects the zero ID and falls back to a `contents` lookup for the `already_complete` snapshot path.
* [x] **Revised `POST /page_fetch` handler** (`internal/http/api/page_fetch.go`):
  * Accepts `{candidate_ids: []uuid}`; cap at 100.
  * Creates one `fetches` row (user_id from authn context, NULL for v1).
  * Per candidate (response `items[]` preserves input order):
    * not in DB → echo `{candidate_id, status:"not_found"}`; **no** `fetch_items` row.
    * `CreateTask` success → insert item with `task_id=new_task.id`, `snapshot_status=NULL`; echo `created`.
    * `ErrTaskAlreadyActive` with populated `task.ID` (CTE recovery branch) → insert item with `task_id=task.id`, `snapshot_status=NULL`; echo `created`. **`created` collapses fresh-insert and shared-active-task** — never expose `already_active`.
    * `ErrTaskAlreadyActive` with zero `task.ID` (race: conflict at insert + colliding row no longer active by recovery-SELECT) → query `contents` by URL. If present, insert item with `task_id=NULL`, `snapshot_status='ALREADY_COMPLETE'`; echo `already_complete`. If absent, return `500` — collector ordering (`CreateContent` → `CompleteTask`) makes this window non-existent, so miss = invariant violation.
  * Returns `202 Accepted` with `{fetch_id, items: [{candidate_id, status}]}`, `status ∈ {created, already_complete, not_found}`. **Never** expose `task_id` or `already_active`. `not_found` items are response-only (no row in `fetch_items`), so progress counts only reflect stored items.
* [x] **New endpoint `GET /api/v1/fetches/{id}`:** returns `{fetch_id, total, pending, running, completed, failed, already_complete, terminal}`. URL named `/fetches/` rather than `/batches/` so user-facing language does not leak the internal `batches` term.
  * Future: server-side Valkey cache on `fetch:progress:{id}` (1–2s live, 60s+ terminal); rate limit reusing `infra.RateLimiter`; filter by `user_id` from authn (v1 single-user).
  * Client-pull model (5s fixed or `2→5→10s` backoff). No long-polling.
* [x] **Dropped `PageFetchTaskResult` / `PageFetchResponse.Results`** + Swagger regen (`task swag`).
* [x] **Tests:** `created` path with task_id, not_found echo + ordering + no task_id leak in JSON, `already_active` collapse to `created` with no identifier leak, `already_complete` snapshot, race-miss → 500, `GetFetch` happy path / 404 / invalid UUID.
* [x] **Stage 3:** server-side Valkey cache + per-IP rate limit on `GET /fetches/{id}`; wire Valkey flags into `cmd/api-server`. Both features opt-in (default OFF), independently toggleable.
  * `ProgressCache` interface in `internal/http/api/cache.go` with `NoOpProgressCache` default; `ValkeyProgressCache` impl in `cache_valkey.go` keyed by `fetch:progress:{fetch_id}`. Two TTLs: live (default 2s) and terminal (default 60s); terminal flag from response body picks bucket.
  * Per-IP token-bucket limiter in `internal/http/middleware/ratelimit.go`: `IPLimiter` interface, `NoOpIPLimiter`, `InMemoryIPLimiter` (LRU-bounded `*rate.Limiter` per client IP). `RateLimit` middleware returns `429 Too Many Requests` + `Retry-After: 1` when over budget. `ClientIP` honors leftmost `X-Forwarded-For` then falls back to `RemoteAddr`.
  * `Server` gains `Cache` + `GetFetchLimiter` fields wired via `WithProgressCache` / `WithGetFetchLimiter` functional options. `Register` always wraps `/fetches/{id}` in `RateLimit`; with the noop default the wrap is a passthrough.
  * `cmd/api-server` flags: `--cache-enabled`, `--cache-live-ttl`, `--cache-terminal-ttl`, `--rate-limit-enabled`, `--rate-limit-rps`, `--rate-limit-burst`, `--rate-limit-ip-cache-size`, plus `--valkey-host/-port/-username/-password/-password-file/-db`. `main.go` only dials Valkey when cache is enabled, and only constructs the limiter when rate-limit is enabled.
  * Tests: cache hit short-circuits repo, cache miss populates, rate-limit returns 429, per-IP isolation, `ClientIP` precedence. `go test -short ./...` = 417 pass.
* [ ] **Stage 4 (next):** e2e — bring up stack, `POST /page_fetch` real, observe collector, `GET /fetches/{id}` terminal.
* [ ] **Notification deferred:** `fetches.completed_at` column persisted but unused in v1; v2 sets via sweeper or compute-on-write transition and fires webhook/email.
* [ ] **Update done.md / spec.md cross-refs after Stage 3/4 merge.**

This unblocks Phase 2.8 (Operator TUI), which consumes `GET /fetches/{id}` for the fetch monitor view.

## Phase 2.8 — Operator TUI (`cmd/tui`)

* [ ] Bubble Tea app with four views — candidates list / submit-confirmation modal / batch monitor / content viewer.
* [ ] **List view:** filter bar (`q`, `source_abbr`, `since/until`), paginated table, multi-select (`space`), `f` to submit `POST /page_fetch`, `Enter` to view content, `b` to open fetch-request monitor by ID.
* [ ] **Submit modal:** show returned `fetch_id` and a per-candidate status table from `items[]` (`created` / `already_complete` / `not_found`); each row is keyed by `candidate_id` so the underlying list view can mark rows accordingly. Offer `[m] monitor` (jump to fetch view) / `[c] copy id` / `[↵] dismiss`. No `task_id` is ever displayed.
* [ ] **Fetch monitor view:** input fetch_id (or arrive from modal), client-pull `GET /fetches/{id}` on a fixed interval (default 5s; configurable via `--fetch-poll-interval`). Render progress bar + counters (pending / running / completed / failed / already_complete). Stop polling when `terminal=true`; `r` forces immediate refresh; `esc` back.
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
