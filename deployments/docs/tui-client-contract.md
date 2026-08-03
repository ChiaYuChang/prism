# Prism TUI Client Contract

This document is for building the Prism operator TUI as a separate repository. The TUI is an API client only. It does not start Postgres, workers, scheduler, or the API server.

## Startup Workflow

1. Read TUI config from flags/env.
2. If no token is configured, prompt the operator for the API token with masked input.
3. Test connectivity by calling `GET /api/v1/candidates?limit=1`.
4. If the API returns `200`, enter the candidate list view.
5. If the API returns `401`, prompt for token again.
6. If the API is unreachable, show the error and offer retry, edit API URL, or quit.

Recommended TUI config:

```text
--api-url=http://localhost:8090
--token=<token>
--token-file=/path/to/token
--page-size=25
--fetch-poll-interval=5s
```

Recommended env names for the external TUI repo:

```text
PRISM_TUI_API_URL
PRISM_TUI_AUTH_TOKEN
PRISM_TUI_AUTH_TOKEN_FILE
```

## Auth

Protected Prism API routes use this header:

```text
X-PRISM-TOKEN: <token>
```

The API middleware constant is `middleware.TokenAuthHeader`. The TUI repo should hardcode the string `X-PRISM-TOKEN` unless it imports Prism internals, which is not recommended.

The TUI should attach the token to every `/api/v1/*` request. Health endpoints such as `/healthz` and `/readyz` are not sufficient for TUI startup because they do not prove token validity or candidate API availability.

Expected auth handling:

```text
200: token accepted, continue
401: token missing or invalid, ask again
403: reserved for future authorization; treat as fatal unless product changes
```

## Endpoints

Base path is `/api/v1`.

### List Candidates

```http
GET /api/v1/candidates?q=<keyword>&source_abbr=<abbr>&since=<rfc3339>&until=<rfc3339>&limit=<n>&offset=<n>
X-PRISM-TOKEN: <token>
```

Response DTO lives in `internal/http/api.ListCandidatesResponse`:

```json
{
  "items": [
    {
      "id": "uuid",
      "batch_id": "uuid",
      "source_abbr": "dpp",
      "title": "...",
      "url": "https://...",
      "description": "...",
      "published_at": "2026-05-20T00:00:00Z",
      "discovered_at": "2026-05-20T00:00:00Z",
      "ingestion_method": "DIRECTORY",
      "trace_id": "..."
    }
  ],
  "limit": 25,
  "offset": 0,
  "count": 25
}
```

Example response:

```json
{
  "items": [
    {
      "id": "01971a7b-7c8d-7d26-9f1e-8c89912a0001",
      "batch_id": "01971a7b-7c8d-7d26-9f1e-8c89912a1001",
      "source_abbr": "dpp",
      "title": "Party releases energy policy statement",
      "url": "https://example.org/news/energy-policy",
      "description": "Press release summary shown in the candidate list.",
      "published_at": "2026-05-20T00:00:00Z",
      "discovered_at": "2026-05-20T00:05:00Z",
      "ingestion_method": "DIRECTORY",
      "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
    }
  ],
  "limit": 25,
  "offset": 0,
  "count": 1
}
```

Notes:

- `count` is the current page count, not total matches.
- `q`, `source_abbr`, `since`, and `until` are backend filters.
- The TUI may additionally apply a local page filter over loaded rows, but it should label it clearly.

### Request Page Fetch

```http
POST /api/v1/page_fetch
Content-Type: application/json
X-PRISM-TOKEN: <token>

{"candidate_ids":["uuid","uuid"]}
```

Response DTO lives in `internal/http/api.PageFetchResponse`:

```json
{
  "fetch_id": "uuid",
  "items": [
    {"candidate_id": "uuid", "status": "created"},
    {"candidate_id": "uuid", "status": "already_complete"},
    {"candidate_id": "uuid", "status": "not_found"}
  ]
}
```

Example response:

```json
{
  "fetch_id": "01971a7c-1f4b-7a90-8d2c-4051d3b80001",
  "items": [
    {
      "candidate_id": "01971a7b-7c8d-7d26-9f1e-8c89912a0001",
      "status": "created"
    },
    {
      "candidate_id": "01971a7b-7c8d-7d26-9f1e-8c89912a0002",
      "status": "already_complete"
    },
    {
      "candidate_id": "01971a7b-7c8d-7d26-9f1e-8c89912a9999",
      "status": "not_found"
    }
  ]
}
```

Public item statuses:

```text
created
already_complete
not_found
```

The API intentionally never exposes task IDs.

### Monitor Fetch Progress

```http
GET /api/v1/fetches/{fetch_id}
X-PRISM-TOKEN: <token>
```

Response DTO lives in `internal/http/api.FetchProgressResponse`:

```json
{
  "fetch_id": "uuid",
  "total": 2,
  "pending": {
    "count": 0,
    "candidate_ids": []
  },
  "running": {
    "count": 1,
    "candidate_ids": ["uuid"]
  },
  "completed": {
    "count": 1,
    "candidate_ids": ["uuid"]
  },
  "failed": {
    "count": 0,
    "candidate_ids": []
  },
  "already_complete": {
    "count": 0,
    "candidate_ids": []
  },
  "terminal": false
}
```

Example in-flight response:

```json
{
  "fetch_id": "01971a7c-1f4b-7a90-8d2c-4051d3b80001",
  "total": 3,
  "pending": {
    "count": 1,
    "candidate_ids": ["01971a7b-7c8d-7d26-9f1e-8c89912a0003"]
  },
  "running": {
    "count": 1,
    "candidate_ids": ["01971a7b-7c8d-7d26-9f1e-8c89912a0001"]
  },
  "completed": {
    "count": 0,
    "candidate_ids": []
  },
  "failed": {
    "count": 0,
    "candidate_ids": []
  },
  "already_complete": {
    "count": 1,
    "candidate_ids": ["01971a7b-7c8d-7d26-9f1e-8c89912a0002"]
  },
  "terminal": false
}
```

Example terminal response:

```json
{
  "fetch_id": "01971a7c-1f4b-7a90-8d2c-4051d3b80001",
  "total": 3,
  "pending": {
    "count": 0,
    "candidate_ids": []
  },
  "running": {
    "count": 0,
    "candidate_ids": []
  },
  "completed": {
    "count": 2,
    "candidate_ids": [
      "01971a7b-7c8d-7d26-9f1e-8c89912a0001",
      "01971a7b-7c8d-7d26-9f1e-8c89912a0003"
    ]
  },
  "failed": {
    "count": 0,
    "candidate_ids": []
  },
  "already_complete": {
    "count": 1,
    "candidate_ids": ["01971a7b-7c8d-7d26-9f1e-8c89912a0002"]
  },
  "terminal": true
}
```

Notes:

- `count` is generated by the API from `candidate_ids.length`.
- `candidate_ids` contains only persisted fetch items. Candidates returned as `not_found` by `POST /page_fetch` do not appear in progress.
- The response may be served from cache, but cached and uncached responses use the same shape.

Polling guidance:

- Poll at `--fetch-poll-interval`, default `5s`.
- Stop automatic polling when `terminal=true`.
- Provide a manual refresh key.

### Get Content

```http
GET /api/v1/contents/{candidate_id}
X-PRISM-TOKEN: <token>
```

Response DTO lives in `internal/http/api.Content`:

```json
{
  "id": "uuid",
  "batch_id": "uuid",
  "type": "PARTY_RELEASE",
  "source_abbr": "dpp",
  "candidate_id": "uuid",
  "url": "https://...",
  "title": "...",
  "content": "...",
  "author": "...",
  "published_at": "2026-05-20T00:00:00Z",
  "fetched_at": "2026-05-20T00:00:00Z",
  "trace_id": "..."
}
```

Example response:

```json
{
  "id": "01971a7d-2b6c-7f01-b6c8-a90d39cc0001",
  "batch_id": "01971a7b-7c8d-7d26-9f1e-8c89912a1001",
  "type": "PARTY_RELEASE",
  "source_abbr": "dpp",
  "candidate_id": "01971a7b-7c8d-7d26-9f1e-8c89912a0001",
  "url": "https://example.org/news/energy-policy",
  "title": "Party releases energy policy statement",
  "content": "Full article text fetched by the collector.",
  "author": "Democratic Progressive Party",
  "published_at": "2026-05-20T00:00:00Z",
  "fetched_at": "2026-05-20T00:06:30Z",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

Expected status handling:

```text
200: render content
404: content not fetched yet; show waiting state and allow retry/poll
400: invalid candidate id; client bug or stale state
500: server/repo error; show error
```

## Recommended Views

### Candidate List

Default view after successful startup.

Suggested keys:

```text
j/k or arrows: move cursor
space: select/unselect candidate
n/p: next/previous backend page
/: edit backend keyword q
s: edit source_abbr
d: edit since/until date range
l: edit local page filter
f: submit selected candidates to page_fetch
enter: open content view for current candidate
b: open fetch monitor by fetch_id
q: quit
```

Display both backend and local filters:

```text
Backend: q="energy" source="dpp" since="2026-05-01" until="-"
Local: "lai"
Showing 8/25 loaded candidates
```

### Submit Result Modal

Shown after `POST /page_fetch`.

Required content:

- `fetch_id`
- per-candidate status table
- actions: monitor, dismiss

Suggested keys:

```text
m: monitor fetch_id
enter/esc: dismiss
```

### Fetch Monitor

Polls `GET /fetches/{id}`.

Required content:

- fetch ID
- progress status groups with counts and candidate IDs
- terminal state
- last refreshed time

Suggested keys:

```text
r: refresh now
esc: back
q: quit
```

### Content View

Calls `GET /contents/{candidate_id}`.

Required content:

- title
- source abbreviation
- URL
- published/fetched timestamps
- article body

Suggested keys:

```text
r: retry if content is not available
esc: back
q: quit
```

## Error Handling

The API error body is generally:

```json
{"error":"message"}
```

Examples:

```json
{"error":"missing auth token"}
```

```json
{"error":"invalid fetch id"}
```

```json
{"error":"content not found"}
```

The TUI should preserve and display the server message when possible. For non-JSON errors, display HTTP status and raw body preview.

## API Gaps To Plan Around

- Candidate list has no total match count. Use offset paging and page count from loaded rows.
- Fetch monitor groups candidate IDs by status, but it does not expose task IDs or task internals.
- No source-list endpoint. Use free-text `source_abbr` for now.
- No content list endpoint. Content is fetched by candidate ID.
