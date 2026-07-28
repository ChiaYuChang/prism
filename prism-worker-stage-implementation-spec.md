# Prism Worker / Stage Pipeline 實作規格

> 用途：將本文件直接交給 coding agent，依序完成 Worker pipeline、Retry、QC、NATS delivery 與測試。
>
> 本文件描述的是目前已定案的架構 contract。若現有 repository 的命名或 package 佈局不同，可配合既有結構調整檔名與 package，但不可改變本文列出的責任邊界與行為語意。

## 1. 任務目標

實作一個可重用的泛型 Worker pipeline，固定執行：

```text
Decode message
  → 建立新的 Packet
  → Load → QC
  → Build → QC
  → Call → QC
  → Parse → QC
  → Store → QC
  → 視需要 Publish
  → Ack
```

系統必須同時支援：

- 每個 Stage 只處理領域工作，不接觸 NATS delivery policy。
- Worker 統一負責流程編排、QC、Retry、Telemetry、Publish 與 Ack/NAK/Term。
- 每次 attempt 使用新的 `Packet`，retry 時不得沿用上一次的 `State` 或 `Output`。
- 短期 retry 可留在常駐 Worker 內等待。
- 超過 local retry 次數後，使用 JetStream `NakWithDelay` 重排同一則訊息。
- Lambda 類部署可將 `MaxLocalRetries` 設為 `0`，讓所有 retry 都交回 JetStream。
- `Store` 必須冪等，以容忍 at-least-once delivery。

## 2. 開工前規則

Agent 在修改程式前必須：

- [ ] 閱讀 repository 內的 `AGENTS.md`、`README`、`go.mod` 與現有 worker/stage package。
- [ ] 找出既有 NATS consumer、publisher、task message、telemetry 與 error handling。
- [ ] 記錄目前 baseline：`go test ./...` 是否通過。
- [ ] 保留使用者尚未提交的變更，不重寫無關程式碼。
- [ ] 優先沿用既有 package 佈局與命名；只有 contract 不足時才新增 abstraction。
- [ ] 將 external dependency 包在窄介面後，單元測試不得依賴真實 PostgreSQL、NATS、S3 或 LLM。

若現有實作與本文 contract 衝突，Agent 應以本文為目標，但需在最後報告列出：

1. 原本行為。
2. 修改後行為。
3. 相容性影響。
4. 尚未解決的阻礙。

## 3. 已定案的責任邊界

| 元件 | 責任 |
|---|---|
| `Description` | 描述 Worker 的固定設定，包含 Retry policy |
| `Worker` | Decode、attempt orchestration、QC、Retry、Telemetry、Publish、Ack/NAK/Term |
| `Stage[I,S,O,V]` | 實作 `Load → Build → Call → Parse → Store` |
| `Packet[I,S,O]` | 保存單次 attempt 的 input、state、output 與 metadata |
| `Snapshot` / `V` | 提供 QC 使用的不可變 value view |
| `QualityControl[V]` | 在每個成功 step 後檢查 postcondition |
| Concrete Stage | 定義自己的 storage、LLM、API 窄介面及領域型別 |

### Worker 不得知道

- 特定資料表或 SQL。
- 特定 LLM prompt。
- 特定 Stage 的 state/output 型別。
- Stage 名稱對應的特殊分支。
- 領域資料如何判定「足夠」。

### Stage 不得處理

- NATS Ack、NAK、Term。
- JetStream `NumDelivered`。
- NATS subject 或 consumer policy。
- Exponential backoff。
- Worker shutdown policy。
- 自己的 retry loop。

## 4. 核心 contract

實際 package 與 identifier 名稱可依 repository 調整，但行為需等價。

```go
type Step uint8

const (
	StepLoad Step = iota
	StepBuild
	StepCall
	StepParse
	StepStore
)

type Packet[I, S, O any] struct {
	Meta   Metadata
	Input  I
	State  S
	Output O
}

type Stage[I, S, O, V any] interface {
	Name() string

	Load(context.Context, *Packet[I, S, O]) error
	Build(context.Context, *Packet[I, S, O]) error
	Call(context.Context, *Packet[I, S, O]) error
	Parse(context.Context, *Packet[I, S, O]) error
	Store(context.Context, *Packet[I, S, O]) error

	Snapshot(*Packet[I, S, O]) V
}

type QualityControl[V any] interface {
	Check(context.Context, Step, V) error
}
```

### Packet 規則

- `Input` 在 execution 期間視為 immutable。
- `State` 與 `Output` 只屬於一個 attempt。
- 同一次 attempt 的所有 step 使用同一個 `Packet`。
- 每次 retry 必須重新建立 `Packet`，並從 `Load` 開始。
- `Packet` 不保證 thread-safe。
- 不為公開欄位額外建立無用途的 getter/setter。
- Stage instance 可被多個 execution 共用，因此 Stage struct 不可保存單次執行狀態。

### Optional output

不得使用 `O` 的 zero value 猜測是否需要 Publish。

Agent 必須先檢查現有 Stage：

- 若所有成功工作都一定 Publish：不需要 optional-output abstraction。
- 若某些成功工作不需 Publish：採用明確表示法，例如 `HasOutput bool` 或 repository 既有的 `Option` 型別。

選擇後需補上測試，確認「合法的 zero-value output」不會被誤判為沒有 output。

## 5. Error contract

Stage 與 QC 只回報錯誤是否可重試，不決定如何重試。

```go
var (
	ErrRetryable = errors.New("retryable")
	ErrPermanent = errors.New("permanent")
)
```

包裝錯誤時需同時保留分類與底層 cause：

```go
return fmt.Errorf(
	"%w: load article %q: %w",
	worker.ErrRetryable,
	articleID,
	err,
)
```

要求：

- [ ] 使用 `errors.Is` 判斷，不依賴錯誤字串。
- [ ] 不丟失底層 cause。
- [ ] 同一 error chain 不可同時包含 `ErrRetryable` 與 `ErrPermanent`。
- [ ] `context.Canceled` 與 shutdown 不得被錯誤記成 permanent domain failure。
- [ ] 未分類錯誤與雙重分類錯誤必須視為 contract violation，記錄 telemetry，且不可進入無限制 retry。
- [ ] Agent 應依現有 task failure/dead-letter 機制決定 contract violation 的終止方式；若 repository 尚無對應機制，需在最終報告標成待決事項，不可靜默忽略。

## 6. 單次 attempt executor

將單次執行抽成容易測試的函式。概念上可使用：

```go
func runAttempt[I, S, O, V any](
	ctx context.Context,
	stage Stage[I, S, O, V],
	qc QualityControl[V],
	meta Metadata,
	input I,
) (*Packet[I, S, O], error)
```

固定順序：

```text
New Packet
  → Load → Snapshot → QC
  → Build → Snapshot → QC
  → Call → Snapshot → QC
  → Parse → Snapshot → QC
  → Store → Snapshot → QC
```

執行規則：

- Step 回傳 error 後立即短路。
- Step 失敗時，不執行該 step 的 Snapshot/QC。
- QC 失敗後，不執行後續 step。
- `Store` 及 Store QC 成功後，attempt 才算成功。
- Retry 時丟棄失敗 attempt 的整個 Packet。
- `runAttempt` 不呼叫 Ack、NAK、Term，也不計算 backoff。

## 7. Snapshot 與 QC

每一個成功 step 後都執行：

```text
Snapshot(packet) → QC.Check(ctx, step, snapshot)
```

要求：

- Snapshot 是 value DTO 或真正的 deep copy。
- 不可將 Packet 中的 map、slice、pointer alias 直接暴露給 QC。
- QC 只能評價，不得修改 Packet。
- QC error 與 Stage error 使用相同的 retryable/permanent 分類規則。
- Telemetry 要能區分 Stage step failure 與 QC rejection。

`Load` 的特殊語意：

> `Load` 回答「本次讀取程序是否完成」；Load 後的 QC 回答「取得的資料是否足夠進入下一步」。

部分資源讀取失敗、但仍能完成收集時，可記錄：

```go
type LoadIssue struct {
	Resource string
	Err      error
}
```

此時 `Load` 可回傳 `nil`，由 QC 根據 `State.LoadIssues` 及實際資料判斷是否繼續。

## 8. 各 Stage step 的限制

### Load

- 只進行本次 attempt 的資料蒐集。
- 不在內部 retry。
- 完全無法執行讀取時，回傳 classified error。
- 部分失敗但可繼續時，將問題寫入 State，交由 QC 判斷。

### Build

- 只依賴 Load 已建立的 postcondition。
- 建立 prompt、request、batch input 等衍生資料。
- 不修改原始 Input。
- 正規化或衍生結果寫入 State。

### Call

- 負責 LLM 或其他外部 API 呼叫。
- 必須傳遞 Worker 提供的 `ctx`。
- timeout、rate limit、transport error、invalid request 需正確分類。
- 不建立第二套 retry loop。

### Parse

- 將 raw response 轉成領域型別。
- 不進行 durable write。
- malformed response、缺少必填欄位、未知 enum 與邊界值需有明確處理。
- 適合加入 fuzz test。

### Store

- Durable write 應在單一 local transaction 中完成。
- 不將 transaction 橫跨多個 Stage step。
- 必須冪等。
- 相同 task/message 再執行時，不可產生重複資料或不可逆的錯誤副作用。

冪等是必要條件，因為以下流程無法完全避免：

```text
Store 成功
  → Publish 或 Ack 前 Worker crash
  → JetStream 重新投遞
  → 整個 Stage 從 Load 重跑
```

## 9. Retry contract

### 設定

```go
type RetryDescription struct {
	BaseDelay       time.Duration
	MaxDelay        time.Duration
	MaxRetries      uint64
	MaxLocalRetries uint64
}
```

語意：

- `MaxRetries`：總 retry budget，不含 initial attempt。
- `MaxLocalRetries`：在第一次 JetStream delivery 中，最多有幾次 retry 留在 Worker 內等待。
- `BaseDelay`：retry 1 的等待時間。
- `MaxDelay`：backoff 上限。
- Message payload 不攜帶 retry count。
- 超過 local retry 範圍後，使用 `NakWithDelay` 重排同一則 stream message。
- 不得用 `Publish(new retry message) → Ack(old message)` 更新 retry count。

### Backoff

```text
retry 1 → BaseDelay
retry 2 → BaseDelay × 2
retry 3 → BaseDelay × 4
...
```

概念式：

```go
delay := backoff(
	desc.BaseDelay,
	desc.MaxDelay,
	nextRetry-1,
)
```

`backoff` 必須：

- [ ] 正確處理 retry 1 的 off-by-one。
- [ ] 防止整數及 `time.Duration` overflow。
- [ ] 不超過 `MaxDelay`。
- [ ] 對不合法設定進行 validation。
- [ ] 實作成純函式，測試不可真的 sleep。

### Retry index

第一次 delivery 從 `nextRetry = 1` 開始。

JetStream 重新投遞後：

```go
nextRetry := uint64(meta.NumDelivered)

if meta.NumDelivered > 1 {
	nextRetry += desc.MaxLocalRetries
}
```

較明確的等價寫法：

```go
func nextRetryForDelivery(
	numDelivered uint64,
	maxLocalRetries uint64,
) uint64 {
	if numDelivered <= 1 {
		return 1
	}

	return numDelivered + maxLocalRetries
}
```

這個值代表「本次 attempt 失敗後，接下來要安排的 retry 編號」。

### 範例

設定：

```text
BaseDelay       = 30s
MaxLocalRetries = 2
```

| 目前執行 | 失敗後安排 | 動作 |
|---|---:|---|
| initial attempt | retry 1 | local wait 30s |
| retry 1 | retry 2 | local wait 60s |
| retry 2 | retry 3 | `NakWithDelay(120s)` |
| delivery 2 執行 retry 3 | retry 4 | `NakWithDelay(240s)` |
| delivery 3 執行 retry 4 | retry 5 | `NakWithDelay(480s)`，受 `MaxDelay` 限制 |

注意：

- `NumDelivered` 計算 NATS delivery，不是 Worker 內的實際 attempt 數。
- Worker crash、AckWait timeout 或 shutdown 可能使兩者少量不一致。
- 這個誤差已被接受；以較寬鬆的 `MaxDeliver` 吸收。
- 一旦訊息已是 redelivery，不再重新取得一整組 local retry budget。

### Lambda 模式

```go
MaxLocalRetries: 0
```

預期流程：

```text
initial attempt
  → retryable error
  → NakWithDelay(BaseDelay)

delivery 2
  → retry 1 執行失敗
  → NakWithDelay(BaseDelay × 2)
```

不需要另一套 Lambda retry implementation。

### Retry 控制流

以下為概念流程，Agent 應配合現有 NATS abstraction 實作：

```go
nextRetry := nextRetryForDelivery(
	uint64(meta.NumDelivered),
	desc.MaxLocalRetries,
)

for {
	packet, err := runAttempt(ctx, stage, qc, metadata, input)
	if err == nil {
		if hasOutput(packet) {
			if publishErr := publisher.Publish(ctx, packet.Output); publishErr != nil {
				err = fmt.Errorf(
					"%w: publish output: %w",
					ErrRetryable,
					publishErr,
				)
			} else {
				return msg.Ack()
			}
		} else {
			return msg.Ack()
		}
	}

	switch classify(err) {
	case Permanent:
		return msg.Term()
	case Retryable:
		// continue below
	default:
		return handleContractViolation(err)
	}

	if nextRetry > desc.MaxRetries {
		return handleRetryExhausted(ctx, msg, err)
	}

	delay := backoff(
		desc.BaseDelay,
		desc.MaxDelay,
		nextRetry-1,
	)

	if meta.NumDelivered == 1 &&
		nextRetry <= desc.MaxLocalRetries {
		if err := waitWithHeartbeat(ctx, msg, delay); err != nil {
			return err
		}

		nextRetry++
		continue // runAttempt 會建立新的 Packet
	}

	return msg.NakWithDelay(delay)
}
```

上例只是 contract 說明，不要求逐字照抄。需特別注意：

- Publish failure 不得 Ack。
- Publish failure 若進入 local retry，整個 Stage 仍從 `Load` 重跑。
- `NakWithDelay` 失敗時不得 Ack 原訊息。
- Ack 失敗可能導致 redelivery，因此 Store 仍需冪等。
- 超過 `MaxRetries` 後的 task 狀態更新、告警及 dead-letter 應沿用 repository 現有機制。

## 10. AckWait、heartbeat 與 shutdown

Local wait 可能長於 consumer `AckWait`。等待期間必須避免 JetStream 將同一訊息同時投遞給另一個 Worker。

要求：

- 使用可由 `context.Context` 中止的 timer，不直接使用不可取消的 `time.Sleep`。
- Local wait 期間依適當 interval 呼叫 `InProgress()`。
- heartbeat interval 必須明顯短於 `AckWait`，並由設定或 consumer 資訊推導。
- `InProgress()` 失敗需記錄 telemetry，並採取不會 Ack 未完成工作的安全行為。
- Graceful shutdown 取消 context 後，停止目前 attempt/wait，不將它誤記為 permanent failure。
- 不要在 goroutine 中遺留 timer、ticker 或 heartbeat loop。

## 11. NATS Handler 的結果矩陣

| 結果 | Publish | Message action |
|---|---|---|
| Decode 失敗 | 否 | 依 malformed-message policy 終止，不可無限 redelivery |
| Stage/QC 成功且有 output | 是 | Publish 成功後 Ack |
| Stage/QC 成功且無 output | 否 | Ack |
| Retryable 且在 local budget | 否 | heartbeat wait，建立新 Packet 重跑 |
| Retryable 且超過 local budget | 否 | `NakWithDelay(delay)` |
| Permanent | 否 | 更新既有 failure state 後 Term |
| Retry budget exhausted | 否 | 更新既有 exhausted/dead-letter state，停止 retry |
| Publish 失敗 | 已嘗試 | 不 Ack，依 retry policy 處理 |
| Ack 失敗 | 可能已 Publish | 允許 redelivery，依靠 Store/Publish 冪等性 |

不得聲稱此架構提供 exactly-once。正確目標是：

> at-least-once delivery + idempotent side effects。

## 12. Telemetry

至少提供：

```text
worker_runs_total{stage,status}
worker_duration_seconds{stage,status}
worker_step_duration_seconds{stage,step,status}
worker_qc_total{stage,checkpoint,result}
worker_retry_total{stage,location}
worker_publish_total{stage,status}
```

規則：

- `location` 僅使用 `local` / `jetstream` 等低 cardinality 值。
- task ID、message ID、execution ID 只放 trace/log，不放 metric label。
- 每個 attempt 應有獨立 execution/attempt 資訊。
- Retry log 至少包含 stage、next retry、delay、`NumDelivered` 與 local/JetStream 決策。
- Telemetry 自身失敗不得改變 Stage 的執行結果。

## 13. Concrete Stage

每個 concrete Stage package 定義自己的窄介面，例如：

```go
type Storage interface {
	FindArticle(context.Context, string) (Article, error)
	SaveAnalysis(context.Context, Analysis) error
}

type LLM interface {
	Generate(context.Context, Request) (Response, error)
}
```

由 composition root 注入：

- PostgreSQL / `pgxpool.Pool` adapter。
- S3 adapter。
- LLM adapter。
- Concrete Stage storage implementation。
- QC。
- Telemetry。
- NATS publisher/consumer。

禁止：

- 為了方便測試把全域 client 寫入 package variable。
- 在 Stage struct 保存目前 task、Packet、retry count 或其他 execution state。
- 在 Worker 以 Stage name switch 到領域程式碼。

## 14. 建議實作順序與 Checklist

### Phase 0 — Repository inventory

- [ ] 找出現有 Worker、Stage、Packet、NATS、task message 與 tests。
- [ ] 確認 Go version、NATS client API 與 JetStream consumer 型態。
- [ ] 確認現有 publish/output 是否為 optional。
- [ ] 確認既有 terminal failure、retry exhausted、dead-letter 處理。
- [ ] 執行並記錄 baseline `go test ./...`。
- [ ] 將本文概念映射到實際 package/file，避免建立平行且重複的 framework。

### Phase 1 — Core types and validation

- [ ] 建立或調整 `Step`。
- [ ] 建立或調整泛型 `Packet`。
- [ ] 建立或調整 `Stage` 與 `QualityControl`。
- [ ] 移除沒有用途的 Packet getter/setter。
- [ ] 定案並實作 explicit optional-output 表示法。
- [ ] 建立 `ErrRetryable` / `ErrPermanent`。
- [ ] 實作 error classifier 及 contract violation guard。
- [ ] 建立 `RetryDescription` validation。
- [ ] 補齊上述純 contract 的單元測試。

### Phase 2 — Attempt executor

- [ ] 實作固定的五步順序。
- [ ] 每個成功 step 後建立 Snapshot 並執行 QC。
- [ ] Step/QC error 正確短路。
- [ ] Attempt 成功時回傳可供 Publish 的 Packet/result。
- [ ] Attempt executor 不引用 NATS。
- [ ] 以 table-driven tests 覆蓋每個 step 與 QC checkpoint。

### Phase 3 — Retry primitives

- [ ] 實作 overflow-safe exponential backoff。
- [ ] 實作 `nextRetryForDelivery`。
- [ ] 注入 clock/timer/sleeper，單元測試不等待真實時間。
- [ ] 實作可取消且含 heartbeat 的 local wait。
- [ ] `MaxLocalRetries=0` 行為正確。
- [ ] `MaxRetries=0` 行為正確。
- [ ] 超過 retry budget 時不再 wait/NAK。

### Phase 4 — NATS Handler

- [ ] Decode message。
- [ ] 讀取 JetStream message metadata。
- [ ] 成功時依 output presence Publish。
- [ ] Publish 成功後才 Ack。
- [ ] Retryable error 依 local/JetStream policy 處理。
- [ ] 重排使用 `NakWithDelay`，不 publish 新 retry message。
- [ ] Permanent error 使用既有 terminal path。
- [ ] `NakWithDelay`、Ack、Publish 失敗時不造成錯誤 Ack。
- [ ] Shutdown/cancellation 不被記成 domain permanent failure。

### Phase 5 — Concrete Stage migration

- [ ] 為 Stage 定義窄 Storage/API/LLM 介面。
- [ ] `Load` 正確區分程序失敗與部分資料問題。
- [ ] `Build` 不修改 Input。
- [ ] `Call` 傳遞 context 且無內部 retry。
- [ ] `Parse` 不寫 durable state。
- [ ] `Store` 使用 local transaction 且冪等。
- [ ] `Snapshot` 無 alias。
- [ ] Stage struct 無 execution-scoped mutable state。

### Phase 6 — Telemetry

- [ ] Run、step、QC、retry、publish 都有低 cardinality metrics。
- [ ] Trace/log 可關聯 task、message、execution、attempt。
- [ ] 可區分 local retry 與 JetStream retry。
- [ ] Telemetry failure 不影響工作結果。

### Phase 7 — Integration and CI

- [ ] PostgreSQL integration test。
- [ ] NATS JetStream integration test。
- [ ] Store 成功後、Publish 前失敗的 redelivery 測試。
- [ ] Publish 成功後、Ack 前失敗的冪等測試。
- [ ] AckWait/heartbeat 測試。
- [ ] Graceful shutdown 測試。
- [ ] `go test ./...` 通過。
- [ ] `go test -race ./...` 通過。
- [ ] 若 repository 已有 lint/staticcheck，全部通過。

## 15. 測試檔案與責任

檔名可依現有 package 調整。

| 測試單元 | 必測內容 |
|---|---|
| `packet_test.go` | 初始化、Input 語意、attempt 間不共享 State/Output |
| `error_test.go` | `errors.Is`、cause 保留、雙重分類、未分類錯誤 |
| `backoff_test.go` | base、倍增、MaxDelay、overflow、off-by-one |
| `retry_index_test.go` | `NumDelivered`、`MaxLocalRetries`、Lambda 模式 |
| `attempt_test.go` | step/QC 固定順序、短路、同一 Packet |
| `handler_test.go` | Decode、Publish、Ack、NAK、Term、exhausted |
| `wait_test.go` | timer、context cancellation、heartbeat、goroutine cleanup |
| `stage_contract_test.go` | 所有 concrete Stage 共用的 contract |
| Concrete Stage tests | Load/Build/Call/Parse/Store/Snapshot |
| Integration tests | PostgreSQL、NATS JetStream、必要 adapter |
| Fault-injection tests | crash window、timeout、Publish/Ack/heartbeat failure |
| Race tests | 相同 Stage instance 並行處理多個 Packet |

## 16. Worker orchestration 必測矩陣

### 成功路徑

- [ ] Step 順序為 `Load → Build → Call → Parse → Store`。
- [ ] 每個 step 後恰好執行一次 Snapshot/QC。
- [ ] 同一 attempt 內使用同一個 Packet。
- [ ] 有 output 時只 Publish 一次。
- [ ] Publish 成功後 Ack。
- [ ] 無 output 時不 Publish，直接 Ack。

### 每個 Stage step 分別失敗

對 Load、Build、Call、Parse、Store 各自測試：

- [ ] 立即短路。
- [ ] 不執行失敗 step 的 Snapshot/QC。
- [ ] 不執行後續 step。
- [ ] 不 Publish。
- [ ] Retryable 進入 retry policy。
- [ ] Permanent 進入 terminal path。

### 每個 QC checkpoint 分別失敗

對 Load、Build、Call、Parse、Store 後的 QC 各自測試：

- [ ] 對應 Stage step 已成功。
- [ ] 後續 step 不執行。
- [ ] 不 Publish。
- [ ] Telemetry 記為 QC rejection，而非 Stage error。
- [ ] Retryable/permanent 分類被正確尊重。

### Retry

設定：

```text
BaseDelay       = 30s
MaxLocalRetries = 2
```

必須驗證：

- [ ] Initial failure 安排 retry 1，local wait 30s。
- [ ] Retry 1 failure 安排 retry 2，local wait 60s。
- [ ] Retry 2 failure 安排 retry 3，`NakWithDelay(120s)`。
- [ ] Delivery 2 執行 retry 3；失敗後安排 retry 4，`NakWithDelay(240s)`。
- [ ] `MaxLocalRetries=0` 時第一次 failure 直接 NAK。
- [ ] `MaxRetries=0` 時第一次 failure 不安排 retry。
- [ ] `MaxDelay` 正確截斷延遲。
- [ ] 每次 local retry 都建立新的 Packet。
- [ ] Retry 後從 Load 開始，不從失敗 step 繼續。
- [ ] Context cancellation 能中止 wait。
- [ ] Local wait 期間送出 `InProgress()`。
- [ ] Redelivery 不重新取得整組 local retry budget。
- [ ] `NumDelivered` 因異常多一次時，不發生 overflow 或 panic。

### Publish、Ack 與冪等

- [ ] Store 成功、Publish 失敗：不 Ack，後續 redelivery 可安全重跑。
- [ ] Publish 成功、Ack 失敗：redelivery 不產生重複 durable data。
- [ ] `NakWithDelay` 失敗：不 Ack。
- [ ] Ack 前 shutdown：允許 redelivery。
- [ ] 不存在 `Publish(retry message) → Ack(old)` 流程。

## 17. Concrete Stage 必測矩陣

### Load

- [ ] 全部資源成功。
- [ ] 部分失敗寫入 `LoadIssues`，並由 QC 判斷。
- [ ] Storage unavailable → retryable。
- [ ] 明確不存在且不應重試 → permanent。

### Build

- [ ] 固定輸入產生固定 request。
- [ ] 缺少 Load postcondition 時正確失敗。
- [ ] Input 未被修改。

### Call

- [ ] Request 傳遞正確。
- [ ] Timeout、rate limit、transport error 分類正確。
- [ ] Invalid request 分類正確。
- [ ] Context cancellation 正確下傳。
- [ ] 沒有內部 retry。

### Parse

- [ ] 正常 response。
- [ ] Malformed JSON/資料。
- [ ] 缺少必填欄位。
- [ ] 邊界值。
- [ ] 未知 enum。
- [ ] Fuzz input 不 panic。

### Store

- [ ] Transaction commit。
- [ ] 發生錯誤時 rollback。
- [ ] 相同 task 重複執行不產生重複資料。
- [ ] Store 成功、Publish 失敗、redelivery 後仍冪等。

### Snapshot

- [ ] 欄位完整。
- [ ] QC 修改 snapshot 的 slice/map 不影響 Packet。
- [ ] Snapshot 不暴露 mutable pointer alias。
- [ ] 相同 Stage instance 並行執行無 data race。

## 18. Out of scope

除非 repository 已有現成機制，本次不要擴大為：

- Exactly-once delivery。
- 分散式 transaction。
- 為 Store + Publish 新建 transactional outbox。
- 將 retry count 寫入 message payload 或 task table。
- 在 Stage 內建立 provider-specific retry loop。
- 大規模重構所有 concrete Stage。
- 另建與現有 framework 平行的第二套 Worker abstraction。

若現有系統已使用 outbox 或 dead-letter，應整合既有機制，不要移除。

## 19. Definition of Done

以下項目全部滿足才算完成：

- [ ] Worker core contract 已實作，責任邊界符合本文。
- [ ] 固定五步 pipeline 及每步 QC 已實作。
- [ ] 每次 retry 使用新的 Packet 並從 Load 開始。
- [ ] Retry backoff、index 與 local/NATS 切換符合本文表格。
- [ ] `MaxLocalRetries=0` 可直接作為 Lambda 模式。
- [ ] Retry 重排只使用 `NakWithDelay`。
- [ ] Store 與其他必要副作用具備冪等保護。
- [ ] Ack/NAK/Term/Publish 的順序正確。
- [ ] Local wait 支援 context cancellation 與 `InProgress()`。
- [ ] Snapshot 不暴露 mutable alias。
- [ ] Stage instance 可安全被並行共用。
- [ ] 核心單元測試覆蓋所有 step、QC checkpoint 與 retry 邊界。
- [ ] Fault-injection 測試覆蓋 Store/Publish/Ack crash windows。
- [ ] `go test ./...` 通過。
- [ ] `go test -race ./...` 通過。
- [ ] Agent 已提供變更檔案列表、測試結果、相容性影響及剩餘待決事項。

## 20. Agent 最終回報格式

完成後請使用以下格式回報：

```markdown
## 完成項目

- ...

## 主要設計

- Packet lifecycle:
- QC checkpoints:
- Retry index:
- Local wait / heartbeat:
- Ack/NAK/Term:
- Idempotency:

## 變更檔案

- `path/to/file.go`: ...
- `path/to/file_test.go`: ...

## 驗證結果

- `go test ./...`: PASS/FAIL
- `go test -race ./...`: PASS/FAIL
- Integration tests: PASS/FAIL/未執行（原因）

## 與原實作的相容性影響

- ...

## 尚未完成或待決事項

- ...
```

不得只回報「已完成」；每個未通過或未執行的測試都必須說明原因。
