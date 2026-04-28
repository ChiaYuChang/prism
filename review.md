# Repository Review Plan

This checklist is for a repo-wide engineering review of Prism. The review should focus on logic design, extensibility, alignment with Go and industry norms, and security. The review order is intentionally dependency-aware so that lower-level shared assumptions are validated before higher-level orchestration code.

## Review Principles

- [x] Apply the Code Review Pyramid in this order of attention: API Semantics -> Implementation Semantics -> Documentation -> Tests -> Code Style.
- [x] Review findings should prioritize correctness, design risk, security risk, and behavioral regressions over style comments.
- [x] Each review pass should produce three outputs: findings, design notes, and action items.
- [x] `plan.md` should be treated as an architectural baseline, not as an unquestionable source of truth.
- [x] Any mismatch between `plan.md` and implementation should be classified as one of:
  - [x] implementation bug
  - [x] outdated documentation
  - [x] design drift that requires an explicit decision
- [x] Review comments should distinguish between immediate fixes, deferred improvements, and acceptable tradeoffs.

## Review Standards For Every Layer

### API Semantics

- [x] Verify each API is as small as possible and as large as necessary.
- [x] Verify there is one clear way to perform one task, rather than multiple overlapping paths.
- [x] Verify APIs follow the principle of least surprise in naming, defaults, and behavior.
- [x] Verify public APIs are cleanly separated from internals and do not leak internal implementation details.
- [x] Verify no unintended breaking changes were introduced in user-facing APIs, configuration, metrics, log formats, message schemas, or operational contracts.
- [x] Verify any new API is generally useful and not overly tailored to a single narrow call site.

### Implementation Semantics

- [x] Verify logic design: responsibility boundaries, state transitions, error paths, and invariants are explicit and defensible.
- [x] Verify extensibility: adding a new source, trigger, parser, backend, or workflow should not require invasive rewrites.
- [x] Verify Go norms: package boundaries are coherent, interfaces are not oversized, constructors validate required dependencies, `context.Context` is propagated correctly, and errors are wrapped with useful context.
- [x] Verify industry norms: abstractions are justified, configuration does not replace code design, tests exist at the right level, and naming matches intent.
- [x] Verify security: external input validation, secret handling, logging safety, archive and database integrity, idempotency, and failure recovery paths.
- [x] Verify observability: logs, traces, and metadata are sufficient to reconstruct failures and lifecycle transitions.
- [x] Verify implementations satisfy the original requirements rather than only matching local code structure.
- [x] Verify no unnecessary complexity or indirection was introduced.
- [x] Verify concurrency behavior, retries, and shutdown semantics are robust.
- [x] Review newly added dependencies for necessity, maintenance risk, and license acceptability.

### Documentation

- [ ] Verify new or changed behavior is documented at the right level: README, operator guide, code comments, config reference, or architecture notes.
- [ ] Verify user-facing and operator-facing docs reflect actual runtime behavior.
- [ ] Verify docs are understandable and free of significant grammar or terminology issues.

### Tests

- [x] Verify all relevant tests pass, or document precisely why they do not.
- [x] Verify new features are reasonably tested.
- [x] Verify corner cases and failure paths are tested.
- [x] Verify unit tests are preferred unless integration tests are necessary to validate real contracts.
- [x] Verify important non-functional requirements are covered where relevant, such as performance, retry behavior, timeout behavior, or resource usage.

### Code Style

- [x] Verify formatting, naming, duplication control, and readability follow project conventions.
- [x] Treat code style as lower priority than API, correctness, security, and test coverage.

## Phase 1: `pkg/*` and Shared Foundations

### `pkg/utils`

- [x] Check whether each helper belongs in a stable shared package rather than a catch-all utility bucket.
- [x] Identify helpers that should be moved into a dedicated package because they carry domain, infrastructure, or storage semantics.
- [x] Verify `pkg/utils` is not acting as a dumping ground for unrelated helpers.
- [x] Classify each file in `pkg/utils` as one of:
  - [x] keep in `pkg/utils`
  - [x] split into a dedicated package
  - [x] delete or avoid further reuse
- [x] Review `archive.go` for package placement and determine whether compression logic belongs in a dedicated archive codec package or near collector archiving code.
- [x] Review `secrets.go` and determine whether secret loading should live in a dedicated config or secret package.
- [x] Review `vectors.go` and determine whether pg/pgvector-specific conversion logic should live near persistence adapters instead of `pkg/utils`.
- [x] Review `strings.go` for mixed concerns such as normalization, encoding conversion, secret masking, and truncation.
- [x] Review `utils.go` and challenge helpers like `IfElse` and `DefaultIfZero` for readability, actual reuse, and necessity.
- [x] Verify helpers do not hide important semantics or silently normalize behavior that callers should handle explicitly.
- [x] Verify helper APIs are small, unsurprising, and testable.

### Other shared packages

- [x] Review `pkg/logger` for safe redaction, stable hook behavior, and predictable structured logging semantics.
- [x] Review `pkg/errorcode` for error composition clarity, interoperability with standard Go errors, and risk of accidental misuse.
- [x] Review `pkg/schema` for API clarity, versioning assumptions, and schema contract stability.
- [x] Review `pkg/pgconv` for correct nullability handling and safe adapter semantics.
- [x] Review `pkg/rss`, `pkg/functional`, and `pkg/testutils` for package scope discipline and whether any code should be relocated or narrowed.

## Phase 2: `internal/batch` (formerly `internal/trigger/batch`)

- [x] Verify trigger packages only detect or emit lifecycle signals and do not absorb unrelated business logic.
- [x] Review batch completion logic for correctness, race conditions, duplicate triggering, and re-entrancy.
- [x] Verify trigger state transitions are explicit and traceable.
- [x] Verify trigger code exposes enough context for debugging and auditing.
- [x] Check whether the trigger design can support additional trigger classes without rewriting current flow assumptions.
- [x] Review command-layer trigger wiring under `cmd/batch/*` after the core trigger review is complete.

## Phase 3: `internal/discovery`

- [x] Review package boundaries among planner, scout, sink, search client, extractor, config, and backfiller.
- [x] Verify the discovery layer still reflects a recall-first design rather than drifting into mixed responsibilities.
- [x] Verify task semantics for `DIRECTORY_FETCH` and `KEYWORD_SEARCH` are explicit and consistent with `plan.md`.
- [x] Review planner logic for bounded output, prompt coupling, downstream task safety, and failure isolation.
- [x] Review scout registry and scout implementations for extensibility when onboarding new sources.
- [x] Verify config-driven scout design reduces coupling instead of merely moving complexity into YAML.
- [x] Review candidate sink behavior for deduplication, metadata merging, persistence invariants, and operational safety.
- [x] Review search client integrations for request safety, timeout behavior, authentication handling, and result normalization.
- [x] Review backfiller logic for replay safety, duplicate creation risk, and bounded execution.
- [x] Verify discovery code distinguishes domain errors, transient integration failures, and malformed input.
- [x] Verify discovery tests cover behavioral contracts, not only happy paths.

## Phase 4: `internal/collector`

- [x] Review the collector pipeline against its intended invariant: Fetch -> Minify -> Transform -> Save/Parse.
- [x] Verify minify, transform, save, parse, archive, and recover responsibilities are separated cleanly.
- [x] Review error handling for fetch, minify, transform, parse, archive save, archive load, and database persistence.
- [x] Verify recovery behavior is idempotent and does not silently create duplicates or corrupt metadata.
- [x] Verify archive metadata is sufficient for replay, debugging, and provenance tracking.
- [x] Review parser registry and parser composition for extensibility and host-specific behavior safety.
- [x] Review fetchers for timeout handling, retry policy, HTTP safety, and cancellation behavior.
- [x] Review S3/local archiver behavior for data integrity, deletion semantics, and scan correctness.
- [x] Verify collector tests protect pipeline invariants and failure-path behavior.

## Phase 5: `cmd/*`, integration wiring, and operational composition

- [ ] Review each `cmd/*` package as a composition layer rather than a domain layer.
- [ ] Verify constructors, config loading, dependency wiring, and startup validation are consistent across binaries.
- [ ] Verify command flags and config semantics are predictable and aligned with actual runtime behavior.
- [ ] Verify command-line, config, and message-surface changes do not introduce accidental breaking changes for operators.
- [ ] Verify command packages do not reimplement logic that belongs in `internal/*`.
- [ ] Review scheduler, workers, recover, backfiller, and RSS entrypoints for consistency of observability and shutdown behavior.
- [ ] Verify runtime assembly preserves trace propagation, error context, and logging discipline.

## Phase 6: `internal/repo`, `internal/obs`, `internal/infra`, and cross-cutting concerns

- [ ] Review repository interfaces for coherence, size, and separation between orchestration and persistence concerns.
- [ ] Review PostgreSQL adapters and generated access layers for null handling, model translation, and hidden assumptions.
- [ ] Verify task claim, completion, release, and failure semantics match scheduler and worker expectations.
- [ ] Review observability code for trace propagation, logger enrichment, and health reporting correctness.
- [ ] Review infra abstractions for messenger, rate limiting, and global wiring to ensure they are justified and testable.
- [ ] Verify cross-package contracts do not rely on undocumented side effects or fragile call ordering.

## Final Architecture Reconciliation

- [ ] Re-read `plan.md` after code review findings are collected.
- [ ] List where implementation matches the intended architecture.
- [ ] List where implementation has drifted from the intended architecture.
- [ ] Decide whether each drift should result in a code change or a `plan.md` update.
- [ ] Identify unresolved architectural risks that should be tracked separately from immediate code fixes.

---

## Review Pass 1-4 Summary

### Findings
1.  **pkg/errorcode**: `Error` struct implements `error` but lacks `Unwrap() []error`, limiting interoperability with `errors.Join`.
2.  **internal/batch**: Refactored from `internal/trigger/batch`. Split into `Detector` and `Publisher` to support Lambda-ready architecture. Added bulk SQL detection to minimize DB load.
3.  **internal/discovery**: Fixed a resource leak in `Backfiller.Run` where `defer cancel()` was called inside a loop.
4.  **internal/repo/pg**: Centralized all hardcoded SQL into SQLC templates. Added `FindNewlyCompletedBatches` for optimized detection.
5.  **internal/collector**: `Dispatcher` and `Handler` have duplicate logic. `Handler` re-implements the F-M-T-P pipeline instead of using `Dispatcher`.
6.  **internal/collector/fetcher**: `RetryAfterHandler` uses `time.Sleep`, blocking the goroutine and ignoring context cancellation.
7.  **Collector Archive Gap**: Minified content is only archived if the entire pipeline (including DB persistence) succeeds. If `Parse` fails, the "archive point" content is lost, preventing parser replay.

### Design Notes
*   **Lambda-Ready Evolution**: The move from long-running Trigger Workers to discrete `Detector`/`Publisher` components allows for seamless transition to AWS Lambda/EventBridge.
*   **Bulk Detection Pattern**: Using optimized SQL to detect batch completion is significantly more efficient than N+1 Go-level checks.
*   **Scout Registry**: The hostname-based routing in `Scout Registry` provides a clean and extensible plugin architecture for new sources.
*   **Archive Point Stability**: The `Minifier` output is the intended stable replay point. The architecture should guarantee this is saved even if downstream components (`Parser`, `DB`) fail.

### Action Items
*   [ ] Implement `Unwrap() []error` in `pkg/errorcode/error.go`.
*   [ ] Update `pkg/logger/logger.go` comments to remove outdated `utils.SecretMask` references.
*   [ ] Consider moving `pkg/rss/yahoo.go` to `internal/discovery/scout/custom/yahoo` (Logic already partially moved during Phase 3).
*   [ ] Refactor `internal/collector.Dispatcher` to handle both `errorSaver` and `successSaver` to guarantee archiving at the stable "archive point".
*   [ ] Replace `cmd/worker/collector/handler.go` custom pipeline with a call to `internal/collector.Dispatcher`.
*   [ ] Fix `fetcher.RetryAfterHandler` to honor `context.Context` during backoff.
