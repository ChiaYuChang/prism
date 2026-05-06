# Project Prism — Done

Append-only history of completed work, grouped by phase. Carved from `plan.md` Phase 0, completed `[x]` items in Phase 2.1–2.6, and §7 items 1–10.

## Phase 0 — Infrastructure and Seeding (2026-01)

* [x] 0.1 Database schema migration to v3.
* [x] 0.2 Initial seed data for sources (Parties and Media).
* [x] 0.3 Initial PENDING tasks for directory fetching.

## Phase 2.1 — Scheduler (2026-02)

* [x] `cmd/scheduler` with structured configuration.
* [x] PostgreSQL claim logic for runnable tasks with `FOR UPDATE SKIP LOCKED`.
* [x] Watermill publisher for task messages.
* [x] Logging and task lifecycle tests.
* [x] Request-oriented `TaskSignal` payload.

## Phase 2.2 — LLM Infrastructure (2026-02)

* [x] Polymorphic LLM clients (Gemini, OpenAI, Ollama).
* [x] Shared structured-response decoding via `llm.DecodeJsonSchema(...)`.
* [x] Prompt contract externalized into `assets/prompts/analysis/extractor.md`.
* [x] Extractor moved under `internal/discovery/extractor`.
* [x] Extraction I/O contracts in `internal/model/extraction.go`.
* [x] Focused `internal/llm` unit tests.

## Phase 2.3 — Scheduler Enhancement, PAGE_FETCH support (2026-02)

* [x] Add `--kinds` filter to `ClaimTasks` query (`AND kind = ANY($kinds::task_kind[])`).
* [x] Add `--kinds` flag to scheduler config.
* [x] Add `PAGE_FETCH` back to `task_kind` ENUM and `uq_tasks_active_page_fetch` dedup index on `(kind, url)` WHERE active.
* [x] Add `scheduler-fast` and `scheduler-slow` Taskfile entries.
* [x] Add source-type priority split: MEDIA PAGE_FETCH claimed first (user-waiting), PARTY fills remainder via two-step ClaimTasks.
* [x] Add `source_types` optional filter to `ClaimTasks`; add `ReleaseTasks :exec` for over-claim release.
* [x] Add in-memory token-bucket rate limiter (`InMemoryRateLimiter`) with per-source config (`assets/ratelimits/scheduler.yaml`).
* [x] Refactor free functions into `Scheduler` struct methods; dispatch tests updated.

## Phase 2.4 — Discovery Worker, KEYWORD_SEARCH execution path (2026-03)

* [x] Implement `cmd/worker/discovery`.
* [x] Add command-level config and handler tests for `cmd/worker/discovery`.
* [x] Route `PARTY + DIRECTORY_FETCH` tasks by `source_type` / `base_url`.
* [x] Establish scout core packages under `internal/discovery/scout/{html,rss,atom}`.
* [x] Implement config-driven `HTMLScout` for party directory pages.
* [x] Implement config-driven `RSSScout` for standard RSS XML feeds.
* [x] Implement config-driven `AtomScout` for Atom feeds.
* [x] Establish centralized scout config schema with `version` and YAML/JSON-compatible tags.
* [x] Implement scout config repository and factory for `html`, `rss`, `atom`, and `custom` scouts.
* [x] Consolidate scout definitions into a single `scouts.yaml` config document.
* [x] Verify current scout fixtures (DPP, TPP, CNA, TTV, PTS, KMT, Yahoo).
* [x] Implement config-driven backfiller that reuses scouts + a generic `IndexPager`.
* [x] Add `CandidateSink` contract and first persistence implementation for discovered briefs.
* [x] Consume runnable `tasks`.
* [x] Persist discovered article briefs into `candidates` from `cmd/worker/discovery`.
* [x] Replace direct `prism.page_fetch` MQ publish with `CreateTask(PAGE_FETCH, PARTY)` after PARTY candidate persistence.
* [x] Remove `PageFetchSignal` and `PageFetchTopic` from `internal/message` once Collector Worker uses `prism.task`.
* [x] Load scout instances from runtime app config instead of package-local test config.
* [x] Establish `internal/discovery/planner` with tests for seed-content extraction and MEDIA task creation.
* [x] Connect planner to a concrete executable worker / trigger path (`cmd/worker/planner`).
* [x] Seed `brave` source row in `000003_seed_sources.up.sql` (`MEDIA`, `https://api.search.brave.com`).
* [x] Implement `internal/discovery/search/brave/client.go` — `SearchClient` calling Brave News Search API (POST `/res/v1/news/search`).
* [x] Add `searchClients map[string]discovery.SearchClient` to discovery `Handler`; update `NewHandler` signature.
* [x] Add `handleKeywordSearch()` to handler: decode `MediaTaskPayload{Query, Site}` from `sig.Payload`, call client, sink with `IngestionMethod=SEARCH`.
* [x] Expand `process()` routing: `KEYWORD_SEARCH + MEDIA` → `handleKeywordSearch()`; keep `DIRECTORY_FETCH + PARTY` path unchanged.
* [x] Add `--brave-api-key` flag to discovery worker config; wire `BraveClient` into handler in `main.go`.
* [x] Add KEYWORD_SEARCH handler tests: happy path with mock `SearchClient`, missing client error path.

## Phase 2.5 — Collector Worker (2026-04)

* [x] Subscribe to `prism.task`, filter `kind=PAGE_FETCH`.
* [x] Implement F→M→T→(S||P) pipeline with `Dispatcher` struct (`internal/collector/dispatcher.go`).
* [x] Implement `Minifier` interface and `HTMLMinifier` (`internal/collector/minifier/html.go`).
* [x] Implement `NoOpTransformer` placeholder (`internal/collector/transformer/noop.go`).
* [x] Implement host-aware `Parser` registry with HTML + JSON-LD composite parsers. (Refactored to variadic `CompositeParser` with coalesce logic).
* [x] `html.RuleConfig` uses slice fields `Title/Author/Date/Content []string`; `DateLayouts` at `ParserConfig` top level.
* [x] `parser/config/parsers.yaml` defines per-host parser rules (DPP, TPP, Yahoo).
* [x] JSON-LD extraction via regex (not goquery); handles multi-block and `@graph` structures.
* [x] LLM parser (`parser/llm`) generates `ToConfigSnippet()` YAML for human review; no automatic promotion to registry.
* [x] Prompt updated to `assets/prompts/collector/article_parser.md` (primary: extract content; secondary: record CSS selectors).
* [x] Avoid refetch when content already exists by URL or candidate ID (`GetContentByCandidateID` + `GetContentByURL` checks).
* [x] Handle PARTY PAGE_FETCH (automatic) and MEDIA PAGE_FETCH (user-triggered) via same worker; `sourceTypeToContentType()` maps to `PARTY_RELEASE` / `ARTICLE`.
* [x] Wire error Saver for Minify failures: `errorSaver collector.Saver` added to Handler; `saveOnMinifyError()` archives raw content with `stage:"raw"` metadata; `--archive=<uri>` flag enables it (URI dispatches to `LocalArchiver` for `file://` or `S3Archiver` for `s3://`).
* [x] Refactor `internal/collector/saver/` + `internal/collector/fetcher/recover.go` → `internal/collector/archiver/`: `Archiver` interface (embeds `collector.Saver`, plus Load / Scan / Remove), `LocalArchiver` (file://), `S3Archiver` stub (s3://), `ParseURI` factory. `saver/` and `fetcher/recover.go` deleted.
* [x] Harden `LocalArchiver`: soft-delete via `deleted_at` stamp in `.meta.json` (`Remove`); `Purge(traceID)` / `PurgeAll()` for operator hard-delete (not on `Archiver` interface); `payload_sha256` (hex SHA-256) written by `Save` and verified by `Load` (returns `ErrCorrupted` on mismatch or absence); `created_at` (second-precision UTC) replaces path-derived `Timestamp` in `Meta`; `version` field (`MetaVersion = 1`) for future format migration; `ScanOptions.IncludeRemoved` to expose soft-deleted entries.
* [x] Implement `cmd/recover` operator CLI: `status` / `list` / `run` / `clean` subcommands using `archiver.Archiver`; `--archive` flag accepts URI (`file://` or `s3://`); `--dry-run`, `--since`, `--until`, `--limit`, `--trace-id`.
* [x] Soft-delete in `LocalArchiver.Remove` (stamp `deleted_at` in meta JSON); `Purge` / `PurgeAll` for hard-delete; `clean --purge` wires both.
* [x] Enrich `saveOnMinifyError` metadata with `source_abbr`, `source_type`, `batch_id` so `cmd/recover run` can build `CreateContentParams`.
* [x] Complete `S3Archiver` for production SeaweedFS/S3; `LocalArchiver` (`file://`) and `S3Archiver` (`s3://`) remain parallel options selected by URI scheme via `ParseURI` — `LocalArchiver` stays the default for local development and testing. `S3Archiver` implements Save / Load (with SHA-256 integrity check) / Scan / Remove (soft-delete); hard-delete is intentionally delegated to S3 lifecycle policies rather than application code.

## Phase 2.6 — User-Facing Candidate Query API (2026-04)

* [x] `GET /candidates` — query by `q`, `source_abbr`, `since`, `until`, `limit`, `offset`; returns JSON list.
* [x] `POST /page_fetch` — accepts `candidate_ids`, creates MEDIA `PAGE_FETCH` tasks (idempotent: `ErrTaskAlreadyActive` → `already_active`); returns per-candidate results.
* [x] `GET /contents/{candidate_id}` — returns content JSON, 404 (pgx.ErrNoRows) if pending.
* [x] Handlers in `internal/http/api/`; wired onto `cmd/api-server` mux via `Server.Register`. Swagger regenerated.

## Phase 2.9 — Layer 1 Unit Test Gaps, Phase A (2026-04)

Phase A of Immediate Next Steps #11 — `ArticleParser` removal + tests for kept components.

* [x] Delete `internal/collector/parser/article.go` (`ArticleParser` is the implicit-default-parser anti-pattern; parsers should only exist via config)
* [x] `internal/collector/parser/registry.go`: remove `generic` field and fallback path; on host miss, return `ErrNoMatchingParser` directly
* [x] `cmd/recover/recover.go`: build parser via `config.BuildRegistry()` (same as worker); on `ErrNoMatchingParser` for an archive, `skipped++` + warn log with host/url/trace_id (best-effort, do not abort batch)
* [x] `cmd/dev/parse-probe/main.go`: remove `__generic__` line in `--all-parsers` mode
* [x] Add `internal/collector/parser/registry_test.go`: host match, host lowercasing, no-match returns `ErrNoMatchingParser`, URL parse error, nil-param checks
* [x] Add `internal/collector/parser/llm/schema_test.go`: `Value`, `Selectors`, `ToRuleConfig`, `ToConfigSnippet`, `ToArticleContent` round-trip
* [x] Add `internal/collector/transformer/noop_test.go`: identity behavior on empty / large / nil inputs
* [x] Run coverage → `parser` 36.4% → 79.5%, `parser/llm` 0% → 95.6%, `transformer` 75% → 100%; 149 tests passing across `internal/collector/...`

## §7 Immediate Next Steps — Items 1–10 (2026-01 → 2026-04)

1. Centralize domain enum constants into `internal/repo/constants.go`.
2. Add `PAGE_FETCH` to `task_kind` ENUM; add dedup index; add `--kinds` filter.
3. Update Discovery Worker: replace direct `prism.page_fetch` publish with `CreateTask(PAGE_FETCH, PARTY)`.
4. Add scheduler-fast/slow priority split, rate limiting, over-claim+release pattern.
5. Implement Collector Worker F→M→T→(S||P) pipeline; avoid-refetch; PARTY/MEDIA routing.
6. Implement KEYWORD_SEARCH execution path (2.4): Brave client, handler routing, config wiring, handler tests.
7. Wire error Saver in Collector Worker (`saveOnMinifyError` + `--archive` URI).
8. Refactor to `internal/collector/archiver/`: `Archiver` interface + `LocalArchiver` + `S3Archiver` stub + `ParseURI`; delete `saver/` and `fetcher/recover.go`.
9. Implement `cmd/recover`: `status` / `list` / `run` / `clean` subcommands; soft-delete + purge; enriched archive metadata.
10. Implement User-Facing Candidate Query API (2.6): `GET /candidates`, `POST /page_fetch`, `GET /contents/{candidate_id}`.

## Integration test plan — Phase 2 + 3 (2026-05-01)

Phases 2 (replay) and 3 (fail-minify recover) of `docs/integration-test-plan.md`. Phase 2 verified 26/26; Phase 3 verified 3/3 (path proven; 23/26 lost to known archive-key collision — see `future.md` Archive Catalog refactor). Recover gained dual-path replay (raw → M+T+P, minified → T+P, canonical → P). Archiver `Scan` / `Remove` / sidecar `meta.json` / YYYY/MM/DD path layout annotated as deprecated. Replay schedulers tick at 5s/3s with permissive `assets/ratelimits/replay.yaml`.

Commits:
- `0a54e23` fix(taskfile): add NATS auth flags to worker:start:replay
- `6837a3e` feat(collector): add --force-minify-error for Phase 3 recovery test
- `22ad358` feat(recover): support multi-stage replay and deprecate sidecar archiver

## Plan restructure + item A verification (2026-05-04)

* [x] Split `plan.md` (576 lines) into `docs/plan/{spec,todo,done,future}.md`; `plan.md` rewritten as ~15-line index.
* [x] Cross-refs updated in `internal/collector/archiver/{archiver,meta,local}.go`, `docs/integration-test-plan.md`, `SESSION_SUMMARY.md`.
* [x] Deleted `docs/database-tables.md`; per-table semantics live in `COMMENT ON` statements in `db/migrations/000001_init.up.sql`.
* [x] **Item A verified** — replay tick speedup (`22ad358`): 26/26 PAGE_FETCH + 3/3 DIRECTORY_FETCH COMPLETED, 0 failures, drained well under 30s target.
* [x] Stale todo items reconciled — old §7 #12 (archive publisher wired in `fbbb82c`), #13 (cmd/recover dual-path, in `22ad358`), #14 (deprecation annotations, in `22ad358`) all already shipped; removed from todo.md, remaining items renumbered.

Commits:
- `192d709` docs: restructure project plan and add schema comments
- `2aa742f` docs: update cross-references to new plan structure and schema comments

## Phase 4 — Containerize Workers + Security Refactor (2026-05)

Closes integration-test-plan.md Phase 4. Compose layout reorganized
into base + per-env overlays + profile-gated tools + worker file;
secret handling formalized via `script/secrets-bake.sh` +
`script/compose-bake.sh`; threat model and runbooks documented in
`docs/security.md`.

* [x] `security(workers): keep secrets off argv + redact in logs` —
      worker binaries read secrets from `PRISM_<APP>_*` env / `*_FILE`,
      no flag carries a secret; DSN structs implement `LogValue()`.
* [x] `security(workers): file-based secret loading for prod overlay`
      — `*_FILE` indirection so prod can mount docker secrets.
* [x] `feat(env): layered dotenv loading and secrets bake script` —
      Taskfile dotenv layering (`env/local/<env>.user.env` →
      `<env>.local.env` → `env/<env>.env` → `env/base.env`);
      `script/secrets-bake.sh` writes `env/local/<env>.local.env`
      from `.secrets/*` with `umask 077` + `chmod 0600` + atomic
      `mktemp` + `mv`; refuses `ENV=prod`.
* [x] `refactor(compose): split base/test/prod overlays + consolidate
      tools under profiles` — base file unbootable on its own (no
      ports / restart); `docker-compose.test.yaml` + `prod.yaml`
      overlays; `docker-compose.tool.yaml` for pgadmin / nats-nui /
      redisinsight / victoria-logs / grafana / ollama gated by
      per-service profiles.
* [x] `refactor(compose): rename workers profile/file to singular
      worker` — `docker-compose.worker.yaml`, profile `[ worker ]`,
      `compose:worker*` Taskfile tasks driven via merged file.
* [x] `test(e2e): complete teardown and implement compose bake script`
      — `script/compose-bake.sh` runs `docker compose config
      --no-interpolate` (keeps `${VAR}` literal in merged output);
      per-`(ENV, PROFILES)` output path; `PRISM_PROD_OK=1` gate;
      `umask 077` + `chmod 0600`; `test:e2e:teardown` explicitly
      stops worker + tool profiles before `compose:clean`.
* [x] `fix(discovery): ignore unsupported task kinds to prevent
      erroneous failures` — discovery worker ignores PAGE_FETCH it
      does not own; regression test in
      `cmd/worker/discovery/handler_test.go`.
* [x] `fix(db): restrict task status updates to prevent late status
      clobbering` — `CompleteTask` / `FailTask` only flip rows still
      in `RUNNING`.
* [x] `docs(security): add threat model and secret-handling runbook`
      — `docs/security.md` covers actors / trust boundaries / assets /
      mitigations / dev vs prod / runbooks (bring-up, rotation,
      rotation-procedure validation via isolated `.secrets-test/`,
      audit checklist).
* [x] **Phase 4 acceptance gate verified 2026-05-05**: e2e drain
      through containers — DIRECTORY_FETCH=3 COMPLETED,
      PAGE_FETCH=26 COMPLETED, contents dpp=10 / kmt=10 / tpp=6.
      `rtk go test ./cmd/worker/discovery` green. e2e stack fully
      torn down post-verification (no leaked `prism-e2e-*`
      containers).

## Phase 3.2 — Vectorization (partial, 2026-03)

* [x] Schema for 768-dimensional pgvector embeddings.
* [x] `Embedder` interface and provider implementations.
* [x] SQLC pgvector integration via `public.vector` mapping.
