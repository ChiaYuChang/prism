# Per-Source Pipeline Wiring

## Problem

Collector pipeline stages are currently wired as a fixed chain in `Dispatcher`,
with only the `Parser` being source-aware (per-host in `parsers.yaml`). Two
weaknesses follow:

1. **Minifier cannot know input type.** `Minifier.Minify(ctx, raw string)` takes
   a plain string. `HTMLMinifier` silently assumes HTML; feeding JSON/XML/PDF
   to goquery produces garbage without erroring.
2. **Pipeline shape is not a first-class concept.** A future JSON source has
   nowhere to plug in its own extraction logic without branching inside
   Dispatcher.

## Mental model: ETL

The collector pipeline maps cleanly onto Extract–Transform–Load:

| Stage           | ETL role                 | Interface           | Notes                              |
|-----------------|--------------------------|---------------------|------------------------------------|
| Fetch           | Extract                  | `Fetcher`           | One impl today (`HTTPFetcher`)     |
| Minify (slot)   | Transform — reduce noise | `Transformer`       | Output is the archive point        |
| Transform chain | Transform — reshape      | `[]Transformer`     | Type-specific; may be empty        |
| Parse           | Transform → Load-ready   | `Parser`            | Distinct — output is `*Article`    |
| Save            | Load                     | `Saver`             | Persists archive + DB record       |

Minifier, the transformer chain, and Parser all belong to the T band. Parser
is kept as its own interface because its output type leaves the "string → string"
monad — the semantic break is real, not cosmetic.

## Guiding principles

1. **A source's content type is fixed and known ahead of time.** `dpp.org.tw`
   always returns HTML; `partyX-api` would always return JSON. Runtime
   content-type sniffing is the wrong abstraction — the pipeline choice
   belongs to the source, decided before any document is fetched.
2. **Parser rules are data; stage wiring is logic.** Selectors and date
   layouts live in `parsers.yaml` and change without code review. Which
   transformer runs for which source is a logic decision — wire it in Go via
   a registry, not YAML.
3. **Minifier is a role, not a type.** Only one `Transformer` interface
   exists; the Pipeline struct names a distinguished slot ("Minifier") whose
   output is archived. Any `Transformer` implementation can fill the slot.

## Target shape

```go
// internal/collector/pipeline/pipeline.go (new)

type Transformer interface {
    Transform(ctx context.Context, in string) (string, error)
}

type Pipeline struct {
    Fetcher      collector.Fetcher
    Minifier     Transformer     // slot: output is archived
    Transformers []Transformer   // post-archive chain; may be empty
    Parser       collector.Parser
}

type Registry struct {
    bySource map[string]Pipeline
    fallback Pipeline
}

func (r *Registry) For(sourceID string) Pipeline { ... }
```

Wiring (Go, startup-time):

```go
reg := pipeline.NewRegistry(htmlFallback)
reg.Register("dpp",        pipeline.HTMLPipeline(parserRegistry))
reg.Register("partyX_api", pipeline.JSONPipeline(jqExpr))  // future
```

`parsers.yaml` is untouched — passed into `HTMLPipeline` as data.

Dispatcher becomes thin:

```go
func (d *Dispatcher) Dispatch(ctx context.Context, sig PageFetchSignal) error {
    p := d.registry.For(sig.SourceID)

    raw, _      := p.Fetcher.Fetch(ctx, sig.URL)
    minified, _ := p.Minifier.Transform(ctx, raw)
    d.saver.Save(ctx, Archive{Payload: minified, Kind: PayloadKindMinified, ...})

    doc := minified
    for _, t := range p.Transformers {
        doc, _ = t.Transform(ctx, doc)
    }
    article, _ := p.Parser.Parse(ctx, sig.URL, doc)
    // ... persist article ...
}
```

Error-path archive (store `raw` when Minifier fails) is preserved.

## Consequences

- **One transformer interface**, not two. `collector.Minifier` is deleted;
  `MockMinifier` goes with it. Less interface surface, same expressive power.
- **Archive point is explicit and deliberate.** Sits at Minifier-slot output:
  smaller than raw (HTML chrome stripped), preserves enough fidelity to
  re-run the transformer chain and parser on replay.
- **Passthrough is a first-class default.** Sources without meaningful
  minification use `NoopTransformer` in the Minifier slot — pipeline shape
  stays uniform, archive degrades to "raw-sized" without special casing.
- **Fetcher signature unchanged** (`(string, error)`). No ripple through
  mocks, archiver, or fetcher tests.
- **Adding a stage implementation is a Go change.** `tdewolff/minify`, `gojq`,
  PDF extractors — each is a new `Transformer` implementation plus one
  registry line. No config-schema churn.

## Deliberate limitations

- **Pipeline is strictly linear F → M → T[] → P, not a DAG.** Branching,
  re-ordering, or "parse twice" would drift toward n8n/Airflow. If a source
  needs it, special-case the Pipeline in Go rather than generalising.
- **No pipeline versioning in archives.** Replaying against a newer pipeline
  may produce a different `Article`. Accepted IaC trade-off; add
  `pipeline_version` metadata later if exact replay becomes a requirement.
- **Wiring scope is M/T/Parser only.** Fetcher stays single-impl today; the
  same registry pattern absorbs a second fetcher (headless browser, fixture
  replay) without schema change when needed.

## Migration path

1. **Stopgap.** Add a `Content-Type` assertion in `fetcher/http.go`: reject
   responses whose header does not start with `text/html`. Prevents silent
   corruption immediately; compatible with every subsequent step.
2. **Unify interface.** Absorb `collector.Minifier` into a new
   `collector.Transformer` (identical signature, different name).
   `HTMLMinifier` becomes `HTMLChromeStripper` implementing `Transformer`.
3. **Introduce `pipeline.Pipeline` + `pipeline.Registry`** under
   `internal/collector/pipeline/`. Register one entry: the current HTML
   pipeline as fallback. No behavioural change.
4. **Refactor Dispatcher** to take a `pipeline.Registry` instead of
   individual fields. `dispatcher_test.go` shifts from "minifier was called"
   to "registry.For(sourceID) was invoked and its Pipeline ran end-to-end".
5. **Second pipeline lands opportunistically.** When a real non-HTML source
   arrives, write `JSONPipeline` and register it. Only then pick a JSON
   library (gojq vs alternatives).

## Open questions (defer to step 5)

- **Unknown `source_id` behaviour.** Fallback to default HTML pipeline
  (forgiving) vs hard-fail (safer)? Fail-fast preferred, but decide when
  second pipeline lands and the rollout risk is concrete.
- **`source_id` propagation.** Verify `PageFetchSignal` carries it (see
  `internal/message/`); add to the signal if absent rather than re-deriving
  from URL host.
- **Transformer chain config.** If a source needs multiple post-archive
  transforms (e.g. `decode_entities` → `jq`), is the chain order specified
  in the Go factory or surfaced to `parsers.yaml`? Lean toward Go — chain
  order is logic, not data — but revisit if operators start asking.
