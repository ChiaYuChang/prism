# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run all unit tests (short mode)
go test -v -short -cover ./...

# Run a single package's tests
go test -v -short ./internal/discovery/planner/...

# Generate SQLC query code (after editing db/queries/*.sql or db/schema.sql)
task sqlc

# Generate mocks (after editing interfaces in internal/collector, internal/discovery, internal/llm, or internal/repo)
task mocks

# Tidy dependencies
task tidy

# Run migrations
task migrate:up
task migrate:down

# Bake env/local/<env>.local.env from .secrets/* (run before any compose task)
ENV=test ./script/secrets-bake.sh

# Bake .<env>-<profiles>.docker-compose.merged.yaml from base + overlays + tools + worker
# (compose:bake delegates to script/compose-bake.sh; ENV=prod requires PRISM_PROD_OK=1)
task compose:bake ENV=test COMPOSE_PROFILES=dev,obs

# Start local infra (Postgres, NATS, Valkey, SeaweedFS); honors ENV / COMPOSE_PROFILES
task compose:up

# Build + start containerized workers alongside the running infra stack
task compose:worker

# Run the scheduler locally (gochannel mode, no NATS required)
task test:scheduler

# Run the discovery worker locally (gochannel mode)
task test:discovery

# Build all binaries
task build:all
```

## Architecture

### Pipeline Model: F-T-(S||P)

Every full-article ingestion follows: **Fetch → Transform → (Save || Parse)** in parallel. Discovery is deliberately kept upstream and separate from this pipeline.

### Two Distinct Lifecycle Stages

| Stage | Table | Entry Point |
|---|---|---|
| Discovery | `candidates` | Scout discovers briefs; Planner creates MEDIA tasks |
| Collection | `contents` | `page_fetch` consumer fetches and parses full articles |

These stages must not be conflated. Discovery ends at persisted `candidates`. The collector starts at `page_fetch`.

### Trigger Classes

Three trigger classes exist in the system:

- **`schedule`** — time-based: `cmd/scheduler` polls Postgres for runnable tasks and publishes them via Watermill
- **`resource`** — state-based: `cmd/trigger/batch` polls for completed PARTY batches and publishes `batch.completed`
- **`manual`** — operator-initiated: `cmd/backfiller` for historical replay

### Discovery Flow

```
cron
 └─► [PARTY + DIRECTORY_FETCH tasks in `tasks`]
      └─► cmd/scheduler  (claims tasks, publishes TaskSignal to prism.task)
           └─► cmd/worker/discovery  (Scout crawls party directory pages)
                └─► candidates (persisted) + page_fetch published
                     └─► [collector: fetches full press release into contents]
                          └─► cmd/trigger/batch  (polls for completed PARTY batch)
                               └─► batch.completed published
                                    └─► cmd/worker/planner  (Planner extracts phrases, creates MEDIA tasks)
                                         └─► [MEDIA + DIRECTORY_FETCH tasks in `tasks`]
                                              └─► cmd/scheduler  (claims and dispatches)
                                                   └─► cmd/worker/discovery  (calls search API → candidates)
```

### Repository Layer

`internal/repo/repo.go` defines worker-oriented interfaces:

- `Scheduler` — claim/complete/fail tasks
- `Scout` — sources + candidates
- `Tasks` — create/query tasks
- `Pipeline` — contents CRUD
- `BatchTrigger` — read-side for batch completion detection
- `Embeddings` / `Analysis` — vector and extraction assets

`internal/repo/pg/` holds the SQLC-generated Postgres implementation. `pg.NewFactory(config).NewRepository(ctx)` returns `(repo.Repository, repo.Closer, error)`. All commands wire repos through `repo.Factory`, never constructing `pgxpool` directly.

`internal/repo/contracts.go` — read-side contract types (e.g. `Task`, `Candidate`, `Content`)  
`internal/repo/params.go` — write-side param structs (e.g. `CreateTaskParams`, `UpsertCandidateParams`)  
`internal/repo/pg/adapters.go` — maps SQLC-generated rows to repo contract types

### Config Pattern

Every command uses the same bootstrap pattern:
1. `pflag` for CLI flags using **prefixed flat names** (`--pg-host`, `--valkey-host`, not `--postgres.host`)
2. `viper` with `BindPFlag` to map flat flags to nested config keys (`postgres.host`)
3. Config unmarshalled into a struct embedding shared types from `internal/appconfig`:
   - `appconfig.PostgresConfig` — Postgres connection
   - `appconfig.ValkeyConfig` — Valkey/Redis connection
   - `appconfig.MessengerConfig` (interface) — polymorphic Watermill backend (`NatsConfig` or `GoChannelConfig`)
4. `go-playground/validator` validates the assembled struct

Messenger config is polymorphic (NATS or GoChannel) because multiple backends exist. Repository config remains concrete `PostgresConfig` until a second backend exists.

### Message Topics

| Topic | Signal | Publisher → Consumer |
|---|---|---|
| `prism.task` | `TaskSignal` | scheduler → discovery worker, scheduler → collector worker (filtered by `Kind`) |
| `prism.batch.completed` | `BatchCompletedSignal` | batch trigger → planner worker |

PAGE_FETCH is **not** a separate topic. The discovery candidate sink writes a `PAGE_FETCH` row into the `tasks` table (PARTY only); the scheduler claims it like any other task and publishes a `TaskSignal{Kind=PAGE_FETCH}` to `prism.task`. The collector worker subscribes to `prism.task` and filters by `Kind`. Routing through the tasks table gives PAGE_FETCH the same retry / rate-limit / observability handling as every other task class — keeping discovery and the collector decoupled via the scheduler.

Signal structs live in `internal/message/`. They use plain `json.Marshal`/`json.Unmarshal` with explicit `Marshal()`/`Unmarshal()` methods only when needed for non-standard field handling.

### Scout Architecture

`internal/discovery/scout/` contains config-driven scouts:
- `html/` — CSS-selector-based HTML directory pages
- `rss/` — standard RSS XML feeds
- `atom/` — Atom feeds
- `custom/yahoo/` — non-standard embedded-JSON pages

Scout definitions are centralized in `configs/worker/discovery/scouts.yaml`. The `config.Factory` builds the correct scout type from config. New sources should prefer the shared `HTMLScout`, `RSSScout`, or `AtomScout` implementations over new per-source packages.

### LLM Infrastructure

`internal/llm/` defines `Generator`, `Embedder`, and `Provider` interfaces with implementations for Gemini (`internal/llm/gemini/`), OpenAI (`internal/llm/openai/`), and Ollama (`internal/llm/ollama/`).

`llm.DecodeJsonSchema(schema, rawJSON, &out)` is the shared structured-output decoder used after all LLM calls that return JSON schema responses.

### Code Generation

- **SQLC**: edit `db/queries/*.sql` then run `task sqlc`. Generated code lands in `internal/repo/pg/`. Do not hand-edit files there.
- **Mocks**: edit interfaces in `internal/collector`, `internal/discovery`, `internal/llm`, or `internal/repo`, then run `task mocks`. Mocks land in the `mocks/` subdirectory of the interface's package. Config is in `.mockery.yaml`.

### Error Handling Convention

Each package defines one `ErrParamMissing` sentinel for constructor dependency checks:
```go
var ErrParamMissing = errors.New("param missing")

if logger == nil {
    return nil, fmt.Errorf("%w: logger", ErrParamMissing)
}
```

Full struct validation via `go-playground/validator` is reserved for one-shot config loads. Do not add validation tags to internal param structs until worker flows are stable.

### Tracing

`internal/infra/global.go` initializes a shared OTel tracer provider. Components obtain named tracers from it via `infra.Tracer()` (or by name). Do not create per-component global tracer variables; pass `trace.Tracer` as a constructor dependency.

### Key Design Rules

- `fingerprint` is a dedup key on `candidates`, not a table. Use `model.Candidates.Fingerprint()` to compute it.
- `batch_id` is a business batch identifier — do not conflate with trace IDs.
- `IngestionMethod` is `"DIRECTORY"` for both daily party scouting and backfill runs.
- `page_fetch` is only emitted for `PARTY` source type candidates, not `MEDIA`.
- Do not add `Marshal()`/`Unmarshal()` methods to structs unless special field handling is required.
- Prefer narrow concrete sinks over shared sink frameworks until repeated patterns are proven.
