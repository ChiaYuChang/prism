# Integration Test Plan: First End-to-End Pipeline Run

Goal: verify the existing microservices (scheduler, discovery worker, collector worker, recover tool) compose into a working pipeline, and build a local fixture dataset so future iterations do not hit real sites.

## Phase 0 — Pre-work (~30–45 min, code changes)

- [ ] **Confirm discovery → collector message wiring**
  - Verify which topic discovery publishes `page_fetch` task signals to, which topic collector subscribes to, and that the signal struct (`TaskSignal` with `kind=PAGE_FETCH`) matches on both ends.
  - Read targets: `cmd/worker/discovery/handler.go`, `cmd/worker/collector/main.go:122`, `internal/message/*`.

- [ ] **Add `CaptureTransport` to fetcher**
  - New file `internal/collector/fetcher/capture.go`, ~30 lines.
  - Wraps any `http.RoundTripper`; tees successful response bodies to `<dir>/<host>/<path>.html`.
  - Opt-in via `--capture-dir` flag on discovery and collector.

- [~] **`--max-directory-pages` cap — not needed by design**
  - Daily scouts intentionally fetch a single index page per run; one DIRECTORY_FETCH task = one URL = one page. Pagination is only implemented in the backfiller (where it already has `MaxPages`).
  - Design rationale: ~10 press releases/day per source is the expected upper bound. Sources that publish more frequently should be handled by raising scout cadence (multiple DIRECTORY_FETCH tasks per day) rather than adding a daily-mode pager — pagination introduces ordering / dedup / "where to stop" complexity that scheduling solves for free.
  - Phase 1 implication: to broaden the fixture corpus, seed DIRECTORY_FETCH tasks for *multiple sources*, not multiple pages of one source.

- [ ] **Seed SQL for one DIRECTORY_FETCH task**
  - Target `source_abbr='dpp'`, `runnable_at=NOW()`, state `pending`.
  - Include all NOT NULL columns required by the `tasks` schema.

## Phase 1 — One-shot real-site run (~10 min, only time we touch real sites)

- [ ] `task compose:up` — infra up (postgres, nats, seaweedfs)
- [ ] `task migrate:up` — migrations (sources/candidates/contents/tasks tables)
- [ ] Apply seed SQL from Phase 0
- [ ] Start scheduler + discovery + collector in three terminals
  - Discovery: `--capture-dir=testdata/fixtures`
  - Collector: `--capture-dir=testdata/fixtures --archive=file://./tmp/archives`
- [ ] Target roughly 1 directory page × ~10 articles per source; seed 3–4 source tasks to reach ~30 articles in the fixture set
- [ ] Verify after run:
  - `candidates` table has ~30 rows, `source_abbr='dpp'`, `source_type='PARTY'`
  - `contents` table has ~30 rows with article title/body
  - `testdata/fixtures/www.dpp.org.tw/...` holds the captured HTML files
  - `./tmp/archives/` has the minified archives (normal success path)

## Phase 2 — Local replay mode (fixture server)

- [ ] Add `cmd/tools/fixture-server/main.go` — 5–10 line `http.FileServer` serving `testdata/fixtures`
- [ ] Add URL rewriter in fetcher: when `--fixture-base` is set, transform `https://<host>/<path>` → `<fixture-base>/<host>/<path>`. Shared helper used by discovery + collector.
- [ ] Re-run pipeline with `--fixture-base=http://localhost:9999` instead of `--capture-dir`, confirm same output with zero real-site traffic.

## Phase 3 — Error recovery smoke test

- [ ] Add `--force-minify-error` dev flag to collector
  - Wraps minifier in a `FailingMinifier` shim that always errors.
  - Logs `WARN: minify error injection enabled, DEV ONLY` at startup.
- [ ] Clear `candidates` / `contents`, re-run pipeline on fixtures with the flag enabled
- [ ] Verify:
  - Collector log shows "minify error (injected), archiving raw"
  - `./tmp/archives/` contains raw HTML entries with `stage=raw` metadata
  - `contents` table is empty (minify failed, nothing reached DB)
- [ ] Run `cmd/recover` pointed at `./tmp/archives/`, confirm:
  - Archived raw items are picked up and replayed through minify → transform → parse → DB
  - `contents` table now populated
  - No real-site traffic (replay reads from local archive)

## Phase 4 — Containerize once host flow is stable

- [ ] Write `deployments/Dockerfile.worker` (multi-stage, `CMD_PATH` build arg, `RUNTIME_IMAGE` build arg)
  - Production runtime: `gcr.io/distroless/static-debian12:nonroot`
  - Test/debug runtime: `alpine:3.20`
- [ ] Write `deployments/docker-compose.workers.yaml` with scheduler / discovery / collector entries
- [ ] Env-var audit: confirm all workers read postgres host / nats url from env cleanly (service names, not localhost)
- [ ] Add `.dockerignore`
- [ ] Add `task compose:workers` to build and bring up the worker stack

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

## Out of scope (deferred until load proves need)

- Stage split (F→M→S vs T→P) into separate workers (方案 Z)
- In-process worker pool / two-stage fan-out
- Broker-native delayed retry for 429 (NATS JetStream `NakWithDelay`, SQS `ChangeMessageVisibility`) — revisit when wiring real NATS JetStream or SQS
- Composite parser debug log enrichment (nice-to-have, ~5 lines whenever convenient)
