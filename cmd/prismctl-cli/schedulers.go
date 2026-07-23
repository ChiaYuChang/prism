package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type schedulerView struct {
	Name             string `json:"name"`
	Enabled          bool   `json:"enabled"`
	EffectiveEnabled bool   `json:"effective_enabled"`
	Parent           string `json:"parent,omitempty"`
}

func adminSchedulerPauseCommand(ctx *cliContext) *cobra.Command {
	return schedulerMutationCommand(ctx, "pause", "Pause a scheduler, or all schedulers when no name is given", false)
}

func adminSchedulerStartCommand(ctx *cliContext) *cobra.Command {
	return schedulerMutationCommand(ctx, "start", "Start a scheduler, or all schedulers when no name is given", true)
}

func schedulerMutationCommand(ctx *cliContext, verb, short string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " [name]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = strings.TrimSpace(args[0])
			}
			path := "/admin/schedulers/"
			if name == "" {
				path = "/admin/schedulers/"
				if enabled {
					path += "resume"
				} else {
					path += "pause"
				}
			} else {
				path += url.PathEscape(name) + "/"
				if enabled {
					path += "resume"
				} else {
					path += "pause"
				}
			}
			var out schedulerView
			if err := a.request(cmd.Context(), http.MethodPost, path, nil, &out); err != nil {
				return renderError(ctx, "admin", "schedulers_"+verb, err, warnings)
			}
			return render(ctx, "admin", "schedulers_"+verb, out, warnings)
		},
	}
}

func adminSchedulerStatusCommand(ctx *cliContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Show scheduler runtime state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			name := "global"
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				name = strings.TrimSpace(args[0])
			}
			var out schedulerView
			path := "/admin/schedulers"
			if name != "global" {
				path += "/" + url.PathEscape(name)
			}
			if err := a.request(cmd.Context(), http.MethodGet, path, nil, &out); err != nil {
				return renderError(ctx, "admin", "schedulers_status", err, warnings)
			}
			return render(ctx, "admin", "schedulers_status", out, warnings)
		},
	}
}
