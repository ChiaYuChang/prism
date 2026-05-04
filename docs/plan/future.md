# Project Prism — Future Roadmap

Deferred refactors, dual-mode deployment plans, archive catalog refactor, and cloud anti-patterns. Carved verbatim from `plan.md` §5. Cloud anti-patterns themselves live in `spec.md` §6.

## Future Roadmap

* [ ] Finish consolidating command bootstrap config:
  * [x] move shared Postgres and Messenger config types into `internal/appconfig`
  * [x] move shared Valkey config type into `internal/appconfig`
  * [x] migrate `cmd/scheduler` to reuse the same shared config types
  * [x] route command-side repository construction through `repo.Factory`
  * [x] keep repository config concrete for now; defer polymorphic repository config until another backend exists
  * [x] add command-level tests for `cmd/backfiller`
  * [x] add dispatch-path tests for `cmd/scheduler`
  * [x] switch command CLI overrides from dotted flags to prefixed flat names such as `--pg-host` and `--valkey-host`
  * [ ] review whether command-local `LoadConfig()` logic should remain separate or be partially unified (In progress: added `bindflag.go` for unified flag binding)
  * [ ] remove remaining duplicated bootstrap helpers once worker commands stabilize
* [x] Rework infra configuration source-of-truth:
  * [x] temporarily exclude generated `build/` artifacts from version control
  * [ ] move version-controlled infra config sources into `deployments/` or another template directory
  * [ ] introduce `*.tmpl` / `*.example` files for rendered runtime config such as Valkey ACL and server config
  * [ ] keep `build/infra/` for generated local output only
* [ ] Runtime application wiring for config-driven scout registration and enable/disable controls.
* [ ] RSS and official API ingestion for additional media sources that support them.
* [ ] More robust search-provider abstraction and quota management.
* [ ] LLM-assisted quality control for parser drift detection.
* [ ] **Dedup `LLMTargetNodeList.Value()` by trimmed value before joining.** Currently when an LLM returns the same text via multiple selectors (e.g. headline available through `<title>`, `<h1>`, and `<meta property="og:title">`, all yielding "Breaking News"), `Value()` joins them with `\n\n` separator and produces "Breaking News\n\nBreaking News\n\nBreaking News". Should keep first occurrence and drop subsequent matches. DOM order is preserved because LLM-returned nodes are already in reading order (per `article_parser.md` prompt). Not blocking; surfaces as visual duplication in extracted Title/Author/Content fields.
* [ ] TUI and Web dashboard.
  * TUI: `cmd/tui` (2.8) — Bubble Tea, single Go binary for operator use.
  * Web: Alpine.js + server-side Go templates, static assets `//go:embed`-ed into `cmd/api-server`. Keep the API JSON-only; Alpine consumes the same `/api/v1/*` endpoints as TUI — no separate HTML-fragment endpoints (would be the HTMX alternative). One API surface, two renderers.
* [ ] Email / webhook notification on user-triggered batch completion (builds on 2.7 `GET /batches/{batch_id}`). Batch meta should reserve a `notify` field (`{email, webhook_url}`) so `POST /page_fetch` callers can opt in without a schema change later.
* [ ] JS-rendered scraping via Playwright where legally and operationally acceptable.
* [ ] Persist rolling-window seed clustering as analysis assets.
* [ ] Model cluster lineage as a directed graph or DAG for issue evolution analysis.
* [ ] **Move scheduler rate limiter from in-memory to Valkey.**
  * **Why:** the current `infra.InMemoryRateLimiter` keeps per-`source_abbr` token buckets inside the scheduler process. That state is tied to the binary's lifetime, which blocks two scaling moves: (a) switching the scheduler to a short-lived `--once` / cron / Lambda deployment (each invocation would reset every bucket and blow past per-source quotas), and (b) running multiple scheduler instances concurrently for horizontal throughput (each would throttle in isolation, letting the aggregate exceed the quota).
  * **What:** implement a Valkey-backed token bucket (Lua script for atomic `TAKE`). Same `infra.RateLimiter` interface so call sites (`applyRateLimit`) stay unchanged. Keep the in-memory implementation for tests and `gochannel` mode.
  * **When:** before migrating scheduler to cron/Lambda or scaling it past one instance. Not urgent while a single long-running scheduler is the only deployment shape.
* [ ] **Adopt testcontainers-go for integration tests + GitHub Actions CI.**
  * **Why:** `internal/collector/archiver/s3_test.go` previously required SeaweedFS at `localhost:8333` (via `task compose:up`). `services:` in GH Actions would fix CI but diverges from local and gives one shared instance per job rather than per-test isolation.
  * **What:** phase in by scope — (1) [x] `internal/collector/archiver` SeaweedFS via `TestMain` (`chrislusf/seaweedfs:4.05`); (2) [ ] `internal/repo/pg` real migrations + SQLC queries when schema churn accelerates; (3) [ ] `cmd/scheduler` concurrency against real Postgres when multi-instance lands; (4) [ ] Valkey-backed rate limiter when that roadmap item lands. Still TODO: add `//go:build integration` tag, pin all image tags, adopt `testcontainers.CleanupContainer`, extract shared helpers to `internal/testsupport/`.
  * **CI:** [ ] GH Actions two-job split — fast `go test -short ./...` on PR, gated `go test -tags=integration ./...` on merge (`ubuntu-latest` has Docker preinstalled).
  * **See:** `docs/integration-test-plan.md` Phase 5 for the full plan.
* [ ] **Migrate scheduler to short-lived execution (`--once` + cron / Lambda / Fargate Scheduled Task).**
  * **Why:** tick interval is 10 minutes but the tick itself takes < 1s; a long-running EC2 instance burns resources for < 0.1% utilization and holds idle Postgres connections (see `pg.Factory` defaults lowered to `MaxConnIdleTime=1m`). A short-lived model releases PG connections between ticks and maps cleanly onto serverless cron.
  * **What:** add `--once` flag to `cmd/scheduler` that runs `RunTick` once and exits; keep the Valkey distributed lock as a safety net against overlapping invocations; ensure the rate limiter migration above lands first so bucket state survives.
  * **Cost caveat:** Lambda-in-VPC with private Postgres/Valkey typically requires a NAT Gateway (~$32/mo), which is more expensive than a `t4g.nano` long-running instance (~$3/mo). Prefer Fargate Scheduled Task or keep long-running EC2 unless DB endpoints are already public or behind RDS Proxy.
* [ ] **Dual-mode deployment target: local `worker` ↔ cloud `SQS + Lambda` (queue-driven async processing).**
  * **Why:** prism's actual workload is per-page (per-message) and bursty — not stream (no <1s SLA, no time ordering) and not batch (not a periodic large chunk). The AWS-canonical pattern for this shape is **SQS + Lambda Event Source Mapping** with `batch_size` + `maximum_batching_window` tuning. The platform handles queue-depth-driven autoscaling and micro-batching; we keep one handler implementation that runs as a long-lived worker locally and as a Lambda in the cloud.
  * **Component-by-component target:**
    * `cmd/scheduler` (cron-triggered, runs-and-exits): EventBridge + Lambda
    * `cmd/worker/discovery`, `cmd/worker/collector`, `cmd/worker/archiver`: SQS + Lambda (`batch_size=5–20`, `window=30s`)
    * `cmd/worker/planner` (LLM call may exceed Lambda 15-min limit): SQS + ECS Fargate service with queue-depth autoscaling
    * `cmd/trigger/batch` (cron-triggered): EventBridge + Lambda
  * **Required handler-side changes (must be done before either mode can switch):**
    * **`--mode={worker,lambda}` flag dispatch** in each `cmd/worker/*` `main.go` — same handler, different runtime shell. `worker` = current `msgr.Subscribe + for-select`; `lambda` = `lambda.Start(adapter)` where adapter decodes SQS event and calls `HandleMessage` per record.
    * **Batch handler adapter** — Lambda delivers `events.SQSEvent` with up to `batch_size` records; adapter loops over records and reports partial-batch failures via `SQSBatchResponse.BatchItemFailures`. Handler stays single-record.
    * **Package-level connection pools** — `pgxpool`, NATS/SQS client, Valkey client must live at package scope inside lambda binaries, initialized once in cold start via `sync.Once`. Reuses across warm invocations (>90% hit rate typical).
    * **Archive payload via S3 pointer in cloud mode** — `ArchiveSignal.Page` carries gzipped HTML inline (currently fine on gochannel); SQS has a 256KB message limit that large pages will breach. Cloud mode must upload payload to S3 first and put only the S3 key in the message. Handler interface needs to abstract this so worker mode can keep inline payload.
    * **Idempotency check on every handler entry** — already in place (`GetContentByURL` / `GetContentByCandidateID` skip-checks); SQS at-least-once means duplicates can arrive. Verify all handlers (not just collector) have equivalent guards before enabling `lambda` mode.
    * **OTel sync flush in lambda mode** — short-lived containers exit before async exporter flushes; Lambda extension or explicit `tracerProvider.ForceFlush(ctx)` at handler return.
  * **Required infra-side changes:**
    * Rate limiter must be Valkey-backed (already a Future item above) — in-memory limiter resets on every Lambda cold start and lets aggregate throughput exceed quota.
    * Connection pool init time < 1s — current `pgxpool` defaults are fine; verify with cold-start benchmarks before committing.
    * RDS Proxy in front of Postgres only if observed connection-count fanout becomes a problem (1000 concurrent Lambdas = 1000 PG connections without a proxy). Defer until measured; SQLC's prepared-statement use complicates Proxy transaction-mode.
  * **Anti-goals (do NOT do these):**
    * Do not invent a `Runtime` interface abstracting worker/lambda — three small `runXxx(ctx, h, cfg)` functions in `main.go` are clearer.
    * Handler code must NOT branch on `cfg.Mode` — once that creeps in the abstraction is broken.
    * Do not pursue Kinesis Data Streams; prism has no per-key time ordering requirement and the stream model is more expensive and rigid.
    * Do not add Provisioned Concurrency unless a user-facing SLA appears; batch analytics tolerates cold starts.
  * **When:** sequence is (1) Valkey rate limiter → (2) `--mode` dispatch + batch adapter on collector worker as pilot → (3) S3-pointer archive payload → (4) extend to discovery/archiver/planner. Scheduler `--once` Lambda migration (item above) is the prerequisite proof-of-shape for this larger move.
* [ ] **Move archive metadata into PG (catalog + storage separation); reduce `Archiver` to bytes-only Save/Load.**
  * **Why:** the current `Archiver` interface (`Save / Load / Scan / Remove`) treats the storage backend as a self-describing catalog — sidecar `meta.json` next to every payload, queries via `Scan(opts)` that must read every meta to filter. This was filesystem-shaped thinking and creates several reverse-anti-patterns on S3: O(N) GETs for any filtered scan (1M objects ≈ $0.40 + minutes), soft-delete via read-modify-write on meta with no atomicity, dual PUTs (payload + meta) without crash safety, hard-coded date prefix `YYYY/MM/DD/<traceID>` that does not help direct `Load(traceID)` lookups (OTel trace IDs are random 16-byte values, not time-encoded). Deeper cause: ~70% of meta fields are duplicates of `contents`/`tasks`/`batches` columns already in PG (URL, trace_id, source_abbr, batch_id, fetched_at, fingerprint), and the truly archive-specific fields (kind, sha256, size, error, storage_uri) belong in a thin PG catalog table — not next to the payload. PG is the natural index store and is already a hard dependency; storing meta alongside bytes was reinventing what PG provides for free.
  * **What — schema:**
    * New `archives` table:
      ```sql
      CREATE TABLE archives (
          id           UUID PRIMARY KEY,           -- UUID v7, embeds creation timestamp
          content_id   UUID REFERENCES contents(id), -- NULL when Minify failed pre-content
          trace_id     TEXT NOT NULL,
          kind         TEXT NOT NULL,              -- raw | minified | canonical
          storage_uri  TEXT NOT NULL,              -- file:///… or s3://…
          sha256       TEXT NOT NULL,
          size_bytes   BIGINT NOT NULL,
          error        TEXT,                       -- non-empty for raw (Minify failure)
          created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
          deleted_at   TIMESTAMPTZ
      );
      CREATE INDEX archives_trace_id_idx ON archives (trace_id);
      CREATE INDEX archives_content_id_idx ON archives (content_id) WHERE content_id IS NOT NULL;
      ```
    * No sidecar files anywhere — local layout becomes pure `<root>/<archive_id>` and S3 becomes `s3://bucket/archives/<archive_id>/data`. The `YYYY/MM/DD/` date prefix is dropped (date is recoverable from UUID v7 if ever needed for debugging).
  * **What — interface:**
    * Narrow `Archiver` to bytes-only:
      ```go
      type Archiver interface {
          Save(ctx, archiveID UUID, payload []byte) (storageURI string, err error)
          Load(ctx, storageURI string) ([]byte, error)
      }
      ```
    * Drop `Scan`, `Remove`, and the `Meta` struct. Caller (handler / recover CLI) generates `archive_id`, calls `Save`, then INSERTs to `archives` table. Queries are SQL joins against `archives` + `contents` + `tasks`.
  * **What — write ordering (S3-first):**
    * Save bytes to S3/local first → then INSERT to PG. If PG INSERT fails the storage object becomes orphan and is reaped by lifecycle (S3) or a periodic sweeper (local). The reverse order would leave PG rows pointing to non-existent objects, which is harder to detect.
  * **What — `cmd/recover` rework:**
    * `recover list/run` issues PG SQL (`SELECT … FROM archives WHERE kind='raw' AND error IS NOT NULL AND created_at > now() - INTERVAL '7 days'`), then calls `Load(storage_uri)` for each row. No more `Scan`, no Inventory dependency, no backend-specific reader.
    * `recover clean` becomes either soft-delete (set `deleted_at`) followed by lifecycle-managed payload removal, or hard delete that DELETEs the row and the object together — picked per retention rule, not hardcoded.
  * **What — retention:**
    * Time-only rules (raw payloads older than 90 days) → S3 lifecycle on the `archives/` prefix; equivalent local sweeper.
    * State-dependent rules (recovered, content-linked) → SQL queries with explicit predicates.
    * Path layout for lifecycle separation: `archives/raw/<id>`, `archives/canonical/<id>`, `archives/recovered/<id>` so each prefix carries one rule.
  * **Trade-offs (real ones):**
    * Dual-write (storage + PG) is not transactional. S3-first ordering accepts orphan objects on PG-INSERT failure (reaped by lifecycle / local sweeper) and rejects orphan PG rows pointing to missing objects (harder to detect). Standard data-lake reconciliation pattern.
    * Loss of self-describing storage. Today every archive object carries enough sidecar context (URL, source, batch_id, error) to be reconstructable in isolation; after the refactor, an object without its PG row is opaque bytes. Mitigation: write a minimal subset (trace_id, kind) to S3 Object Tags as a partial-recovery index for the PG-loss scenario. Cost is negligible (no extra request, just a Tagging header on the same PUT).
    * Lifecycle / PG `deleted_at` are not synchronized — possible inconsistency window (S3 lifecycle reaps payload while PG row still says alive, or vice versa). Acceptable for prism's threat model; do not add EventBridge → PG sync to "fix" it.
    * Local dev/test now requires PG to exercise archive paths. testcontainers plan above already covers this; CI startup cost +30s.
    * Implementation is ~2–3 focused days, not a weekend afternoon.
    * Existing `LocalArchiver` data has no migration cost — production has not yet promoted to S3, so the refactor lands before any data exists at scale.
  * **NOT trade-offs (corrected from earlier framing):**
    * "Save now depends on PG availability" is not a new failure mode. PG is already on the critical path for every other component (scheduler, discovery, collector, planner, api-server) — making archive align to the same availability tier is correctness, not a new coupling. PG availability is an infrastructure concern (RDS Multi-AZ, Aurora, Patroni), not an application concern; do not write code that "handles PG being down" — handler returns error, broker buffers via redelivery, that is the entire strategy.
    * "Schema migration becomes harder" is not unique to this table. The same constraint already applies to `contents` / `tasks` / `candidates` and the team handles it via SQLC + ordered migrations. The `archives` table is no different.
  * **Anti-goals:**
    * Do not retain `Scan` "for symmetry with local" — symmetry that hides a 1000× cost difference is what created the original problem.
    * Do not duplicate the full meta into S3 Object Tags. A minimal partial-recovery index (trace_id, kind) is fine; full denormalization is reinventing the sidecar problem.
    * Do not keep `meta.json` sidecar as a "compatibility layer" during transition — production has no S3 data yet, do the cutover cleanly.
    * Do not put archive payload bytes into PG. PG is the catalog only; bytes stay in storage layer.
    * Do not write application-level retry/circuit-breaker for PG outages. Use pgx's built-in retry, set per-query `context.WithTimeout`, and let the broker handle redelivery on failure. PG HA is solved at infra layer.
  * **When:** **defer implementation; lock the interface direction now.** Pipeline end-to-end correctness (archive publisher wiring, recover.go dual-path, layer 1 unit test gaps in Immediate Next Steps #11) is higher priority than this refactor. Concrete principle: keep the current `Archiver` shape as-is for daily work, but
    * Do not add new `Scan`-heavy callers — recover is the only legitimate caller and is already known.
    * Do not add new fields to `meta.json` — push new metadata into PG `contents` or related tables instead so it is already in the right place when the cutover happens.
    * Treat the current `Scan` / `Remove` / `Meta` / sidecar design as deprecated-in-place; document the deprecation in archiver code.
    Schedule the actual cutover for the week before promoting S3 to production (the migration cost is zero only while no real data exists at scale, and that window will not stay open forever). Bundle it with the SQS+Lambda dual-mode rollout above.
