package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/ChiaYuChang/prism/internal/infra/natsdiag"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/spf13/cobra"
)

type adminDiagnostics struct {
	Services    map[string]obs.HealthStatus `json:"services"`
	NATS        *natsdiag.Snapshot          `json:"nats,omitempty"`
	NATSError   string                      `json:"nats_error,omitempty"`
	TaskSummary []adminTaskStatusSummary    `json:"task_summary"`
}

type adminTaskStatusSummary struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

func adminDiagnosticsCommand(ctx *cliContext) *cobra.Command {
	var failureLimit int32
	var strict bool
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Show aggregate service, task, and NATS diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			var out adminDiagnostics
			path := "/admin/diagnostics?failure_limit=" + strconv.FormatInt(int64(failureLimit), 10)
			if err := a.request(cmd.Context(), http.MethodGet, path, nil, &out); err != nil {
				return renderError(ctx, "admin", "diagnostics", err, warnings)
			}
			if err := render(ctx, "admin", "diagnostics", out, warnings); err != nil {
				return err
			}
			if strict {
				if reason := diagnosticsBlocker(out); reason != "" {
					return fmt.Errorf("diagnostics blocked: %s", reason)
				}
			}
			return nil
		},
	}
	cmd.Flags().Int32Var(&failureLimit, "failure-limit", 10, "Number of recent failures")
	cmd.Flags().BoolVar(&strict, "strict", false, "Return an error when diagnostics detect a blocker")
	return cmd
}

func diagnosticsBlocker(out adminDiagnostics) string {
	if out.NATSError != "" {
		return "NATS diagnostics unavailable"
	}
	for name, status := range out.Services {
		if status.Level == "ERROR" || status.Level == "CRITICAL" {
			return fmt.Sprintf("service %s is %s: %s", name, status.Level, status.Message)
		}
	}
	for _, item := range out.TaskSummary {
		if item.Status == "FAILED" && item.Count > 0 {
			return fmt.Sprintf("%d %s tasks are FAILED", item.Count, item.Kind)
		}
	}
	return ""
}
