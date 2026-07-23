package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type sourceView struct {
	Abbr      string     `json:"abbr" yaml:"abbr"`
	Name      string     `json:"name" yaml:"name"`
	Type      string     `json:"type" yaml:"type"`
	BaseURL   string     `json:"base_url" yaml:"base_url"`
	CreatedAt time.Time  `json:"created_at,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type sourceListResponse struct {
	Items []sourceView `json:"items"`
}
type sourceManifest struct {
	Sources []sourceView `yaml:"sources"`
}

type sourceAPI struct {
	baseURL, token string
	client         *http.Client
}

func (c *cliContext) sourceAPI() (*sourceAPI, []string, error) {
	cred, err := c.adminCredential()
	if err != nil {
		return nil, nil, err
	}
	return &sourceAPI{baseURL: strings.TrimRight(c.adminAPIURL, "/"), token: cred.Secret, client: &http.Client{Timeout: 15 * time.Second}}, cred.Warnings, nil
}

func (c *cliContext) publicAPI() (*sourceAPI, []string, error) {
	cred, err := c.adminCredential()
	if err != nil {
		return nil, nil, err
	}
	return &sourceAPI{baseURL: strings.TrimRight(c.apiURL, "/"), token: cred.Secret, client: &http.Client{Timeout: 15 * time.Second}}, cred.Warnings, nil
}

func (a *sourceAPI) request(ctx context.Context, method, path string, body any, out any) error {
	var reader *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-PRISM-TOKEN", a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var msg struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&msg)
		if msg.Error == "" {
			msg.Error = res.Status
		}
		return fmt.Errorf("API %s %s: %s", method, path, msg.Error)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (a *sourceAPI) list(ctx context.Context) ([]sourceView, error) {
	var out sourceListResponse
	if err := a.request(ctx, http.MethodGet, "/admin/sources?limit=500&next=1", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func adminSourcesCreateCommand(ctx *cliContext) *cobra.Command {
	var abbr, name, typ, baseURL string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a source",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			var out sourceView
			body := sourceView{Abbr: abbr, Name: name, Type: typ, BaseURL: baseURL}
			path := "/admin/sources?abbr=" + url.QueryEscape(abbr)
			if err := a.request(cmd.Context(), http.MethodPost, path, body, &out); err != nil {
				return renderError(ctx, "admin", "sources_create", err, warnings)
			}
			return render(ctx, "admin", "sources_create", out, warnings)
		},
	}
	cmd.Flags().StringVar(&abbr, "abbr", "", "Source abbreviation")
	cmd.Flags().StringVar(&name, "name", "", "Source display name")
	cmd.Flags().StringVar(&typ, "type", "", "Source type: PARTY or MEDIA")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Source base URL")
	_ = cmd.MarkFlagRequired("abbr")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("base-url")
	return cmd
}

func adminSourcesListCommand(ctx *cliContext) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List sources", RunE: func(cmd *cobra.Command, args []string) error {
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		items, err := a.list(cmd.Context())
		if err != nil {
			return renderError(ctx, "admin", "sources_list", err, warnings)
		}
		return render(ctx, "admin", "sources_list", map[string]any{"items": items, "count": len(items)}, warnings)
	}}
}

func adminSourcesSyncCommand(ctx *cliContext) *cobra.Command {
	var manifest string
	cmd := &cobra.Command{Use: "sync", Short: "Synchronize sources from a YAML manifest", RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(manifest)
		if err != nil {
			return err
		}
		var desired sourceManifest
		if err := yaml.Unmarshal(data, &desired); err != nil {
			return err
		}
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		current, err := a.list(cmd.Context())
		if err != nil {
			return renderError(ctx, "admin", "sources_sync", err, warnings)
		}
		byAbbr := make(map[string]sourceView, len(current))
		for _, item := range current {
			byAbbr[item.Abbr] = item
		}
		changed := make([]sourceView, 0, len(desired.Sources))
		for _, item := range desired.Sources {
			path := "/admin/sources/" + url.PathEscape(item.Abbr)
			if old, ok := byAbbr[item.Abbr]; ok {
				var out sourceView
				if err := a.request(cmd.Context(), http.MethodPut, path, item, &out); err != nil {
					return renderError(ctx, "admin", "sources_sync", err, warnings)
				}
				if old.DeletedAt != nil {
					if err := a.request(cmd.Context(), http.MethodPost, path+"/restore", nil, &out); err != nil {
						return renderError(ctx, "admin", "sources_sync", err, warnings)
					}
				}
				changed = append(changed, out)
			} else {
				var out sourceView
				if err := a.request(cmd.Context(), http.MethodPost, "/admin/sources?abbr="+url.QueryEscape(item.Abbr), item, &out); err != nil {
					return renderError(ctx, "admin", "sources_sync", err, warnings)
				}
				changed = append(changed, out)
			}
		}
		return render(ctx, "admin", "sources_sync", map[string]any{"items": changed, "count": len(changed)}, warnings)
	}}
	cmd.Flags().StringVar(&manifest, "manifest", "configs/registry/sources.yaml", "Source manifest path")
	return cmd
}

func adminSourcesDeleteCommand(ctx *cliContext) *cobra.Command {
	return sourceMutationCommand(ctx, "delete", http.MethodDelete, "Delete (soft-delete) a source")
}
func adminSourcesRestoreCommand(ctx *cliContext) *cobra.Command {
	return sourceMutationCommand(ctx, "restore", http.MethodPost, "Restore a source")
}

func sourceMutationCommand(ctx *cliContext, verb, method, short string) *cobra.Command {
	return &cobra.Command{Use: verb + " <abbr>", Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, warnings, err := ctx.sourceAPI()
		if err != nil {
			return err
		}
		var out sourceView
		path := "/admin/sources/" + url.PathEscape(args[0])
		if verb == "restore" {
			path += "/restore"
		}
		if err := a.request(cmd.Context(), method, path, nil, &out); err != nil {
			return renderError(ctx, "admin", "sources_"+verb, err, warnings)
		}
		return render(ctx, "admin", "sources_"+verb, out, warnings)
	}}
}
