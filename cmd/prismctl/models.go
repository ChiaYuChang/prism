package main

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

type modelView struct {
	ID       int16  `json:"id" yaml:"id"`
	Name     string `json:"name" yaml:"name"`
	Provider string `json:"provider" yaml:"provider"`
	Type     string `json:"type" yaml:"type"`
}

func adminModelsCreateCommand(ctx *cliContext) *cobra.Command {
	var name, provider, typ, tag string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Register a model",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			var out modelView
			body := modelView{Name: name, Provider: provider, Type: strings.ToUpper(typ)}
			if tag != "" {
				bodyMap := map[string]any{"name": body.Name, "provider": body.Provider, "type": body.Type, "tag": tag}
				if err := a.request(cmd.Context(), http.MethodPost, "/admin/models", bodyMap, &out); err != nil {
					return renderError(ctx, "admin", "models_create", err, warnings)
				}
			} else if err := a.request(cmd.Context(), http.MethodPost, "/admin/models", body, &out); err != nil {
				return renderError(ctx, "admin", "models_create", err, warnings)
			}
			return render(ctx, "admin", "models_create", out, warnings)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Model name from worker configuration")
	cmd.Flags().StringVar(&provider, "provider", "", "Model provider")
	cmd.Flags().StringVar(&typ, "type", "EXTRACTOR", "Model type: EXTRACTOR, EMBEDDER, or ANALYZER")
	cmd.Flags().StringVar(&tag, "tag", "", "Optional model tag")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("provider")
	return cmd
}
