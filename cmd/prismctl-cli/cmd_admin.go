package main

import (
	"net/http"
	"net/url"
	"strconv"

	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newAdminCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{Use: "admin", Short: "Admin commands"}
	tokens := &cobra.Command{Use: "tokens", Short: "Token commands"}
	tokens.AddCommand(adminTokensCreateCommand(ctx), adminTokensListCommand(ctx), adminTokensGetCommand(ctx), adminTokensRevokeCommand(ctx))
	sources := &cobra.Command{Use: "sources", Short: "Source registry commands"}
	sources.AddCommand(adminSourcesCreateCommand(ctx), adminSourcesListCommand(ctx), adminSourcesSyncCommand(ctx), adminSourcesDeleteCommand(ctx), adminSourcesRestoreCommand(ctx))
	models := &cobra.Command{Use: "models", Short: "Model registry commands"}
	models.AddCommand(adminModelsCreateCommand(ctx), adminModelsListCommand(ctx))
	prompts := &cobra.Command{Use: "prompts", Short: "Prompt version commands"}
	prompts.AddCommand(adminPromptsUploadCommand(ctx), adminPromptsListCommand(ctx))
	tasks := &cobra.Command{Use: "tasks", Short: "Task inspection commands"}
	tasks.AddCommand(adminTasksListCommand(ctx), adminTasksGetCommand(ctx), adminTasksRetryCommand(ctx), adminTasksCreateCommand(ctx))
	candidates := &cobra.Command{Use: "candidates", Short: "Candidate inspection commands"}
	candidates.AddCommand(adminCandidatesListCommand(ctx), adminCandidatesGetCommand(ctx))
	batches := &cobra.Command{Use: "batches", Short: "Batch inspection commands"}
	batches.AddCommand(adminBatchesListCommand(ctx))
	contents := &cobra.Command{Use: "contents", Short: "Content inspection commands"}
	contents.AddCommand(adminContentsGetCommand(ctx))
	fetches := &cobra.Command{Use: "fetches", Short: "Fetch progress commands"}
	fetches.AddCommand(adminFetchesGetCommand(ctx))
	nats := &cobra.Command{Use: "nats", Short: "NATS JetStream administration"}
	nats.AddCommand(adminNATSStatusCommand(ctx), adminNATSStreamsCommand(ctx), adminNATSConsumersCommand(ctx), adminNATSApplyCommand(ctx), adminNATSTestMessageCommand(ctx))
	schedulers := &cobra.Command{Use: "schedulers", Short: "Scheduler runtime controls"}
	schedulers.AddCommand(adminSchedulerPauseCommand(ctx), adminSchedulerStartCommand(ctx), adminSchedulerStatusCommand(ctx))
	cmd.AddCommand(tokens, sources, models, prompts, tasks, candidates, batches, contents, fetches, nats, schedulers, adminStatusCommand(ctx), adminDiagnosticsCommand(ctx))
	return cmd
}

func adminTokensCreateCommand(ctx *cliContext) *cobra.Command {
	var typ string
	var name string
	var expiresAt string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a token",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			expires, err := parseOptionalTime(expiresAt)
			if err != nil {
				return err
			}
			var out tokenSecretView
			err = a.request(cmd.Context(), http.MethodPost, "/admin/tokens", map[string]any{
				"type":       typ,
				"name":       name,
				"expires_at": expires,
			}, &out)
			if err != nil {
				return renderError(ctx, "admin", "tokens_create", err, warnings)
			}
			return render(ctx, "admin", "tokens_create", out, warnings)
		},
	}
	cmd.Flags().StringVar(&typ, "type", string(authtoken.TypeUser), "Token type: admin or user")
	cmd.Flags().StringVar(&name, "name", "", "Token name; required for admin/user tokens")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "Token expiry RFC3339 timestamp")
	return cmd
}

func adminTokensListCommand(ctx *cliContext) *cobra.Command {
	var limit int32
	var next int32
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			var out struct {
				Items []tokenView `json:"items"`
				Limit int32       `json:"limit"`
				Next  int32       `json:"next"`
				Count int         `json:"count"`
			}
			path := "/admin/tokens?limit=" + strconv.FormatInt(int64(limit), 10) + "&next=" + strconv.FormatInt(int64(next), 10)
			err = a.request(cmd.Context(), http.MethodGet, path, nil, &out)
			if err != nil {
				return renderError(ctx, "admin", "tokens_list", err, warnings)
			}
			return render(ctx, "admin", "tokens_list", out, warnings)
		},
	}
	cmd.Flags().Int32Var(&limit, "limit", 100, "Result limit")
	cmd.Flags().Int32Var(&next, "next", 1, "Next offset token")
	return cmd
}

func adminTokensGetCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get token by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			id, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			var out tokenView
			err = a.request(cmd.Context(), http.MethodGet, "/admin/tokens/"+url.PathEscape(id.String()), nil, &out)
			if err != nil {
				return renderError(ctx, "admin", "tokens_get", err, warnings)
			}
			return render(ctx, "admin", "tokens_get", out, warnings)
		},
	}
	return cmd
}

func adminTokensRevokeCommand(ctx *cliContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke token by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			id, err := uuid.Parse(args[0])
			if err != nil {
				return err
			}
			var out tokenView
			err = a.request(cmd.Context(), http.MethodPost, "/admin/tokens/"+url.PathEscape(id.String())+"/revoke", nil, &out)
			if err != nil {
				return renderError(ctx, "admin", "tokens_revoke", err, warnings)
			}
			return render(ctx, "admin", "tokens_revoke", out, warnings)
		},
	}
	return cmd
}
