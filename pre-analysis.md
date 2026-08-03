# Pre-Analysis Fetch And Confirmation Plan

## Historical Implementation Audit (2026-08-04)

`[x]` means the current uncommitted server implementation provides the stated
behavior. `[ ]` means it is incomplete or unsafe for merge, even if a handler,
schema, or test already exists. Client implementation belongs to another
repository and is out of scope here.

`rtk go test -short ./...` passes (895 tests in 106 packages), and focused lint
is clean. This does not establish the missing transactional and end-to-end
guarantees below.

## Current Implementation Status (2026-08-05)

The direct report-delivery implementation described in the later sections now
exists in the working tree. It includes:

- `000008_report_delivery` and `000009_pipeline_snapshot_columns` migrations,
  SQLC queries/models, repository contracts, and regenerated mocks.
- Local/S3 `PutIfAbsent` and bounded `Stat` storage operations.
- A deterministic `GENERATE_REPORT` pipeline stage writing
  `reports/<execution-id>.md` with a size limit and SHA-256 metadata.
- Analysis metadata/report routes, admin report removal, configuration, and
  Compose report mounts.
- Focused report, API, storage, and PostgreSQL integration tests.

Verification is current as of this audit: `go test ./...` passes with 904 tests
in 106 packages, `task test:integration` passes, `golangci-lint run ./...`
passes, and the local Compose auth, source, and JetStream bootstrap paths have
completed successfully. The implementation is not feature-complete: report
recovery, rerun behavior, full report-removal crash/race coverage, analysis
request idempotency, and the production LLM-backed renderer remain open.

## Historical Code Review Findings (2026-08-05)

The findings below are a pre-implementation review snapshot. They are retained
as historical implementation findings, not unresolved product choices. The
current status section above supersedes statements that report schema, storage,
pipeline, routes, or generated code are absent.

1. **[Blocker] The public API and idempotency contract are not implemented.**
   `internal/http/api/api.go:304-312` registers
   `POST /analysis/preflight` and `/analysis-runs`, not
   `POST /analyses/preflight` and `/analyses`; there is no
   `GET /analyses/{id}` handler. `CreateAnalysisRun` generates a fresh UUIDv7
   at `internal/http/api/analysis_runs.go:123-126`, returns
   `analysis_run_id`, and has no request fingerprint or replay lookup. The
   migration also has no `request_fingerprint` column. A lost `202` response
   therefore creates another fetch/run on retry, contrary to the idempotency,
   resource-rename, and analysis-read decisions later in this document.

2. **[Blocker] Availability and validation behavior contradict the documented
   contract.** `CreateAnalysisRun` returns `409` with a preflight-shaped body
   as soon as any candidate is unavailable (`analysis_runs.go:99-109`), so it
   never persists the original selection plus unavailable provenance. An
   all-unavailable selection is not returned as `422 ANALYSIS_INPUT_EMPTY`.
   `validateAnalysisIDs` (`analysis_runs.go:424-436`) does not reject duplicate
   IDs, does not provide the documented stable validation codes, and lets a
   duplicate reach the `fetch_items` primary-key conflict after earlier rows
   have already been created. This contradicts the unavailable-ID, API error,
   and validation decisions later in this document.

3. **[Blocker] Analysis creation is not one transaction.** The handler creates
   `fetches`, then `analysis_runs`, then creates each item/task in separate
   repository calls (`analysis_runs.go:117-173`). `analysisRunError` only marks
   the already-created run as failed (`analysis_runs.go:467-477`); it cannot
   roll back the fetch, prior items, or newly-created shared tasks. A failure
   on a later candidate leaves partial durable acquisition state, violating
   the rollback guarantees later in this document.

4. **[Critical] Fetch and analysis ownership is not enforced, and the cache
   bypasses even the fetch existence check.** `GetFetch` reads the progress
   cache before loading the fetch (`fetches.go:63-72`), while the cache key is
   only `fetch_id` (`cache_valkey.go:41-46`). Any authenticated user who guesses
   another user's UUID can receive cached candidate IDs. On a cache miss,
   `GetUserFetch` still queries by ID only (`db/queries/user_fetches.sql:6-10`)
   and the handler never compares `user_id`. The analysis queries and
   `loadAnalysisRun` likewise have no principal predicate
   (`db/queries/analysis_runs.sql:25-29`, `analysis_runs.go:449-464`). This
   contradicts the ownership requirements later in this document; the
   nullable ownership columns in
   `000007_analysis_runs.up.sql:3-24` and the existing nullable `fetches.user_id`
   also do not enforce the clean-database constraint.

5. **[Critical] Terminal reconciliation remains a GET side effect.** The only
   call to `confirmAnalysisRun` outside the resolution endpoint is from
   `GET /fetches/{id}` (`fetches.go:89-99`). There is no
   `cmd/trigger/analysis` or repository claim/reconciliation operation in the
   tree. If a client stops polling, a terminal fetch remains `FETCHING` and no
   manifest or Root is created. A cached terminal fetch can also suppress the
   only reconciliation attempt for the cache TTL. This contradicts the
   server-owned reconciliation decisions later in this document.

6. **[High] Fetch progress violates the response boundary and one-status-per-
   item invariant.** `FetchProgressResponse` still exposes `analysis_run_id`
   and `analysis_status` (`fetches.go:18-31`). The handler puts completed and
   already-complete IDs into `ready`, while also returning them in
   `completed`/`already_complete` (`fetches.go:106-124`), so one candidate can
   appear in multiple groups. The SQL aggregation also leaves an item with a
   missing task in no group and keeps `terminal=false`
   (`db/queries/user_fetches.sql:43-81`). This contradicts the fetch boundary
   and grouped-status decisions later in this document.

7. **[High] Failure resolution is race-prone, permits cancellation, and uses
   unstable failure semantics.** `ResolveFetchFailures` accepts `CANCEL`
   (`analysis_runs.go:228-243`) and `CancelAnalysisRun` exposes a cancel route
   for fetching runs (`analysis_runs.go:248-272`), despite the explicit no-user-
   cancel decision. `SetAnalysisRunStatus` has no conditional expected-state
   predicate (`db/queries/analysis_runs.sql:58-65`), so concurrent or stale
   resolution requests are not converted into the documented `409`. `RETRY_FAILED`
   revives the old task in place (`analysis_runs.go:200-218`) instead of
   creating/reusing fresh work, and `STOP` records `FETCH_FAILED` rather than
   the documented `ANALYSIS_FETCH_FAILED` (`analysis_runs.go:300-304`).

8. **[High] PAGE_FETCH deadlines, expiry sweeping, and late-completion guards
   are absent.** The analysis/page-fetch handlers do not set `ExpiresAt`
   (`page_fetch.go:154-162`), `CompleteTask` only checks `status='RUNNING'`
   and has no expiry predicate (`db/queries/tasks.sql:172-190`), and the
   scheduler merely filters expired tasks without marking them failed
   (`db/queries/tasks.sql:121-170`). There is no global sweeper or
   `TASK_EXPIRED` path, and `RetryFailedTask` reuses the old task/deadline
   (`db/queries/tasks.sql:216-236`). A stalled PAGE_FETCH can therefore leave
   an analysis permanently non-terminal, and a late worker can complete it
   after the intended deadline. This contradicts the deadline decisions later
   in this document.

9. **[High] User-selected fetch work can be rejected solely because the
   candidate's discovery batch is complete.** `recordPageFetchItem` uses the
   candidate's `BatchID` for a new shared PAGE_FETCH task
   (`page_fetch.go:154-162`). `createTaskRepo` locks that batch and rejects it
   when `completed_at` is set before it attempts active-task recovery
   (`internal/repo/pg/repo.go:925-946`). Candidates are normally selected
   after discovery batches finish, so both new acquisition and reuse of an
   active shared URL task can fail before the URL-level deduplication is even
   checked. This violates the shared-work scenarios later in this document.

10. **[High] Soft-deleted content is treated as successfully fetched by the
   collector.** `GetContentByCandidateID` and `GetContentByURL` do not filter
   `deleted_at` (`db/queries/contents.sql:7-17`). The collector's fast paths
   accept any returned row without checking `DeletedAt`
   (`cmd/worker/collector/worker/handler.go:253-283`), then completes the task
   without creating readable content. The analysis query filters that row out,
   producing a completed acquisition item with no READY content and no retry.
   This conflicts with the readable-content precedence and terminal-state
   rules later in this document.

11. **[High] Canonical URL identity is inconsistent across producers.**
   `normalizeCandidateURL` only trims whitespace, lowercases scheme/host, and
   validates HTTP(S) (`analysis_runs.go:439-447`); it does not implement the
   documented default-port removal, fragment removal, or user-info rejection.
   The discovery sink writes the raw candidate URL into PAGE_FETCH tasks
   (`internal/discovery/sink/candidate.go:207-220`), while the user handler
   writes the partially-normalized URL. Equivalent URLs can therefore create
   separate active tasks, and the content URL lookup can miss an existing row.
   There is also no persisted `candidates.canonical_url` or current-candidate
   uniqueness constraint, contrary to the candidate-identity decisions later
   in this document.

12. **[Blocker] Manifest confirmation, input validation, and Root/P0 creation
   are separate and non-exact.** `confirmAnalysisRun` writes the manifest,
   then `startAnalysisRoot` creates the Root, then `SetRoot` updates the run
   (`analysis_runs.go:275-393`). The database operation that snapshots selected
   candidates/contents uses `INSERT ... SELECT` without asserting that every
   requested ID was inserted (`db/queries/batches.sql:141-177`), and the
   handler reloads candidates but never verifies the exact readable candidate/
   content pairs before Root creation. A Root can therefore exist without a
   linked `ANALYZING` run, or a manifest can be confirmed without a Root/P0;
   missing inputs can be silently omitted. `SetAnalysisRunManifest` also
   persists the unreachable `READY_TO_ANALYZE` state
   (`db/queries/analysis_runs.sql:45-56`). This violates the manifest and
   atomic-confirmation decisions later in this document.

13. **[Blocker] Root identity and completion are still coupled to the first
   candidate and are not connected to analysis lifecycle.**
   `startAnalysisRoot` chooses `candidates[0].BatchID` as `ParentBatchID` and
   derives the Root from the request run ID (`analysis_runs.go:346-365`), so a
   mixed-batch selection depends on ordering and uses collection-batch
   idempotency semantics. `FindFinishedPipelineRootBatches` still requires
   `parent_id IS NOT NULL` (`db/queries/batches.sql:215-229`), and the finisher
   only marks the batch (`internal/batch/finisher.go:86-103`); it never updates
   the linked analysis to `COMPLETED` or `FAILED`. Parentless analysis Roots
   can be missed entirely and all successfully finished Roots can leave the
   analysis stuck in `ANALYZING`, contrary to the Root identity and completion
   decisions later in this document.

14. **[High] The ownerless execution model is absent.** There is only the
    request-owned `analysis_runs` table (`000007_analysis_runs.up.sql:3-24`), no
    `analysis_executions`, no report fingerprint, no Root-to-execution bridge,
    no success-only reports cache, and no atomic N:1 attachment. Equivalent
    requests cannot share an active Root, and the current request-owned Root ID
    cannot provide the required concurrent attachment and retry-history semantics
    described later in this document.

15. **[High] Test coverage does not exercise the claimed resolved behavior.**
    `internal/http/api/analysis_runs_test.go` contains only two tests, covering
    basic preflight classification and already-present content
    (`analysis_runs_test.go:18-76`). There are no API or Postgres integration
   tests for ownership, replay, rollback, terminal reconciliation, expiry,
   resolution races, exact input revalidation, Root completion, or the
    documented response contract. The passing 895-test suite therefore does not
    validate the later `[x]` acceptance claims.

## Historical Follow-up Findings For Execution And Reports (2026-08-05)

The revised execution-bridge/report design introduces the following additional
issues. The fifteen findings above remain open against the current codebase.

16. **[Blocker] The existing Root repository path cannot safely create a
    parentless, idempotent analysis Root.** The new contract requires
    `parent_batch_id = NULL` and an execution-scoped Root, but
    `PGPipelineRuntime.CreatePipelineRoot` dereferences `arg.ParentBatchID`
    whenever `PipelineIdempotencyKey` is non-nil
    (`internal/repo/pg/repo.go:295-299`), and the concurrent recovery path does
    the same (`repo.go:359-362`). Passing an idempotency key for the required
    parentless Root can panic; omitting it avoids the panic but removes the
    lookup/recovery boundary. A dedicated analysis transaction or a nil-safe
    execution-bridge path is required before the resolved Root design is
    implementable.

17. **[Blocker] A successful Root has no report producer in the current
    pipeline.** `configs/llm_pipeline.yaml` contains only
    `EMBED_CANDIDATE` and `EMBED_CONTENT`, while report generation and
    `report.md` persistence remain unchecked in
    `deployments/docs/plan/todo.md:64-74`. The revised contract nevertheless
    says every successful Root creates a success-only `reports` cache entry
    (`pre-analysis.md` report/Root decisions) and exposes a report reference.
    There is no `reports` migration, report repository, report generation stage,
    artifact key contract, or API read path. Root success must not be treated
    as report success until the report producer and its failure semantics are
    defined.

18. **[High] The report row and `report.md` artifact cannot be made atomic by
    the stated PostgreSQL transaction alone.** `internal/storage.Store` exposes
    independent `Put`/`Get` operations and has no transaction or commit
    protocol (`internal/storage/store.go:27-36`). If the artifact is uploaded
    before PostgreSQL terminalization, a DB rollback leaves an orphan; if the
    report row commits first, a storage failure leaves a cache entry pointing
    at a missing artifact. The requirement to create the report row in the same
    Root terminalization transaction needs a deterministic artifact protocol and
    recovery tests for both failure orders. The later “Report Artifact Delivery
    Contract” supersedes the earlier outbox/PENDING proposal.

19. **[High] Rerun eligibility has no durable source of truth or API contract.**
    The execution failure taxonomy requires a stable internal `failure_kind`
    for rerun eligibility, but the revised execution data contract stores only
    the report fingerprint, input/pipeline identity, and timestamps; neither
    the current schema nor the planned `batches.analysis_execution_id` change
    stores `failure_kind`. The taxonomy also explicitly leaves the retry policy
    unresolved, while the final failed-Root decision already assumes an
    explicit rerun operation. No rerun endpoint, authorization rule, cooldown,
    request transition, or persistence location for the classification exists.
    After restart, an operator cannot safely determine whether a failed Root
     may create the next Root without guessing from task failure text.

## Superseded Review Questions (2026-08-05)

Report-delivery questions below describe the outbox/PENDING-report design and
must not drive implementation. “Report Artifact Delivery Contract” is the
authoritative replacement for those report-delivery questions; non-report items
remain review context until their explicit resolved decisions are added.

1. **Who owns the Root terminalization boundary when the report outbox is
   asynchronous?** Decision 4 creates a PENDING report and outbox row, while
   decisions 5 and 18 say the report task is the final Root stage and the Root
   remains running until the report is READY. Does the report outbox worker own
   the final task's `CompletePageFetch`-style terminal update, or does the
   pipeline worker complete the task before the outbox upload? What exact row
   locks prevent `FindFinishedPipelineRootBatches` from marking the Root before
   the report is READY?

2. **What is the report table state model?** The design calls `reports` a
   success-only cache but also requires a PENDING row before artifact delivery.
   Should `reports` have `PENDING`, `READY`, and terminal failure states, or
   should PENDING delivery live in a separate report-attempt table? Which unique
   constraints apply while a report is PENDING, and how is a stale PENDING row
   recovered after an outbox worker crash?

3. **How does `ANALYSIS_REPORT_FAILED` fit the public failure taxonomy?** The
   persisted failure-code list currently ends at `ANALYSIS_PIPELINE_FAILED`, but
   the resolved report decisions introduce `ANALYSIS_REPORT_FAILED`. Is report
   delivery failure a public distinct code, an internal `failure_kind` under
   `ANALYSIS_PIPELINE_FAILED`, or both? Which code does `GET /analyses/{id}` and
   the rerun endpoint return after report retries are exhausted?

4. **What is the exact rerun request contract?** The answer names
   `POST /analyses/{id}/rerun`, but does not define its body, response status,
   idempotency identity, or behavior when two rerun requests arrive during the
   one-minute cooldown. Should repeated requests return the same active Root,
   `409`, or a distinct rerun operation ID? How is a rerun distinguished from a
   create replay in `analysis_resolution_events`?

5. **Which timestamp and clock enforce the rerun cooldown?** The answer says
   one minute from the failed Root terminal timestamp. Is that PostgreSQL
   `completed_at`, a separate `failed_at`, or the timestamp at which
   `failure_kind` is recorded? Must the comparison use `clock_timestamp()` and
   be performed under the execution/request row lock to prevent two owners from
   passing the cooldown check concurrently?

6. **How is `failure_kind` classified and made stable?** The taxonomy lists
   provider, infrastructure, configuration, model-output, snapshot, policy,
   and unknown failures, but no mapping exists from worker/task errors to those
   kinds. Which component classifies the error, what happens when multiple
   tasks fail with different kinds, and can an operator override the stored
   classification without mutating an immutable Root history row?

7. **What happens to the execution fingerprint when a rerun changes deployed
   configuration?** `report_fingerprint` includes every output-affecting setting,
   while eligible rerun creates the next Root under the same immutable execution
   bridge. Must a rerun freeze the original pipeline/prompt/model versions, or
   should changed settings create a new execution bridge and report fingerprint?
   How is that choice represented in the rerun request and audit event?

8. **Does `report_fingerprint` need candidate snapshot identity, not only
   content IDs?** The analysis Root currently snapshots both candidates and
   contents and the deployed pipeline includes `EMBED_CANDIDATE`. If candidate
   title/metadata or candidate snapshot content can affect report output, two
   requests with the same `ready_content_ids` can still produce different
   results. Should the fingerprint include canonical candidate IDs or a hash of
   all output-affecting candidate snapshot fields?

9. **How are cross-table execution associations enforced by PostgreSQL?** The
   planned schema separately adds `analysis_runs.execution_id`,
   `observed_root_batch_id`, `report_id`, `batches.analysis_execution_id`, and
   report foreign keys. What prevents a request from referencing a Root or
   report belonging to a different execution bridge? Are composite foreign keys
   or deferred constraints required, or is this invariant intentionally
   repository-only? Add the negative integration test for a mismatched link.

10. **How does PAGE_FETCH append work to a completed discovery batch without
    violating `n_subtasks`?** Decision 8 allows a new task on a completed batch,
    but the existing repository also rejects inserts when the task count reaches
    the batch's declared `n_subtasks`. Should user PAGE_FETCH tasks bypass that
    count, should `n_subtasks` be immutable discovery-only metadata, or should a
    separate task-batch association be introduced? The answer must preserve
    discovery completion and scheduler claim semantics.

11. **At what point is soft-deleted content classified as unavailable?**
    Decision 9 says soft-deleted content is unavailable before Root snapshot,
    while the preflight availability rule checks candidate existence and URL
    normalization. Should preflight inspect readable content and return such a
    candidate as `UNAVAILABLE`, or may preflight say available and let creation
    fail through fetch policy? Which error/provenance field records this reason?

12. **What is the migration sequence for canonical URLs in a clean database?**
    Decision 10 says no legacy-row migration, but the current initial migration
    still creates raw `candidates.url` and `contents.url` uniqueness. Which
    migration adds `canonical_url`, changes task/content uniqueness, and updates
    discovery/upsert before any later migration can insert data? Must
    `schema.sql`, SQLC output, and fresh-database migration tests be updated in
    the same change?

13. **What is the exact `CompletePageFetch` contract for URL-only or malformed
    task metadata?** The new method receives a task and writes content in one
    transaction, but existing PAGE_FETCH rows encode `candidate_id` in JSON
    metadata and some task paths may not have it. Does the method require a
    candidate ID, resolve by canonical URL, or reject the task? How does it
    preserve the candidate/content one-to-one invariant when a URL already has
    readable content for a different candidate?

14. **How are automatic resolution events attributed?**
    `analysis_resolution_events` requires an actor token ID, but reconciliation,
    expiry sweeping, and report outbox delivery are system actions without a
    user token. Should actor identity support a typed system actor, nullable
    actor, and component name? Which automatic actions are recorded as events,
    and which are only task history?

15. **What is the report retrieval endpoint despite “no direct report-ID
    endpoint”?** Decision 17 says artifact retrieval authorizes the owning
    request's `report_id`, but the API checklist only defines `GET
    /analyses/{id}` and no report-content route. Is report content embedded in
    the analysis response, streamed through `GET /analyses/{id}/report`, or
    obtained through an internal signed storage reference? Define response
    content type, range/size limits, and authorization checks.

16. **How does a failed request adopt a successful report without losing Root
    history?** The state matrix permits `FAILED -> COMPLETED` through explicit
    cache adoption, while requests retain their observed Root/report locally.
    Does adoption update `observed_root_batch_id` and `report_id` to the
    successful Root, or retain the failed Root and add an adopted-report link?
    What audit event and API fields distinguish rerun adoption from a Root that
    originally completed for the request?

17. **What happens when the report fingerprint is already READY while the
    request is still acquiring?** The cache lookup occurs after manifest
    confirmation in the flow, but an equivalent successful report may already
    exist before the new request's fetch items finish. May the request attach to
    the READY report early, or must it always finish and persist its own READY
    manifest first? If early attachment is allowed, which selected/unavailable
    provenance is retained and what prevents a report cache hit from bypassing
    input validation?

18. **Which constraints make report-cache races idempotent?** When two outbox
    workers upload the same content-addressed report concurrently, which row
    wins, and how are the losing artifact and outbox attempt cleaned up? When a
    report row is PENDING and an explicit rerun arrives, does rerun attach to
    that delivery, create another Root, or wait? Add the exact unique indexes and
    concurrent integration tests.

19. **What is the boundary between Root failure and report-delivery failure in
    the finisher?** The Root finisher is currently the sole authority for Root
    terminalization, but the report outbox worker may discover the final failure
    later. Does the finisher mark the Root terminal only after report READY, or
    can the outbox worker perform a second conditional Root terminalization?
    Which transaction updates `analysis_runs`, `reports`, outbox state, and
    `failure_kind` together?

20. **How are the new outbox, report, rerun, and bridge guarantees tested in a
    clean database?** Please name the required test cases for: report render
    failure, upload failure, outbox retry after crash, duplicate report upload,
    rerun cooldown race, changed pipeline fingerprint, mismatched execution
    foreign keys, completed-batch PAGE_FETCH insertion, and cross-owner report
    access. Are these mandatory integration tests before any related `[x]` is
    retained?

**Naming:** the external API resource is an **analysis**: use `/analyses` and
`analysis_id`. `analysis_runs` / `AnalysisRun` remain internal database and Go
implementation names only.

**Creation identity:** the client creates one UUIDv7 `analysis_id` when the user
begins an analysis session, before candidate search and recommendation. It sends
that ID in `POST /analyses`; the server uses it as the analysis primary key and
idempotency identity. There is no separate `Idempotency-Key` header.

**Credential scope:** this is an internal tool using service-owned provider
credentials. Analyses are non-withdrawable once created. Per-user provider keys,
key revocation during execution, billing isolation, and user-initiated
mid-pipeline cancellation are deferred to a future multi-tenant design.

**Request, execution, Root, and report identity:** `analysis_runs` is a
user-owned request. `analysis_executions` is an immutable ownerless bridge keyed
by `report_fingerprint`, calculated from canonical READY content IDs, normalized
topic/brief, deployed pipeline and prompt versions, and every output-affecting
setting. Its existence means the bridge has created at least one Root; it has no
status, failure, result, or current-Root fields.

Each actual pipeline run is a UUIDv7 Root batch with
`batches.analysis_execution_id` pointing to its bridge. Roots are immutable once
terminal, and their rows are the complete execution history. Only a successful
Root creates a `reports` row and its `report.md` artifact; reports are the
success-only cache looked up by `report_fingerprint`.

Requests with the same fingerprint attach N:1 to the same execution bridge and,
when a Root is active (`completed_at IS NULL`), observe that Root. A request
persists its observed Root, report, lifecycle status, and failure locally. A
failed Root never changes an already failed request. An explicit rerun first
checks the report cache, then attaches to an active Root, and only creates the
next Root after a retryable terminal failure.

## Purpose

Implement the user-facing flow that resolves selected articles into a stable,
fetch-ready analysis manifest before Stage 0 begins. Fetching is separate from
later LLM analysis. Stage 0 and later stages must use a confirmed manifest, not
the latest candidate list.

## Decisions

- [x] Treat `fetches` as user/session-specific acquisition sessions.
- [x] Treat `fetch_items` as one selected candidate's status within one
  `fetch_id`.
- [x] Keep `PAGE_FETCH` tasks shared globally by canonical URL. The existing
  active-task uniqueness rule on `(kind, url)` is the shared work identity.
- [x] Do not add `fetch_tasks`; `tasks` owns scheduling and retry, while
  `fetch_items` records each session's interest in a task.
- [x] Once an analysis is created, acquisition work is not user-cancellable.
  Shared `PAGE_FETCH` tasks always finish or fail through the normal scheduler
  lifecycle; fetched content remains reusable.
- [x] Use user-facing statuses:

  ```text
  UNAVAILABLE
  PENDING
  FETCHING
  READY
  FAILED
  ```

- [x] Define status meanings:

  ```text
  UNAVAILABLE: candidate cannot be acquired before any task is created.
  PENDING:     PAGE_FETCH task is queued.
  FETCHING:    PAGE_FETCH task is running.
  READY:       readable content exists and can enter analysis.
  FAILED:      PAGE_FETCH reached terminal failure after retries.
  ```

- [x] Resolve `UNAVAILABLE` synchronously before creating a fetch session.
  Users remove unavailable articles or do not submit an analysis.
- [x] Persist the chosen failure policy before acquisition:

  ```text
  STOP
  IGNORE_FAILED
  ```

- [ ] `STOP` blocks analysis when any fetch item is `FAILED`.
  Current gap: this is evaluated only when a client polls `GET /fetches/{id}`;
  a user who leaves never reaches `AWAITING_RESOLUTION`.
- [ ] `IGNORE_FAILED` excludes failed items and starts analysis with `READY`
  items, provided at least one item is ready.
  Current gap: it runs only after polling observes a terminal fetch and does
  not atomically persist the manifest with Root/P0 creation.
- [ ] Never start Stage 0 with zero `READY` items.
  Current gap: zero-ready handling is also polling-driven and has no
  integration test that proves Root/P0 cannot be created.
- [ ] Create the analysis run before fetches finish, so the user may leave
  after confirmation. The server advances the run asynchronously.
  Current gap: run advancement currently occurs only as a `GET /fetches/{id}`
  side effect, so it is not asynchronous server-owned progress.
- [x] Persist topic, brief, selected IDs, policy, and fetch ID with the run.
- [ ] Persist the final manifest from `READY` items only, while retaining
  original selected, unavailable, and failed IDs as report provenance.
  Current gap: manifest persistence is a separate transaction from Root/P0
  creation, so the confirmed corpus can exist without a Root or vice versa.

## API Checklist

### Selection Preflight

- [x] Add `POST /analyses/preflight`.
- [x] Accept selected candidate IDs.
- [x] Validate candidate availability synchronously.
- [x] Return available and unavailable IDs without creating a Root, P0, fetch
  session, fetch item, or task.

```json
{
  "total": 3,
  "available_candidate_ids": ["...", "..."],
  "unavailable_candidate_ids": ["..."]
}
```

### Create Run And Fetch Session

- [x] Add or adapt `POST /analyses` after preflight conflicts are
  resolved.
- [x] Accept selected candidate IDs, topic, brief, and
  `fetch_failure_policy`.
- [ ] Accept and validate the client-created UUIDv7 `analysis_id`.
  Current gap: the current handler generates its own run ID and has not yet
  implemented client-supplied analysis identity or replay conflict detection.
- [x] Create the analysis run in a fetching state.
- [ ] Create a `fetch_id` and one `fetch_item` per available selected candidate.
  Current gap: fetch, run, and items are inserted through independent calls;
  a mid-request error leaves partial durable state.
- [x] Reuse an active shared `PAGE_FETCH` task for the same canonical URL.
- [x] Mark an item `READY` immediately when readable content already exists.
- [x] Return immediately; do not wait for fetching to finish.

```json
{
  "analysis_id": "...",
  "fetch_id": "...",
  "status": "FETCHING"
}
```

### Fetch Progress

- [x] Keep `GET /fetches/{fetch_id}` as the polling endpoint.
- [x] Map internal states to `PENDING`, `FETCHING`, `READY`, and `FAILED`.
- [x] Map existing completed/already-complete outcomes to `READY`.
- [x] Return grouped candidate IDs, counts, and total; do not return titles.

```json
{
  "total": 10,
  "pending": { "count": 1, "candidate_ids": ["..."] },
  "fetching": { "count": 3, "candidate_ids": ["..."] },
  "ready": { "count": 4, "candidate_ids": ["..."] },
  "failed": { "count": 2, "candidate_ids": ["..."] }
}
```

### Terminal Resolution And Cancellation

- [ ] Detect when every item for an analysis run is terminal.
  Current gap: detection is a side effect of `GET /fetches/{id}`, not an
  asynchronous server transition.
- [ ] For `IGNORE_FAILED`, build the manifest from `READY` items and create
  Root/P0 automatically.
- [ ] For `STOP` with failed items, set the run to `AWAITING_RESOLUTION`; do
  not create Root/P0.
- [ ] For zero ready items, set the run to `FAILED`; do not create Root/P0.
  Current gap for all terminal-policy transitions above: the implementation
  calls `confirmAnalysisRun` only from polling and does not make the resulting
  manifest and Root/P0 transition atomic.
- [x] Add `POST /analyses/{analysis_id}/resolve-fetch-failures` for a
  stopped run. It accepts exactly one action:

  ```text
  RETRY_FAILED
  IGNORE_FAILED
  ```

- [ ] `RETRY_FAILED` creates or reuses acquisition work only for failed fetch
  items and returns the run to `FETCHING`. It clears
  `failure_code=ANALYSIS_FETCH_FAILED`; that code represents the active STOP
  condition, not historical evidence. `failed_candidate_ids` contains only
  items still failed/excluded from the current manifest. Retry attempts and
  prior failures remain available from fetch-item/task history and the recorded
  resolution action.
- [ ] `IGNORE_FAILED` records the decision, builds a manifest from READY items,
  and starts Root/P0 only when at least one item is READY.
  Current gap for retry and ignore: the current handler retries existing task
  rows directly and then performs non-atomic manifest/Root operations.
- [x] Do not expose a user cancel API after `POST /analyses` commits. The
  analysis is durable and proceeds through acquisition and pipeline completion.
  Pipeline faults become `FAILED` with a failure code; they are not treated as
  user cancellation.

## Run State Model

```text
FETCHING
-> AWAITING_RESOLUTION  (STOP with FAILED items)
-> FAILED               (zero READY items)
-> ANALYZING
-> COMPLETED

```

## Stage 0 Gate

- [ ] Revalidate final `READY` inputs before Stage 0 starts.
  Current gap: `startAnalysisRoot` reloads candidates but does not verify that
  the exact READY content IDs are still readable before calling the pipeline.
- [ ] Persist the confirmed manifest, execution bridge, first Root/P0, and
  `FETCHING -> ANALYZING` request transition in one row-locked transaction. The
  repository operation revalidates exact READY content IDs, finds or creates the
  immutable execution bridge, locks it, attaches to any Root with
  `completed_at IS NULL`, or creates the first UUIDv7 Root/P0. No committed
  request may be ANALYZING without an observed active Root. Retry after an
  ambiguous commit finds the bridge and active Root under the bridge row lock;
  it never creates duplicate active Roots.
- [ ] Manifest minimum fields:

  ```text
  analysis_id
  fetch_id
  topic
  brief
  fetch_failure_policy
  original_selected_candidate_ids
  ready_candidate_ids
  ready_content_ids
  unavailable_candidate_ids
  failed_candidate_ids
  confirmed_at
  ```

- [ ] Never substitute a newer candidate/content item without a new user
  confirmation flow.
- [ ] If a confirmed live candidate/content becomes missing or unreadable before
  atomic Root snapshot creation, fail that analysis run with the stable reason
  `INPUT_CANDIDATE_MISSING`. After snapshot creation, stages consume the
  immutable snapshot rather than live source rows.
- [ ] Do not silently omit the item, substitute a newer item, or continue with
  a changed corpus after manifest confirmation.
- [ ] Notify the user that the run failed because a confirmed input became
  unavailable; the user must create a new selection/run to proceed.

## Given / When / Then Acceptance Tests

### Preflight

- [x] **Available selection**
  - Given all selected candidates exist and are available.
  - When `POST /analyses/preflight` is called.
  - Then every ID is available, none are unavailable, and no fetch session,
    task, Root, or P0 is created.

- [x] **Unavailable selection**
  - Given one selected candidate is deleted or cannot be loaded.
  - When `POST /analyses/preflight` is called.
  - Then its ID is unavailable, remaining IDs are available, and no acquisition
    or analysis work is created.

### Shared Page Fetch Work

- [ ] **Reuse active task**
  - Given fetch session A references a candidate whose PAGE_FETCH task T is
    PENDING or RUNNING.
  - When fetch session B requests the same canonical URL.
  - Then B receives a separate fetch item referencing T and no second active
    PAGE_FETCH task exists for that URL.

- [x] **Reuse completed content**
  - Given readable content already exists for a selected URL.
  - When a new fetch session requests it.
  - Then its item is READY and no PAGE_FETCH task is created.

### Progress And Policy

- [ ] **Progress mapping**
  - Given one item in each acquisition outcome: queued, running, ready, and
    terminal failure.
  - When `GET /fetches/{fetch_id}` is called.
  - Then each candidate ID appears exactly once in PENDING, FETCHING, READY, or
    FAILED and total equals the sum of group counts.

- [ ] **Stop policy**
  - Given policy STOP and at least one failed item.
  - When every item is terminal.
  - Then the run becomes AWAITING_RESOLUTION and no Root/P0 is created.

- [ ] **Ignore-failed policy**
  - Given policy IGNORE_FAILED, two READY items, and one FAILED item.
  - When every item is terminal.
  - Then the manifest contains the two READY items, preserves the failed ID as
    provenance, and exactly one Root/P0 is created.

- [ ] **No ready input**
  - Given every fetch item is FAILED.
  - When every item is terminal.
  - Then the run becomes FAILED and no Root/P0 is created.

- [ ] **Resolve stopped run by retry**
  - Given a STOP-policy run is AWAITING_RESOLUTION with one FAILED item.
  - When `POST /analyses/{id}/resolve-fetch-failures` is called with
    `RETRY_FAILED`.
  - Then only failed items are returned to acquisition, existing READY items
    remain READY, and no Root/P0 is created yet.

- [ ] **Resolve stopped run by ignoring failures**
  - Given a STOP-policy run is AWAITING_RESOLUTION with READY and FAILED items.
  - When `POST /analyses/{id}/resolve-fetch-failures` is called with
    `IGNORE_FAILED`.
  - Then only READY items enter the manifest, failed IDs remain provenance, and
    exactly one Root/P0 is created.

### Manifest And Stage 0

- [ ] **Stable manifest**
  - Given terminal fetch items and a valid READY set.
  - When the server confirms the manifest.
  - Then topic, brief, policy, selected IDs, READY IDs, and excluded IDs are
    persisted atomically with Root/P0.

- [ ] **No latest-list reload**
  - Given a confirmed manifest contains candidates A and B.
  - When candidate C later appears in recommendations.
  - Then Stage 0 and later stages use only A and B.

- [ ] **Final revalidation race**
  - Given fetch progress reports a candidate READY.
  - When it becomes unavailable before manifest creation.
  - Then Stage 0 does not start with that missing input and the defined
    availability outcome is recorded.

- [ ] **Input disappears before Root snapshot**
  - Given terminal fetch items select candidate C as READY, but atomic manifest
    confirmation has not begun.
  - When C or its required content becomes unreadable.
  - Then the analysis run fails with `INPUT_CANDIDATE_MISSING`, no replacement
    candidate is used, and no manifest, execution bridge, Root/P0, or observed
    Root is created.

- [ ] **Live source changes after Root snapshot**
  - Given Root/P0 was created from a snapshot containing candidate C and its
    content.
  - When C or its live content row is later changed or soft-deleted.
  - Then later stages use the immutable Root snapshot and do not replace or omit
    C based on the live source row.

## Resolved Decisions

- [x] A candidate is available for pre-analysis only when it exists and its URL
  can be normalized into a valid canonical URL. A missing candidate or an
  invalid/unusable URL is returned as `UNAVAILABLE`; neither may create a
  `PAGE_FETCH` task.
- [x] Defensively reject an empty candidate-ID list if one reaches the server.
  Return `ANALYSIS_INPUT_EMPTY` and create no fetch session, fetch item,
  analysis run, Root, or P0.

### Additional Acceptance Tests

- [ ] **Invalid candidate URL**
  - Given a selected candidate exists but its URL cannot be normalized into a
    valid canonical URL.
  - When `POST /analyses/preflight` is called.
  - Then its ID is returned as `UNAVAILABLE` and no PAGE_FETCH task is created.

- [ ] **Defensive empty submission**
  - Given a malformed client submits an empty candidate-ID list.
  - When it calls the analysis-run creation endpoint.
  - Then the server returns `ANALYSIS_INPUT_EMPTY` and creates no fetch session,
    fetch item, analysis run, Root, or P0.

## Remaining Implementation Checklist

These are the minimum server-side steps before this feature can be called
complete. Do not add a second fetch/task framework; extend the existing
`fetches`, `fetch_items`, `tasks`, and `PipelineRuntime` paths.

- [ ] **Make creation transactional.** Add one repository method that creates
  the fetch session, analysis run, and all fetch items in a single transaction.
  Reuse the existing `CreateTask` conflict/recovery behavior inside that
  transaction. A request error must leave none of those rows behind.
  - Given acquisition of the second of two selected candidates fails.
  - When `POST /analyses` returns an error.
   - Then no `fetches`, `fetch_items`, or `analysis_runs` row from that request
     exists, and no new PAGE_FETCH task from that request remains active.
- [ ] **Enforce current-candidate canonical URL identity.** A canonical URL
  identifies one current article candidate, not merely shared fetch work. Keep
  the discovered raw URL for source provenance, add a persisted
  `candidates.canonical_url`, and enforce uniqueness for current candidates.
  Discovery/upsert resolves a canonical-URL conflict to the existing candidate
  rather than creating a second candidate with different title, source, or
  publication metadata. Consequently `contents.candidate_id` remains a true
  one-to-one association and no `fetch_items.content_id` or candidate/content
  fan-out table is required. Shared PAGE_FETCH reuse applies only when multiple
  users or sessions select that same candidate. The future immutable version
  chain may permit predecessor/successor candidates with the same canonical URL;
  it replaces this current-slice uniqueness rule as part of its own migration.
  - Given discovery sees a second record whose raw URL canonicalizes to an
    existing candidate's canonical URL.
  - When it persists the record.
  - Then it returns the existing current candidate and creates neither another
    candidate nor another content identity.
- [ ] **Move terminal resolution out of `GET /fetches/{id}`.** Add a small
  server-owned reconciliation entry point that finds `FETCHING` analysis runs
  whose fetch items are all terminal and applies the policy. Invoke it from a
  scheduler/trigger or collector-completion path; polling may read state but
  must not be the only way a run advances.
  - Given a FETCHING analysis run whose last PAGE_FETCH task becomes terminal.
  - When the server reconciliation path runs without a `GET /fetches/{id}`
    request.
   - Then the run reaches `AWAITING_RESOLUTION`, `FAILED`, or Root/P0 creation
     according to its policy and READY inputs.
- [ ] **Expire stalled shared fetch work.** Configure a hard deadline only for
  PAGE_FETCH tasks (`page-fetch-deadline`, default `2h`); do not apply this
  policy to unrelated task kinds. Add the scheduler-owned global sweeper that
  claims expired PENDING/RUNNING PAGE_FETCH tasks and marks them
  `FAILED/TASK_EXPIRED`. `RETRY_FAILED` creates or attaches fresh acquisition
  work with a new `clock_timestamp() + page-fetch-deadline`; it must not revive an expired
  task with its old deadline. Completion must be conditional on both RUNNING
  status and an unexpired deadline.
  - Given a PAGE_FETCH task remains unfinished for more than two hours.
  - When the sweeper runs.
  - Then it becomes FAILED with `TASK_EXPIRED`, and its linked analyses proceed
    through their normal STOP/IGNORE_FAILED reconciliation.
- [ ] **Make manifest, execution bridge, and Root/P0 one transaction.** Extend
  the pipeline-root repository transaction, or add an analysis-run repository
  operation, so it revalidates exact candidate/content IDs, stores the manifest,
  finds or creates the immutable execution bridge, snapshots Root inputs, creates
  P0, and records the request's observed Root atomically.
  - Given a terminal run with valid READY inputs and a database error while
    creating P0.
  - When manifest confirmation is attempted.
    - Then no manifest confirmation, execution bridge, Root batch, P0 task, or
      observed Root is persisted; the request remains retryable or records one
      explicit failure.
- [ ] **Add request/execution persistence and atomic attachment.** Keep
  `analysis_runs` as the user-owned request/provenance record and add immutable
  ownerless `analysis_executions`: UUIDv5 `id` from unique
  `report_fingerprint CHAR(64)`, canonical READY `content_ids`, deployed
  pipeline/prompt identity, and timestamps only. Add nullable
  `analysis_runs.execution_id REFERENCES analysis_executions(id)`,
  `observed_root_batch_id REFERENCES batches(id)`, and
  `report_id REFERENCES reports(id)`; request status and failure remain local.
  Add immutable success-only `reports`: UUIDv7 `id`, unique
  `report_fingerprint`, unique `root_batch_id REFERENCES batches(id)`,
  `analysis_execution_id REFERENCES analysis_executions(id)`, report storage URI,
  content hash, and timestamps. A report row is inserted only by successful Root
  terminalization, never by a request cache hit.
  Add nullable `batches.analysis_execution_id REFERENCES
  analysis_executions(id)` with a constraint allowing it only on
  `ANALYZER_PIPELINE_ROOT` batches, plus an index on
  `(analysis_execution_id, completed_at)`. Roots use UUIDv7 IDs and are never
  rewritten after terminalization. In one transaction, lock the execution bridge,
  attach the request to a Root with `completed_at IS NULL`, or, after explicit
  retry eligibility, create the first or next Root/P0. `GET /analyses/{id}`
  reads request-owned provenance, observed Root, report, lifecycle status, and
  failure; report content is retrieved through its immutable report reference.
  - Given two requests have different selections but resolve to the same READY
    content IDs and output-affecting settings.
  - When they attach concurrently.
  - Then both link to one execution bridge and one active Root/P0, while each retains
    its own selected/unavailable IDs and fetch ID.
  - Given two equivalent requests race to create an execution.
  - When one creates the immutable execution bridge and first Root.
  - Then both requests attach to that Root; neither receives ownership of the
    bridge or the Root.
  - Given requests A and C observe failed Root R1 under execution X, while an
    explicit rerun creates R2 and R2 completes with report R.
  - When A and C read their analyses without rerun.
  - Then they remain FAILED and return R1's failure; an explicit rerun by A may
    adopt report R without changing C.
- [ ] **Finish parentless analysis Roots.** Widen
  `FindFinishedPipelineRootBatches` only for an analysis-owned Root: retain
  `parent_id IS NOT NULL` for ordinary Roots, but also select a parentless Root
  when `batches.analysis_execution_id IS NOT NULL`. In the same transaction as
  the winning `MarkPipelineRootFinished` update, create a report only on success
  and update ANALYZING requests whose `observed_root_batch_id` is that Root to
  COMPLETED or FAILED with `ANALYSIS_PIPELINE_FAILED`. Do not add a second
  analysis-completion poller.
  - Given a parentless analysis Root has all subtasks terminal.
  - When two finisher instances detect it concurrently.
  - Then exactly one marks the Root and linked analysis terminal; the other sees
    zero affected rows, and an unrelated parentless Root is not selected.
- [ ] **Enforce exact confirmed inputs.** Before Root creation, require every
  `ready_candidate_id` and `ready_content_id` to resolve to the same readable
  pair. If any is missing, set `FAILED` with `INPUT_CANDIDATE_MISSING`; do not
  use a replacement content row or reduce the corpus.
  - Given a READY manifest containing candidate C and content X.
  - When X is soft-deleted or C no longer resolves before Root creation.
  - Then the run is `FAILED` with `INPUT_CANDIDATE_MISSING`, and no Root/P0 or
    replacement content is used.
- [ ] **Define multi-batch Root parent semantics.** User selections can span
  candidate batches. The current code chooses the first candidate's batch as
  `ParentBatchID` only to satisfy pipeline idempotency. Replace that incidental
  parent choice with an explicit analysis-run Root identity that is valid for a
  mixed-batch manifest.
  - Given one confirmed manifest contains READY candidates from collection
    batches A and B.
  - When its analysis Root is created and the request is retried.
  - Then exactly one Root/P0 is associated with the analysis run, snapshots
    inputs from both batches, and does not depend on candidate ordering.
- [ ] **Harden retries.** `RETRY_FAILED` must handle an item whose old failed
  task was deleted, replaced, or is shared. It should attach/recreate only the
  failed item work and preserve READY item rows unchanged.
  - Given a STOP run with one READY item and one FAILED item whose task is no
    longer retryable.
  - When `RETRY_FAILED` is requested.
  - Then the READY item remains READY, only the failed item receives new or
    shared acquisition work, and the run returns to `FETCHING`.
- [ ] **Enforce fetch and analysis ownership.** `fetches.user_id` and
  `analysis_runs.user_id` already persist the authenticated principal, but the
  current `GET /fetches/{id}` and failure-resolution paths do not compare them.
  Require ownership in repository reads/updates so an authenticated user cannot
  use a guessed UUID to observe or control another user's acquisition.
  - Given user A owns a fetch and analysis run, and user B has a valid user
    token.
  - When B gets the fetch or resolves failures using A's ID.
  - Then every endpoint returns `404 Not Found` without exposing state or
    changing A's fetch items, run, task, manifest, Root, or P0.
- [ ] **Add repository integration coverage.** Use Postgres-backed tests for
  transaction rollback, shared active task reuse, terminal reconciliation,
  STOP, IGNORE_FAILED, zero READY inputs, retry, ownership isolation, and
  manifest/Root atomicity, expired task sweeping, and worker/sweeper completion
  races. Each test must use the Given/When/Then scenarios in this document as
  its test name or a directly traceable subtest name.
- [ ] **Add API coverage.** Cover empty IDs, invalid URLs, unavailable IDs,
  response status groups, both failure-resolution actions, ownership, and
  the `INPUT_CANDIDATE_MISSING` race. The current two API tests cover only
  basic preflight classification and already-present content. For every API
  transition, assert the Given state, the When request, the Then response, and
  the resulting persisted state, including that forbidden fetch/run/Root/P0
  rows were not created.
  - Given every API error condition listed above.
  - When its endpoint is called with the corresponding request.
  - Then the documented status/code is returned and the database contains only
    the rows permitted by that scenario.
- [ ] **Test derived pre-Root result status.** Add an API test named after the
  Given/When/Then scenario below. Seed or mock a FETCHING analysis with
  `confirmed_at=NULL` and `observed_root_batch_id=NULL`; call `GET /analyses/{id}`; then
  assert lifecycle `status="FETCHING"`, derived
  `result={"status":"UNKNOWN","message":"Awaiting confirmed manifest"}`,
  and `retry_after_seconds=30`. Also assert no repository write/manifest/Root
  creation occurs while serving the read.
  - Given a persisted FETCHING analysis without a confirmed manifest or Root/P0.
  - When its owner calls `GET /analyses/{id}`.
  - Then the lifecycle is FETCHING, the result is UNKNOWN, and the request has
    no persistence side effects.
- [ ] **Close every Given/When/Then scenario.** Do not mark this feature
  complete until every unchecked scenario under “Given / When / Then Acceptance
  Tests” and “Additional Acceptance Tests” has a corresponding automated test.
  Add a test reference beside each checked scenario.
  - Given an item is about to be marked complete in this document.
  - When its implementation review is performed.
  - Then the item links to a passing automated Given/When/Then test that covers
    both the expected result and its critical no-side-effect guarantee.
- [ ] **Regenerate and verify.** After SQL changes, run `task sqlc`, `task
  mocks` if interfaces change, `gofmt`, focused package tests, the full short
  suite, and `golangci-lint run`.

## Resolved Implementation Questions

- [x] **Terminal reconciliation owner:** add a dedicated lightweight resource
  trigger, `cmd/trigger/analysis`, that polls only eligible `FETCHING` analysis
  runs and advances them. Reuse the repository polling/claiming pattern from
  existing triggers, but do not add this unrelated lifecycle to the collector
  completion path or `GET /fetches/{id}`. Fetch polling remains read-only.
  - Given a terminal fetch and no client traffic.
  - When `cmd/trigger/analysis` runs.
  - Then it applies the run failure policy exactly once.

- [x] **Independent analysis Root identity:** analysis and collection are
  separate processes. Create an analysis-execution-scoped Root with
  `parent_batch_id = NULL`; do not create a synthetic collection batch or attach
  it to any collection batch. Selected candidates/contents are immutable input
  provenance only, captured in the Root snapshot. Every Root gets its own UUIDv7
  ID and stores `analysis_execution_id`; an execution may therefore retain
  multiple immutable Root history rows after retry. Do not use the generic
  collection-batch idempotency key for this path. Do not store an `analysis_id`
  in `batches.parent_id`: that column is batch-to-batch lineage, not request
  ownership. The request-to-execution and Root-to-execution links are the
  associations.
  - Given the same mixed-batch analysis run is reconciled concurrently twice.
  - When both attempts confirm its manifest.
  - Then one Root/P0 exists and its snapshots contain only the confirmed IDs.

- [x] **Missing-input notification:** for the current server-only scope,
  `FAILED` with `failure_code=INPUT_CANDIDATE_MISSING` is exposed through
  fetch/analysis-run polling. Do not add email, webhook, or a delivery worker.
  A future client may display the failure and ask the user to make a new run.
  - Given a confirmed input disappears before Root creation.
  - When reconciliation attempts confirmation.
  - Then polling reports `FAILED` and `INPUT_CANDIDATE_MISSING`.

## Deferred Review Questions

- [x] **Article refresh and versioning:** this is intentionally separate from
  the first pre-analysis slice. The agreed immutable version-chain design,
  admin-only refresh boundary, and future repository API are in
  `deployments/docs/plan/future.md`.

- [x] **Treat content bodies as append-only application data.** Do not expose a
  repository or HTTP API that updates `contents.content`; existing metadata and
  soft-delete operations remain available. A future article correction creates a
  new candidate/content version rather than changing the old body. PostgreSQL
  triggers are not required in the current API-only scope; controlled admin SQL
  may directly repair content when necessary, with an operator backup first.

- [x] **Fetch ownership:** this is a clean-database deployment. Make
  `fetches.user_id` required for all new rows and require the authenticated
  principal on every fetch/run read or mutation. Do not retain a NULL-owner
  compatibility path; unknown or foreign IDs return `404 Not Found`.
  - Given a user-owned fetch and a different authenticated user.
  - When the different user gets the fetch or resolves its failures.
  - Then the API returns `404 Not Found` and changes no rows.

- [x] **Reconciliation claim:** use one short PostgreSQL transaction with
  `SELECT ... FOR UPDATE SKIP LOCKED` over eligible FETCHING runs. The winning
  transaction performs policy resolution and, where applicable, atomic
  manifest/execution attachment/Root/P0 creation. Do not add a lease column:
  row locking plus the unique report fingerprint, an execution-row lock, and the
  `completed_at IS NULL` active-Root predicate are sufficient and reuse the
  existing trigger claim pattern.
  - Given two `cmd/trigger/analysis` instances observe the same terminal run.
  - When both reconcile concurrently.
  - Then one claims and advances it; the other skips it, and one Root/P0 exists.

- [x] **Shared-task rollback:** rollback the entire analysis-run creation
  transaction. A newly inserted PAGE_FETCH task is uncommitted and therefore
  cannot be referenced by another session; rollback removes it safely. A task
  recovered through the active-task uniqueness rule belongs to an existing
  transaction history and must not be changed or deleted.
  - Given creation creates one new task then fails while recording a later item.
  - When the transaction rolls back.
  - Then the new task and all new run/fetch rows disappear, while a recovered
    shared task and its existing memberships remain unchanged.

- [x] **Post-confirmation input validation:** validate the exact live
  candidate/content pairs before atomic Root creation. Once Root snapshots are
  persisted, stages consume only those snapshots and must not reread live
  candidates or contents. A later stage fails with `INPUT_CANDIDATE_MISSING`
  only if its required Root snapshot is absent or unreadable, not because a
  mutable source row was later changed or soft-deleted.
  - Given a Root snapshot contains candidate C and content X, then C is
    soft-deleted from the live candidates table.
  - When a later stage consumes its Root inputs.
  - Then it uses the snapshot of C/X and continues; it fails only if that
    snapshot record itself is unavailable.

- [x] **Verification claim:** the `895 passed in 106 packages` result was run
  locally in this workspace on 2026-08-04 with
  `rtk go test -short ./...`; it is not a CI claim. The same workspace also ran
  `rtk golangci-lint run ./internal/http/api ./internal/repo/... ./cmd/api-server`
  successfully. CI should rerun these commands in its own Docker-capable
  environment before merge.

## Resolved Scope Questions

- [x] **Scope:** keep pre-analysis focused on acquisition, ownership,
  reconciliation, and manifest/Root atomicity. The application exposes no
  content-body update path; privileged SQL remains an operator responsibility.
  Defer candidate version
  chains, body hashes, URL-constraint replacement, and admin refresh to a
  separate versioning follow-up. This avoids coupling the already-large
  pre-analysis change to collector and default-query redesign.
  - Given a pre-analysis implementation needs to correct an article body.
  - When it designs the write path.
  - Then it does not add a content-body UPDATE API; candidate-version and admin
    refresh behavior remain deferred from the pre-analysis merge.

- [x] **Ownership constraint:** enforce ownership in PostgreSQL, not only the
  API. The next migration makes `fetches.user_id` and `analysis_runs.user_id`
  `NOT NULL`; all creation paths already have an authenticated principal. This
  is a clean-database deployment, so no NULL-owner compatibility or backfill is
  required.
  - Given any insertion into `fetches` or `analysis_runs` omits `user_id`.
  - When PostgreSQL executes it.
  - Then PostgreSQL rejects the row.

- [x] **Version migration:** no body-hash migration is part of pre-analysis.
  The later versioning phase starts from the clean schema, adds `body_sha256`
  before `UNIQUE (url, body_sha256)`, and removes `UNIQUE(contents.url)` in the
  same ordered migration. Existing rows cannot contain duplicate URLs while the
  current unique constraint exists. If an in-place upgrade is ever required,
  add the hash nullable, backfill in a dedicated controlled job, validate it,
  then apply the composite unique constraint; do not mix that compatibility work
  into the clean-deployment phase.
  - Given the clean versioning migration creates V1 and V2 at the same URL.
  - When their body hashes differ.
  - Then both rows satisfy `UNIQUE (url, body_sha256)`.

## Resolved API Contract Questions

- [x] **Create analysis idempotency:** the client-created `analysis_id` is the
  idempotency identity for `POST /analyses`. Persist a request fingerprint over
  the original client selection, topic, brief, and policy. Reusing the same ID
  with the same fingerprint returns the original analysis; reusing it with
  different content returns `409 ANALYSIS_REQUEST_CONFLICT`. A client that
  changes its selection creates a new analysis ID.
  - Given a client loses the `202 Accepted` response after a run is committed.
  - When it retries the identical request with the same `analysis_id`.
  - Then it receives the original `analysis_id` and `fetch_id`, and no
    second run, fetch, item, task, Root, or P0 is created.

- [x] **Canonical request fingerprint:** implement
  `CreateAnalysisParams.Fingerprint() ([]byte, error)` after authentication and
  request normalization. It returns the SHA-256 digest of a length-prefixed,
  versioned binary encoding of: fingerprint format version, authenticated
  `user_id`, normalized topic, normalized brief, normalized failure policy, and
  the canonical sorted **original** candidate-ID set. It must validate and
  reject duplicate or nil IDs before hashing. Persist the digest as the existing
  64-character hexadecimal `request_fingerprint`; do not hash arbitrary JSON.
  Availability is intentionally excluded because it is server-observed state,
  not client intent.
  - Given request A selects `[a, b, c, d]` and `d` is unavailable, while request
    B selects `[a, b, c]`.
  - When both requests are normalized and fingerprinted for the same user.
  - Then their fingerprints differ even though A creates fetch work only for
    `[a, b, c]`.
  - This fingerprint protects one client-created `analysis_id`; it is not a
    pipeline-cost deduplication key.

- [x] **Analysis read contract:** add owner-scoped `GET /analyses/{id}`.
  `GET /fetches/{fetch_id}` remains acquisition-progress-only. The run response
  exposes `analysis_id`, `fetch_id`, status, failure code, policy,
  confirmed candidate/content IDs, excluded IDs, the observed `root_batch_id`,
  optional `report_id`, and timestamps;
  it exposes neither task IDs nor presentation snapshots.
  Before a manifest and Root/P0 exist, it also returns a derived result object:
  `{"status":"UNKNOWN","message":"Awaiting confirmed manifest"}` plus
  `retry_after_seconds: 30`. This is a polling hint, not a completion guarantee.
  `result.status` is never persisted: it is `UNKNOWN` when `confirmed_at` and
  `observed_root_batch_id` are both NULL, while the persisted lifecycle status remains
  `FETCHING` or `AWAITING_RESOLUTION`.
  - Given a reconciled run has `failure_code=INPUT_CANDIDATE_MISSING`.
  - When its owner calls `GET /analyses/{id}`.
  - Then the response includes the failure code and persisted manifest state.
  - Given a FETCHING analysis without a confirmed manifest or Root/P0.
  - When its owner calls `GET /analyses/{id}`.
  - Then the response has lifecycle `status="FETCHING"`, derived
    `result.status="UNKNOWN"`, and `retry_after_seconds=30` without creating
    any manifest or Root/P0 row.

- [x] **Duplicate selection:** reject duplicate candidate IDs before preflight
  or persistence with `400 ANALYSIS_INPUT_DUPLICATE_CANDIDATE`. Do not silently
  normalize input: it masks client bugs and changes the request the user made.
  - Given a create-run request contains candidate C twice.
  - When `POST /analyses` is called.
  - Then it returns `400 ANALYSIS_INPUT_DUPLICATE_CANDIDATE` and creates no
    fetch, fetch item, analysis run, PAGE_FETCH task, Root, or P0.

## Resolved Completion And Validation Questions

- [x] **Analysis completion:** extend the existing `internal/batch.Finisher`
  Root completion path. When its winning `MarkRootBatchFinished` transition
  marks an analysis Root terminal, the same repository transaction creates a
  report only when `succeeded=true`, then updates linked `analysis_runs` where
  `observed_root_batch_id` matches and status is `ANALYZING`: `COMPLETED` with
  the report reference on success, otherwise `FAILED` with
  `failure_code=ANALYSIS_PIPELINE_FAILED`. Roots not linked through
  `analysis_execution_id` are unchanged. Do not add a separate
  analysis-completion poller.
  - Given an ANALYZING analysis whose Root has all stages terminal and succeeded.
  - When the batch finisher marks the Root completed.
  - Then the same transaction marks the analysis COMPLETED.
  - Given an ANALYZING analysis whose Root terminally fails.
  - When the batch finisher marks the Root failed.
  - Then the same transaction marks the analysis FAILED with
    `ANALYSIS_PIPELINE_FAILED`.

- [x] **Missing analysis ID:** `POST /analyses` requires a valid non-nil UUIDv7
  `analysis_id` in its JSON body and returns `400 ANALYSIS_ID_REQUIRED` when it
  is absent or invalid. Validate it immediately after JSON decoding and before
  candidate preflight, so no database/API work occurs for an invalid request.
  - Given a request without a valid `analysis_id`.
  - When `POST /analyses` is called.
  - Then it returns `400 ANALYSIS_ID_REQUIRED` without looking up
    candidates or creating fetch, analysis, task, Root, or P0 rows.

## Resolved Routing And State Questions

- [x] **Resource rename:** use only `/analyses` and `analysis_id`. Remove the
  unshipped `/analysis-runs` routes and `analysis_run_id` response fields; do
  not add compatibility aliases in a clean-database, API-only deployment.
  Internal table and Go names remain `analysis_runs` / `AnalysisRun`.
  - Given a request uses `/analysis-runs`.
  - When the API is deployed.
  - Then it returns `404 Not Found`; `/analyses` is the sole public contract.

- [x] **Preflight route:** use `POST /analyses/preflight`. It is a collection
  operation that validates a proposed analysis before creation, so it belongs
  under the same public resource namespace without persisting an analysis.
  - Given selected candidate IDs.
  - When `POST /analyses/preflight` is called.
  - Then it returns availability groups and creates no analysis, fetch, task,
    Root, or P0.

- [x] **Idempotency storage:** use the client-provided `analysis_runs.id` as
  the primary-key idempotency identity and add only
  `request_fingerprint CHAR(64) NOT NULL`. The atomic create transaction inserts
  this row with all fetch/run/items/task work. If it rolls back, the analysis ID
  is unused and the caller may retry; after commit, the same ID always resolves
  to the original analysis, including an eventually FAILED analysis.
  - Given creation fails before transaction commit.
  - When the client retries with the same `analysis_id`.
  - Then the retry may create the analysis because no analysis row exists.

- [x] **Run states:** persist only reachable states:
  `FETCHING`, `AWAITING_RESOLUTION`, `ANALYZING`, `COMPLETED`, and `FAILED`.
  `READY_TO_ANALYZE`, `DRAFT`, `PREFLIGHT`, and
  `WAITING_FOR_CONFIRMATION` are client/UI concepts; preflight intentionally
  creates no database row.
  - Given a successful `POST /analyses/preflight`.
  - When no create request follows.
  - Then no analysis row exists in a draft or preflight state.

## Resolved Response And Immutability Questions

- [x] **Fetch response boundary:** `GET /fetches/{fetch_id}` returns only
  acquisition-progress groups and `terminal`; remove `analysis_id` and
  `analysis_status` from it. `POST /analyses` supplies the analysis ID, and
  `GET /analyses/{id}` is the sole analysis lifecycle/manifest endpoint.
  - Given an analysis-owned fetch is terminal.
  - When its owner calls `GET /fetches/{id}`.
  - Then it receives only fetch progress; `GET /analyses/{analysis_id}` returns
    the analysis status, failure code, and Root state.

- [x] **Idempotent replay response:** the first successful create returns
  `202 Accepted` with `created=true`. An identical replay after commit returns
  `200 OK` with the existing analysis ID, fetch ID, current status, and
  `created=false`, `result="ANALYSIS_ALREADY_CREATED"`, regardless of whether
  that status is FETCHING, FAILED, or COMPLETED. Do not invent
  status-specific response shapes; clients use `GET /analyses/{id}` for details.
  - Given an analysis later reaches COMPLETED.
  - When its original `POST /analyses` request is replayed with the same
    `analysis_id`.
  - Then it returns `200 OK`, `ANALYSIS_ALREADY_CREATED`, and the same IDs with
    status COMPLETED, without creating any work.

- [x] **Content update boundary:** do not add `UpdateContentByURL`,
  `UpdateContentByID`, or an HTTP endpoint that can modify `contents.content`.
  Existing metadata and soft-delete operations remain explicit and do not carry
  body fields. A PostgreSQL trigger is deferred as optional defense in depth,
  not a current requirement. Privileged admin SQL may modify data during the
  prototype; take a Postgres backup before manual changes and record the reason
  plus affected content IDs.
  - Given an API or repository change proposes a content-body update.
  - When it is reviewed.
  - Then it is rejected in favor of a future version-creation path.

## Resolved Race And Ownership Questions

- [x] **Unavailable IDs at create:** `POST /analyses` classifies every selected
  candidate synchronously, but it does not silently remove
  unavailable IDs. If at least one candidate is available, create the analysis
  from all original selected IDs, persist unavailable IDs as provenance, and
  create fetch items only for available IDs. If every ID is unavailable, return
  `ANALYSIS_INPUT_EMPTY` and create nothing.
  - Given a create request contains one valid and one unavailable candidate ID.
  - When `POST /analyses` runs its synchronous availability check.
  - Then it persists both original IDs, records the unavailable ID, creates
    acquisition work only for the valid ID, and never treats the unavailable ID
    as READY input.

- [x] **No cancel/reconcile race:** analyses cannot be cancelled after creation.
  `cmd/trigger/analysis` is the only path that transitions a FETCHING analysis
  after acquisition becomes terminal, and batch completion is the only path
  that transitions ANALYZING to a terminal pipeline outcome.
  - Given an analysis has been created.
  - When the user attempts to withdraw it.
  - Then no public cancel endpoint exists; it proceeds or records FAILED if the
    acquisition or pipeline fails.

- [x] **Cross-owner `analysis_id`:** return `404 Not Found`. The API must not
  disclose that another user's analysis ID exists, whether through create,
  read, or failure resolution.
  - Given user A owns analysis ID X and user B submits X.
  - When B calls any `/analyses` endpoint using X.
  - Then it receives `404 Not Found` and learns no analysis state, fingerprint,
    fetch ID, or ownership information.

- [x] **Resolution retries:** each action uses a conditional transition from
  `AWAITING_RESOLUTION`, protected by the same row lock. The first winning
  `RETRY_FAILED` or `IGNORE_FAILED` succeeds; later requests return
  `409 ANALYSIS_NOT_AWAITING_RESOLUTION` and clients read the analysis resource
  to discover the resulting state.
  - Given two `IGNORE_FAILED` requests arrive for the same awaiting analysis.
  - When one commits its Root/P0 transition.
  - Then the other returns `409 ANALYSIS_NOT_AWAITING_RESOLUTION`, and exactly
    one Root/P0 exists.

## Resolved Fetch And Future-Scope Questions

- [x] **Fetch progress total:** `GET /fetches/{id}` counts only persisted,
  fetchable `fetch_items`; it has no `UNAVAILABLE` group. Unavailable IDs are
  request provenance, not acquisition work, and are returned by
  `GET /analyses/{id}`. This keeps fetch progress totals equal to the sum of
  PENDING, FETCHING, READY, and FAILED groups.
  - Given an analysis selected `[a, b]`, where `b` is unavailable.
  - When its fetch progress is read.
  - Then `total=1` and only `a` appears in a progress group; the analysis
    resource records `b` as unavailable provenance.

- [x] **Replay status coverage:** identical `POST /analyses` replay always
  returns the same `200 OK` shape with `created=false`,
  `result="ANALYSIS_ALREADY_CREATED"`, IDs, and the current status. This applies
  to FETCHING, AWAITING_RESOLUTION, ANALYZING, FAILED, and
  COMPLETED; clients use `GET /analyses/{id}` for details.
  - Given an analysis is AWAITING_RESOLUTION.
  - When its identical create request is replayed.
  - Then it returns `200 ANALYSIS_ALREADY_CREATED` with status
    AWAITING_RESOLUTION and creates no work.

- [x] **Canonical URL normalization:** canonicalize only what is unambiguously
  transport-irrelevant: trim outer whitespace; require an absolute HTTP(S) URL
  with host; lowercase scheme and host; remove the default HTTP/HTTPS port;
  normalize an empty path to `/`; and remove the fragment. Preserve non-root
  trailing slashes, user info rejection, query ordering, query bytes, percent
  encoding, and all query parameters because changing them can alter page or
  signature semantics. Preserve the raw discovered candidate URL; persist and
  deduplicate on the canonical URL only for PAGE_FETCH tasks and collected
  contents.
  - Given `HTTPS://Example.com:443/article#section`.
  - When it becomes a PAGE_FETCH identity.
  - Then the canonical URL is `https://example.com/article`; a query string is
    otherwise preserved byte-for-byte.

## Resolved Input And Response Questions

- [x] **Fetch-item cardinality:** create exactly one `fetch_item` per available
  selected candidate. Unavailable selected IDs remain only in analysis
  provenance; they are never fetch items and never contribute to fetch progress
  totals.
  - Given selected IDs `[a, b]`, where `b` is unavailable.
  - When the analysis is created.
  - Then one fetch item exists for `a`, none exists for `b`, and the analysis
    retains both original IDs plus `b` as unavailable.

- [x] **Request normalization:** before request fingerprinting, normalize topic
  and brief by trimming outer Unicode whitespace, normalizing line endings to
  LF, and applying Unicode NFC. Preserve internal whitespace, paragraph breaks,
  and case because they may affect LLM output. Normalize failure policy by
  trimming and uppercasing ASCII values. Sort a copied candidate-ID list by raw
  UUID bytes; do not mutate client input order and do not casefold natural text.
  - Given two requests differ only by outer whitespace, CRLF versus LF, or
    composed versus decomposed Unicode.
  - When both are normalized and fingerprinted.
  - Then they have the same fingerprint; requests differing in internal text
    whitespace or case have different fingerprints.

- [x] **Pre-Root analysis response:** all list fields are JSON arrays and never
  `null`. Before confirmation, `ready_candidate_ids`, `ready_content_ids`, and
  `failed_candidate_ids` are empty arrays unless failures have been observed;
  `confirmed_at`, observed `root_batch_id`, `report_id`, and `failure_code` are
  `null` until set.
  This response shape remains stable for FETCHING, AWAITING_RESOLUTION, and
  pre-Root FAILED analyses. The derived `result.status="UNKNOWN"` is distinct
  from lifecycle status and means no confirmed manifest/result exists yet.
  - Given a newly created FETCHING analysis.
  - When its owner calls `GET /analyses/{id}`.
  - Then all ID collections are arrays, confirmation/root/failure scalar fields
    are null, and no field changes JSON type after confirmation.

- [x] **Failure-code taxonomy:** API validation errors are response codes, not
  persisted analysis failures: `ANALYSIS_ID_REQUIRED`,
  `ANALYSIS_INPUT_DUPLICATE_CANDIDATE`, `ANALYSIS_REQUEST_CONFLICT`, and
  `ANALYSIS_INPUT_EMPTY`. `ANALYSIS_INPUT_EMPTY` intentionally covers an
  all-unavailable selection because no analysis is created. Persisted analysis
  failure codes are: `ANALYSIS_NO_READY_INPUT` (terminal acquisition leaves no
  READY input), `ANALYSIS_FETCH_FAILED` (STOP policy awaits resolution after
  terminal fetch failures), `INPUT_CANDIDATE_MISSING` (confirmed input missing
  before Root snapshot), and `ANALYSIS_PIPELINE_FAILED` (terminal Root failure).
  - Given every selected candidate is unavailable.
   - When `POST /analyses` performs availability classification.
   - Then it returns `ANALYSIS_INPUT_EMPTY` and persists no analysis failure row.

## Execution Failure Taxonomy (Pending Retry Policy)

Only failures of a Root under an `analysis_execution` bridge belong to this
taxonomy. Fetch/input failures remain request-local and use the existing
`ANALYSIS_NO_READY_INPUT`, `ANALYSIS_FETCH_FAILED`, and
`INPUT_CANDIDATE_MISSING` codes; rerun must not bypass their fetch-policy or
input-validation rules. A terminal Root failure keeps the public
`ANALYSIS_PIPELINE_FAILED` code and records one stable internal `failure_kind`
for rerun eligibility:

- [ ] `PROVIDER_TRANSIENT`: provider timeout, connection reset, rate limit, or
  provider 5xx after ordinary task retries are exhausted.
- [ ] `INFRA_TRANSIENT`: temporary database, message broker, storage, or worker
  infrastructure failure after ordinary task retries are exhausted.
- [ ] `PIPELINE_CONFIGURATION_INVALID`: deployed pipeline definition, prompt,
  model/provider configuration, or required secret is invalid or absent.
- [ ] `MODEL_OUTPUT_INVALID`: a provider returned output that fails required
  schema/contract validation after ordinary task retries are exhausted.
- [ ] `INPUT_SNAPSHOT_INVALID`: a required execution Root snapshot is absent or
  unreadable after execution attachment.
- [ ] `POLICY_DENIED`: execution is prohibited by a server-side policy or
  credential boundary.
- [ ] `UNKNOWN`: no safe classification is available; it is conservative by
  default and must not become retryable merely because a caller requests it.

The retry policy is intentionally unresolved: decide which kinds permit an
explicit rerun to create the next Root under the same execution, whether a
cooldown applies, and whether that operation is user-facing or operator-only.

## Resolved Implementation Decisions (2026-08-04)

- [x] **Retry failure provenance:** `ANALYSIS_FETCH_FAILED` and
  `failed_candidate_ids` describe the current blocking/excluded state, not
  append-only history. A successful retry clears that failure code and removes
  the candidate from the current failed list; fetch-item/task history and the
  resolution action retain the prior failure evidence.

- [x] **Root/P0 lifecycle transition:** the atomic confirmation repository
  operation changes the run directly to `ANALYZING` as it writes the manifest,
  execution bridge, Root, and P0. `READY_TO_ANALYZE` is not a persisted analysis
  status. A retry resolves an ambiguous committed transaction by locking the
  bridge and finding the active Root (`completed_at IS NULL`) before creating
  work, so it restores the linked ANALYZING state without duplicate Roots.

- [x] **Execution reuse boundary:** request-to-execution reuse is part of the
  active pre-analysis slice. Requests share only the immutable execution bridge
  and any active Root; success reuse is through report-fingerprint cache lookup.
  Each request retains its owner, selection, unavailable IDs, fetch progress,
  observed Root/report, status, and API identity. Future work in
  `deployments/docs/plan/future.md` decides cross-user result isolation and
  multi-tenant reuse policy, not whether the current N:1 bridge exists.

## Resolved API And Recovery Decisions (2026-08-04)

- [x] **Ignore-failed failure state:** clear
  `failure_code=ANALYSIS_FETCH_FAILED` when `IGNORE_FAILED` atomically confirms
  the READY manifest and transitions to `ANALYZING`. The analysis is no longer
  blocked or failed. Preserve excluded failures in `failed_candidate_ids` and
  the recorded resolution action; `GET /analyses/{id}` thereby exposes manifest
  provenance without presenting an active failure on a running analysis.

- [x] **API error contract:** analysis endpoints return client errors as
  `{"code":"...","message":"..."}`; `code` is stable and `message` is safe
  for display but not a programmatic contract. Use `400 Bad Request` for
  malformed JSON (`ANALYSIS_INVALID_JSON`), missing/invalid UUIDv7 `analysis_id`
  (`ANALYSIS_ID_REQUIRED`), an empty candidate array
  (`ANALYSIS_INPUT_EMPTY`), malformed candidate UUID
  (`ANALYSIS_INPUT_INVALID_CANDIDATE_ID`), duplicate/nil candidate IDs
  (`ANALYSIS_INPUT_DUPLICATE_CANDIDATE`), too many candidates
  (`ANALYSIS_INPUT_LIMIT_EXCEEDED`), and invalid policy
  (`ANALYSIS_POLICY_INVALID`); `422 Unprocessable Content` for a syntactically
  valid non-empty selection with no available candidates
  (`ANALYSIS_INPUT_EMPTY`); `409 Conflict` for an existing `analysis_id` with a
  different normalized request (`ANALYSIS_REQUEST_CONFLICT`) or a stale failure
  resolution (`ANALYSIS_NOT_AWAITING_RESOLUTION`); `404 Not Found` for absent or
  foreign analysis/fetch resources; and `401 Unauthorized` for no principal.
  Unexpected failures return `500` with
  `{"code":"ANALYSIS_INTERNAL_ERROR","message":"internal server error"}`.
  Successful replay remains the documented `200` response and is not an error.

- [x] **Missing task handling:** distinguish a broken task link from a normal
  terminal fetch outcome. Reconciliation repairs an unfinished `fetch_item`
  only when its `task_id` is NULL, its task row is missing, or the row is not the
  expected PAGE_FETCH task for the candidate's canonical URL. In the same
  transaction it validates that URL, reuses or creates the shared active task,
  and updates the item task link. A linked PENDING/RUNNING task is healthy. For
  every terminal task state, reconciliation checks readable content first: any
  readable content makes the item READY even when the task says FAILED. Only a
  terminal FAILED task without readable content is an acquisition failure; a
  COMPLETED task without readable content is terminally FAILED as a
  collector-contract failure. Terminal failures use the normal
  STOP/IGNORE_FAILED policy rather than being repaired as dangling links. If a
  broken-link candidate/URL is no longer acquirable, reconciliation also makes
  the item terminally FAILED. The
  invariant is: every non-terminal item is either an already-complete readable
  content snapshot or references an existing PENDING/RUNNING PAGE_FETCH task;
  progress reads do not repair state and never leave a dangling item indefinitely
  PENDING/FETCHING.

## Resolved Trigger And Validation Decisions (2026-08-04)

- [x] **Dangling-task reachability:** `cmd/trigger/analysis` claims FETCHING
  runs that need either terminal reconciliation or repair. Its claim query uses
  `FOR UPDATE SKIP LOCKED` and selects a run when all fetch items are terminal
  **or** an unfinished item has a broken link: NULL/missing task ID, wrong task
  kind, or a task URL that differs from the candidate's canonical URL. Repair is
  performed before terminal-policy evaluation, so a missing task is never
  fabricated into a terminal failure merely to make the run discoverable. A
  linked FAILED task, or a linked COMPLETED task with no readable content, is
  instead evaluated as a genuine terminal acquisition failure.

- [x] **Periodic ownership:** `cmd/trigger/analysis` is a resource trigger that
  runs periodically, analogous to `cmd/trigger/batch`; it is not part of
  `cmd/scheduler`. The scheduler owns generic task claiming, retry counting, and
  dispatch to workers. The analysis trigger owns the domain transition from
  fetch acquisition to analysis because it must inspect `fetch_items`, analysis
  ownership/provenance, failure policy, and manifest/Root creation. The periodic
  trigger is sufficient; task-completion events are an optional future
  optimization, not a correctness dependency.

- [x] **Expired PAGE_FETCH cleanup:** `expires_at` is a hard acquisition
  deadline for a PAGE_FETCH task, not merely a scheduler filter. Every newly
  created PAGE_FETCH task receives a configured absolute deadline; joining a
  second fetch/analysis session may reuse that task but must not extend the
  deadline indefinitely. A global periodic task sweeper, owned alongside the
  scheduler rather than by a single analysis, claims expired PENDING/RUNNING
  PAGE_FETCH tasks with `FOR UPDATE SKIP LOCKED` and atomically sets
  `status=FAILED`, `failure_message=TASK_EXPIRED`, and `updated_at=NOW()`.
  `FAILED` remains the existing terminal task state: do not add an `EXPIRED`
  task-status enum value. A PAGE_FETCH worker persists content and completes its
  task in one transaction guarded by `status=RUNNING` and an unexpired deadline.
  If that guard fails, it rolls back content persistence; it must not write late
  content after a sweeper has won. The analysis reconciler then observes a
  normal failed fetch item and applies STOP or IGNORE_FAILED, while still
  treating any readable content from a pre-expiry winning transaction as READY.
  - Given a PAGE_FETCH task remains PENDING or RUNNING past `expires_at`.
  - When the global sweeper claims it.
  - Then exactly one sweeper marks it FAILED with `TASK_EXPIRED`; any late
    worker completion cannot overwrite that terminal status.
  - Given a worker loses the expiry race before writing its content.
  - When its guarded content-and-completion transaction evaluates the task.
  - Then it rolls back and writes no late content.

- [x] **Validation codes:** `ANALYSIS_INVALID_JSON`,
  `ANALYSIS_ID_REQUIRED`, `ANALYSIS_INPUT_EMPTY`,
  `ANALYSIS_INPUT_INVALID_CANDIDATE_ID`,
  `ANALYSIS_INPUT_DUPLICATE_CANDIDATE`, `ANALYSIS_INPUT_LIMIT_EXCEEDED`, and
  `ANALYSIS_POLICY_INVALID` are the complete stable codes for analysis-create
  and preflight validation. Clients branch on `code`, never `message`.

- [x] **Empty-selection status:** an empty `candidate_ids` array is `400
  ANALYSIS_INPUT_EMPTY`; it is a client request-construction error and must not
  reach availability lookup. A non-empty, syntactically valid selection whose
  candidates are all unavailable is `422 ANALYSIS_INPUT_EMPTY`; the request is
  valid but cannot be fulfilled against current server state. Sharing the code
  preserves one business concept while the HTTP status distinguishes invalid
  shape from unfulfillable input.

## Resolved Validation And Expiry Decisions (2026-08-04)

- [x] **Invalid candidate IDs:** `POST /analyses/preflight` and
  `POST /analyses` return `400 ANALYSIS_INPUT_INVALID_CANDIDATE_ID` for a
  candidate ID that is not a UUID, before candidate lookup. A nil UUID remains
  `400 ANALYSIS_INPUT_DUPLICATE_CANDIDATE`, because it is invalid selection
  identity rather than a parse failure.

- [x] **Selection limit:** both preflight and create accept at most 100 candidate
  IDs. More returns `400 ANALYSIS_INPUT_LIMIT_EXCEEDED` before availability
  lookup. A single shared constant supplies this limit to both handlers, keeping
  the preflight result usable by create.

- [x] **Expired-task completion race:** completion remains a conditional update:
  `UPDATE tasks ... WHERE id=$1 AND status='RUNNING' AND (expires_at IS NULL OR
  expires_at > clock_timestamp())`. The sweeper uses its own conditional update over
  PENDING/RUNNING rows where `expires_at <= clock_timestamp()`, both under row locking. If a
  worker wins before expiry it marks COMPLETED and the sweeper skips it; if the
  sweeper wins, its FAILED status makes the worker update affect zero rows and a
  late worker cannot resurrect the task. Content written before that losing
  completion attempt remains reusable; analysis reconciliation checks readable
  content before interpreting the terminal task status.

## Resolved Atomicity And Deadline Decisions (2026-08-04)

- [x] **Atomic-confirmation scenario:** a clean-database deployment never
  commits a confirmed manifest without its execution bridge, Root/P0, observed
  Root, and ANALYZING transition. The disappearance test therefore covers the final
  revalidation before the atomic transaction writes anything; it is not a
  legacy partial-row recovery scenario.

- [x] **Expiry implementation scope:** expired PAGE_FETCH cleanup is required
  for this feature, not deferred scheduler work. Without it, the existing
  scheduler's expired-task filter can leave a fetch item indefinitely
  PENDING/RUNNING and prevent analysis reconciliation. The explicit checklist
  and integration coverage above are merge requirements.

- [x] **Task deadline contract:** PAGE_FETCH alone has the configurable
  `page-fetch-deadline`, defaulting to `2h`. It is an absolute deadline assigned
  at acquisition-work creation and is not extended by later sessions joining a
  shared task. Every `RETRY_FAILED` creates or attaches new active acquisition
  work with a fresh deadline of `clock_timestamp() + page-fetch-deadline`; no other task kind
  receives this deadline policy.

## Resolved Content And Deadline Consistency Decisions (2026-08-05)

- [x] **Terminal-task/content precedence:** readable content always wins for a
  fetch item. Reconciliation checks it before interpreting every terminal task
  state, not just `TASK_EXPIRED`. A task only represents an acquisition failure
  when it is terminal and no readable content exists for the selected candidate.

- [x] **Late content after expiry:** forbid it rather than relying on a second
  wake-up. PAGE_FETCH content persistence and task completion occur in one
  transaction whose initial row lock requires `status=RUNNING` and
  `expires_at > clock_timestamp()`. A sweeper that commits FAILED first causes the worker
  transaction to roll back; a worker that commits first makes the sweeper skip
  the completed task. This prevents a terminal analysis from becoming stale due
  to unobserved late content.

- [x] **NULL `expires_at`:** PAGE_FETCH tasks must have a non-NULL deadline.
  Add a database CHECK constraint equivalent to
  `kind <> 'PAGE_FETCH' OR expires_at IS NOT NULL`, and have every PAGE_FETCH
  creation/retry path supply `clock_timestamp() + page-fetch-deadline`. Other task kinds may
  retain NULL expiry. This clean-database deployment needs no backfill; the
  completion query may retain its NULL branch only for those other task kinds.

## Resolved Deadline Clock Decision (2026-08-05)

- [x] **Deadline clock semantics:** use PostgreSQL `clock_timestamp()` for every
  PAGE_FETCH deadline assignment, completion guard, and sweeper predicate;
  never use transaction-stable `NOW()`/`CURRENT_TIMESTAMP` for this purpose.
  A worker performs network I/O outside the final database transaction. That
  short transaction locks the task row, verifies RUNNING and
  `expires_at > clock_timestamp()`, writes content, and conditionally completes
  the task; a zero-row completion rolls the transaction back. The hard-deadline
  boundary is this locked terminalization decision, rather than the physical
  commit instant a few instructions later. A deferred commit-time trigger adds
  complexity without meaningful protection once that row lock prevents a
  competing sweeper update.

## Resolved Candidate Identity And Root Completion Decisions (2026-08-05)

- [x] **Same URL, different candidates:** treat this as duplicate discovery, not
  two distinct current articles. The current data model permits only one
  candidate per canonical URL, regardless of source title or publication
  metadata. Analysis therefore has one candidate and one content per selected
  canonical URL; shared PAGE_FETCH reuse is between sessions selecting that same
  candidate. The sole exception is the explicitly deferred immutable version
  chain, where predecessor/successor records intentionally share a URL but only
  the current version is selectable.

- [x] **Root completion discovery:** widen
  `FindFinishedPipelineRootBatches` from `parent_id IS NOT NULL` to the
  equivalent of `parent_id IS NOT NULL OR analysis_execution_id IS NOT NULL`.
  On an analysis Root success, the winning terminalization transaction creates
  the unique report cache entry and updates every ANALYZING request whose
  `observed_root_batch_id` is that Root. On failure it updates only those
  requests with the Root failure. This preserves the existing finisher as the
  sole completion authority, makes retries idempotent through `completed_at IS
  NULL`, and does not select unrelated parentless Roots.

## Resolved Execution Persistence Decisions (2026-08-05)

- [x] **Execution data contract:** `analysis_runs` is the request/provenance
  row; `analysis_executions` is the ownerless immutable bridge keyed by unique
  report fingerprint; and `batches` stores parentless Root history through
  `analysis_execution_id`. `analysis_runs.execution_id` is the N:1 bridge link,
  while each request's observed Root/report/status/failure is its own source of
  truth for `GET /analyses`. `reports` is a success-only unique cache keyed by
  report fingerprint and references the successful Root/report artifact. The
  atomic attach operation and concurrency scenario above are required
  implementation and integration-test coverage.

 - [ ] **Failed Root rerun:** a request observing a FAILED Root retains that
   failure and is never silently rewritten. For an eligible failure kind, explicit
   rerun checks the report cache, attaches to an existing active Root, or creates
   the next UUIDv7 Root under the same immutable execution bridge. Requests that
   still observe the older Root remain FAILED. If the new Root succeeds, its
   report is cached; a later explicit rerun may adopt that report for only its
   caller's request.

## Resolved Follow-up Decisions (2026-08-05)

1. **Active scope:** report generation and durable `report.md` storage are part
   of this feature. A Root is not successful analysis output until its report
   stage has produced and linked `reports/<execution-id>.md`. Schema adds
   execution bridges, Root history, and reports; the implementation checklist and
   storage-failure recovery tests are merge requirements.

2. **Identity chain:** client `analysis_id` is UUIDv7 and the request primary
   key/idempotency identity. `request_fingerprint` hashes authenticated owner,
   normalized request text/policy, and original selected IDs. `report_fingerprint`
   hashes canonical READY content IDs plus every output-affecting setting.
   `analysis_executions.id` is UUIDv5 of the versioned report fingerprint; Root
   and report IDs are UUIDv7. Insert request/fetch state first; after manifest
   confirmation, insert-or-lock the execution bridge on its primary key, lock it,
   then attach to an active Root or insert the first Root. The execution primary
   key is the concurrent bridge-creation winner; `reports.report_fingerprint`
   is the concurrent successful-cache winner.

3. **Analysis Root runtime:** add a narrow analysis-root repository operation;
   do not weaken generic `CreatePipelineRoot` collection-parent assumptions. It
   locks the execution bridge, queries `batches` for an active Root, then creates
   a parentless UUIDv7 Root/P0 with `analysis_execution_id` when none exists.
   After an ambiguous commit it repeats that locked lookup and attaches to the
   committed Root rather than creating another.

4. **Report artifact transaction:** superseded by “Report Artifact Delivery
   Contract”; report delivery uses a deterministic direct write plus DB-link
   recovery, not a PENDING row or transactional outbox.

5. **Report producer:** a report-generation task is the final stage of the
   analysis Root. The current embedding-only definition is not a completed user
   analysis and cannot create a report. The report task owns rendering, direct
   deterministic storage write, and DB-link upsert; its terminal failure fails
   the Root, while successful embedding assets
   remain reusable by a report-task retry.

6. **Rerun eligibility:** store `failure_kind` on the terminal analysis Root
   batch. Only `PROVIDER_TRANSIENT` and `INFRA_TRANSIENT` are retryable; every
   other listed kind is non-retryable until an operator changes the underlying
   input/configuration. The owner calls `POST /analyses/{id}/rerun`; a one-minute
   cooldown is enforced from the failed Root terminal timestamp. The action locks
   the request and bridge, checks READY report then active Root, and creates the
   next Root only when eligible.

7. **Ownership principal:** `middleware.Principal.TokenID` is the stable owner
   identity for the current internal deployment. Token rotation must retain that
   token ID; revocation blocks future authentication but does not rewrite stored
   ownership. A separate users/principals table and multi-token identity are
   future multi-tenant work.

8. **PAGE_FETCH after discovery completion:** retain the candidate discovery
   batch on task rows, but allow PAGE_FETCH create/recover against a completed
   batch without reopening it. Active canonical-URL lookup occurs before any
   completed-batch guard; a new PAGE_FETCH is also permitted as an append-only
   task on that completed batch. Batch completion queries stay guarded by
   `completed_at IS NULL`, so this cannot republish discovery completion.

9. **Soft-delete/reacquisition:** soft-deleted content is unreadable. Before a
   Root snapshot it is not restored or overwritten; the candidate is unavailable
   for acquisition and the request follows normal failed-input policy. After a
   Root snapshot, stages consume the snapshot. Immutable replacement content is
   deferred to the article-versioning phase.

10. **Canonical URL migration:** this clean-database deployment adds persisted
    `candidates.canonical_url`, derives it before candidate persistence, and
    enforces one current candidate per canonical URL. Raw URL remains source
    provenance. Tasks and contents use canonical URL. A canonical conflict always
    returns the existing current candidate; there is no legacy-row migration.

11. **Resolution history:** add append-only `analysis_resolution_events` with
    request ID, action, actor token ID, timestamp, prior/current failed IDs,
    affected task IDs, and retry Root ID when created. It is operator/audit data,
    not part of the default user response; users see current request provenance
    and status only.

12. **Reconciliation claim:** `cmd/trigger/analysis` claims eligible FETCHING
    requests with `SELECT ... FOR UPDATE SKIP LOCKED` and holds that lock through
    broken-link repair, policy evaluation, manifest confirmation, bridge lock,
    and Root/P0 attachment/creation. Shared task state is reread under the
    transaction before transition; a concurrent resolution request blocks, then
    receives `409 ANALYSIS_NOT_AWAITING_RESOLUTION` if it lost the conditional
    state transition.

13. **Expiry worker API:** add narrow repository method `CompletePageFetch` that
    locks the RUNNING unexpired task, persists readable content, and completes the
    task in one transaction. Existing readable content completes the task without
    another insert; soft-deleted content is unreadable and follows the
    reacquisition failure rule. A failed expiry guard rolls back all writes.

14. **Fetch progress:** the only groups are PENDING, FETCHING, READY, and FAILED;
    READY is the sole terminal-success group. `GET /fetches/{id}` validates owner
    before any cache lookup and cache keys include owner identity. Analysis
    reconciliation changes no fetch state, so it does not invalidate fetch
    progress cache entries.

15. **Unavailable selections:** mixed availability is accepted. Original selected
    IDs, including unavailable IDs, participate in `request_fingerprint` and are
    preserved as provenance; fetch items exist only for available IDs. A nonempty
    all-unavailable selection returns `422 ANALYSIS_INPUT_EMPTY`; an empty array
    returns `400 ANALYSIS_INPUT_EMPTY` before availability lookup.

16. **Request transitions:** FETCHING -> AWAITING_RESOLUTION (STOP failures),
    FAILED (no READY input or missing confirmed input), or ANALYZING (active/new
    Root); AWAITING_RESOLUTION -> FETCHING (RETRY_FAILED), ANALYZING
    (IGNORE_FAILED with READY input), or FAILED (no READY input); ANALYZING ->
    COMPLETED/FAILED only through the Root finisher; FAILED -> ANALYZING or
    COMPLETED only through explicit eligible rerun/cache adoption; COMPLETED has
    no automatic outgoing transition. SQL conditional predicates enforce every
    source state and `observed_root_batch_id` match.

17. **Report authorization:** current internal policy permits an owner to obtain
    a shared READY report only through that owner's newly created/re-run analysis
    request with the same report fingerprint. There is no public direct report-ID
    endpoint. Artifact retrieval authorizes the owning request's `report_id`;
    cross-tenant report visibility is future work.

18. **Report persistence failure:** report generation/direct delivery is the
    final Root stage. Until the deterministic artifact and DB link exist the Root remains running; an
    exhausted final-stage failure marks Root/request FAILED with
    `ANALYSIS_REPORT_FAILED`. Retrying that final task reuses prior embedding
    assets and does not rerun successful embedding work.

19. **Acceptance gates:** every `[ ]` item requires a directly named automated
    Given/When/Then test before it becomes `[x]`. Mandatory merge gates are
    Postgres integration tests for rollback, owner isolation, active-Root race,
    rerun history, and report-cache uniqueness; storage-failure recovery tests;
    focused API tests; then the full short suite and lint. The existing 895-test
    short suite alone is insufficient.

## Superseded Follow-up Questions (2026-08-05)

The following historical questions include the superseded outbox/PENDING-report
design. “Report Artifact Delivery Contract” is authoritative for report delivery;
no outbox or PENDING report state is to be implemented from this section.

1. **Who owns Root terminalization when report delivery is asynchronous?** A
   PENDING `reports` row and outbox record are created before an outbox worker
   renders/uploads `report.md`, while the Root must remain running until the
   report is READY. Does the report task remain RUNNING until the outbox worker
   completes, or may the pipeline worker complete it earlier? What exact lock
   prevents `FindFinishedPipelineRootBatches` from marking the Root before the
   report is READY?

2. **What is the `reports` state model?** The design calls `reports` a
   success-only cache but also creates a PENDING row before delivery. Should
   `reports` have `PENDING`, `READY`, and terminal failure states, or should
   pending delivery use a separate report-attempt table? Which unique indexes
   apply to PENDING rows, and how is a stale PENDING row recovered after an
   outbox worker crashes?

3. **How does `ANALYSIS_REPORT_FAILED` fit the public failure taxonomy?** The
   persisted failure-code list currently ends at `ANALYSIS_PIPELINE_FAILED`, but
   the resolved report decisions introduce `ANALYSIS_REPORT_FAILED`. Is report
   delivery failure a distinct public code, an internal `failure_kind` under
   `ANALYSIS_PIPELINE_FAILED`, or both? What does `GET /analyses/{id}` return
   after report retries are exhausted?

4. **What is the exact rerun request contract?** The answer names
   `POST /analyses/{id}/rerun`, but does not define its body, response status,
   idempotency identity, or behavior when two rerun requests arrive during the
   one-minute cooldown. Should repeated requests return the same active Root,
   `409`, or a separate rerun operation ID? How is a rerun represented in
   `analysis_resolution_events`?

5. **Which timestamp and clock enforce the rerun cooldown?** Is the one-minute
   boundary based on `batches.completed_at`, a separate `failed_at`, or the time
   `failure_kind` is recorded? Must the comparison use PostgreSQL
   `clock_timestamp()` under the request/execution lock so concurrent reruns
   cannot both pass the check?

6. **How is `failure_kind` classified and made stable?** The taxonomy lists
   provider, infrastructure, configuration, model-output, snapshot, policy,
   and unknown failures, but no mapping exists from worker/task errors to those
   kinds. Which component classifies an error, what happens when multiple Root
   tasks fail with different kinds, and can an operator correct the
   classification without mutating an immutable Root history row?

7. **What happens to the execution fingerprint when a rerun changes deployed
   configuration?** `report_fingerprint` includes every output-affecting setting,
   while an eligible rerun creates the next Root under the same immutable
   execution bridge. Must rerun freeze the original pipeline/prompt/model
   versions, or should changed settings create a new bridge and fingerprint?

8. **Does `report_fingerprint` need candidate snapshot identity as well as
   content IDs?** The Root snapshots candidates and contents and the pipeline
   includes `EMBED_CANDIDATE`. If candidate title/metadata can affect report
   output, two requests with the same `ready_content_ids` can produce different
   results. Should the fingerprint include candidate IDs or a hash of all
   output-affecting candidate snapshot fields?

9. **How are cross-table execution associations enforced by PostgreSQL?** The
   planned schema separately adds `analysis_runs.execution_id`,
   `observed_root_batch_id`, `report_id`, `batches.analysis_execution_id`, and
   report foreign keys. What prevents a request from referencing a Root or
   report belonging to another execution bridge? Are composite or deferred
   foreign keys required, or is this repository-only? Add the negative test for
   a mismatched link.

10. **How does PAGE_FETCH append work to a completed discovery batch without
    violating `n_subtasks`?** The decision allows a new task on a completed
    batch, but the current repository also rejects inserts when task count
    reaches the batch's declared `n_subtasks`. Should user PAGE_FETCH tasks
    bypass that count, should `n_subtasks` be discovery-only, or should user
    work use another batch association?

11. **At what point is soft-deleted content classified as unavailable?** The
    decision says soft-deleted content is unavailable before Root snapshot, while
    preflight availability checks candidate existence and URL normalization.
    Should preflight inspect readable content and return `UNAVAILABLE`, or may
    preflight report available and let creation fail through fetch policy? Which
    failure/provenance field records the reason?

12. **What is the migration sequence for canonical URLs in a clean database?**
    The decision adds `candidates.canonical_url` and canonical uniqueness, but
    the initial migration still creates raw `candidates.url` and `contents.url`
    uniqueness. Which migration changes task/content uniqueness and discovery
    upsert order before new rows can be inserted? Must `schema.sql`, SQLC output,
    and fresh-database migration tests change together?

13. **What is the exact `CompletePageFetch` contract for malformed task
    metadata?** Existing PAGE_FETCH rows encode `candidate_id` in JSON metadata,
    but a task may be URL-only or have invalid metadata. Does the method require
    a candidate ID, resolve by canonical URL, or reject the task? How does it
    preserve one candidate/content identity when the URL already has readable
    content for another candidate?

14. **How are automatic resolution events attributed?**
    `analysis_resolution_events` stores an actor token ID, but reconciliation,
    expiry sweeping, and report outbox delivery are system actions without a
    user token. Should actor identity support a typed system actor, nullable
    actor, and component name? Which automatic actions are events versus only
    task history?

15. **What is the report retrieval endpoint despite no direct report-ID route?**
    The answer says artifact retrieval authorizes the owning request's
    `report_id`, but the API checklist only defines `GET /analyses/{id}`. Is
    content embedded in that response, streamed through
    `GET /analyses/{id}/report`, or retrieved through an internal signed storage
    reference? Define content type, size/range limits, and authorization.

16. **How does a failed request adopt a successful report without losing Root
    history?** The state matrix permits `FAILED -> COMPLETED` through explicit
    cache adoption, while requests retain an observed Root/report. Does adoption
    replace `observed_root_batch_id` and `report_id`, or retain the failed Root
    and add an adopted-report link? What audit event distinguishes adoption from
    a Root that originally completed for the request?

17. **What happens when a READY report already exists while a new request is
    still acquiring?** May the request attach to the report before its own fetch
    session reaches a confirmed manifest, or must it finish and persist its own
    READY manifest first? If early attachment is allowed, which selected and
    unavailable provenance is retained, and what prevents cache lookup from
    bypassing input validation?

18. **Which constraints make report-cache races idempotent?** When two outbox
    workers upload the same content-addressed report concurrently, which row
    wins and how are the losing artifact and outbox attempt cleaned up? When a
    report row is PENDING and an explicit rerun arrives, does rerun attach to
    that delivery, create another Root, or wait? Add the unique indexes and
    concurrent integration tests.

19. **What is the boundary between Root failure and report-delivery failure in
    the finisher?** The finisher is currently the Root terminalization authority,
    but the outbox worker may discover final failure later. Does the finisher
    mark the Root only after report READY, or can the outbox worker perform a
    second conditional Root terminalization? Which transaction updates
    `analysis_runs`, `reports`, outbox state, and `failure_kind` together?

20. **Which clean-database tests are mandatory before keeping these `[x]`
    decisions?** Please name tests for report render/upload failure, outbox
    recovery, duplicate report upload, rerun cooldown race, changed pipeline
    fingerprint, mismatched execution foreign keys, completed-batch PAGE_FETCH
    insertion, and cross-owner report access. Are these integration/storage
    tests hard merge gates alongside the existing short suite and lint?

## Report Artifact Delivery Contract (2026-08-05)

This section supersedes every earlier outbox, PENDING-report, content-addressed
upload, and directory-scan proposal in this document.

- [ ] **Deterministic artifact identity:** each immutable execution bridge has
  exactly one report artifact URI: `reports/<execution-id>.md`. The path never
  contains a request ID because equivalent requests share the same execution and
  report. `reports` stores that execution ID, Root ID, URI, byte size, and
  SHA-256 hash; only a successfully linked artifact is a report cache hit.

- [ ] **Direct delivery protocol:** the final report pipeline task renders the
  Markdown, writes `reports/<execution-id>.md`, verifies the written bytes/hash,
  upserts the `reports` DB link, then completes its task. The Root can terminally
  succeed only after this task completes. A retry uses the same deterministic URI
  and accepts an existing object only when its hash matches; it otherwise fails
  rather than overwriting an unexpected artifact.

- [ ] **Exceptional recovery:** normal request/report reads query PostgreSQL only
  and never `Stat`, `HEAD`, or list storage. If a crash or completion-signal/DB
  link failure leaves a successful report task/Root without a `reports` row, a
  low-frequency recovery worker selects only those unresolved database rows,
  derives the one expected URI, and performs a bounded `Stat`/`HEAD`. A matching
  artifact restores the DB link; a missing artifact returns the final report task
  to retry. It never scans the `reports/` directory, and uses exponential backoff
  with a bounded retry count.

- [ ] **Failure and access:** write/render failure keeps the report task
  incomplete for ordinary retries. Exhaustion fails the Root and its observing
  requests with public `ANALYSIS_REPORT_FAILED`; prior embedding assets remain
  reusable by the report-task retry. Report retrieval is only through an
  owner-authorized analysis request that references the report; there is no public
  report-ID endpoint.

- [ ] **Artifact availability and administrative removal:** `reports.expires_at`
  is the application cache expiry and must be earlier than the configured
  S3/SeaweedFS lifecycle deletion time. At/after `expires_at`, reads do not
  contact storage and return `410 ANALYSIS_RESULT_EXPIRED`. Before that expiry,
  storage `Get` NotFound is unexpected: record `artifact_missing_at`, exclude the
  row from cache hits, and return `410 ANALYSIS_RESULT_ARTIFACT_MISSING`. A
  managed eviction uses `DELETE /admin/reports/{execution_id}` with an operator
  reason; it derives `reports/<execution-id>.md`, never accepts an arbitrary URI,
  and records `artifact_removed_at`, `artifact_removed_by`, and
  `artifact_removal_reason`. Reads of an administratively removed report return
  `410 ANALYSIS_RESULT_REMOVED_BY_ADMIN`, not an expiry response. All three
  responses may expose the request-local rerun URL; none automatically starts a
  new Root during a read.

- [ ] **Required tests:** storage tests must cover write-before-link crash
  recovery, DB-link failure recovery, duplicate deterministic write with matching
  and mismatching hashes, no storage lookup after expected expiry, unexpected
  pre-expiry artifact loss, administrative removal response, and bounded recovery
  retry/backoff. These are mandatory merge gates with the existing Root, rerun,
  and report-cache integration tests.

## Questions After Report Artifact Delivery Contract (2026-08-05)

The direct-delivery contract supersedes the earlier outbox proposal, but several
implementation and document-boundary questions remain. Please answer these
before marking the report-related decisions complete.

1. **Which report-delivery design is authoritative?** “Resolved Follow-up
   Decisions” still says the feature adds a report-delivery outbox and creates a
   PENDING report row, while “Report Artifact Delivery Contract” explicitly
   forbids both. Should the earlier statements be rewritten as superseded
   rather than merely described as historical questions, and should all
   outbox/PENDING references be removed from the implementation checklist?

2. **What transaction contains the DB link and final report-task completion?**
   The direct protocol writes storage, upserts the `reports` link, and then
   completes the final task. Are the link upsert and task completion one
   PostgreSQL transaction? If the process crashes after the link but before task
   completion, what exact retry query observes the existing link and completes
   the task without duplicating or changing the artifact?

3. **How can recovery detect a missing link without an unresolved-row table?**
   Recovery is described as selecting successful report tasks/Roots that lack a
   `reports` row, but no `reports` row exists to mark as unresolved. What is the
   exact SQL predicate joining Root batches, final report tasks, and reports, and
   how does it distinguish an expected in-progress Root from a crashed
   post-storage/pre-link attempt?

4. **How does missing-artifact recovery preserve immutable Root history?** The
   contract says a successful Root/report task with a missing artifact returns
   the final report task to retry, while Root rows are immutable once terminal.
   Can a task in a terminal Root be reopened? If not, must the Root remain
   FAILED, must a new Root attempt be created, or is the Root terminalization
   query required to prove the artifact exists before setting `succeeded=true`?

5. **What happens when the deterministic URI contains an unexpected hash?** The
   contract says a matching existing object is accepted and a mismatching object
   fails rather than being overwritten. Is this a permanent
   `ANALYSIS_REPORT_FAILED`/`failure_kind` outcome requiring operator cleanup,
   or can the report task retry? How is an operator allowed to quarantine or
   replace a corrupted object without violating immutable Root/report history?

6. **Is report rendering deterministic across Root retries?** Every execution
   bridge has one URI, but Root retries may call a nondeterministic provider and
   produce different Markdown bytes under the same fingerprint. Should report
   generation pin temperature/model seed and template bytes, include a render
   version in `report_fingerprint`, or treat a hash mismatch from a retry as a
   hard failure?

7. **How is conditional object creation implemented by `storage.Store`?** The
   current interface exposes `Put`, `Get`, `List`, and `Delete`, but no
   `Stat`/`HEAD` or create-if-absent operation. How does direct delivery avoid
   two workers overwriting the same deterministic URI, and how does recovery
   perform a bounded metadata/hash check without downloading an unbounded
   artifact or scanning the storage prefix?

8. **What are the artifact byte and metadata rules?** Define the exact UTF-8
   rendering, newline normalization, content type, byte-size calculation, and
   SHA-256 input. Does the stored hash cover the exact bytes returned to the
   owner, and are maximum report size and storage key validation enforced before
   `Put`?

9. **What prevents a Root finisher from succeeding before the report link
   exists?** The current finisher decides Root completion from terminal task
   counts. Is the final report task the only gate, or must
   `FindFinishedPipelineRootBatches` also require a matching READY `reports` row?
   Which conditional update is the single winner when the finisher and report
   recovery run concurrently?

10. **What is the final-task retry and expiry policy?** PAGE_FETCH has a defined
    deadline, but the report-generation `PIPELINE_STAGE` task uses different
    scheduler/retry semantics. What retry limit, timeout, failure message, and
    `failure_kind` apply to render, storage Put, DB-link, and task-completion
    failures? Can report delivery retry independently after embeddings succeed?

11. **What exactly is a “normal report read”?** The contract says normal
    request/report reads query PostgreSQL only and never storage `Stat`, `HEAD`,
    or list operations, but returning `report.md` content necessarily requires
    storage `Get` unless the API returns metadata only. Is `GET /analyses/{id}` a
    metadata response and a separate owner-authorized content endpoint performs
    `Get`, or is report content embedded elsewhere?

12. **How are report links associated with executions and Roots?** `reports`
    stores execution ID and Root ID, while requests store `execution_id`,
    `observed_root_batch_id`, and `report_id`. What composite uniqueness or
    foreign-key constraints guarantee that the report Root belongs to the same
    execution and that the observed Root is the report's successful Root?

13. **What is the report-cache behavior during an active failed/retrying
    execution?** If Root R1 fails before rendering, R2 is active under the same
    execution, and a third request arrives, does it attach to R2 immediately?
    If a stale successful `reports` link exists from a prior attempt, does the
    cache always win, and how does explicit rerun distinguish cache adoption from
    creating a new Root?

14. **How are report delivery failures exposed in request transitions?** The
    state matrix says `ANALYZING -> FAILED`, while report delivery says Root
    remains running until READY. At what point is the request status changed,
    which `observed_root_batch_id` is retained, and how are `failure_kind` and
    `ANALYSIS_REPORT_FAILED` persisted if the Root never reaches successful
    terminalization?

15. **How does report retrieval authorization work with shared reports?** A
    report is keyed by execution fingerprint and may be reused by a different
    owner through a new analysis request, but artifact reads authorize the
    request's `report_id`. Must the repository verify the report's execution
    fingerprint matches the request's execution ID on every read, and what is
    the response for a valid owner whose report link is missing or whose object
    hash no longer matches?

16. **What system actor is recorded for direct report repair?** The resolution
    event design stores actor token IDs, but direct report recovery, hash
    verification, and operator cleanup are not user resolution actions. Should
    these be separate report-recovery events with component/attempt IDs, and
    what audit record is required when an object is quarantined?

17. **Which migration and generated-code order is required?** The new contract
    needs `analysis_executions`, `reports`, `analysis_resolution_events`, Root
    execution linkage, report failure fields, and likely storage metadata. What
    is the ordered up/down migration plan that avoids circular foreign keys, and
    when must SQLC and mocks be regenerated? Add a fresh-database migration test
    that exercises the exact order.

18. **Which direct-delivery crash tests are mandatory?** In addition to the
    listed write-before-link and DB-link failures, please specify tests for
    crash after task completion, finisher/recovery races, missing artifact after
    Root success, unexpected hash, concurrent conditional writes, report-size
    limit, and owner-authorized retrieval. Which persisted rows must exist or
    not exist in every failure case?

## Resolved Direct-Delivery Details (2026-08-05)

This section answers and supersedes report-delivery questions above.

1. **Authoritative design:** deterministic direct delivery is authoritative.
   Remove outbox, PENDING report, report-attempt, and content-addressed-upload
   work from active implementation. `reports` has only a READY row, created after
   a verified deterministic artifact write.

2. **Final task transaction:** storage write occurs first. The report worker then
   runs one PostgreSQL transaction that upserts the READY report link and marks
   its final pipeline task COMPLETED. A retry after link-before-completion finds
   the matching link/hash and completes the task without another write.

3. **Root gate and recovery:** the final report task is the only Root completion
   gate. Since that task cannot complete without its report link, a normal Root
   success without `reports` is impossible. Recovery selects nonterminal Roots
   whose final report task is not completed, derives `reports/<execution-id>.md`,
   and performs one bounded `Stat`/`HEAD`; it never lists a prefix. A terminal
   successful Root missing its object/link is data corruption: alert operators;
   do not reopen immutable Root history.

4. **Object integrity:** extend storage with conditional `PutIfAbsent` and
   bounded `Stat` metadata (size, content type, SHA-256 or equivalent ETag plus
   verified hash). Existing matching bytes are idempotent success. A mismatching
   object is permanent `ANALYSIS_REPORT_FAILED`, records a report-recovery audit
   event, and requires operator quarantine/cleanup before a new Root rerun.

5. **Rendering contract:** report task uses UTF-8 Markdown, LF line endings,
   `text/markdown; charset=utf-8`, maximum 1 MiB, and SHA-256 over the exact
   stored/returned bytes. `report_fingerprint` includes candidate snapshot IDs
   and hashes of all output-affecting candidate fields, content IDs, prompt
   template/version, model configuration, and deterministic render settings.
   Report generation pins provider/model/template settings; a configuration
   change creates a new report fingerprint/execution bridge, never a rerun under
   the old bridge.

6. **Rerun contract:** owner calls `POST /analyses/{id}/rerun` with an empty body.
   Under request and execution locks, it returns `200` and adopts a READY report,
   returns `202` and attaches to an active Root, or returns `202` after creating
   the next eligible Root. During cooldown it returns `409 ANALYSIS_RERUN_COOLDOWN`.
   Cooldown is `batches.completed_at + 1 minute`, checked with
   `clock_timestamp()` under those locks. Every action records an
   `analysis_resolution_events` row; automatic actions use typed system actors
   (`component`, nullable token ID).

7. **Failure classification:** the worker maps typed provider/storage/configuration
   errors to one Root `failure_kind`; when multiple tasks fail, the first terminal
   Root-causative failure wins and later failures remain task history. Operators
   append a correction event rather than mutate the Root; only PROVIDER_TRANSIENT
   and INFRA_TRANSIENT permit rerun.

8. **Cross-table invariants:** add `UNIQUE (analysis_execution_id, id)` to Root
   batches and use composite foreign keys so request observed Root and report Root
   share the request/report execution bridge. Negative integration tests must
   reject mismatched request/Root/report associations.

9. **Remaining operational boundaries:** completed discovery batches accept
   append-only PAGE_FETCH tasks outside `n_subtasks`; preflight does not require
   readable content, but creation records soft-deleted content as unavailable
   acquisition provenance; malformed PAGE_FETCH metadata fails the task rather
   than guessing another candidate. `GET /analyses/{id}` is metadata-only;
   owner-authorized `GET /analyses/{id}/report` streams at most 1 MiB Markdown
   after verifying request/report/execution association and stored hash.

10. **Required tests:** fresh migration, bridge/active-Root race, rerun cooldown,
     report write-before-link, link-before-task-completion, final-task/finisher
     race, conditional-write match/mismatch, size limit, no normal-read storage
     metadata call, mismatched composite link, completed-batch PAGE_FETCH insert,
     and cross-owner report read are mandatory integration/storage merge gates.

## Questions After Resolved Direct-Delivery Details - Initial Follow-up (2026-08-05)

The direct-delivery details answer the previous report questions, but the
following implementation boundaries are still unspecified. Please answer them
with concrete DDL, repository methods, worker ownership, response contracts, and
test names where applicable.

1. **Which document sections must be rewritten as superseded?** The active
   implementation checklist and “Resolved Follow-up Decisions” still contain
   report outbox/PENDING-report language, while the authoritative contract says
   those mechanisms must not be implemented. Should those sections be edited
   now, or is a later agent expected to interpret the direct-delivery section as
   an override? Which `[x]` claims are invalid until the text is reconciled?

2. **What task kind and worker own report generation?** The current task enum and
   pipeline definition expose generic `PIPELINE_STAGE` but no report-specific
   task. Should report rendering be a `PIPELINE_STAGE` payload, a new task kind,
   or a final stage handled by the existing analyzer worker? Which worker writes
   the artifact, performs `PutIfAbsent`, upserts `reports`, and completes the
   task, and how is its retry policy configured?

3. **What are the exact `PutIfAbsent` and `Stat` semantics for local and S3
   stores?** Define whether `PutIfAbsent` returns an existing-object result or a
   conflict, whether the object is ever overwritten, and how a bounded `Stat`
   obtains a verified SHA-256 for multipart/encrypted S3 objects. What interface
   and fake-store contract must be added so matching and mismatching hash races
   behave identically in tests and production?

4. **What is the complete link/task transaction and retry predicate?** The
   report worker stores the artifact, then upserts the READY report link and
   completes the final task in one PostgreSQL transaction. Specify the SQL
   `UPDATE ... WHERE` conditions, including task ID, Root ID, task status, and
   report execution ID. What happens if the task is already COMPLETED, the
   report link exists with another Root ID, or the request has already observed a
   different Root?

5. **How is recovery claimed and synchronized with the report worker?** Recovery
   selects nonterminal Roots whose final task is incomplete and performs `Stat`.
   Does it lock the final task row with `FOR UPDATE SKIP LOCKED`, acquire the
   execution bridge lock, or use a recovery-attempt row? How does it avoid
   declaring a currently-running report write missing while the report worker is
   between storage Put and the database transaction?

6. **What is the retry state after recovery finds no artifact?** The contract
   says a missing artifact returns the final report task to retry, but it also
   requires bounded retries and immutable Root history. Which row stores recovery
   attempt count/backoff, does retrying increment the normal task retry count,
   and what exact update makes an exhausted recovery fail the Root with
   `ANALYSIS_REPORT_FAILED`?

7. **What is the authoritative Root-finish predicate?** The final report task is
   the only Root gate, but `FindFinishedPipelineRootBatches` currently counts
   terminal tasks and `MarkRootBatchFinished` marks the batch separately. Must
   the query require a READY `reports` row, or is a completed final task plus a
   report-link invariant sufficient? Which transaction wins if finisher and
   recovery both attempt terminalization?

8. **What exact DDL enforces one report per execution and valid Root history?**
   The answer proposes `UNIQUE (analysis_execution_id, id)` on Root batches and
   composite foreign keys, but does not specify uniqueness for
   `reports.analysis_execution_id`, `reports.root_batch_id`, or the report URI.
   Please provide the full constraints, including NULL behavior and the check
   that `analysis_execution_id` is allowed only on `ANALYZER_PIPELINE_ROOT`
   batches.

9. **How do composite foreign keys behave for nullable request fields?**
   `analysis_runs.execution_id`, `observed_root_batch_id`, and `report_id` are
   nullable during FETCHING and pre-Root states. Should the schema use
   `MATCH FULL`, paired NULL checks, or deferred composite FKs so a request
   cannot have an observed Root without an execution bridge while still
   permitting pre-Root rows? Add insert/update negative cases for partial NULL
   pairs.

10. **How is report content hash verified on the streaming endpoint?**
    `GET /analyses/{id}/report` streams at most 1 MiB, while the stored SHA-256
    covers exact bytes. Does the handler read and hash the whole object before
    writing the response, stream through a hashing writer and risk a late
    mismatch, or trust storage metadata after a bounded `Stat`? What status is
    returned if the object is truncated or its hash mismatches after headers are
    sent?

11. **What are the report endpoint response and failure semantics?** Define
    `Content-Type`, `Content-Length`, `Content-Disposition`, range support, and
    behavior for a missing `reports` row, missing artifact, hash mismatch,
    storage timeout, or owner/request association mismatch. Does a report read
    failure mutate database state or only emit an audit/metric event?

12. **What exactly changes during READY-report adoption?** The rerun contract
    returns `200` when adopting a READY report, but does not state whether it
    updates `observed_root_batch_id`, `report_id`, status, `updated_at`, and
    request history in one transaction. Which Root is observed after adoption,
    and how does the API preserve the previous FAILED Root while exposing the
    newly adopted COMPLETED result?

13. **How are rerun responses made idempotent?** Repeated reruns after the first
    request creates an active Root should return the same active Root response,
    not create another Root. Is the original `analysis_id` enough as the rerun
    idempotency key, or is an explicit rerun event ID required? What happens if
    the first request times out after creating the Root but before returning
    `202`?

14. **How is “first Root-causative failure” determined concurrently?** The
    failure decision says the first terminal Root-causative error wins, but
    workers can fail multiple tasks concurrently. Is ordering defined by the
    first committed task failure, Root row lock order, task creation order, or a
    monotonic failure sequence? How is the selected `failure_kind` stable across
    finisher retries?

15. **Where is `ANALYSIS_REPORT_FAILED` added to the API and DB contracts?**
    The resolved report contract uses this public code, but the failure-code
    taxonomy and migration/check constraints were originally written without it.
    Which constants, API error mapping, persisted failure validation, OpenAPI
    schema, and client-facing test assert the new code?

16. **How is soft-deleted content recorded as unavailable during creation?**
    Preflight intentionally does not inspect readable content, while creation
    records soft-deleted content as unavailable acquisition provenance. Does the
    transactional create operation omit that candidate from `fetch_items`, add
    it directly to `unavailable_candidate_ids`, and continue with other IDs? Is
    this classification included in current request state but excluded from the
    request fingerprint as server-observed availability?

17. **What is the migration/code-generation order for the complete schema?**
    The implementation now requires execution bridges, reports, resolution
    events, Root linkage, failure fields, canonical URLs, composite FKs, and
    storage metadata. Please list migration numbers and down-order, identify
    circular-FK handling, and state exactly when `task sqlc`, mocks, schema dump,
    and fresh-database integration tests must run.

18. **Which tests prove direct delivery rather than only mocked success?** The
    current code has no report worker or storage implementation for this flow.
    Please name tests that use a fake store with conditional-write races, a
    Postgres transaction that fails after artifact write, a task completion race,
    finisher/recovery concurrency, a 1 MiB boundary, owner mismatch, hash
    mismatch, and rerun timeout recovery. Which persisted rows and task statuses
    must be asserted for each test?

## Questions After Resolved Direct-Delivery Details - Implementation Follow-up (2026-08-05)

The latest direct-delivery answers are more concrete, but the following issues
remain before those decisions can be treated as implemented behavior.

1. **What does `[x]` mean for resolved design decisions that are not in the
   code?** The document defines `[x]` as behavior provided by the current
   uncommitted server implementation, yet the direct-delivery details are marked
   `[x]` while the current tree has no report worker, `PutIfAbsent`, `Stat`,
   `reports` migration, or report route. Should these entries be changed to
   `[ ]` until code and tests land, or should the document introduce a separate
   marker for an accepted design decision?

2. **What is the exact report task representation?** The answer still does not
   specify whether report rendering is a `PIPELINE_STAGE` task with a payload or
   a new task kind. Provide the task payload schema, logical key, batch/root
   linkage, source type/abbreviation, worker subscription, retry maximum, and
   the condition that identifies this task as the final Root gate.

3. **What repository transaction replaces generic `CompleteTask`?** The report
   worker must upsert a READY `reports` row and complete the final pipeline task
   together, but the current repository exposes separate task and pipeline
   interfaces. What narrow method owns this transaction, what rows does it lock,
   and what rows-affected result distinguishes a retry winner from a stale or
   already-terminal worker?

4. **What are the precise local/S3 guarantees of `PutIfAbsent`?** Define the
   interface result for: object absent, matching object present, mismatching
   object present, concurrent writer, partial upload, and provider timeout. Does
   local storage use an exclusive create, and does S3 use an If-None-Match or
   equivalent conditional request? How is the verified SHA-256 obtained for
   multipart or encrypted objects where ETag is not the content hash?

5. **How are recovery attempts claimed?** Recovery selects nonterminal Roots
   whose final report task is incomplete, but the report worker can be between
   Put and its PostgreSQL transaction. Should recovery lock the task row with
   `FOR UPDATE SKIP LOCKED`, use `last_run_at`/retry state, or acquire the Root
   and execution locks first? Give the exact ordering that avoids two workers
   both performing `Stat` and changing retry state.

6. **What happens after recovery discovers a missing artifact?** The answer says
   the final task is returned to retry, but does not define whether this is a
   normal `PENDING` transition, a new task attempt, or a Root-level operation.
   Which retry counter and deadline are used, and what transaction changes the
   Root/request to `ANALYSIS_REPORT_FAILED` when the bounded recovery limit is
   exhausted?

7. **How is a terminal Root protected from late report repair?** A terminal
   successful Root with a missing object/link is called data corruption and must
   not be reopened. What database invariant prevents the recovery worker from
   updating its task or report link anyway, and what operator repair procedure
   can restore the artifact without changing immutable Root history?

8. **What exact constraints enforce one report per execution?** The document
   specifies Root composite uniqueness and composite foreign keys, but does not
   state whether `reports.analysis_execution_id`, `reports.root_batch_id`, and
   the deterministic URI are individually unique. Provide the complete DDL,
   including `CHECK` constraints limiting execution linkage to analyzer Root
   batches and the behavior of nullable Root/report references.

9. **What is the final report retrieval hash protocol?** The report endpoint
   streams up to 1 MiB after association/hash verification, but the storage
   contract distinguishes bounded `Stat` from `Get`. Does the endpoint read the
   full object before sending headers, trust a verified stored hash, or hash
   while streaming? What response is possible after headers are sent if storage
   returns truncated or altered bytes?

10. **What are the report endpoint's exact HTTP semantics?** Define status and
    body for missing report row, missing artifact, hash mismatch, storage
    timeout, owner mismatch, execution mismatch, and a request whose report was
    superseded by a rerun. Also define `Content-Type`, `Content-Length`,
    `Content-Disposition`, range support, and whether report content is ever
    cached by the API layer.

11. **What is the rerun response body and replay behavior?** The resolved answer
    gives `200` for READY-report adoption and `202` for active/new Root, but does
    not define their JSON fields. Do both return `analysis_id`, observed Root,
    report ID, and status? If the client times out after Root creation, does a
    repeated rerun return the same Root without consuming another cooldown or
    creating another resolution event?

12. **How is request history represented when cache adoption changes the
    observed Root?** A failed request may adopt a READY report from another Root
    under the same execution. Does the transaction replace
    `observed_root_batch_id`, preserve a separate prior-Root history row, or add
    an adoption relation? Which fields does `GET /analyses/{id}` expose so the
    caller can distinguish rerun adoption from original completion?

13. **How is `ANALYSIS_REPORT_FAILED` wired end to end?** The direct-delivery
    section introduces this public failure code, but the earlier taxonomy,
    constants, schema checks, generated API docs, and failure response tests
    were written before it existed. Which package owns the constant, which SQL
    columns accept it, and what exact API response is required after final report
    task exhaustion or hash mismatch?

14. **What is the exact `failure_kind` ordering rule?** “First Root-causative
    failure wins” is ambiguous when multiple tasks fail concurrently. Is the
    winner the first committed failure row, the first task in pipeline order,
    or the failure that causes Root convergence? How does the finisher preserve
    that choice if it retries after an ambiguous commit?

15. **How is soft-deleted content represented in the create transaction?**
    Preflight does not inspect content, but creation records a soft-deleted
    candidate as unavailable provenance. Does the atomic create operation insert
    no `fetch_item`, append the ID to `unavailable_candidate_ids`, and continue
    with available IDs? If every selected candidate is soft-deleted, is the
    response still `422 ANALYSIS_INPUT_EMPTY` and does it create no fetch/run?

16. **What does “tasks and contents use canonical URL” mean for provenance?**
    The decision preserves raw candidate URL but says contents use canonical URL.
    Does `contents` gain a separate raw URL column, or is raw content-source URL
    intentionally discarded? How are existing `contents.url` and the
    candidate/content one-to-one constraint migrated without silently changing
    the URL used by archive or collector lookups?

17. **What is the complete migration dependency order?** Provide the exact
    migration sequence for canonical candidates, execution bridges, Root linkage,
    reports, resolution events, failure fields, and composite foreign keys. How
    are circular references between `analysis_runs`, `analysis_executions`,
    `batches`, and `reports` created safely, and what must the down migrations
    drop first?

18. **Which worker owns automatic system events?** Resolution events now support
    typed system actors, but reconciliation, report recovery, expiry cleanup,
    Root finishing, and operator quarantine have different ownership. Which
    component writes each event, what `component` values are allowed, and which
    events are required for audit versus represented only by task history?

19. **What expiry/retry rules apply to the final report task?** PAGE_FETCH has a
    hard deadline, but the final report `PIPELINE_STAGE` task has no specified
    deadline or storage timeout. Define task expiry, retry count, backoff,
    provider/storage context timeout, and whether a report retry can run after
    the Root has been observed by another request.

20. **What is the minimum real integration test matrix before these decisions
    become `[x]`?** Please name tests for conditional local/S3 writes, matching
    and mismatching hashes, crash after Put, crash after DB-link, retry after
    task completion failure, finisher/recovery locking, rerun timeout replay,
    composite-FK rejection, soft-deleted mixed selection, report streaming
    mismatch, and cross-owner access. For each, specify the expected persisted
    task, Root, request, report, and audit-event state.

## Answers After Resolved Direct-Delivery Details (2026-08-05)

The answers below distinguish the current working tree from the required
implementation. The direct-delivery contract is the target behavior; it is not
evidence that the target behavior has shipped.

1. **Status markers and superseded text:** The document's definition at the
   beginning of this file says `[x]` means that current uncommitted server code
   provides the behavior. It does not currently provide that meaning for the
   direct-delivery entries. The smallest consistent choice is to change the
   report-delivery `[x]` entries in "Resolved Follow-up Decisions", "Report
   Artifact Delivery Contract", and "Resolved Direct-Delivery Details" to `[ ]`
   until code and tests land. Do not add a second marker unless the document
   explicitly changes the definition of `[x]`. The outbox/PENDING paragraphs
   should remain only under the already-labelled historical/superseded sections;
   remove them from active implementation checklists. In particular, report
   producer, report persistence, rerun, report authorization, and report
   recovery claims in `pre-analysis.md` are design decisions, not current
   behavior. The current tree has no `reports` table, execution bridge, report
   worker, `PutIfAbsent`, `Stat`, report route, or report tests.

2. **Report task and worker:** Reuse the existing `PIPELINE_STAGE` task kind;
   adding a new enum value is unnecessary. Add a final `generate_report` stage
   to `configs/llm_pipeline.yaml` depending on
   `embed_selected_contents`, with `config.task_type = "GENERATE_REPORT"`.
   `Coordinator.Initialize` in
   `internal/analyzer/pipeline/coordinator.go` already serializes a
   `pipeline.StageSpec` as the control task payload, uses logical key
   `pipeline:stage:<name>`, and gives the task URL
   `pipeline://stage/<name>`. The concrete final task is therefore a singleton
   `PIPELINE_STAGE` control task with payload equivalent to
   `{"Name":"generate_report","DependsOn":["embed_selected_contents"],"Config":{"task_type":"GENERATE_REPORT"}}`,
   the analysis Root's `batch_id`, `SourceType=MEDIA`, and the Root's source
   abbreviation. The existing analyzer pipeline worker in
   `cmd/worker/analyzer/pipeline/main.go` owns the task-topic subscription. Its
   `pipeline.Handler` must recognize `GENERATE_REPORT` before the generic
   `Coordinator.RunStage` path, render and deliver the report, and complete this
   control task. The final gate is the last direct Root task: the Root may be
   selected by `FindFinishedPipelineRootBatches` only after this task is
   `COMPLETED`. The worker's existing `retry-max` configuration defaults to
   `repo.DefaultTaskRetryMax` (3) in
   `cmd/worker/analyzer/pipeline/config.go` and
   `internal/repo/errors.go`. None of this task or worker behavior exists yet;
   the current pipeline YAML contains only the two embedding stages.

3. **Report completion transaction:** Add a narrow method such as
   `PipelineRuntime.CompleteAnalysisReport(ctx, arg)` rather than changing the
   generic `TaskReporter.CompleteTask`. The transaction should lock the final
   task with `SELECT ... FOR UPDATE`, then lock its Root and execution bridge in
   the same order used by recovery. It must verify `task.id = $task_id`,
   `task.batch_id = $root_id`, `task.kind = 'PIPELINE_STAGE'`, the report-stage
   marker, `task.status = 'RUNNING'`, and `root.completed_at IS NULL`.
   It then inserts the READY `reports` row only if its
   `analysis_execution_id`, `root_batch_id`, URI, size, and SHA-256 match the
   supplied values, and updates the task with
   `WHERE id = $task_id AND batch_id = $root_id AND status = 'RUNNING'`.
   Return a typed result, for example `Completed`, `AlreadyCompleted`, or
   `Stale`, rather than hiding rows-affected in an `error`. One updated task row
   means this worker won. An already completed task is success only when the
   immutable report link matches; a failed/cancelled task, wrong Root, wrong
   execution, or mismatching existing report is stale/conflict. A request
   observing a different Root is not a reason to reject this transaction:
   equivalent requests may legitimately retain older failed Root history. The
   current `TaskReporter.CompleteTask` interface and `db/queries/tasks.sql`
   `CompleteTask` query have no Root, execution, or report checks.

4. **`PutIfAbsent` and `Stat`:** Extend `internal/storage.Store`, currently
   defined in `internal/storage/store.go`, with a conditional write returning
   `Created` or `Existing`, and a bounded `Stat` returning key, size, content
   type, and a verified SHA-256. Existing matching bytes return `Existing` and
   are never overwritten. An existing object with a different size/hash returns
   a permanent integrity error. A concurrent writer is handled identically to an
   existing object: conditional creation loses, then `Stat` verifies the
   winner. A local store must use exclusive creation, such as a temporary file
   followed by a no-replace link/rename protocol, instead of the current
   overwrite-capable `os.Rename` in `internal/storage/filesystem/local.go:36-64`.
   S3 must use `PutObject` with `If-None-Match: *` or the provider-equivalent
   conditional request, not the current unconditional `PutObject` in
   `internal/storage/objectstore/s3.go:43-63`.
   `Stat` must not trust an S3 ETag for multipart or encrypted objects. Store
   SHA-256 in object metadata on creation and verify it; if metadata is absent,
   perform one bounded read of at most 1 MiB plus one byte. A provider timeout
   with unknown commit is resolved by `Stat`: matching object means success,
   absent means retry, and mismatching object means permanent corruption. The
   fake store must expose all of these outcomes, including a partial-upload
   failure, so local and S3 tests exercise the same contract. Current storage
   has none of these methods; its tests only cover round-trip/list/delete and
   constructor validation (`TestLocalStoreRoundTripListAndDelete`,
   `TestNewS3StoreRequiresClient`).

5. **Recovery claim:** Do not inspect every pending report task. Recovery should
   select only nonterminal analysis Roots whose marked final report task is
   `RUNNING` and stale, for example
   `last_run_at <= clock_timestamp() - interval '5 minutes'`, using
   `FOR UPDATE OF task SKIP LOCKED`. A `PENDING` task has not started its Put
   and must be left to the scheduler. The report worker writes storage before
   opening its database transaction; the five-minute grace prevents recovery
   from racing the normal short Put-to-link window. After claiming the task,
   both worker and recovery use the same lock order: final task, Root, then
   execution bridge. Recovery performs bounded `Stat` only while holding that
   task claim. A matching artifact runs the same link/task transaction as the
   worker; an absent artifact schedules a retry; a mismatch fails the attempt.
   The current repository has no recovery query or claim method. The existing
   `ClaimTasks` query uses `FOR UPDATE SKIP LOCKED` for ordinary scheduling, but
   does not identify report tasks.

6. **Missing artifact retry:** A missing artifact is a normal report-task retry,
   not a new Root. In the claim transaction set the stale `RUNNING` task back to
   `PENDING`, retain its already-counted `retry_count`, set
   `next_run_at = clock_timestamp() + backoff`, and record a bounded failure
   message. The next scheduler claim increments `retry_count`, as specified by
   `db/queries/tasks.sql:199-214`. Use the normal total-attempt limit of 3 and
   bounded exponential recovery backoff, for example 30s, 60s, and 120s; this
   needs a report-specific query because current `FailTask` uses `NOW()` with no
   backoff. On exhaustion, a transaction sets the report task `FAILED` and
   stores the Root's first `failure_kind`; the Root remains unterminalized until
   the finisher observes all direct tasks terminal, then the winning finisher
   transaction sets the Root failed and observing requests to `FAILED` with
   `failure_code = ANALYSIS_REPORT_FAILED`. A hash mismatch bypasses retry,
   records the same public code and a non-retryable internal classification, and
   requires operator quarantine. Current `RetryFailedTask` in
   `db/queries/tasks.sql:216-236` revives any failed task in place and is not a
   sufficient report-recovery protocol.

7. **Terminal Root protection:** Every worker/recovery update must include
   `root.completed_at IS NULL`, and report-link insertion/completion must run
   only for a Root whose final task is not terminal. The Root-finish update must
   retain `completed_at IS NULL`; a terminal Root is therefore excluded from
   repair. The report table is READY-only, so a successful terminal Root and
   its report link are expected to have been committed before
   `completed_at` is set. A terminal Root missing its link/object is corruption,
   not a retry state. A privileged operator may restore the exact artifact from
   trusted backup at `reports/<execution-id>.md`, or restore a missing READY
   link after independently verifying the stored bytes, without changing Root
   `completed_at`, `succeeded`, or task history. That repair must record an
   operator/audit event. The recovery worker must never update a terminal Root.
   Current `MarkPipelineRootFinished` in `db/queries/batches.sql:262-274`
   checks `completed_at IS NULL`, but current report/task paths have no
   execution or artifact invariant to protect.

8. **Report and Root DDL:** Use one immutable READY report per execution:

   ```sql
   CREATE TABLE analysis_executions (
       id UUID PRIMARY KEY,
       report_fingerprint CHAR(64) NOT NULL UNIQUE,
       created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
   );

   ALTER TABLE batches
       ADD COLUMN analysis_execution_id UUID REFERENCES analysis_executions(id),
       ADD COLUMN failure_kind TEXT,
       ADD COLUMN failure_task_id UUID,
       ADD COLUMN failure_recorded_at TIMESTAMPTZ,
       ADD CONSTRAINT batches_analysis_execution_purpose_check
           CHECK (analysis_execution_id IS NULL OR purpose = 'ANALYZER_PIPELINE_ROOT');
   ALTER TABLE batches ADD CONSTRAINT batches_execution_id_key
       UNIQUE (analysis_execution_id, id);

   CREATE TABLE reports (
       id UUID PRIMARY KEY DEFAULT uuidv7(),
       analysis_execution_id UUID NOT NULL UNIQUE REFERENCES analysis_executions(id),
       root_batch_id UUID NOT NULL UNIQUE,
       storage_uri TEXT NOT NULL UNIQUE,
       byte_size BIGINT NOT NULL CHECK (byte_size BETWEEN 0 AND 1048576),
       sha256 CHAR(64) NOT NULL,
       created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
       UNIQUE (analysis_execution_id, id),
       FOREIGN KEY (analysis_execution_id, root_batch_id)
           REFERENCES batches (analysis_execution_id, id)
   );
   ```

   The composite Root key permits multiple immutable Root rows for one
   execution; `reports.analysis_execution_id UNIQUE` permits only one READY
   report. `root_batch_id UNIQUE` prevents two reports for one Root, and the
   URI uniqueness plus the deterministic URI rule prevents aliases. `reports`
   has no PENDING/status column. Add a lower-case hexadecimal hash check if the
   application does not validate it. The current `batches` table has no
   `analysis_execution_id` or `failure_kind`, and the current `analysis_runs`
   migration has no report table or execution constraints.

9. **Nullable composite links:** Replace the current internal
   `analysis_runs.root_batch_id` meaning with
   `observed_root_batch_id`; do not maintain two independent Root references.
   Keep `execution_id`, `observed_root_batch_id`, and `report_id` nullable for
   pre-Root rows. Use a separate scalar FK for `execution_id`, a composite
   `MATCH FULL` FK for `(execution_id, observed_root_batch_id)` to
   `batches(analysis_execution_id, id)`, and a `MATCH SIMPLE` composite FK for
   `(execution_id, report_id)` to
   `reports(analysis_execution_id, id)`. `MATCH FULL` is appropriate for the
   observed Root because the atomic confirmation operation sets both or neither;
   the report pair needs `MATCH SIMPLE` because an active execution may have no
   report yet. Add `CHECK (report_id IS NULL OR execution_id IS NOT NULL)` and
   `CHECK (observed_root_batch_id IS NULL OR execution_id IS NOT NULL)` as clear
   paired-null guards. The negative cases are: non-null observed Root with null
   execution, non-null report with null execution, and either non-null pair
   pointing to a different execution, all rejected by `23503`/`23514`. The
   current `000007_analysis_runs.up.sql` has only nullable `user_id` and
   `root_batch_id`, with no such FKs; no current code can enforce this.

10. **Report hash verification:** The report endpoint should buffer the whole
   object before sending headers. After owner and composite-link checks, call
   storage `Get`, read through `io.LimitReader` with a 1 MiB plus one-byte
   limit, close it, verify exact byte count and SHA-256 against `reports`, and
   only then write HTTP headers and bytes. This is bounded delivery, not
   unbounded streaming, and guarantees a truncated or altered object cannot
   produce a successful response after headers are sent. A read error, size
   overflow, or hash mismatch before headers is returned as the defined error
   response. The current `storage.Store` has `Get` but no `Stat`, hash metadata,
   or report route, so this is required behavior, not current behavior.

11. **Report endpoint HTTP contract:** Add owner-scoped
   `GET /analyses/{id}/report`; there is no report-ID route. A valid report
   returns `200 OK`, `Content-Type: text/markdown; charset=utf-8`, exact
   `Content-Length`, `Content-Disposition: inline; filename="report.md"`,
   `Cache-Control: private, no-store`, and no range support. A `Range` header
   returns `416` with code `ANALYSIS_REPORT_RANGE_UNSUPPORTED`. Absent/foreign
   analysis or an execution/report ownership mismatch returns `404 Not Found`.
   An owner whose analysis is valid but has no READY report, including an active
   rerun that has not completed, receives `409 Conflict` with
   `ANALYSIS_REPORT_NOT_READY`. Missing storage or a storage timeout returns
   `503 Service Unavailable` with `ANALYSIS_REPORT_UNAVAILABLE`; a size/hash
   mismatch or impossible execution association returns `500 Internal Server
   Error` with `ANALYSIS_REPORT_CORRUPT`. Reads do not mutate rows, retry state,
   or audit events; emit only a metric/log. A rerun does not silently serve its
   previous report: the endpoint uses the request's current `report_id`, so an
   active request without one returns 409. The intended error body is
   `{"code":"...","message":"..."}`. Current `writeError` in
   `internal/http/api/api.go:376-394` emits only `{"error":"..."}`, and
   `RegisterV1` has no `/analyses` route, so these statuses and headers are not
   implemented.

12. **READY-report adoption:** Under the request row lock and execution bridge
   lock, verify the READY report's fingerprint/execution association, then set
   `analysis_runs.observed_root_batch_id` to the successful report Root,
   `report_id` to that report, `status = 'COMPLETED'`, `failure_code = NULL`,
   and `updated_at = clock_timestamp()` in one transaction. Preserve the prior
   failed Root in an append-only `analysis_resolution_events` row with action
   `ADOPT_REPORT`, `prior_root_batch_id`, `current_root_batch_id`, and
   `report_id`; do not rewrite the old Root. For a caller-visible distinction,
   expose `result_origin = "CACHE_ADOPTION"` in `GET /analyses/{id}` or derive
   it from the latest adoption event. The current schema has only
   `root_batch_id`, no `report_id`, no event table, and no read endpoint, so
   there is currently no history-preserving adoption behavior.

13. **Rerun response and replay:** An empty request body is sufficient. Under
    the request/execution locks, first return a READY report adoption as
    `200 OK` with
    `{"analysis_id":"...","observed_root_batch_id":"...","report_id":"...","status":"COMPLETED","result":"ANALYSIS_REPORT_ADOPTED"}`;
    otherwise return `202 Accepted` with a stable body such as
    `{"analysis_id":"...","observed_root_batch_id":"...","report_id":null,"status":"ANALYZING","result":"ANALYSIS_RERUN_ACCEPTED"}`.
    Use `result = "ANALYSIS_RERUN_ATTACHED"` when an active Root already exists
    and `result = "ANALYSIS_RERUN_CREATED"` only when this call inserted the next
    Root. If the first call times out after commit, a repeat sees the same
    `ANALYZING` request and active Root, returns the same 202/root, and creates
    neither another Root nor another cooldown-consuming event. The original
    `analysis_id` is the idempotency identity while a rerun is active; a new
    attempt after a terminal retryable failure is a new `RERUN` event. Cooldown
    failure is `409 ANALYSIS_RERUN_COOLDOWN`. Current routing has no rerun
    handler, and current `SetAnalysisRunRoot` has no conditional state or
    execution lock, so none of this replay behavior exists.

14. **Failure-kind ordering:** Define the winner as the first transaction that
   records a terminal Root-causative failure while the Root row is still open,
   not the first worker to return an error and not pipeline declaration order.
   In the same transaction that observes task exhaustion, lock the Root and run
   `UPDATE batches SET failure_kind = $kind, failure_task_id = $task_id,
   failure_recorded_at = clock_timestamp() WHERE id = $root_id AND
   completed_at IS NULL AND failure_kind IS NULL`. One affected row wins;
   later concurrent task failures retain task history but cannot replace the
   Root classification. The finisher never reclassifies a Root after
   `completed_at` is set, and an ambiguous retry rereads the persisted
   `failure_kind`. The current `FailPipelineTask` in
   `internal/repo/pg/repo.go:563-598` has no failure-kind column and only
   converges task/root task counts, so the ordering is a required new
   transaction rule.

15. **`ANALYSIS_REPORT_FAILED` contract:** Put the public constant beside the
   analysis lifecycle constants in `internal/repo/contracts.go`, for example
   `AnalysisFailureCodeReportFailed = "ANALYSIS_REPORT_FAILED"`, and use the
   same constant in the API response mapper and report-task failure path. The
   current `analysis_runs.failure_code TEXT` column accepts this string because
   it has no CHECK constraint, but the current code writes unstable values such
   as `FETCH_FAILED`, `ANALYSIS_SETUP_FAILED`, and `ANALYSIS_ROOT_FAILED` in
   `internal/http/api/analysis_runs.go`. After writers are migrated, add a clean
   database CHECK allowing the resolved persisted set, including
   `ANALYSIS_NO_READY_INPUT`, `ANALYSIS_FETCH_FAILED`,
   `INPUT_CANDIDATE_MISSING`, `ANALYSIS_PIPELINE_FAILED`, and
   `ANALYSIS_REPORT_FAILED`. `GET /analyses/{id}` returns `200` for the owned
   analysis resource with `status="FAILED"` and that failure code; the failure
   is not itself an HTTP error. Hash mismatch is non-retryable and the rerun
   route returns `409 ANALYSIS_RERUN_NOT_ELIGIBLE`; ordinary exhausted
   transient report delivery may remain eligible for the explicit rerun policy.
   Update `cmd/api-server/docs/swagger.yaml` and `swagger.json`, and add an API
   test asserting the exact code. No current generated contract or test names
   this code.

16. **Soft-deleted content at create:** Preflight remains candidate existence
   plus canonical URL validation only. In the atomic create transaction, load
   the content by candidate and classify a row with `deleted_at IS NOT NULL` as
   unavailable: append the candidate ID to
   `unavailable_candidate_ids`, create no `fetch_items` row, and create no
   PAGE_FETCH task for it. A readable row becomes an `ALREADY_COMPLETE` fetch
   item; no row becomes normal acquisition work. Mixed selections create one
   run/fetch for available items and preserve all original IDs plus the
   unavailable IDs. If every selected ID is unavailable, return `422
   ANALYSIS_INPUT_EMPTY` and commit no fetch, fetch item, analysis run, task,
   Root, or P0. Availability is server-observed state and is excluded from the
   request fingerprint. The existing handler at
   `internal/http/api/analysis_runs.go:150-173` checks `DeletedAt` only to
   decide whether to create a task, does not append the ID to provenance, and
   is not transactional; it therefore does not implement this decision.

17. **Migration and code-generation order:** Use ordered migrations after the
   current `000007_analysis_runs` migration. `000008_canonical_urls` adds
   `candidates.canonical_url`, `contents.source_url`, canonicalizes all new
   writes, makes canonical candidate identity unique, and makes
   `contents.url` the canonical unique value while retaining the raw source URL
   in `source_url`; existing `uq_tasks_active_page_fetch` then operates on
   canonical task URLs. `000009_analysis_executions` creates
   `analysis_executions`, adds `batches.analysis_execution_id`, `failure_kind`,
   and the analyzer-Root CHECK/composite key. `000010_reports` creates READY-only
   `reports` and its composite Root FK. `000011_analysis_run_links` renames
   `analysis_runs.root_batch_id` to `observed_root_batch_id`, adds
   `execution_id` and `report_id`, and then adds request FKs after both target
   tables exist. `000012_resolution_events` creates the append-only event table
   and actor/component checks. `000013_integrity_hardening` adds the final
   failure-code/hash/size checks and indexes if they were not safely added in
   the earlier migrations. This avoids circular creation: the bridge has no
   Root/report FK, batches reference the bridge, reports reference both, and
   requests reference the already-created reports/batches. Down migrations run
   in reverse: hardening, events, request links, reports, execution linkage,
   then canonical columns/constraints. After SQL changes, run `task sqlc`; if
   Go interfaces change, run `task mocks`; refresh `db/schema.sql` with the
   `taskfile/codegen.yml` schema-dump task; then run a fresh-database migration
   test before package tests. The current tree has only `000007` for analysis
   runs and generated SQLC models contain none of these fields.

18. **Automatic system events:** Add `actor_token_id UUID NULL` and a non-null
   `actor_component` with a CHECK over a small fixed set: `api`,
   `analysis-trigger`, `report-worker`, `report-recovery`, `page-fetch-sweeper`,
   `root-finisher`, and `operator`. API `RETRY_FAILED`, `IGNORE_FAILED`,
   `RERUN`, and `ADOPT_REPORT` events carry the authenticated token ID and
   component `api`. `cmd/trigger/analysis` writes `AUTO_RECONCILE` with a null
   token; `report-recovery` writes retry/corruption/repair events; an operator
   quarantine writes an event with the operator token. Ordinary report success,
   PAGE_FETCH expiry, task retries, and Root finishing are already durable in
   task/batch rows and need no resolution event unless they change analysis
   state or repair storage. The current tree has no
   `analysis_resolution_events` table and no system actor representation, so
   these component names are a required schema/API decision rather than
   existing values.

19. **Final report retry and timeout:** Do not apply the PAGE_FETCH hard
   deadline to the report stage. Keep `expires_at NULL` for the report
   `PIPELINE_STAGE` task, because the PAGE_FETCH-only non-null CHECK should not
   govern pipeline tasks. Give each report worker attempt a five-minute
   context timeout covering render, conditional Put, and the DB-link phase;
   recovery treats `last_run_at` older than five minutes as stale. Use the
   existing total attempt limit of 3, but add report-specific exponential
   backoff of 30s, 60s, and 120s for retryable render, storage, DB-link, or
   ambiguous completion failures. A hash mismatch is immediately terminal and
   records `ANALYSIS_REPORT_FAILED`; it is not retried until an operator
   quarantines the object. A retry may run while equivalent requests observe the
   same active Root, because observation is not ownership and the Root remains
   nonterminal. Once the Root is terminal, no late report retry is allowed;
   explicit rerun must create/attach a new Root under the eligibility rules.
   Current pipeline worker code has only the generic `retry-max` and current
   task SQL has no report timeout/backoff, so these rules are unimplemented.

20. **Minimum real integration matrix:** The merge gate should include
   `TestLocalStorePutIfAbsentMatchingAndMismatchingHashes` and
   `TestS3StorePutIfAbsentUsesConditionalWrite`, asserting one object, no
   overwrite, and matching/mismatching results; `TestReportDeliveryCrashAfterPutBeforeLink`,
   asserting the artifact exists, task remains `RUNNING`, Root remains open,
   and no `reports` row exists until recovery; `TestReportDeliveryCrashAfterLinkBeforeTaskCompletion`,
   asserting one READY report row and a retry that changes the same task to
   `COMPLETED` without a second artifact; `TestReportTaskCompletionRace`,
   asserting one task update and one report row; `TestAnalysisRootFinisherRecoveryRace`,
   asserting one terminal Root and one request transition; and
   `TestRerunTimeoutReplaysActiveRoot`, asserting the second request returns the
   same 202/root and creates no second Root or event. Add
   `TestAnalysisCompositeForeignKeysRejectMismatchedExecution`, asserting
   `23503`/`23514` and no request/report row; `TestCreateAnalysisMixedSoftDeletedSelection`,
   asserting one fetch item for the readable candidate, no item/task for the
   soft-deleted candidate, and the unavailable ID in the request; and
   `TestReportEndpointRejectsStoredHashMismatch`, asserting no response body,
   `500 ANALYSIS_REPORT_CORRUPT`, and no state mutation. Add
   `TestReportEndpointCrossOwnerReturnsNotFound`, asserting `404` and no
    storage `Get`, and `TestReportEndpointEnforcesOneMiBLimit`, asserting exactly
    1,048,576 bytes succeeds while 1,048,577 bytes fails before headers. Each test must assert
   task status/retry count, Root `completed_at`/`succeeded`, request status,
   `failure_code`, observed Root/report IDs, report row count, and resolution
   event count. Existing `internal/repo/pg/pipeline_integration_test.go`
   provides useful patterns in `TestInitializePipelineConcurrentIsAtomicAndIdempotent`
   and `TestCreatePipelineRootRequiresCompletedInputAndStoresParent`, but the
   current `internal/http/api/analysis_runs_test.go` only has
   `TestAnalysisPreflightDoesNotCreateFetch` and
   `TestCreateAnalysisRunReusesExistingContent`; neither proves direct
   delivery. These named tests, plus the fresh migration test and the existing
   short suite/lint, are mandatory before the direct-delivery entries can be
    changed back to `[x]`.

## Answers After Artifact Availability And Administrative Removal (2026-08-05)

This section is authoritative for the `Artifact availability and
administrative removal` checklist item. It resolves the conflicts between that
item and the earlier direct-delivery answers. These are target decisions, not
claims about the current working tree. The current tree has no `reports` table,
`analysis_executions` table, report worker, report storage dependency, report
retrieval route, administrative report route, report repository methods,
`PutIfAbsent`, `Stat`, report-availability configuration, or report tests.

### Contradictions Closed

1. `reports` remains READY-only. The row is inserted only after a verified
   deterministic object exists and the DB link is committed. Expiry, unexpected
   loss, corruption, and administrative removal are availability tombstones on
   that already-READY row, not `PENDING`, `FAILED`, or another report status.
   A tombstoned row is never a cache hit.
2. The earlier DDL called the whole report row immutable. That is too broad.
   `id`, `analysis_execution_id`, `root_batch_id`, `storage_uri`, `byte_size`,
   `sha256`, and `created_at` are immutable. `expires_at` is set at link time;
   the availability and audit columns are the only mutable lifecycle metadata.
   The row is never deleted or replaced merely because its object is unavailable.
3. The earlier retrieval answer returned `503 ANALYSIS_REPORT_UNAVAILABLE` for
   a storage NotFound. This item supersedes that one case with
   `410 ANALYSIS_RESULT_ARTIFACT_MISSING`. A storage timeout remains `503`.
   Hash mismatch remains `500 ANALYSIS_REPORT_CORRUPT`.
4. Delivery recovery and post-READY reads are different. Recovery may retry a
   missing object only while the final report task and Root are nonterminal and
   no READY report row exists. A missing object discovered after a successful
   Root is recorded on the READY row, is not used to reopen the Root or task,
   and is not repaired by a normal read.
5. The earlier no-read-side-effects rule is narrowed. Normal metadata reads and
   storage timeouts do not mutate state. A pre-expiry NotFound and a verified
   hash mismatch do persist availability evidence so future cache lookup has a
   durable answer. The DB update is conditional and does not change Root or
   request lifecycle history.
6. A 410 does not broaden rerun eligibility. The existing rule remains: a
   cache hit wins, an active Root is attached, and a new Root is created only
   for an explicitly eligible retryable terminal Root. A completed request with
   an expired, missing, corrupt, or administratively removed artifact is not
   silently changed to ANALYZING. A read never starts a Root.
7. The phrase "may expose the request-local rerun URL" is optional, not a
   promise that the URL will succeed. Do not emit a link for a completed request
   whose only problem is report availability; the current rerun contract would
   return `409 ANALYSIS_RERUN_NOT_ELIGIBLE`. If a future report-repair operation
   is added, it must get a distinct contract rather than weakening Root
   immutability.
8. The old outbox and PENDING-report design remains superseded. Administrative
   deletion uses a synchronous DB/audit decision plus an idempotent storage
   delete and does not introduce an outbox or a report-attempt status.

### Reports DDL And State

Keep the planned execution bridge and composite Root key from the earlier
direct-delivery answer. The complete report table is:

```sql
CREATE TABLE reports (
    id                          UUID PRIMARY KEY DEFAULT uuidv7(),
    analysis_execution_id      UUID NOT NULL UNIQUE
        REFERENCES analysis_executions(id),
    root_batch_id               UUID NOT NULL,
    storage_uri                 TEXT NOT NULL UNIQUE,
    byte_size                   BIGINT NOT NULL
        CHECK (byte_size BETWEEN 0 AND 1048576),
    sha256                      CHAR(64) NOT NULL
        CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    expires_at                  TIMESTAMPTZ NOT NULL,
    artifact_missing_at         TIMESTAMPTZ,
    artifact_corrupt_at        TIMESTAMPTZ,
    artifact_corruption_reason  TEXT,
    artifact_removed_at         TIMESTAMPTZ,
    artifact_removed_by        UUID REFERENCES tokens(id),
    artifact_removal_reason    TEXT,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT reports_execution_id_key
        UNIQUE (analysis_execution_id, id),
    CONSTRAINT reports_execution_root_key
        UNIQUE (analysis_execution_id, root_batch_id),
    CONSTRAINT reports_root_key
        UNIQUE (root_batch_id),
    CONSTRAINT reports_root_fk
        FOREIGN KEY (analysis_execution_id, root_batch_id)
        REFERENCES batches (analysis_execution_id, id),
    CONSTRAINT reports_uri_check
        CHECK (storage_uri = 'reports/' || analysis_execution_id::text || '.md'),
    CONSTRAINT reports_expiry_check
        CHECK (expires_at > created_at),
    CONSTRAINT reports_corrupt_reason_check
        CHECK ((artifact_corrupt_at IS NULL AND artifact_corruption_reason IS NULL)
            OR (artifact_corrupt_at IS NOT NULL
                AND length(btrim(artifact_corruption_reason)) BETWEEN 1 AND 1024)),
    CONSTRAINT reports_removed_fields_check
        CHECK ((artifact_removed_at IS NULL
                AND artifact_removed_by IS NULL
                AND artifact_removal_reason IS NULL)
            OR (artifact_removed_at IS NOT NULL
                AND artifact_removed_by IS NOT NULL
                AND length(btrim(artifact_removal_reason)) BETWEEN 1 AND 1024)),
    CONSTRAINT reports_missing_corrupt_exclusive_check
        CHECK (NOT (artifact_missing_at IS NOT NULL
                    AND artifact_corrupt_at IS NOT NULL))
);

CREATE INDEX idx_reports_expires_at
    ON reports (expires_at);
CREATE INDEX idx_reports_missing_at
    ON reports (artifact_missing_at)
    WHERE artifact_missing_at IS NOT NULL;
CREATE INDEX idx_reports_removed_at
    ON reports (artifact_removed_at)
    WHERE artifact_removed_at IS NOT NULL;
```

`UNIQUE (analysis_execution_id, id)` is also required, even though `id` is the
primary key, so nullable request links can use the composite foreign key from
the earlier design. The execution, Root, and URI unique constraints prevent
aliases and multiple READY rows. The `(analysis_execution_id, root_batch_id)`
constraint permits Root history in `batches` while allowing only one report
link for a particular Root. Do not add a `status` column to `reports`.

`artifact_corrupt_at` and `artifact_corruption_reason` are required additions
to make the previously documented corruption response durable and to exclude a
known-corrupt artifact from future cache hits. They do not make corruption a
report status. Missing and corrupt are mutually exclusive observations; an
administrative removal may coexist with either observation and has higher read
precedence.

The cache-hit predicate is equivalent to:

```sql
WHERE analysis_execution_id = $1
  AND expires_at > clock_timestamp()
  AND artifact_missing_at IS NULL
  AND artifact_corrupt_at IS NULL
  AND artifact_removed_at IS NULL
```

No cache query calls storage. `expires_at` is evaluated with the PostgreSQL
clock at the query's availability decision. An expired row, missing row,
removed row, or corrupt row remains useful history but is not an attachable
success cache.

### Expiry And Lifecycle

Add a required positive `report-cache-ttl` configuration to the report worker
and API/recovery configuration. The report link transaction obtains
`clock_timestamp()` from PostgreSQL and sets:

```text
expires_at = database_clock_now + report-cache-ttl
```

It must not use the request timestamp, Root `completed_at`, process-local
`time.Now`, or the object's upload timestamp. The storage lifecycle deletion
TTL is infrastructure configuration, not a per-report database value. It must
be greater than the cache TTL plus the maximum configured write/link recovery
grace and clock-skew margin. Deployment validation must reject a storage
lifecycle that can delete an object at or before `expires_at`. The lifecycle
ordering is therefore:

1. Render and conditionally write the deterministic object.
2. Verify exact bytes, size, content type, and SHA-256.
3. In the link/task transaction, insert the READY row and calculate
   `expires_at` from `clock_timestamp()`.
4. Serve the object only while the DB availability predicate is true.
5. At or after `expires_at`, return 410 without `Get`, `Stat`, `HEAD`, or list.
6. Let the configured storage lifecycle delete the object later; lifecycle
   deletion is not the source of the application expiry decision.

The current configuration has no report TTL or storage lifecycle validation.
The current storage factory only constructs prompt storage in
`cmd/api-server/main.go`; report storage wiring is absent.

### Retrieval Precedence And Responses

`GET /analyses/{analysis_id}` remains a PostgreSQL-only metadata read. It
returns the owned request and its `report_id`/availability metadata with HTTP
200 even when the successful Root's artifact later becomes unavailable. It does
not return report bytes or probe storage. The owner-authorized byte endpoint is
`GET /analyses/{analysis_id}/report`.

After authentication, owner lookup, execution/report association checks, and
the existing `NOT_READY` check, the byte endpoint applies this order:

1. `artifact_removed_at IS NOT NULL` returns `410` with code
   `ANALYSIS_RESULT_REMOVED_BY_ADMIN`. This wins even if the row is expired,
   missing, or corrupt, because it is the explicit operator decision.
2. `expires_at <= clock_timestamp()` returns `410` with code
   `ANALYSIS_RESULT_EXPIRED` and performs no storage call. Expiry wins over an
   earlier `artifact_missing_at` or corruption marker.
3. `artifact_missing_at IS NOT NULL` returns `410` with code
   `ANALYSIS_RESULT_ARTIFACT_MISSING` and performs no repeated storage call.
4. `artifact_corrupt_at IS NOT NULL`, or a bounded `Get` whose bytes fail the
   stored size/hash check, returns `500` with code
   `ANALYSIS_REPORT_CORRUPT`. The first detected mismatch records the corrupt
   marker conditionally; it does not rewrite the Root or report identity.
5. A storage timeout or other transient storage error before expiry returns
   `503` with code `ANALYSIS_REPORT_UNAVAILABLE`. It does not set a missing or
   corrupt marker, so a later cache lookup remains eligible and a retry may
   succeed.

The normal pre-existing statuses remain: a foreign or nonexistent analysis is
404, a mismatched execution/report association is 404, an owned analysis with
no READY report yet is `409 ANALYSIS_REPORT_NOT_READY`, an unsupported Range is
`416 ANALYSIS_REPORT_RANGE_UNSUPPORTED`, and a valid verified artifact is 200
with `text/markdown; charset=utf-8`, exact `Content-Length`, inline
`report.md`, `Cache-Control: private, no-store`, and no range support. A
completed analysis whose report row is unexpectedly absent is corruption and
returns `500 ANALYSIS_REPORT_CORRUPT`, not `NOT_READY`.

All report error bodies use the report API shape
`{"code":"...","message":"..."}`. A `rerun_url` is included only when
the request is actually eligible under the explicit rerun rules. The three
410 cases do not automatically create a Root and, for the normal COMPLETED
request described above, omit that field rather than advertise an operation
that must return `409 ANALYSIS_RERUN_NOT_ELIGIBLE`.

The handler buffers at most 1 MiB plus one byte, verifies size and SHA-256
before writing response headers, and rechecks the report availability marker
before committing the response. A removal or expiry that wins before headers
are written returns its 410. A storage error after headers is not recoverable
as an HTTP status; buffering prevents that partial-response case.

### Cache And Rerun Matrix

| Condition | Cache hit | Byte read | Explicit rerun | Root/request mutation from read |
|---|---|---|---|---|
| Available, unexpired, unmarked | Yes | 200 after hash verification | Existing rules: adopt READY report first | None |
| Expired | No | 410 `ANALYSIS_RESULT_EXPIRED`, no storage call | Completed request is not eligible; no URL is emitted; no new Root | None |
| Pre-expiry missing | No after `artifact_missing_at` is recorded | 410 `ANALYSIS_RESULT_ARTIFACT_MISSING` | Completed request is not eligible; no URL is emitted; operator repair is required | Only the missing marker is recorded |
| Administratively removed | No | 410 `ANALYSIS_RESULT_REMOVED_BY_ADMIN` | Owner cannot undo removal; no URL is emitted; only an operator repair can restore it | Only removal/audit metadata changes |
| Corrupt/hash mismatch | No after corruption is recorded | 500 `ANALYSIS_REPORT_CORRUPT` | Not eligible until operator quarantine/repair; no URL is emitted | Only corruption evidence is recorded |
| Storage timeout/transient error | DB row remains a hit | 503 `ANALYSIS_REPORT_UNAVAILABLE` | No rerun is needed or started; retry the read | None |

This deliberately preserves the earlier rule that `COMPLETED` has no automatic
outgoing transition and that a terminal Root is never reopened. If product
requirements later demand a working rerun link for expiry or missing data,
that is a separate report-repair design: it must define a repair task and
history before changing this matrix. It must not silently reuse the analysis
Root rerun path.

### Administrative Route And Authorization

The route is registered only on the existing admin server as:

```text
DELETE /api/v1/admin/reports/{execution_id}
```

`RegisterV1Admin` must register it through `requireAdmin`, and
`cmd/api-server/main.go` already mounts that registrar beneath `/api/v1/admin`
with token authentication and `RequirePermissions(permission.AdminAPI)`. The
minimum authorization is an authenticated non-revoked, non-expired admin
principal with `AdminAPI`; `TokenAdmin` is not required. User tokens, missing
tokens, invalid tokens, and principals without `AdminAPI` receive the existing
401/403 behavior. The handler records `middleware.Principal.TokenID`, never a
caller-supplied actor ID.

The request body is required and contains exactly one non-blank field:

```json
{"reason":"retention incident 2026-08-05"}
```

Reject malformed JSON, unknown fields, blank reasons, and reasons longer than
1024 UTF-8 bytes with `400 ANALYSIS_REPORT_REMOVAL_REASON_INVALID`. The handler
parses a UUIDv7/UUID execution ID, derives
`reports/<execution-id>.md`, and never accepts a URI, bucket, prefix, or object
key from the request. An absent execution/report returns 404 without touching
storage.

### Removal Ordering And Audit

Administrative removal is a logical revoke followed by physical deletion. This
ordering prevents a failed storage delete from continuing to expose an object:

1. Begin a short PostgreSQL transaction and lock the report row with
   `SELECT ... FOR UPDATE` by execution ID.
2. If it is already administratively removed, retain the original actor and
   reason; reuse the existing removal event and continue as an idempotent
   physical-delete retry. Do not overwrite audit history with a second reason.
3. Otherwise set `artifact_removed_at = clock_timestamp()`,
   `artifact_removed_by = principal.TokenID`, and
   `artifact_removal_reason = trimmed reason`, and insert one
   `ADMIN_REMOVE` audit event in the same transaction. Commit before calling
   storage.
4. Derive the URI from the execution ID and call storage `Delete`. Treat
   `storage.ErrNotFound` as success because the desired physical state is
   already achieved.
5. Insert an immutable `ADMIN_REMOVE_ATTEMPT` event with the delete outcome.
   Return 204 only for a successful or NotFound delete. On a timeout or other
   delete failure, return `503 ANALYSIS_REPORT_REMOVAL_PENDING`; the row remains
   removed and reads remain 410 while a bounded operator/recovery retry deletes
   the object.

DB-first ordering means a crash after the audit commit but before deletion
cannot expose the object through the report endpoint. A retry is safe because
the URI is deterministic, Delete is idempotent for the desired state, and the
original reason/actor remain immutable. A concurrent user read rechecks the
row before sending bytes, so it cannot return a successful response after the
removal decision wins.

Add a dedicated append-only `report_audit_events` table rather than overloading
`analysis_resolution_events`, whose purpose is request resolution. Its
minimum exact fields are:

```sql
CREATE TABLE report_audit_events (
    id                    UUID PRIMARY KEY DEFAULT uuidv7(),
    report_id             UUID NOT NULL,
    analysis_execution_id UUID NOT NULL,
    event_type            TEXT NOT NULL CHECK (event_type IN (
        'ADMIN_REMOVE', 'ADMIN_REMOVE_ATTEMPT', 'ARTIFACT_MISSING',
        'ARTIFACT_CORRUPT', 'ARTIFACT_REPAIRED'
    )),
    actor_token_id        UUID REFERENCES tokens(id),
    actor_component       TEXT NOT NULL CHECK (actor_component IN (
        'operator', 'report-read', 'report-recovery'
    )),
    actor_name            TEXT,
    reason                TEXT,
    request_id            TEXT,
    storage_uri           TEXT NOT NULL,
    storage_outcome       TEXT NOT NULL CHECK (storage_outcome IN (
        'NOT_ATTEMPTED', 'PENDING', 'DELETED', 'NOT_FOUND', 'FAILED'
    )),
    storage_error         TEXT,
    occurred_at           TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT report_audit_report_fk
        FOREIGN KEY (analysis_execution_id, report_id)
        REFERENCES reports (analysis_execution_id, id),
    CONSTRAINT report_audit_reason_check
        CHECK (event_type NOT IN ('ADMIN_REMOVE', 'ARTIFACT_REPAIRED')
            OR length(btrim(reason)) BETWEEN 1 AND 1024),
    CONSTRAINT report_audit_actor_check
        CHECK (event_type <> 'ADMIN_REMOVE'
            OR (actor_token_id IS NOT NULL AND actor_component = 'operator'))
);

CREATE UNIQUE INDEX uq_report_admin_remove_event
    ON report_audit_events (report_id)
    WHERE event_type = 'ADMIN_REMOVE';
CREATE UNIQUE INDEX uq_report_availability_observation_event
    ON report_audit_events (report_id, event_type)
    WHERE event_type IN ('ARTIFACT_MISSING', 'ARTIFACT_CORRUPT');
CREATE INDEX idx_report_audit_events_report_time
    ON report_audit_events (report_id, occurred_at DESC);
```

The operator event therefore records report ID, execution ID, deterministic
URI, action, reason, authenticated token ID, actor name snapshot, request ID,
event time, storage outcome, and storage error. System observations use a null
token ID and a typed component. The current token/auth code provides a stable
`Principal.TokenID` and `AdminAPI` permission, but no report audit table or
report-specific authorization handler exists.

### Links, History, SQL, And Repository

`analysis_runs.report_id` remains linked to the same report row after expiry,
missing detection, corruption, or administrative removal. `observed_root_batch_id`
and the immutable Root history are not cleared or rewritten. `GET /analyses/{id}`
may expose the derived availability (`AVAILABLE`, `EXPIRED`, `MISSING`,
`CORRUPT`, or `REMOVED`) without probing storage. `GET /analyses/{id}/report`
uses the request's current `report_id`; it never serves a previous report after
a rerun/adoption changes that request's link. The report row and audit events
preserve the historical association for operators.

Add SQLC queries with these semantics:

- `GetReportForAnalysisRead`: owner-scoped join through
  `analysis_runs.execution_id`, `analysis_runs.report_id`, and the composite
  report execution key; return 404-equivalent no rows for foreign links.
- `FindReportCacheHit`: apply the availability predicate above and never join
  to storage or use a storage existence check.
- `LinkAnalysisReportAndCompleteTask`: in one transaction lock the final task,
  Root, and execution bridge in the established order; insert the READY row or
  validate the matching existing row; then complete the final task only when
  the task, Root, execution, URI, size, hash, and `completed_at IS NULL` checks
  match.
- `MarkReportMissing`: conditionally set `artifact_missing_at` only when the
  report is unexpired, not removed, not corrupt, and the marker is NULL; insert
  one `ARTIFACT_MISSING` event in the same transaction.
- `MarkReportCorrupt`: conditionally set the corruption timestamp/reason and
  insert one `ARTIFACT_CORRUPT` event; never alter Root or request status.
- `BeginAdminReportRemoval`: lock by execution ID, set the three removal
  fields, and insert or return the unique `ADMIN_REMOVE` event in one
  transaction.
- `RecordReportRemovalAttempt`: insert an immutable
  `ADMIN_REMOVE_ATTEMPT` event with the outcome and error for every physical
  delete attempt; never update or duplicate the original `ADMIN_REMOVE` event.
- `ListStaleReportDeliveryTasks` and `RequeueReportDeliveryTask`: retain the
  earlier nonterminal delivery recovery rules; never select a terminal Root.
- `GetReportAvailabilityForAnalysis`: return the DB-derived state for metadata
  reads without storage access.

These now live behind a narrow `repo.Reports` interface and a
`PipelineRuntime.CompleteAnalysisReport` transaction method. The current
implementation keeps report completion out of generic `TaskReporter` and
provides the report accessor, execution link, generated queries, and
conditional storage operations. The full crash/recovery and rerun guarantees
remain open and are not implied by the basic completion path.

The implemented storage layer uses `PutIfAbsent` and bounded `Stat`; the
report read path uses bounded `Get`, not `Stat`, so it can verify the exact
bytes returned. Local storage uses exclusive creation and S3 uses a conditional
write with explicit SHA-256 metadata rather than trusting multipart ETags.

### Migration And Code Generation

The ordered migration list below is the original design plan and is retained
for traceability. The current working tree has `000007_analysis_runs`,
`000008_report_delivery`, and `000009_pipeline_snapshot_columns`; report
schema, generated models, queries, and routes are now present.

1. `000008_canonical_urls`.
2. `000009_analysis_executions` and analyzer Root linkage/failure fields.
3. `000010_reports` with the complete READY-only table and constraints above.
4. `000011_analysis_run_links` with `execution_id`,
   `observed_root_batch_id`, `report_id`, paired-null checks, and composite FKs.
5. `000012_resolution_events` for request/system events already specified.
6. `000013_integrity_hardening` for final failure/hash/size constraints.
7. `000014_report_artifact_availability` for the availability columns, audit
   table, indexes, and removal event uniqueness if they are not included in
   `000010`.

The implemented migrations preserve the dependency order and generated files
under `internal/repo/pg/` were regenerated rather than hand-edited. After any
future SQL or repository-interface changes, run `task sqlc`, `task mocks`,
refresh `db/schema.sql`, and run fresh-database migration tests.

### Mandatory Tests

These are the full contract test targets for merge completion, not optional
examples. The current implementation has a partial subset of this coverage,
including deterministic report writing, local conditional storage, expiry,
missing-artifact handling, owner isolation, and basic report API behavior.

- `TestFreshMigrationCreatesReadyOnlyReportConstraints`: insert one valid
  report, reject invalid URI/hash/size/expiry, reject partial removal fields,
  reject mismatched execution/Root/report composite links, and prove one report
  per execution.
- `TestReportCachePredicateExcludesExpiredMissingCorruptAndRemoved`: verify
  each tombstone is excluded while an unexpired unmarked row is a hit.
- `TestReportReadExpiredDoesNotCallStorage`: set the DB clock at/after expiry,
  assert 410 and zero `Get`, `Stat`, `HEAD`, or list calls.
- `TestReportReadMissingArtifactRecordsOnceAndReturns410`: make a pre-expiry
  `Get` return `storage.ErrNotFound`, assert the marker/event and 410, then
  assert a second read does not call storage.
- `TestReportReadPrecedenceRemovalExpiryMissingCorruptAndTimeout`: exercise
  every pair of overlapping markers and assert removal, expiry, missing,
  corruption, and timeout precedence exactly as specified.
 - `TestReportReadHashMismatchIsBoundedAndDurable`: assert 500, no successful
   response headers/body, one corruption marker/event, and no subsequent cache
   hit.
- `TestAdminReportRemovalRequiresAdminAndReason`: assert missing/invalid/user/
  non-admin callers fail, AdminAPI succeeds, the URI in the request is ignored,
  and the reason/token/request fields are audited.
- `TestAdminReportRemovalIsIdempotentAcrossDeleteFailures`: inject a crash after
  the DB removal commit, retry after a storage NotFound, and retry after a
  timeout; assert one removal event, unchanged original actor/reason, 410 reads,
  and eventual physical deletion.
- `TestAdminReportRemovalConcurrentRequests`: assert one locked removal
  decision, one unique `ADMIN_REMOVE` event, and no reason/actor overwrite.
- `TestReportLinkAndFinalTaskAreAtomic`: cover write-before-link, link-before-
  task-completion, duplicate matching writes, mismatching deterministic bytes,
  and finisher/recovery races; assert task, Root, request, report, and audit
  rows after each crash point.
- `TestTerminalRootMissingArtifactIsNotReopened`: seed a successful terminal
  Root/report, remove the object, and assert no task retry, Root update, or
  request status update from the read path.
- `TestReportRerunAndCacheBehaviorForEveryAvailabilityCase`: assert available
  report adoption, no cache hit for each tombstone, no automatic Root creation,
  and `409 ANALYSIS_RERUN_NOT_ELIGIBLE`/omitted rerun URL for completed
  unavailable reports.
- `TestReportMetadataReadDoesNotProbeStorage`: assert `GET /analyses/{id}`
  exposes DB availability and history without storage calls.
- `TestReportEndpointCrossOwnerAndAssociationIsolation`: assert 404 and zero
  storage calls for a foreign owner or mismatched execution/report link.
- `TestReportEndpointEnforcesOneMiBAndHashBeforeHeaders`: assert exactly
  1,048,576 bytes succeeds and 1,048,577 bytes or altered/truncated content
  fails before headers.
- `TestStoragePutIfAbsentAndStatLocalAndS3`: cover absent, matching,
  mismatching, concurrent writer, partial upload, timeout, and S3 conditional
  request behavior without overwrite.

The current focused tests do not yet cover every crash, race, S3 conditional
write, removal retry, rerun, or final-task atomicity case listed above. The
passing short suite and lint therefore do not make every report checklist
entry `[x]`.

### Historical Blocker, Superseded 2026-08-05

The blocker recorded in the original review stated that report schema,
conditional storage APIs, report generation, repository methods, routes,
configuration, generated code, and tests were all absent. That statement is no
longer current: those implementation layers now exist and fresh migration,
unit, lint, and PostgreSQL integration checks pass.

The remaining `[ ]` items are intentionally not all closed. In particular,
the mandatory crash/race coverage, report-delivery recovery, rerun semantics,
analysis request idempotency, and production LLM renderer still require
implementation or additional verification before merge completion.
