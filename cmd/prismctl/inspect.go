package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type adminModelView struct {
	ID       int16     `json:"id"`
	Name     string    `json:"name"`
	Provider string    `json:"provider"`
	Type     string    `json:"type"`
	Tag      *string   `json:"tag,omitempty"`
	Created  time.Time `json:"created_at"`
}

type adminPromptView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Version   int32     `json:"version"`
	Hash      string    `json:"hash"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type adminCandidateView struct {
	ID              uuid.UUID  `json:"id"`
	BatchID         uuid.UUID  `json:"batch_id"`
	SourceAbbr      string     `json:"source_abbr"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
	Description     *string    `json:"description,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	DiscoveredAt    time.Time  `json:"discovered_at"`
	IngestionMethod string     `json:"ingestion_method"`
	TraceID         string     `json:"trace_id"`
}

type adminTaskView struct {
	ID             uuid.UUID  `json:"id"`
	BatchID        uuid.UUID  `json:"batch_id"`
	TraceID        string     `json:"trace_id"`
	Kind           string     `json:"kind"`
	SourceType     string     `json:"source_type"`
	SourceAbbr     string     `json:"source_abbr"`
	URL            string     `json:"url"`
	Payload        any        `json:"payload,omitempty"`
	Status         string     `json:"status"`
	RetryCount     int        `json:"retry_count"`
	FailureMessage *string    `json:"failure_message,omitempty"`
	NextRunAt      time.Time  `json:"next_run_at"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type adminBatchView struct {
	ID                   uuid.UUID  `json:"id"`
	SourceType           string     `json:"source_type"`
	TraceID              *string    `json:"trace_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	PublishedAt          *time.Time `json:"published_at,omitempty"`
	LastPublishAttemptAt *time.Time `json:"last_publish_attempt_at,omitempty"`
	PublishRetryCount    int        `json:"publish_retry_count"`
	PublishError         *string    `json:"publish_error,omitempty"`
	StalledAt            *time.Time `json:"stalled_at,omitempty"`
}

type adminContentView struct {
	ID          uuid.UUID `json:"id"`
	BatchID     uuid.UUID `json:"batch_id"`
	Type        string    `json:"type"`
	SourceAbbr  string    `json:"source_abbr"`
	CandidateID uuid.UUID `json:"candidate_id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Author      *string   `json:"author,omitempty"`
	PublishedAt time.Time `json:"published_at"`
	FetchedAt   time.Time `json:"fetched_at"`
	TraceID     string    `json:"trace_id"`
}

type adminFetchView struct {
	FetchID         uuid.UUID       `json:"fetch_id"`
	Total           int64           `json:"total"`
	Pending         fetchStatusView `json:"pending"`
	Running         fetchStatusView `json:"running"`
	Completed       fetchStatusView `json:"completed"`
	Failed          fetchStatusView `json:"failed"`
	AlreadyComplete fetchStatusView `json:"already_complete"`
	Terminal        bool            `json:"terminal"`
}

type fetchStatusView struct {
	Count int `json:"count"`
}

type listResponse[T any] struct {
	Items []T   `json:"items"`
	Limit int32 `json:"limit"`
	Next  int32 `json:"next"`
	Count int   `json:"count"`
}

type candidateListResponse struct {
	Items  []adminCandidateView `json:"items"`
	Limit  int32                `json:"limit"`
	Offset int32                `json:"offset"`
	Count  int                  `json:"count"`
}

func adminStatusCommand(ctx *cliContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show API status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			a.baseURL = publicAPIBase(a.baseURL)
			var out map[string]any
			if err := a.request(cmd.Context(), http.MethodGet, "/status", nil, &out); err != nil {
				return renderError(ctx, "admin", "status", err, warnings)
			}
			return render(ctx, "admin", "status", out, warnings)
		},
	}
}

func adminModelsListCommand(ctx *cliContext) *cobra.Command {
	var limit, next int32
	cmd := &cobra.Command{Use: "list", Short: "List registered models", RunE: func(cmd *cobra.Command, _ []string) error {
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out listResponse[adminModelView]
		path := fmt.Sprintf("/admin/models?limit=%d&next=%d", limit, next)
		if err := a.request(cmd.Context(), http.MethodGet, path, nil, &out); err != nil {
			return renderError(ctx, "admin", "models_list", err, warnings)
		}
		return render(ctx, "admin", "models_list", out, warnings)
	}}
	cmd.Flags().Int32Var(&limit, "limit", 100, "Result limit")
	cmd.Flags().Int32Var(&next, "next", 1, "Next offset token")
	return cmd
}

func adminPromptsListCommand(ctx *cliContext) *cobra.Command {
	var name string
	var limit, next int32
	cmd := &cobra.Command{Use: "list", Short: "List prompt versions", RunE: func(cmd *cobra.Command, _ []string) error {
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out listResponse[adminPromptView]
		path := fmt.Sprintf("/admin/prompts?limit=%d&next=%d", limit, next)
		if name != "" {
			path += "&name=" + url.QueryEscape(name)
		}
		if err := a.request(cmd.Context(), http.MethodGet, path, nil, &out); err != nil {
			return renderError(ctx, "admin", "prompts_list", err, warnings)
		}
		return render(ctx, "admin", "prompts_list", out, warnings)
	}}
	cmd.Flags().StringVar(&name, "name", "", "Prompt name")
	cmd.Flags().Int32Var(&limit, "limit", 100, "Result limit")
	cmd.Flags().Int32Var(&next, "next", 1, "Next offset token")
	return cmd
}

func adminCandidatesListCommand(ctx *cliContext) *cobra.Command {
	var query, source string
	var limit, offset int32
	cmd := &cobra.Command{Use: "list", Short: "List candidates", RunE: func(cmd *cobra.Command, _ []string) error {
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out candidateListResponse
		path := fmt.Sprintf("/admin/candidates?limit=%d&offset=%d", limit, offset)
		if query != "" {
			path += "&q=" + url.QueryEscape(query)
		}
		if source != "" {
			path += "&source_abbr=" + url.QueryEscape(source)
		}
		if err := a.request(cmd.Context(), http.MethodGet, path, nil, &out); err != nil {
			return renderError(ctx, "admin", "candidates_list", err, warnings)
		}
		return render(ctx, "admin", "candidates_list", out, warnings)
	}}
	cmd.Flags().StringVar(&query, "q", "", "Keyword filter")
	cmd.Flags().StringVar(&source, "source-abbr", "", "Source abbreviation")
	cmd.Flags().Int32Var(&limit, "limit", 50, "Result limit")
	cmd.Flags().Int32Var(&offset, "offset", 0, "Result offset")
	return cmd
}

func adminCandidatesGetCommand(ctx *cliContext) *cobra.Command {
	return getByIDCommand(ctx, "candidates", "Get candidate", "/admin/candidates/", "candidates_get", adminCandidateView{}, false)
}

func adminBatchesListCommand(ctx *cliContext) *cobra.Command {
	var limit, next int32
	cmd := &cobra.Command{Use: "list", Short: "List batches", RunE: func(cmd *cobra.Command, _ []string) error {
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out listResponse[adminBatchView]
		path := fmt.Sprintf("/admin/batches?limit=%d&next=%d", limit, next)
		if err := a.request(cmd.Context(), http.MethodGet, path, nil, &out); err != nil {
			return renderError(ctx, "admin", "batches_list", err, warnings)
		}
		return render(ctx, "admin", "batches_list", out, warnings)
	}}
	cmd.Flags().Int32Var(&limit, "limit", 100, "Result limit")
	cmd.Flags().Int32Var(&next, "next", 1, "Next offset token")
	return cmd
}

func adminTasksListCommand(ctx *cliContext) *cobra.Command {
	var batchID string
	cmd := &cobra.Command{Use: "list", Short: "List tasks for a batch", RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := uuid.Parse(batchID); err != nil {
			return fmt.Errorf("--batch-id must be a UUID")
		}
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out struct {
			Items   []adminTaskView `json:"items"`
			BatchID uuid.UUID       `json:"batch_id"`
			Count   int             `json:"count"`
		}
		if err := a.request(cmd.Context(), http.MethodGet, "/admin/tasks?batch_id="+url.QueryEscape(batchID), nil, &out); err != nil {
			return renderError(ctx, "admin", "tasks_list", err, warnings)
		}
		return render(ctx, "admin", "tasks_list", out, warnings)
	}}
	cmd.Flags().StringVar(&batchID, "batch-id", "", "Batch UUID")
	_ = cmd.MarkFlagRequired("batch-id")
	return cmd
}

func adminTasksGetCommand(ctx *cliContext) *cobra.Command {
	return getByIDCommand(ctx, "tasks", "Get task", "/admin/tasks/", "tasks_get", adminTaskView{}, false)
}

func adminTasksRetryCommand(ctx *cliContext) *cobra.Command {
	return &cobra.Command{Use: "retry <id>", Short: "Retry a failed task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := uuid.Parse(args[0]); err != nil {
			return fmt.Errorf("id must be a UUID")
		}
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out adminTaskView
		if err := a.request(cmd.Context(), http.MethodPost, "/admin/tasks/"+url.PathEscape(args[0])+"/retry", nil, &out); err != nil {
			return renderError(ctx, "admin", "tasks_retry", err, warnings)
		}
		return render(ctx, "admin", "tasks_retry", out, warnings)
	}}
}

func adminTasksCreateCommand(ctx *cliContext) *cobra.Command {
	var batchID, kind, sourceType, sourceAbbr, rawURL, traceID, payload, meta string
	var nextRunAt, expiresAt string
	cmd := &cobra.Command{Use: "create", Short: "Create a controlled operator task", RunE: func(cmd *cobra.Command, _ []string) error {
		id := batchID
		if id == "" {
			id = uuid.NewString()
		}
		batch, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("--batch-id must be a UUID")
		}
		if traceID == "" {
			traceID = "prismctl-" + uuid.NewString()
		}
		body := map[string]any{
			"batch_id": batch, "kind": kind, "source_type": sourceType,
			"source_abbr": sourceAbbr, "url": rawURL, "trace_id": traceID,
		}
		if payload != "" {
			body["payload"] = json.RawMessage(payload)
		}
		if meta != "" {
			body["meta"] = json.RawMessage(meta)
		}
		if nextRunAt != "" {
			value, parseErr := time.Parse(time.RFC3339, nextRunAt)
			if parseErr != nil {
				return fmt.Errorf("invalid --next-run-at: %w", parseErr)
			}
			body["next_run_at"] = value
		}
		if expiresAt != "" {
			value, parseErr := time.Parse(time.RFC3339, expiresAt)
			if parseErr != nil {
				return fmt.Errorf("invalid --expires-at: %w", parseErr)
			}
			body["expires_at"] = value
		}
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out adminTaskView
		if err := a.request(cmd.Context(), http.MethodPost, "/admin/tasks", body, &out); err != nil {
			return renderError(ctx, "admin", "tasks_create", err, warnings)
		}
		return render(ctx, "admin", "tasks_create", out, warnings)
	}}
	cmd.Flags().StringVar(&batchID, "batch-id", "", "Batch UUID; generated when omitted")
	cmd.Flags().StringVar(&kind, "kind", "DIRECTORY_FETCH", "Task kind")
	cmd.Flags().StringVar(&sourceType, "source-type", "PARTY", "Source type")
	cmd.Flags().StringVar(&sourceAbbr, "source-abbr", "", "Source abbreviation")
	cmd.Flags().StringVar(&rawURL, "url", "", "Absolute task URL")
	cmd.Flags().StringVar(&traceID, "trace-id", "", "Trace identifier")
	cmd.Flags().StringVar(&payload, "payload", "", "Task payload as JSON")
	cmd.Flags().StringVar(&meta, "meta", "", "Task metadata as JSON")
	cmd.Flags().StringVar(&nextRunAt, "next-run-at", "", "Next run RFC3339 timestamp")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "Expiry RFC3339 timestamp")
	_ = cmd.MarkFlagRequired("source-abbr")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func adminContentsGetCommand(ctx *cliContext) *cobra.Command {
	return getByIDCommand(ctx, "contents", "Get content by candidate ID", "/contents/", "contents_get", adminContentView{}, true)
}

func adminFetchesGetCommand(ctx *cliContext) *cobra.Command {
	return getByIDCommand(ctx, "fetches", "Get fetch progress", "/fetches/", "fetches_get", adminFetchView{}, true)
}

func getByIDCommand[T any](ctx *cliContext, use, short, prefix, action string, result T, public bool) *cobra.Command {
	return &cobra.Command{Use: "get <id>", Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := uuid.Parse(args[0]); err != nil {
			return fmt.Errorf("id must be a UUID")
		}
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		if public {
			a.baseURL = publicAPIBase(a.baseURL)
		}
		out := result
		if err := a.request(cmd.Context(), http.MethodGet, prefix+url.PathEscape(args[0]), nil, &out); err != nil {
			return renderError(ctx, "admin", action, err, warnings)
		}
		return render(ctx, "admin", action, out, warnings)
	}}
}

func publicAPIBase(adminBase string) string {
	u, err := url.Parse(adminBase)
	if err != nil || u.Port() != "8091" {
		return strings.TrimRight(adminBase, "/")
	}
	u.Host = u.Hostname() + ":8090"
	return strings.TrimRight(u.String(), "/")
}
