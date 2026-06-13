# Integration Test Plan: First End-to-End Pipeline Run

Goal: verify the existing microservices (scheduler, discovery worker, collector worker, recover tool) compose into a working pipeline, and build a local fixture dataset so future iterations do not hit real sites.

## Phase 0 — Pre-work (~30–45 min, code changes)

- [x] **Confirmed discovery → collector message wiring**
  - PAGE_FETCH does **not** travel on a dedicated topic. The discovery candidate sink writes a PAGE_FETCH row into the `tasks` table; the scheduler claims it like any other task and publishes `TaskSignal{Kind=PAGE_FETCH}` to `prism.task`. Collector subscribes to `prism.task` and filters by `Kind`. Routing through the tasks table gives PAGE_FETCH the same retry / rate-limit / observability handling as every other task class.
  - The unused `PageFetchTopic` / `PageFetchSignal` / `WatermillPageFetchPublisher` infra in `internal/message/page_fetch.go` was removed; CLAUDE.md updated to match.

- [x] **`CaptureTransport` lives in `internal/dev/capture.go`** (not `internal/collector/fetcher/`)
  - Dev-only `http.RoundTripper` that tees successful response bodies to `<dir>/<host>/<path>.html`. Filename rules: trailing-slash / empty paths → `index.html`; non-`.html` paths get `.html` appended; query strings encode as `__<sanitized>` before the extension.
  - Wired into both workers via `--capture-dir` flag and `dev.WrapClient(...)`.
  - Placement decision: shared dev shim across discovery + collector + future `FailingMinifier` (Phase 3); `internal/dev/` clearly signals "non-production" without coupling discovery to the collector domain.

- [~] **`--max-directory-pages` cap — not needed by design**
  - Daily scouts intentionally fetch a single index page per run; one DIRECTORY_FETCH task = one URL = one page. Pagination is only implemented in the backfiller (where it already has `MaxPages`).
  - Design rationale: ~10 press releases/day per source is the expected upper bound. Sources that publish more frequently should be handled by raising scout cadence (multiple DIRECTORY_FETCH tasks per day) rather than adding a daily-mode pager — pagination introduces ordering / dedup / "where to stop" complexity that scheduling solves for free.
  - Phase 1 implication: to broaden the fixture corpus, seed DIRECTORY_FETCH tasks for *multiple sources*, not multiple pages of one source.

- [x] **Seed SQL for DIRECTORY_FETCH tasks** — `testdata/seed-tasks.sql`
  - Three PARTY sources (dpp, tpp, kmt) seeded with index-page URLs from `internal/discovery/backfiller/config/backfillers.yaml`.
  - Apply with `psql "$PRISM_DSN" -f testdata/seed-tasks.sql` after migrations.

## Phase 1 — One-shot real-site run (~10 min, only time we touch real sites)

The flow is driven by Taskfile so a single terminal can orchestrate the run.
`MODE=e2e` namespacing isolates the e2e stack from the daily `dev` stack
(separate `prism-e2e_*` volumes / networks via `COMPOSE_PROJECT_NAME`), and
workers run detached so the operator just tails logs instead of juggling
multiple terminals. Process tracking is via `pgrep`/`pkill` against argv
patterns anchored to `^\./bin/<name> ` (no PID files — task's mvdan-sh
interpreter doesn't return real OS PIDs from `$!`).

- [x] `task test:e2e:setup` — bring up isolated e2e stack + migrate + seed DIRECTORY_FETCH tasks
- [x] `task worker:start` — launch **two scheduler instances** (slow=DIRECTORY_FETCH+KEYWORD_SEARCH, fast=PAGE_FETCH) + discovery + collector, all detached
  - Logs in `logs/<name>.log` (`scheduler-slow.log`, `scheduler-fast.log`, `discovery.log`, `collector.log`)
  - Health endpoints: scheduler-slow:8090, scheduler-fast:8091, discovery:8092, collector:8093
  - Discovery + collector both start with `--capture-dir=testdata/fixtures`; collector also archives to `file://./tmp/archives`
- [x] `tail -f logs/*.log` — watch ~26 articles flow through (drained at ~3/min due to rate limiter)
- [x] Verify after run (run on 2026-04-30):
  - `candidates`: dpp=10, kmt=10, tpp=6 (total 26)
  - `contents`: dpp=10, kmt=10, tpp=6 (total 26 — zero loss)
  - `testdata/fixtures/www.{dpp,tpp,kmt}.org.tw/...` holds the captured HTML
  - `./tmp/archives/` has the minified archives (normal success path)
- [x] `task worker:stop` — `pkill -f` against argv pattern (no PID files involved)
- [x] `task test:e2e:teardown` — drop the e2e stack including volumes (subsequent runs start clean, so seed SQL stays non-idempotent on purpose)

**Bugs uncovered and fixed during the run** (commit `f0a7da0`):

1. **pgx custom enum array codec missing** — `factory.go` now registers all 8 enum types (scalar + array) via `AfterConnect` hook. Without it, `ClaimTasks` failed with `unable to encode []pg.TaskKind ... unknown type (OID 16801)`.
2. **`tasks.payload` NOT NULL violated for PAGE_FETCH** — `sqlc.narg(payload)` emits explicit `INSERT NULL`, bypassing schema's `DEFAULT '{}'::jsonb`. Wrapped with `COALESCE(sqlc.narg(payload), '{}'::jsonb)` so the schema-stated default is preserved regardless of caller.

## Phase 2 — Local replay mode (fixture server)

Code committed in `322a012`; end-to-end run not yet verified.

- [x] Add `cmd/dev/fixture-server/main.go` — `http.FileServer` serving `testdata/fixtures`
- [x] Add URL rewriter in fetcher: `internal/dev/replay.go` — `WrapClientReplay` transforms `https://<host>/<path>` → `<fixture-base>/<host>/<path>`. Shared by discovery + collector via `--fixture-base` flag.
- [x] Taskfile entries `fixture:start/stop` and `worker:start:replay` wired with pgrep-based lifecycle.
- [x] Re-run pipeline with `task fixture:start` + `task worker:start:replay`, confirm same 26/26 output with zero real-site traffic (capture-dir does not grow).
  - Verified 2026-05-01: tasks DIRECTORY_FETCH=3 / PAGE_FETCH=26 all COMPLETED; candidates+contents dpp=10/kmt=10/tpp=6 each; capture-dir delta=0.
  - Bug fixed during run: `worker:start:replay` Taskfile entry was missing `--nats-host/--nats-port/--nats-token` flags on all 4 binaries → workers crashed at startup with `nats: Authorization Violation`. Patched.
  - Noise (not a bug): discovery worker logs `unsupported task kind: kind=PAGE_FETCH source_type=PARTY` — both discovery + collector subscribe to `prism.task` and filter by Kind; collector handles PAGE_FETCH correctly. Discovery's handler-level reject is expected. Consider quiet-loging non-matching kinds at DEBUG.

## Phase 3 — Error recovery smoke test

- [x] Add `--force-minify-error` dev flag to collector
  - `internal/dev/failing_minifier.go` — `FailingMinifier{}` always returns `ErrInjectedMinifyFailure`.
  - `cmd/worker/collector/main.go` swaps `minifier.New()` for `dev.FailingMinifier{}` when flag set; warns at startup.
- [x] `task worker:start:replay:fail-minify` wires the flag through the existing replay task via `FORCE_MINIFY_FLAG` var.
- [x] Re-run pipeline on fixtures with the flag enabled (2026-05-01)
- [x] Verify after run:
  - `tasks`: PAGE_FETCH=26 FAILED, DIRECTORY_FETCH=3 COMPLETED
  - `contents` table empty (minify failed, nothing reached DB)
  - `./tmp/archives/` contains raw archive entries with `kind:raw` + `recover_from:minify` metadata
- [x] Run `cmd/recover run` against archives, confirmed:
  - `Recovery complete: 3 succeeded, 0 skipped, 0 failed`
  - `contents` populated (1 per source after recover)
  - Idempotent: second run returns `0 succeeded, 3 skipped` (URL/candidate-id existence checks)
  - No real-site traffic (replay loads from local archive)

**Known gap (out of scope for Phase 3):** archive path is keyed `archives/YYYY/MM/DD/<traceID>.{data,meta.json}`
(`internal/collector/archiver/local.go:54`). Seed data uses one trace_id per source
(`integ-test-{dpp,kmt,tpp}`), so all 26 PAGE_FETCH archive writes collapsed into 3 files —
each task overwrote its predecessor at the same path. Only the last raw HTML per trace_id
survived to be recovered. This is the same anti-pattern flagged in docs/plan/future.md
("archive metadata catalog separation"; "hot prefix concentration"); it is *not* a Phase 3
regression. Demonstration coverage of the recover path is unaffected — full coverage would
require keying archive paths by task_id / content_id / random suffix and is bundled with
the catalog refactor cutover.

## Phase 4 — Containerize once host flow is stable

- [x] Write `deployments/Dockerfile.worker` (multi-stage, `CMD_PATH` build arg, `RUNTIME_IMAGE` build arg)
  - Production runtime: `gcr.io/distroless/static-debian12:nonroot`
  - Test/debug runtime: `alpine:3.20`
- [x] Write `deployments/docker-compose.worker.yaml` with scheduler / discovery / collector entries
- [x] Env-var audit: confirm all workers read postgres host / nats url from env cleanly (service names, not localhost)
  - Verified 2026-05-05 via containerized e2e drain: `DIRECTORY_FETCH=3 COMPLETED`, `PAGE_FETCH=26 COMPLETED`, contents `dpp=10 / kmt=10 / tpp=6`.
- [x] Add `.dockerignore`
- [x] Add `task compose:worker` to build and bring up the worker stack
  - `task test:e2e:teardown` now explicitly tears down worker and tool-profile containers before `compose:clean`, so the e2e stack exits cleanly with no leftover `prism-e2e-*` containers.

## Phase 5 — testcontainers-go + GitHub Actions CI

Goal: replace implicit "external service must be running" assumptions with self-contained, reproducible integration tests that work identically on dev laptops and CI.

**Current pain:** `internal/collector/archiver/s3_test.go` fails when SeaweedFS isn't running at `localhost:8333`. Nothing in the test harness documents or provisions that dependency — it's a silent requirement on `task compose:up`.

### Scope (start narrow, expand as need proves)

Phase in order; don't do all at once:

1. **`internal/collector/archiver` (done)** — S3Archiver tests spin up SeaweedFS (`chrislusf/seaweedfs:4.05`) via testcontainers-go in `TestMain`. Still TODO: add `//go:build integration` tag, switch to `testcontainers.CleanupContainer(t, ctr)` per-test where practical, extract setup helper to `internal/testsupport/`.
2. **`internal/repo/pg` (when schema churn accelerates)** — new `pg_integration_test.go` runs real migrations and exercises SQLC-generated queries against a fresh Postgres container. Catches migration drift, `ON CONFLICT` semantics, partial-index behavior, pgvector wiring.
3. **`cmd/scheduler` concurrency (when multi-instance lands)** — real Postgres + goroutine fan-out to verify `FOR UPDATE SKIP LOCKED` claim/release semantics under contention. Mocks can't express the race.
4. **Valkey-backed rate limiter (when Future Roadmap item lands)** — real Valkey container to verify Lua-script atomicity of the token bucket.

**Out of scope for testcontainers:** Scout / Minify / Parser (fixtures are faster and more deterministic); discovery/collector/planner handler tests (mocks are already doing the right job).

### Conventions

- **Build tag isolation:** `//go:build integration` at the top of every testcontainers-using file. `go test ./...` stays fast (unit only); `go test -tags=integration ./...` runs the integration layer.
- **Image tag pinning:** always specify an exact version (`chrislusf/seaweedfs:3.91`, `postgres:18-alpine`, `valkey/valkey:9.0`). `:latest` breaks CI unpredictably.
- **Explicit cleanup:** call `testcontainers.CleanupContainer(t, ctr)` per test — don't rely solely on the ryuk reaper.
- **Helper packages:** put shared setup (e.g. `pgtest.Start(t) (dsn string, factory repo.Factory)`) under `internal/testsupport/` so test files stay short.
- **Parallelism:** each test gets its own container by default. If startup cost becomes a bottleneck, share read-only setups via `TestMain` with per-test schema / prefix isolation — but measure first.

### GitHub Actions

Two-job split so PRs get fast unit feedback, with integration as a gate for merges:

```yaml
# .github/workflows/test.yml (outline)
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test -short ./...

  integration:
    runs-on: ubuntu-latest       # Docker preinstalled
    needs: unit
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test -tags=integration ./internal/collector/archiver/...
      # expand paths as Phase 5 scope grows
```

Why testcontainers-go over GH Actions `services:` block:

- **One code path for local and CI** — `go test -tags=integration` runs the same way on a laptop and in Actions. `services:` only exists in CI, which means local developers fall back to `task compose:up` and the two paths silently diverge.
- **No workflow YAML churn** as test dependencies evolve — the containers are declared in Go, next to the tests that need them.
- **Per-test isolation** — fresh DB / bucket per test is trivial; `services:` gives you one shared instance for the whole job.
- Docker is preinstalled on `ubuntu-latest`, so there's no runner setup cost.

### Cost estimates

| Container | Image size | Cold boot | Warm boot |
|---|---|---|---|
| SeaweedFS 3.x | ~100 MB | ~3s | ~2s |
| Postgres 18 | ~350 MB | ~2s | ~1s |
| Valkey 9 | ~30 MB | ~1s | <1s |

First CI run pulls all images (~1–2 min); subsequent runs hit the GH Actions layer cache. Per-test-binary startup is the dominant runtime cost — keep containers per-package, not per-test, when feasible.

## Phase 6 — User-line driver test (Phase 2.7 Stage 4)

Goal: prove the **user-facing path** end-to-end against the live stack, with
automated assertions. Phases 0–5 validated the **background pipeline**
(scout → collector → archive); Phase 6 adds the user line on top:
`POST /page_fetch` → `GET /fetches/{id}` terminal.

Assumes the prior phases stay green. Discovery is *not* exercised here —
candidates are seeded directly via SQL so a Phase 6 failure isolates to
the user line (api-server, fetches/fetch_items, scheduler PAGE_FETCH,
collector, archiver). Discovery regressions are caught by re-running
Phase 1/2.

### Scope

In:
- api-server `POST /api/v1/page_fetch` happy path (`created` items)
- api-server `GET /api/v1/fetches/{id}` polling until `terminal=true`
- collector + archiver running real PAGE_FETCH from a seeded candidate
- Valkey progress cache + per-IP rate limit (Stage 3 toggles ON, so the
  optional paths are exercised on every run)
- post-conditions: `contents` row populated, archive blob present

Out (deferred):
- failure-path matrices (`failed`, `already_complete`, `not_found`)
- multi-candidate batches + per-item ordering
- discovery → planner → MEDIA chain (covered by Phases 1/2; rerun
  separately when needed)
- LLM-fallback parser path (Phase 2.9 item 11)

### Driver layout

- `e2e/page_fetch_test.go` — `//go:build e2e`; first automated driver.
  Uses `net/http`, `pgxpool`, and the live stack. Black-box at the API
  boundary; reads DB only to assert side-effects.
- `e2e/helpers.go` — `//go:build e2e`; small helpers: load env,
  seed candidate, poll fetch, drain archive. Lives next to the
  driver — no `internal/testsupport/` extraction until a second
  driver lands.
- `testdata/seed-page-fetch.sql` — minimal source + candidate row
  (URL points at the in-stack `fixture-server`). Separate file from
  `seed-tasks.sql` so the existing operator e2e (Phase 1) is
  untouched.

### Stack additions

The driver needs the api-server and the fixture-server inside the
isolated `prism-e2e` stack. Verify before assuming work needed:

- **api-server** — confirm whether `deployments/docker-compose.worker.yaml`
  already includes a `prism-api` service or whether one must be added
  (Stage 3 only added flags, not the compose service).
- **fixture-server** — `cmd/dev/fixture-server` exists and serves
  `testdata/fixtures/<host>/<path>.html`. It runs as a host process in
  Phase 2 (`task fixture:start`); for Phase 6 it must run
  inside compose so the collector resolves
  `http://fixture-server:8080/...` from its own network. Add a
  service entry in the worker overlay.
- **scheduler PAGE_FETCH instance** — Phase 1 already runs a
  fast-tier scheduler with `--kinds=PAGE_FETCH`. Confirm the
  containerized worker stack does the same.

If any of the three are missing, fold the additions into a small
preparatory commit before the driver lands.

### Driver flow

```text
0. compose stack up via existing test:e2e:setup (no change)
1. SQL: INSERT sources, INSERT candidates (id, url=http://fixture-server:8080/<host>/<path>.html)
2. POST /api/v1/page_fetch {candidate_ids:[id]} → 202; assert items[0].status=created, capture fetch_id
3. poll GET /api/v1/fetches/{fetch_id} every 1s, cap 30s; assert terminal=true at end
4. assert progress: completed=1, failed=0, already_complete=0, total=1
5. SELECT contents WHERE candidate_id=id; assert row exists with non-empty title+content
6. (optional) S3 HEAD archive blob; skip if SeaweedFS bucket not yet pre-created in compose
7. compose stack down via existing test:e2e:teardown
```

Step 6 stays optional in v1 — the contents row presence is enough to
prove the collector finished the pipeline; archive verification can
land alongside the catalog refactor in `future.md`.

### Task entry

```yaml
test:e2e:page-fetch:
  desc: Phase 6 — automated user-line driver test (Phase 2.7 Stage 4)
  cmds:
    - task: test:e2e:setup
    - psql "{{.POSTGRES_URL}}" -f testdata/seed-page-fetch.sql
    - go test -tags=e2e -count=1 -v ./e2e/...
    - defer: { task: test:e2e:teardown }
```

`-count=1` disables the build cache so a stale stack doesn't mask
real failures. `defer:` ensures teardown even on test failure.

### Toggles

Run with Stage 3 features ON so the live paths are exercised:

```
--cache-enabled
--rate-limit-enabled --rate-limit-rps=100 --rate-limit-burst=200
```

Burst set high so polling at 1Hz never trips the limiter. The
`429` path is already covered by unit tests; live-stack rate-limit
verification is out of scope for Phase 6.

### Risks

1. **Collector pipeline timing** — fetch + transform + parse + save +
   archive can take 5–15 s under load; 30 s poll cap should be enough.
   Bump if flake.
2. **Fixture article shape** — the seeded URL must serve content the
   existing `html` or `jsonld` parser recognizes. Reuse one of the
   captured DPP/KMT/TPP fixtures from Phase 1 rather than
   handcrafting new HTML.
3. **Bucket pre-creation** — SeaweedFS bucket must exist before the
   archiver's first write. Confirm whether `compose:up` already
   handles this; if not, add a one-shot init container or a `psql`-
   style seed step. Step 6 mitigates by being optional.
4. **Worker readiness race** — the driver currently has no
   `wait-for-it` step beyond `compose:up`. If the api-server or
   collector is slow to bind, step 2/3 may hit `connection refused`.
   Add a small `script/e2e-wait.sh` polling `/healthz` if observed.

### Estimate

3–5 hours from inventory to first green test, dominated by compose
service additions if api-server / fixture-server aren't already
wired. Driver code itself is ~150 lines.

## Out of scope (deferred until load proves need)

- Stage split (F→M→S vs T→P) into separate workers (方案 Z)
- In-process worker pool / two-stage fan-out
- Broker-native delayed retry for 429 (NATS JetStream `NakWithDelay`, SQS `ChangeMessageVisibility`) — revisit when wiring real NATS JetStream or SQS
- Composite parser debug log enrichment (nice-to-have, ~5 lines whenever convenient)
