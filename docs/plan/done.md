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

## Phase 2.7 — User-Fetch Model (2026-04 → 2026-05)

Design rationale in `spec.md` §6 (`fetches` / `fetch_items`, three-status `POST /page_fetch` response, cross-user privacy).

* [x] **Stage 1 — schema + repo:** `fetches` (id, request_id, requested_at, completed_at) and `fetch_items` (fetch_id, candidate_id, task_id, snapshot_status). `fetch_items.task_id` is nullable so `already_complete` items have no FK to satisfy. Indices on `(fetch_id)` and `(candidate_id)`.
* [x] **Stage 2 — handler rewrite:** `POST /api/v1/page_fetch` returns `{fetch_id, items[]}` with `status ∈ {created, already_complete, not_found}`. `created` collapses fresh-insert and shared-active-task — never expose `already_active` or `task_id` (cross-fetch shared identity leakage). `CreateTask` is a CTE that inserts-or-recovers an active task in one round-trip, returning `inserted=false` when the recovery branch fires; the adapter maps this to `(task, ErrTaskAlreadyActive)`. `not_found` items are echoed in the response but not persisted (no candidate row to FK).
* [x] **Stage 2 — `GET /api/v1/fetches/{id}`:** returns `{fetch_id, total, pending, running, completed, failed, already_complete, terminal}`. Aggregation `COALESCE(snapshot_status, tasks.status)` per item; `not_found` never appears in progress totals.
* [x] **Stage 3 — Valkey progress cache + per-IP rate limit on `GET /fetches/{id}`:** both opt-in (default OFF), independently toggleable. `ProgressCache` interface (`internal/http/api/cache.go`) with `NoOpProgressCache` default; `ValkeyProgressCache` keyed by `fetch:progress:{fetch_id}` with split TTLs (`LiveTTL` 2s for non-terminal, `TerminalTTL` 60s for terminal). `IPLimiter` interface (`internal/http/middleware/ratelimit.go`) with `NoOpIPLimiter` and `InMemoryIPLimiter` (LRU-bounded `*rate.Limiter` per client IP); `RateLimit` middleware returns `429 Too Many Requests` + `Retry-After: 1`; `ClientIP` honors leftmost `X-Forwarded-For` then falls back to `RemoteAddr`. `Server` wires both via `WithProgressCache` / `WithGetFetchLimiter` functional options. `Register` always wraps `/fetches/{id}` in `RateLimit`; with the noop default the wrap is a passthrough.
* [x] **Stage 3 — `cmd/api-server` flags:** `--cache-enabled`, `--cache-live-ttl`, `--cache-terminal-ttl`, `--rate-limit-enabled`, `--rate-limit-rps`, `--rate-limit-burst`, `--rate-limit-ip-cache-size`, plus full `--valkey-*` set. `main.go` only dials Valkey when cache is enabled, only constructs the limiter when rate-limit is enabled.
* [x] **Stage 4 — e2e driver:** `e2e/page_fetch_test.go` (`//go:build e2e`) seeds a candidate, POSTs `/page_fetch`, polls `/fetches/{id}` to terminal, asserts `contents` row populates with non-empty title + content. `e2e/helpers.go` provides env loader (skip when `PRISM_E2E_DSN` unset), `pgxpool` open, `seedCandidate` (uses `model.Candidates.Fingerprint`), `postPageFetch`, `pollFetch`, `assertContent`. `task test:e2e:page-fetch` orchestrates setup → workers → driver → teardown against an isolated `prism-e2e` compose project. `deployments/docker-compose.worker.yaml` adds `prism-api` and `fixture-server` services (profile `worker`); collector + discovery commands gain `--fixture-base=${FIXTURE_BASE:-}` so Phase 4 real-site mode is preserved when the env var is unset. Verified 2026-05-09 — `--- PASS: TestPageFetch_HappyPath_e2e (4.12s)`.

Notification (`fetches.completed_at` → webhook/email) is deferred to v2; the column is persisted but unused. This phase unblocks Phase 2.8 (Operator TUI fetch monitor view) and `prism-mcp` bootstrap.

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

## Phase 2.9 — LLM Fallback Parser (2026-05)

Phase B of Immediate Next Steps #11. LLM-backed `collector.Parser` activates via a global `fallback:` block in `parsers.yaml`; provider config sits in the same yaml so the operator sees the choice in one place (no hidden flag/env state). API key arrives via `key_file` path, mirroring `PostgresConfig.PasswordFile` / `ValkeyConfig.PasswordFile`.

```yaml
# parsers.yaml
fallback:
  enable: true
  prompt_file: /app/assets/prompts/collector/article_parser.md
  llm:
    provider: gemini      # gemini | openai | ollama
    model: gemini-2.0-flash
    key_file: /run/secrets/llm_key
    timeout: 30s
parsers:
  www.example.com: { ... }
```

The system instruction lives in `assets/prompts/collector/article_parser.md` (already shipped) and is loaded at startup via `parserconfig.LoadFallbackPrompt`. Keeping the prompt out of the binary lets operators iterate on extraction quality without rebuilding worker images.

Plumbing:

* [x] `internal/collector/parser/llm/parser.go` — `Parser` (`collector.Parser` impl) using `llm.Generator` + `ParserConfigJSONSchema`. Constructor takes the system instruction as a parameter (loaded from `assets/prompts/collector/article_parser.md` at startup) so prompt iteration does not require a rebuild. Tests use a stub generator returning canned JSON; nil-generator / nil-logger / empty-prompt constructor checks; generator-error and decode-error paths covered.
* [x] `internal/collector/parser/llm/schema.go` — `ParserConfigJSONSchema` was reusing one `*jsonschema.Schema` pointer across multiple property paths, which violates the validator's tree requirement (`schemas at <anonymous schema> do not form a tree`). Replaced with a `newTargetNodeSchema()` factory so each property path gets a fresh subschema. Latent bug — only surfaced once the schema was actually run through `DecodeJsonSchema`.
* [x] `internal/collector/parser/registry.go` — `NewRegistry` accepts an optional fallback parser. `Parse` routes host-miss to fallback (info log) when set; `ErrNoMatchingParser` when not (existing behavior). Registry tests cover both fallback-used and fallback-not-invoked-on-host-match paths.
* [x] `internal/collector/parser/config/config.go` — added `FallbackConfig{Enable bool; PromptFile string; LLM appconfig.LLMConfig}` and `Config.Fallback`. `LoadConfig` requires `prompt_file` when `Enable=true`, runs `cfg.Fallback.LLM.ResolveSecrets()`, and validates the LLM block only when `Enable=true` — disabled fallback does not require dummy provider/model/key fields. Helper `LoadFallbackPrompt(cfg)` reads the prompt file and trims trailing whitespace.
* [x] `internal/collector/parser/config/factory.go` — `BuildRegistry` signature gains `llmFactory LLMFactory` (a `func() (collector.Parser, error)` so the config package stays free of an `llm` import). When `cfg.Fallback.Enable && llmFactory != nil`, the factory result is wired into the registry as the host-miss fallback. When `Enable=true` but factory is nil, `ErrFallbackEnabledNoFactory` — a guard that fires before any HTTP traffic.

Provider wiring:

* [x] `internal/appconfig/llm.go` — added `KeyFile string` (yaml: `key_file`, mapstructure: `key-file`); `ResolveSecrets()` reads `KeyFile` and overrides `Key`; `String()` and `LogValue()` redact the API key. YAML tags added alongside existing mapstructure tags so the same `LLMConfig` can be loaded via direct yaml.v3 decoding (when embedded under `fallback.llm:` in parsers.yaml) or via viper (when bound to `--llm-*` flags). `validate:"required"` dropped from `Key` since `KeyFile` is the alternative path.
* [x] `internal/llm/factory/factory.go` (new subpackage) — `NewGenerator(ctx, cfg, logger) → llm.Generator` promoted from `cmd/worker/planner/main.go` so collector / recover / parse-probe share one provider-construction path. Subpackage avoids the `internal/llm` ↔ `internal/llm/{gemini,openai,ollama}` import cycle that would arise if the helper lived in the parent package.
* [x] `cmd/worker/planner/main.go` — replaced local `newGenerator` with `llmfactory.NewGenerator`; pruned unused imports.
* [x] `cmd/worker/collector/main.go` — when `cfg.Fallback.Enable`, builds generator via `llmfactory.NewGenerator`, builds factory `func() (collector.Parser, error) { return parserllm.NewParser(gen, logger, model) }`, passes to `BuildRegistry`. Logs the active provider/model at startup.
* [x] `cmd/recover/main.go` — same pattern (uses noop tracer).
* [x] `cmd/dev/parse-probe/main.go` — same pattern; `--all-parsers` mode includes `__llm__` in the result map when fallback is enabled.

Tests added:

* [x] `internal/appconfig/llm_test.go` — secret-leak guard (fmt verbs + slog.Any redact); `ResolveSecrets` reads from file, no-file leaves `Key` intact, missing-file returns error.
* [x] `internal/collector/parser/config/config_test.go` — `LoadConfig` with `fallback.enable=true` resolves `key_file`, missing provider triggers validation error, fallback disabled skips LLM validation.
* [x] `internal/llm/factory/factory_test.go` — unsupported-provider error path (real-provider construction lives in per-provider unit tests).

`docs/plan/spec.md` §6 — LLM dual-role entry covers parse-time fallback + config-snippet generator, global `fallback.enable` activation, no-automatic-promotion workflow, v1 HTML-shape input assumption, and the deliberate non-support of per-host overrides.

`go test -short ./...` = full suite green except the pre-existing `internal/collector/archiver` SeaweedFS testcontainer flake (Phase 5 track).

## Phase 2.9 — Layer 1 Tail Closure (2026-05)

Closes the two remaining real layer-1 gaps surfaced by the post-Stage-3 coverage survey. The journal-listed dispatcher / parser-html / parser-jsonld / minifier / fetcher / model entries were already at 76–100% by the time of survey and needed no additional work; only `parser/config` (`BuildRegistry` + `LoadConfig`) and `internal/message` (Watermill publisher path) were genuinely uncovered.

* [x] `internal/collector/parser/config/config_test.go` — `LoadConfig` happy path / file-not-found / malformed YAML; `BuildRegistry` disabled-host skipping (verified via `Registry.Parse` routing → `ErrNoMatchingParser` for the disabled host), JSONLD-composite branch, nil-logger error propagation, empty-config registry. Coverage 54.2% → 91.7%.
* [x] `internal/message/batch_completed_test.go` — `NewWatermillBatchCompletedPublisher` nil-publisher rejection; `PublishBatchCompleted` round-trip via in-memory `gochannel.GoChannel` (asserts payload + `trace_id` metadata); publisher-error wrapping via a fake `wm.Publisher` returning a sentinel. Coverage 22.2% → 88.9%.

Out of scope (still deferred): `internal/collector/archiver/s3_test.go` testcontainer flake (Phase 5 testcontainers track); LLM-fallback parser config schema (Phase 2.9 §B / Immediate Next Steps #11).

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

## Configurable Search Providers (2026-06)

Commit `5315cd4 feat(discovery): add configurable search providers` shipped the search-provider layer as a separate concern from persisted `sources`. Search providers find candidate URLs; real PARTY/MEDIA sources own the resulting candidates.

* [x] `cmd/worker/discovery` now routes `KEYWORD_SEARCH + MEDIA` through enabled search-provider clients while preserving `DIRECTORY_FETCH + MEDIA` for direct media feeds.
* [x] `cmd/worker/planner` now generates search tasks from configured search targets instead of assuming all MEDIA sources are search targets.
* [x] `internal/discovery/search/config` centralizes provider config, search targets, secret resolution from `api_key_file`, and inline-key warnings.
* [x] Brave News Search, Google Custom Search JSON API, and SerpAPI clients are implemented with focused request/response tests.
* [x] SerpAPI supports provider-level shared credentials plus top-level `google_news`, `duckduckgo_news`, and `bing_news` engine blocks with named params maps.
* [x] Discovery worker registers SerpAPI named variants as provider IDs such as `serpapi-google-news-recent` and `serpapi-duckduckgo-news-weekly`.
* [x] Developer smoke capture was hardened: non-2xx bodies are captured, shared smoke harness lives in `internal/discovery/search/smoke_test.go`, and fixture paths redact API-key query params.
* [x] Verified with `rtk go test -short -count=1 ./...`: 497 tests passed across 71 packages. Focused lint was clean for `./internal/dev ./internal/discovery/search/... ./cmd/worker/discovery ./cmd/worker/planner`.

Deferred follow-ups: normalize Brave and Google CSE to named params maps only if multiple variants are needed; turn selected smoke captures into tracked replay tests if stable provider regression coverage is worth the fixture maintenance; resolve Google CSE `403 PERMISSION_DENIED` as a credential/project-access issue unless fixed credentials prove request params need adjustment.

## Phase 2.9 — SeaweedFS S3 Test Stability (2026-06)

* [x] Fixed the intermittent `internal/collector/archiver/s3_test.go` startup failure where SeaweedFS could pass the master `/cluster/status` check before the S3 API was ready for bucket creation.
* [x] Testcontainers now waits for both the SeaweedFS master HTTP endpoint and the S3 listening port, then probes readiness with a real S3 `ListBuckets` call before creating the test bucket.
* [x] Increased the test setup retry budget for S3 readiness and bucket creation; production `S3Archiver` behavior is unchanged.
* [x] Cleaned up unchecked S3 response body closes in `internal/collector/archiver/s3.go` so package lint passes.
* [x] Verified with `rtk go test -short -count=1 ./internal/collector/archiver`, `rtk golangci-lint run ./internal/collector/archiver`, and `rtk go test -short -count=1 ./...`.

## Phase 4.1 — OpenTelemetry Observability Rollout (2026-06)

Shipped the core OTLP telemetry foundation and the first application metric rollout across schedulers, workers, and providers.

* [x] `internal/obs` now owns shared redaction-safe telemetry config and runtime helpers for OTLP traces and metrics.
* [x] JSON logs and OTLP telemetry are routed through the local Victoria stack: VictoriaLogs, VictoriaTraces, VictoriaMetrics, and Grafana datasource wiring.
* [x] Scheduler, API, discovery worker, collector worker, planner worker, batch detector/publisher, and backfiller initialize the shared telemetry runtime. Recover and RSS/operator tails remain a separate follow-up decision.
* [x] Scheduler metrics: terminal task outcome counter plus tick and dispatch duration histograms.
* [x] Discovery and collector worker metrics: terminal task outcome counters and task duration histograms with low-cardinality task/source/result labels.
* [x] LLM provider metrics: `prism.llm.requests`, `prism.llm.request.duration`, and `prism.llm.tokens`; generation and embedding calls are instrumented via `internal/llm` decorators and the shared factory path.
* [x] LLM token accounting now records provider-specific subcategories where available: cached, tool, reasoning, and thought tokens; Ollama total token accounting now runs after streaming completes.
* [x] Search provider metrics: `prism.search.requests`, `prism.search.request.duration`, and `prism.search.results`, with SerpAPI labels normalized to bounded engine labels plus low-cardinality deployment config profiles.
* [x] Lint cleanup kept normal test builds clean by checking output-finalizing write/close errors and moving manual cassette recorders behind the `manual` build tag.
* [x] Verified after provider metrics with `rtk go test -short -count=1 ./...` (550 passed in 72 packages) and `rtk golangci-lint run` (no issues).

Commits:
- `f0c3a22` feat(obs): add OTLP telemetry runtime
- `33f80f7` feat(obs): ship JSON logs to VictoriaLogs
- `24493a3` feat(obs): route OTLP telemetry to Victoria stack
- `12c2adf` feat(obs): enable OTLP telemetry for workers
- `1c6c453` feat(obs): add scheduler metrics
- `585237e` feat(obs): migrate remaining commands to telemetry runtime
- `33f749e` feat(obs): add discovery worker metrics
- `670651a` feat(obs): add worker task metrics
- `92d6a3e` fix(lint): address unchecked errors in tooling tests
- `38a51da` feat(obs): add LLM provider metrics
- `a14086d` feat(obs): add search provider metrics

## Phase 4.1 (Cont.) — Security, HTTP Middleware, Config Templates, API Monitoring, and Bounded Log Rotation (2026-06)

Shipped HTTP security/observability middlewares, unified auth headers, templated runtime configurations, modular taskfiles, target status monitoring with Valkey persistence, and bounded slot-ring log rotation.

* [x] **HTTP Security & Authentication**:
  * Added IP filter middleware (`ipfilter.go`) restricting API access to configured subnets.
  * Implemented a public HTTP client guard (`public.go` in `internal/http/client`) rejecting outbound SSRF targets.
  * Added token-based Authorization list middleware and unified the `Authorization: Bearer <token>` header checking.
  * Split HTTP middleware implementations into separate files (`auth.go`, `cors.go`, `logger.go`, `recoverer.go`).
* [x] **HTTP Observability & Fetcher Robustness**:
  * Added HTTP metrics middleware to capture API request stats (total requests, duration, outcomes).
  * Updated HTTP fetcher to parse and honor `Retry-After` headers and handle request cancellations properly.
* [x] **Config Modularization, Templating, and Compose Split**:
  * Promoted deploy runtime configurations to target-aligned paths under `configs/` and `assets/`.
  * Supported Go template rendering in configuration files before parsing via Viper to enable env-based defaults.
  * Split monolithic `Taskfile.yml` into modular taskfiles under `taskfile/`.
  * Split application (API, batch trigger) and worker containers into distinct Docker Compose stacks.
* [x] **API Status Monitoring**:
  * Configured status monitoring targets with configurable modes (push/pull) and timeouts.
  * Added storage backends (in-memory and Valkey) to persist and query target status health.
* [x] **Bounded Slot-Ring Log Rotation and OTel Collection**:
  * Implemented a reusable `pkg/rotatingfile` package with `FilePool` supporting cyclic slot writes (`app.log.0`, `app.log.1`, etc.).
  * Updated logging setup in `internal/obs/logging.go` to use `FilePool` for file logs and transitioned extensions from `.json` to `.log`.
  * Configured OTel `filelog` receiver to track `/logs/*/*.log*` with `start_at: beginning` and checkpoint offset persistence using a `file_storage` extension.
  * Added `max-files: 5` configurations across all microservice configs and updated docker-compose files to use isolated host bind mounts.
  * Created an integration test script `test_pipeline.sh` verifying slot truncation, wrap-around tracking, and restart safety.

Commits:
- `52169e5` feat(obs): add bounded file logs and valkey status monitor
- `139eaf8` refactor(api): unify token auth header
- `5f824b8` feat(api): configure status monitoring targets
- `6cc96f8` chore(config): support templated runtime config
- `5d94819` chore(config): promote deploy runtime configs
- `c9ca19e` chore(deploy): split app and worker compose stacks
- `a7e9e26` test(taskfile): clarify automated test entrypoints
- `b598da1` refactor(taskfile): split tasks into modules
- `1ec1d55` chore(obs): configure otel collector deployment
- `96e6b9b` refactor(middleware): split HTTP middleware implementations
- `9204efb` refactor(http): move HTTP client guard package
- `5d7f88d` feat(auth): add token list auth middleware
- `082bfe7` feat(obs): add HTTP observability middleware
- `3c51e70` fix(fetcher): honor Retry-After cancellation
- `6300946` fix(security): add IP filter middleware

