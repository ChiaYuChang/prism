# Why `tasks` has no `candidate_id` foreign key

## Question

`tasks.url` is the dedup key for `PAGE_FETCH`; URL also drives the
collector lookup. PAGE_FETCH tasks always correspond to exactly one
candidate. Why is there no `candidate_id UUID FK candidates(id)`
column on `tasks`?

## Short answer

`tasks` is intentionally **polymorphic across `task_kind`**.
Kind-specific data lives in `payload` / `meta` JSONB, not as typed
columns. A `candidate_id` column would either be NULL for non-PAGE_FETCH
kinds (sparse-column smell) or force every kind to carry a candidate
(semantic distortion). The work-queue layer trades typed FK enforcement
for kind-decoupling. The user-facing layer
(`user_fetch_request_items.candidate_id FK candidates(id)`) supplies
the typed FK where the business cares.

## Three task kinds, three relationships to candidates

| Kind | Candidate? | Why |
|---|---|---|
| `DIRECTORY_FETCH` | None | The task discovers candidates by crawling a directory page. There is no candidate at task-creation time; candidates are the output. |
| `KEYWORD_SEARCH` | None | Configured by `{phrase, site}` payload. Calls a search API and emits candidates. No input candidate. |
| `PAGE_FETCH` | One | Either auto-created by the discovery candidate sink (PARTY) or by the user-fetch handler (MEDIA). Always corresponds to exactly one candidate. |

Adding a typed `candidate_id` column to a table that two of three
kinds don't use is the textbook sparse-column anti-pattern.

## Alternatives considered

### A. `candidate_id UUID NULL FK candidates(id)` + CHECK constraint

```sql
ALTER TABLE tasks ADD COLUMN candidate_id UUID REFERENCES candidates(id);
ALTER TABLE tasks ADD CONSTRAINT tasks_candidate_id_kind_invariant
    CHECK ((kind = 'PAGE_FETCH') = (candidate_id IS NOT NULL));
```

**Pro**: PAGE_FETCH gets typed FK, cascade hooks, btree-friendly
indexing on `candidate_id`.

**Con**: kind-specific schema in the polymorphic table. Future kinds
have to revisit the CHECK invariant. Sparse column for non-PAGE_FETCH
rows. Not free.

### B. Per-kind tables with a shared parent

```
tasks (id, kind, ...)         -- generic header
page_fetch_tasks (task_id, candidate_id FK)
keyword_search_tasks (task_id, query, site)
directory_fetch_tasks (task_id, base_url)
```

**Pro**: Each kind is fully typed. FK constraints land where they
belong. Adding a kind = adding a table, not changing existing ones.

**Con**: Joins everywhere. Scheduler claim path becomes
`tasks JOIN <per-kind>` — kills the simple `WHERE kind = ANY(...)`
filter. Migration cost is large; refactoring impact spans
`internal/repo/pg`, the scheduler claim query, every worker handler.
Worth it at scale; not worth it during prototype.

### C. Status quo — `meta.candidate_id` JSONB

What ships today. handler.go encodes it on PAGE_FETCH creation:

```go
meta, _ := json.Marshal(map[string]any{"candidate_id": c.ID.String()})
// passed via repo.CreateTaskParams.Meta
```

Collector pulls it back via `extractCandidateID(sig.Meta)`.

**Pro**: zero schema cost; tasks stays kind-agnostic; new kinds
require no migration; matches the polymorphic-queue pattern used by
many job systems (Sidekiq, Celery, RQ).

**Con**: weak typing; no FK enforcement; `meta->>'candidate_id'`
extraction is more expensive than a column lookup; Go-side helpers
are required to safely parse the JSONB.

## Why C is the right trade-off (for now)

1. **Prototype velocity**: shipped, tested, working. Investment in A
   or B has not paid off yet.
2. **The validation is supplied at the layer that needs it**:
   `user_fetch_request_items.candidate_id` is a typed FK to
   `candidates(id)`. The user-facing layer enforces the relationship
   directly; the work-queue layer doesn't have to.
3. **Tasks may outlive candidates**: if a candidate is ever
   soft-deleted (planned for later), an active PAGE_FETCH task for
   that URL is still meaningful — `tasks.url` is the natural key for
   PAGE_FETCH dedup, not `candidate_id`. URL persistence on the task
   is what makes the dedup index `uq_tasks_active_page_fetch (kind,
   url)` work across candidate purges.
4. **Cloud-promotion split path**: when `tasks` is decomposed into
   per-kind tables (option B) during the SQS+Lambda promotion (see
   `docs/plan/future.md`), this concern dissolves. Investing now in A
   would just be removed later. Investing in B early multiplies
   refactor cost without payback.

## What the user-fetch handler does to compensate

The handler must:
1. `Scout.GetCandidatesByIDs(candidate_ids)` to look up URLs
2. Pass URL into `Tasks.CreateTask` (URL-keyed dedup)
3. Pass URL into `Tasks.GetActivePageFetchTaskByURL` (URL-keyed lookup)

Step 3 in the user-fetch flow uses
`Pipeline.GetContentByCandidateID(c.ID)` — **candidate_id-keyed**, not
URL-keyed — because `contents` does have a typed `candidate_id` FK
(it's 1:1 with `candidates`). Use the typed key wherever it exists;
fall back to URL only at the work-queue boundary.

This pattern — typed identifiers everywhere except the polymorphic
work queue — is the practical shape of the trade-off.

## When to revisit

- During the SQS+Lambda promotion: prefer option B (per-kind tables).
  At that point the refactor cost is amortized into the broader
  cloud-shape rewrite.
- If a typed `candidate_id` index is genuinely needed for query
  performance on `tasks` (e.g. an admin page listing all PAGE_FETCH
  attempts for a given candidate). Today no such query exists; if one
  appears, an expression index `ON tasks ((meta->>'candidate_id'))`
  resolves it without a column.
- If a future task kind also wants `candidate_id` (e.g.
  `EMBED_CONTENT`), the polymorphic-JSONB pattern continues to scale;
  per-kind tables become more attractive once 4+ kinds exist.

## See also

- `docs/plan/spec.md` §6 — bullets on `tasks` polymorphism, payload
  semantics, and `task_kind` enum.
- `docs/plan/future.md` — cloud anti-patterns + per-kind tables
  candidate refactor.
- `internal/collector/handler.go` `extractCandidateID` — Go-side
  helper that absorbs the weak-typing cost.
- `db/migrations/000001_init.up.sql` — `tasks.payload`, `tasks.meta`,
  `tasks.payload_hash` definitions.
