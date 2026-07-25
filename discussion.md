# Architectural Discussion & Design Decisions

This document summarizes recent design discussions, architectural decisions, and refactoring plans for the **Prism** ingestion pipeline.

---

## 1. Messenger Configuration Deduplication (`internal/appconfig/messenger.go`)

### Problem
Every worker (`collector`, `discovery`, `planner`, `archive`, `embedder`) duplicated ~40 lines of CLI flag registration (`--messenger-type`, `--nats-host`, etc.) and `switch messengerType` unmarshalling logic.

### Decision & Solution
Centralized flag registration and polymorphic config parsing into `internal/appconfig/messenger.go`:
- `appconfig.RegisterMessengerFlags(fs *pflag.FlagSet, defaultQueueGroup string)`
- `appconfig.LoadMessengerConfig(v *viper.Viper) (MessengerConfig, error)`

Each worker `config.go` now requires only 2 lines of code to bind and load its messenger backend (NATS or GoChannel).

---

## 2. Task State Validation Control Flow (`if !running`)

### Discussion
In worker message handlers, `IsTaskRunning(ctx, taskID)` checks if the DB status is `RUNNING`. When `!running` is true:
```go
if !running {
    h.Metrics.recordTask(ctx, sig, "ignored", started)
    return true, nil
}
```

### Rationale
- **ACK (`return true, nil`)**: In Watermill / NATS, returning `true` acknowledges the message, instructing the broker to safely remove it from the queue.
- **Why not NACK (`false`)**: If the task was already completed by another worker, cancelled by an operator, or expired, NACKing would cause the broker to retry redelivering a dead task indefinitely. Returning `(true, nil)` gracefully drops the stale task message.

---

## 3. Model Registry Interface Refactoring (`repo.Models`)

### Problem
Previously, model registry lookups (`GetModelByNameAndType`) were exposed under `repo.Embeddings`, causing non-embedding workers (like `planner`) to call `dbRepo.Embedding().GetModelByNameAndType(ctx, name, "EXTRACTOR")`, leaking domain concerns.

### Decision & Solution
Created a dedicated `repo.Models` sub-repository on `repo.Repository` with type-bound lookup methods:

```go
type Models interface {
    GetModelByID(ctx context.Context, id int16) (Model, error)
    GetModelByNameAndType(ctx context.Context, name string, modelType string) (Model, error)
    
    GetEmbedderByName(ctx context.Context, name string) (Model, error)  // Hardcodes Type = 'EMBEDDER'
    GetExtractorByName(ctx context.Context, name string) (Model, error) // Hardcodes Type = 'EXTRACTOR'
    GetAnalyzerByName(ctx context.Context, name string) (Model, error)  // Hardcodes Type = 'ANALYZER'
}
```

### Usage
- `cmd/worker/embedder`: `dbRepo.Models().GetEmbedderByName(ctx, config.Embedder.Model)`
- `cmd/worker/planner`: `dbRepo.Models().GetExtractorByName(ctx, config.LLM.Model)`

---

## 4. Pipeline Stage Alignment & `cmd/worker/` Directory Hierarchy

### Pipeline Stage Model
The Prism processing pipeline consists of 3 distinct, sequential domain stages:

```
[ Stage 1: Discovery ]  ──► Persisted Candidates ──► 
[ Stage 2: Collection ] ──► Persisted Contents   ──► 
[ Stage 3: Analysis ]   ──► Embeddings / Stance Scores / Topics
```

| Stage | Domain Package | Worker Responsibilities |
|---|---|---|
| **Stage 1: Discovery** | `internal/discovery` | Detect articles to fetch: Scout (HTML/RSS Scraping), Query Extractor, Search Planner, Search API clients |
| **Stage 2: Collection** | `internal/collector` | Fetch & process full articles: Fetcher $\rightarrow$ Minifier $\rightarrow$ Transformer $\rightarrow$ Parser pipeline, S3 HTML Archiver |
| **Stage 3: Analysis** | `internal/analyzer` | Analyze & extract insights: Vector Embedder, Stance Scorer, Topic Extractor |

### Proposed `cmd/worker/` Reorganization
To establish 1-to-1 symmetry between `cmd/worker/` and `internal/`, worker binaries are organized under their domain stage folders:

```
cmd/worker/
├── discovery/             # Stage 1: Discovery Domain
│   ├── worker/            # Scout (HTML/RSS) & Search API candidate execution
│   └── planner/           # Phrase Extraction & Keyword Search Query Planner
│
├── collector/             # Stage 2: Collection Domain
│   ├── worker/            # Main Fetch -> Minify -> Transform -> Parse pipeline
│   └── archive/           # Raw Article S3 Payload Archiver (uses collector.Saver)
│
└── analyzer/              # Stage 3: Analysis Domain
    ├── embedder/          # Candidate & Content Vector Embedding worker
    └── worker/            # Stance Scorer & Topic Extraction worker
```

---

## 5. Struct Layout for `cmd/worker/discovery`

### `Config` (`cmd/worker/discovery/config.go`)
Organized by functional stage:

```go
type ScoutSettings struct {
    ConfigPath  string        `mapstructure:"config-path"  validate:"required"`
    HTTPTimeout time.Duration `mapstructure:"http-timeout" validate:"required,min=1s"`
}

type SinkSettings struct {
    CaptureDir  string `mapstructure:"capture-dir"`
    FixtureBase string `mapstructure:"fixture-base"`
}

type Config struct {
    // 1. General / Infra
    HealthPort      int                       `mapstructure:"health-port"        validate:"required,min=1024,max=65535"`
    ShutdownTimeout time.Duration             `mapstructure:"shutdown-timeout"   validate:"required,min=1s"`
    RetryMax        int                       `mapstructure:"retry-max"          validate:"required,min=1"`
    Logger          obs.LoggingConfig         `mapstructure:"logger"`
    Telemetry       obs.TelemetryConfig       `mapstructure:"telemetry"`
    Postgres        appconfig.PostgresConfig  `mapstructure:"postgres"`
    MessengerType   string                    `mapstructure:"messenger-type"     validate:"oneof=nats gochannel"`
    Messenger       appconfig.MessengerConfig `mapstructure:"-"`

    // 2. Stage Components
    Scout  ScoutSettings       `mapstructure:"scout"`
    Search searchconfig.Config `mapstructure:"search"`

    // 3. Sink / Fixtures
    Sink SinkSettings `mapstructure:"sink"`
}
```

### `HandlerConfig` (`cmd/worker/discovery/handler.go`)

```go
type Store struct {
    Scout        repo.Scout
    Tasks        repo.Tasks
    TaskReporter repo.TaskReporter
}

type HandlerConfig struct {
    Logger   *slog.Logger
    Tracer   trace.Tracer
    Meter    otelmetric.Meter
    Store    Store
    Scout    discovery.Scout
    Search   map[string]discovery.SearchClient
    Sink     discoverysink.CandidateSink
    RetryMax int
}
```
