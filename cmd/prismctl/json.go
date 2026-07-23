package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type commandEnvelope struct {
	Endpoint string          `json:"endpoint"`
	Action   string          `json:"action"`
	Auth     jsonAuth        `json:"auth"`
	Params   json.RawMessage `json:"params"`
}

type jsonAuth struct {
	AdminTokenFile string `json:"admin_token_file"`
	AdminToken     string `json:"admin_token"`
}

func (c *cliContext) runJSON(ctx context.Context) error {
	input, err := c.readJSONInput()
	if err != nil {
		return err
	}
	var env commandEnvelope
	if err := json.Unmarshal(input, &env); err != nil {
		return err
	}
	if env.Endpoint == "" || env.Action == "" {
		return fmt.Errorf("endpoint and action are required")
	}
	if c.output == "" {
		c.output = "json"
	}
	switch env.Endpoint {
	case "admin":
		return c.runJSONAdmin(ctx, env)
	default:
		return renderError(c, env.Endpoint, env.Action, fmt.Errorf("unsupported endpoint %q", env.Endpoint), nil)
	}
}

func (c *cliContext) readJSONInput() ([]byte, error) {
	if c.inputFile != "" {
		return os.ReadFile(c.inputFile)
	}
	return readStdin()
}

func (c *cliContext) runJSONAdmin(ctx context.Context, env commandEnvelope) error {
	if strings.HasPrefix(env.Action, "sources_") {
		return c.runJSONAdminSources(ctx, env)
	}
	cred, err := c.adminCredentialFromJSON(env.Auth)
	if err != nil {
		return err
	}
	a := &sourceAPI{baseURL: strings.TrimRight(c.apiURL, "/"), token: cred.Secret, client: http.DefaultClient}
	warnings := cred.Warnings
	switch env.Action {
	case "tokens_create":
		var params struct {
			Type        string  `json:"type"`
			Name        string  `json:"name"`
			Permissions *uint8  `json:"permissions"`
			ExpiresAt   *string `json:"expires_at"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return err
		}
		expiresAt := ""
		if params.ExpiresAt != nil {
			expiresAt = *params.ExpiresAt
		}
		expires, err := parseOptionalTime(expiresAt)
		if err != nil {
			return err
		}
		var out tokenSecretView
		err = a.request(ctx, http.MethodPost, "/admin/tokens", struct {
			Type        string     `json:"type"`
			Name        string     `json:"name"`
			Permissions *uint8     `json:"permissions,omitempty"`
			ExpiresAt   *time.Time `json:"expires_at,omitempty"`
		}{Type: params.Type, Name: params.Name, Permissions: params.Permissions, ExpiresAt: expires}, &out)
		if err != nil {
			return renderError(c, "admin", "tokens_create", err, warnings)
		}
		return render(c, "admin", "tokens_create", out, warnings)
	case "tokens_list":
		var params struct {
			Limit int32 `json:"limit"`
			Next  int32 `json:"next"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return err
		}
		if params.Limit == 0 {
			params.Limit = 100
		}
		if params.Next == 0 {
			params.Next = 1
		}
		var out struct {
			Items []tokenView `json:"items"`
			Limit int32       `json:"limit"`
			Next  int32       `json:"next"`
			Count int         `json:"count"`
		}
		path := "/admin/tokens?limit=" + strconv.FormatInt(int64(params.Limit), 10) + "&next=" + strconv.FormatInt(int64(params.Next), 10)
		err = a.request(ctx, http.MethodGet, path, nil, &out)
		if err != nil {
			return renderError(c, "admin", "tokens_list", err, warnings)
		}
		return render(c, "admin", "tokens_list", out, warnings)
	case "tokens_get":
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return err
		}
		id, err := uuid.Parse(params.ID)
		if err != nil {
			return err
		}
		var out tokenView
		err = a.request(ctx, http.MethodGet, "/admin/tokens/"+url.PathEscape(id.String()), nil, &out)
		if err != nil {
			return renderError(c, "admin", "tokens_get", err, warnings)
		}
		return render(c, "admin", "tokens_get", out, warnings)
	case "tokens_revoke":
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return err
		}
		id, err := uuid.Parse(params.ID)
		if err != nil {
			return err
		}
		var out tokenView
		err = a.request(ctx, http.MethodPost, "/admin/tokens/"+url.PathEscape(id.String())+"/revoke", nil, &out)
		if err != nil {
			return renderError(c, "admin", "tokens_revoke", err, warnings)
		}
		return render(c, "admin", "tokens_revoke", out, warnings)
	default:
		return renderError(c, "admin", env.Action, fmt.Errorf("unsupported admin action %q", env.Action), warnings)
	}
}

func (c *cliContext) runJSONAdminSources(ctx context.Context, env commandEnvelope) error {
	cred, err := c.adminCredentialFromJSON(env.Auth)
	if err != nil {
		return err
	}
	a := &sourceAPI{baseURL: strings.TrimRight(c.apiURL, "/"), token: cred.Secret, client: http.DefaultClient}
	switch env.Action {
	case "sources_create":
		var params sourceView
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return err
		}
		var out sourceView
		path := "/admin/sources?abbr=" + url.QueryEscape(params.Abbr)
		if err := a.request(ctx, http.MethodPost, path, params, &out); err != nil {
			return renderError(c, "admin", env.Action, err, cred.Warnings)
		}
		return render(c, "admin", env.Action, out, cred.Warnings)
	case "sources_list":
		items, err := a.list(ctx)
		if err != nil {
			return renderError(c, "admin", env.Action, err, cred.Warnings)
		}
		return render(c, "admin", env.Action, map[string]any{"items": items, "count": len(items)}, cred.Warnings)
	case "sources_delete", "sources_restore":
		var params struct {
			Abbr string `json:"abbr"`
		}
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return err
		}
		path := "/admin/sources/" + url.PathEscape(params.Abbr)
		method := http.MethodDelete
		if env.Action == "sources_restore" {
			method = http.MethodPost
			path += "/restore"
		}
		var out sourceView
		if err := a.request(ctx, method, path, nil, &out); err != nil {
			return renderError(c, "admin", env.Action, err, cred.Warnings)
		}
		return render(c, "admin", env.Action, out, cred.Warnings)
	default:
		return renderError(c, "admin", env.Action, fmt.Errorf("unsupported source action %q", env.Action), cred.Warnings)
	}
}

func (c *cliContext) adminCredentialFromJSON(auth jsonAuth) (credential, error) {
	if auth.AdminTokenFile != "" || auth.AdminToken != "" {
		return loadCredential(credentialRequest{File: auth.AdminTokenFile, Raw: auth.AdminToken, Name: "admin"})
	}
	return c.adminCredential()
}
