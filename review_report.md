# Prism Repository Review Report

This report evaluates the **Prism** repository, focusing on code quality, security risks, and test coverage / meaningfulness.

---

## 1. Repetitive Code

### 🔴 Yahoo News Scraping Logic Duplication
The logic to parse and extract news items from `tw.news.yahoo.com` is duplicated across two layers:
*   **RSS Proxy layer**: [yahoo.go](file:///home/cychang/Projects/prism/pkg/rss/yahoo.go#L20-L101)
*   **Custom Scout layer**: [scout.go](file:///home/cychang/Projects/prism/internal/discovery/scout/custom/yahoo/scout.go#L29-L142)

**Duplicated details:**
*   Identical regex pattern for JSON extraction: `(?s)"stream_items"\s*:\s*(\[.*?\])\s*,\s*"stream_total"`.
*   Almost identical structs representing Yahoo news items (`YahooNewsItem` in `pkg` vs `yahooNewsItem` in `internal`).
*   Duplicate time parsing logic (converting timestamp division by `1000` to `Asia/Taipei` timezone).
*   Virtually identical request headers and client execution logic.

> [!IMPORTANT]  
> Unifying this logic into a shared parser package under `internal/discovery/scout/custom/yahoo` or `pkg/rss` would prevent drift when Yahoo shifts their JSON format or attributes.

### 🟡 Text Normalization Divergence
There are multiple text cleaning and whitespace normalization utilities:
*   `NormalizeString` in [strings.go](file:///home/cychang/Projects/prism/pkg/utils/strings.go#L21-L32) (collapses whitespace, removes control characters, replaces `\u00A0` with a space).
*   `NormalizeText` in [common.go](file:///home/cychang/Projects/prism/internal/discovery/scout/common.go#L38-L40) (collapses whitespace and calls `html.UnescapeString`).

> [!TIP]  
> Unifying these into a single utility function in `pkg/utils/strings.go` ensures consistent HTML decoding and whitespace treatment across parsing and crawling pipelines.

### 🟡 HTTP Fetching Abstractions
There are multiple custom HTTP `Fetch` wrappers implementing headers, body closing, and error checks:
*   `Fetch` in [common.go](file:///home/cychang/Projects/prism/internal/discovery/scout/common.go#L15-L36) (general GET, no custom headers).
*   `Fetch` in [scout.go](file:///home/cychang/Projects/prism/internal/discovery/scout/html/scout.go#L296-L322) (HTML scout-specific GET with headers).
*   `fetchWithStatus` in [http.go](file:///home/cychang/Projects/prism/internal/collector/fetcher/http.go#L42-L74) (Collector HTTP Fetcher).

---

## 2. Security Issues

### ⚠️ Lack of SSRF Protection (Server-Side Request Forgery)
The system fetches URL feeds and candidate articles based on URLs found in candidate feeds or search results:
*   **Scout fetcher**: [common.go](file:///home/cychang/Projects/prism/internal/discovery/scout/common.go#L25)
*   **Collector fetcher**: [http.go](file:///home/cychang/Projects/prism/internal/collector/fetcher/http.go#L51)

Neither of these HTTP client calls restricts requests to private subnets or local loopback addresses (e.g. `127.0.0.1`, `10.0.0.0/8`, `192.168.0.0/16`, or cloud metadata endpoints like `169.254.169.254`). 
*   **Risk**: If an external RSS feed or search index is maliciously updated to redirect requests to internal network endpoints, the crawlers will execute requests against those endpoints.
*   **Mitigation**: Restrict connection requests in the `http.Client.Transport.DialContext` to validate that the resolved IP address is public.

### ⚠️ Concurrency Blocking in `RetryAfterHandler`
In [retry.go](file:///home/cychang/Projects/prism/internal/collector/fetcher/retry.go#L39-L46), `RetryAfterHandler` honors `Retry-After` headers via:
```go
time.Sleep(time.Duration(secs) * time.Second)
```
*   **Risk**: Direct calls to `time.Sleep` block the executing goroutine without checking `context.Context` cancellation. If a shutdown signal is sent or the parent context times out, the goroutine remains blocked.
*   **Mitigation**: Replace `time.Sleep` with a select statement checking context cancellation:
    ```go
    select {
    case <-ctx.Done():
        return false, ctx.Err()
    case <-time.After(time.Duration(secs) * time.Second):
    }
    ```

### ⚠️ Insecure Default HTTP Client (No Timeout)
In [yahoo.go](file:///home/cychang/Projects/prism/pkg/rss/yahoo.go#L47), the RSS parser executes the fetch via:
```go
resp, err := http.DefaultClient.Do(req)
```
*   **Risk**: `http.DefaultClient` has **no timeout**. If the Yahoo server slows down or hangs, the HTTP request blocks indefinitely, consuming resources and eventually causing a denial-of-service in the RSS server.
*   **Mitigation**: Always instantiate a custom `&http.Client{Timeout: ...}`.

### 🛡️ Positive Security Controls
*   **SQL Injection Prevention**: Database interaction is handled through SQLC-generated queries (e.g., [batches.sql.go](file:///home/cychang/Projects/prism/internal/repo/pg/batches.sql.go)). The use of prepared statements and parameterized inputs completely mitigates SQL injection.
*   **ReDoS (Regex DoS) Immunity**: Since the project is in Go, the native `regexp` package uses the RE2 engine. RE2 executes regex in linear time and does not support backtracking, making it immune to ReDoS attacks.
*   **Active Telemetry & Logging Sanitization**: Logging config includes automated tests (e.g., `TestLoggingConfig_NoOTELHeaderLeak`, `TestConfig_NoSecretLeak`) ensuring database credentials, API keys, and authorization headers are masked.

---

## 3. Test Coverage & Meaningfulness

Overall test coverage across the repository is **35.5%**.

### 📉 Key Coverage Gaps

1.  **Core Utilities (`pkg/`)**: 
    *   `pkg/functional` (**0%** coverage): Fundamental list manipulation helpers (`Map`, `Filter`, `Reduce`, `FlatMap`) are completely untested.
    *   `pkg/pgconv` (**0%** coverage): Critical type mapping helpers (translating Go pointers to PostgreSQL/pgtype types) have no coverage. Errors here can lead to silent null pointer panics or data truncation.
    *   `pkg/rss` (**0%** coverage): The RSS feed generator server and Yahoo scraper have no tests.
    *   `pkg/schema` (**0%** coverage): Schema translation helpers for LLM processing are untested.
    *   `pkg/utils` (**0%** coverage): Text normalization and hashing functions are untested.
2.  **Database layer (`internal/repo/pg`)**:
    *   `internal/repo/pg` (**0.9%** coverage): Unit tests mock repository interfaces (e.g. `repo.Pipeline`) but the actual SQL execution code, model adapters (`adapters.go`), and SQLC queries are virtually untested in unit testing.

### 🔬 Evaluation of Test Meaningfulness

*   **HTML & Custom Scout Tests (High Quality)**:
    Tests like [scout_test.go](file:///home/cychang/Projects/prism/internal/discovery/scout/custom/yahoo/scout_test.go) read static HTML files from local fixtures to assert regex extraction and binding logic. This is a highly meaningful strategy because it runs calculations against actual layout snapshots.
*   **Mock-Heavy Orchestration Tests (High Fragility)**:
    Tests such as [dispatcher_test.go](file:///home/cychang/Projects/prism/internal/collector/dispatcher_test.go) and [composite_test.go](file:///home/cychang/Projects/prism/internal/collector/parser/composite_test.go) make heavy use of mock expectations (`EXPECT().Fetch()`, `On("Parse")`).
    *   *Strengths*: Confirms error propagation and execution order.
    *   *Weaknesses*: Highly coupled to function signature calls. Minor refactors to how parameters flow or pipeline stages execute will break tests despite functional correctness.
*   **Missing Invariant Tests in `errorcode`**:
    The [errorcode_test.go](file:///home/cychang/Projects/prism/pkg/errorcode/errorcode_test.go) only serializes a basic `Warning` struct. It fails to test the composite `Error` struct's multi-error accumulation (`AppendError`), message output (`Error()`), warning accumulation, or compatibility with standard Go wrapping.
