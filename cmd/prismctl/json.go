package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	prismauth "github.com/ChiaYuChang/prism/internal/auth"
	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

type commandEnvelope struct {
	Endpoint string          `json:"endpoint"`
	Action   string          `json:"action"`
	Auth     jsonAuth        `json:"auth"`
	Params   json.RawMessage `json:"params"`
}

type jsonAuth struct {
	RootTokenFile  string `json:"root_token_file"`
	RootToken      string `json:"root_token"`
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
	case "root":
		return c.runJSONRoot(ctx, env)
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

func (c *cliContext) runJSONRoot(ctx context.Context, env commandEnvelope) error {
	cred, err := c.rootCredentialFromJSON(env.Auth)
	if err != nil {
		return err
	}
	service, err := c.service(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	switch env.Action {
	case "init":
		tok, err := service.InitRoot(ctx, cred.Secret)
		if err != nil {
			return renderError(c, "root", "init", err, cred.Warnings)
		}
		return render(c, "root", "init", toTokenView(tok), cred.Warnings)
	case "check":
		if err := service.CheckRoot(ctx, cred.Secret); err != nil {
			return renderError(c, "root", "check", err, cred.Warnings)
		}
		return render(c, "root", "check", map[string]bool{"valid": true}, cred.Warnings)
	case "admin_create":
		var params struct {
			Name      string  `json:"name"`
			ExpiresAt *string `json:"expires_at"`
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
		res, err := service.CreateAdminWithRoot(ctx, cred.Secret, params.Name, expires)
		if err != nil {
			return renderError(c, "root", "admin_create", err, cred.Warnings)
		}
		return render(c, "root", "admin_create", toTokenSecretView(res.Token, res.Raw), cred.Warnings)
	case "tokens_revoke_all":
		count, err := service.RevokeAllWithRoot(ctx, cred.Secret)
		if err != nil {
			return renderError(c, "root", "tokens_revoke_all", err, cred.Warnings)
		}
		return render(c, "root", "tokens_revoke_all", map[string]int64{"revoked": count}, cred.Warnings)
	default:
		return renderError(c, "root", env.Action, fmt.Errorf("unsupported root action %q", env.Action), cred.Warnings)
	}
}

func (c *cliContext) runJSONAdmin(ctx context.Context, env commandEnvelope) error {
	if strings.HasPrefix(env.Action, "sources_") {
		return c.runJSONAdminSources(ctx, env)
	}
	actor, warnings, err := c.adminActorFromJSON(ctx, env.Auth)
	if err != nil {
		return err
	}
	service, err := c.service(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	switch env.Action {
	case "tokens_create":
		var params struct {
			Type      string  `json:"type"`
			Name      string  `json:"name"`
			ExpiresAt *string `json:"expires_at"`
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
		res, err := service.CreateToken(ctx, actor, prismauth.CreateTokenRequest{Type: authtoken.Type(params.Type), Name: params.Name, ExpiresAt: expires})
		if err != nil {
			return renderError(c, "admin", "tokens_create", err, warnings)
		}
		return render(c, "admin", "tokens_create", toTokenSecretView(res.Token, res.Raw), warnings)
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
		rows, err := service.ListTokens(ctx, actor, repo.ListOperatorParams{Limit: params.Limit, Next: params.Next})
		if err != nil {
			return renderError(c, "admin", "tokens_list", err, warnings)
		}
		items := make([]tokenView, 0, len(rows))
		for _, row := range rows {
			items = append(items, toTokenView(row))
		}
		return render(c, "admin", "tokens_list", map[string]any{"items": items, "count": len(items), "limit": params.Limit, "next": params.Next}, warnings)
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
		tok, err := service.GetToken(ctx, actor, id)
		if err != nil {
			return renderError(c, "admin", "tokens_get", err, warnings)
		}
		return render(c, "admin", "tokens_get", toTokenView(tok), warnings)
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
		tok, err := service.RevokeToken(ctx, actor, id)
		if err != nil {
			return renderError(c, "admin", "tokens_revoke", err, warnings)
		}
		return render(c, "admin", "tokens_revoke", toTokenView(tok), warnings)
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

func (c *cliContext) rootCredentialFromJSON(auth jsonAuth) (credential, error) {
	if auth.RootTokenFile != "" || auth.RootToken != "" {
		return loadCredential(credentialRequest{File: auth.RootTokenFile, Raw: auth.RootToken, Name: "root"})
	}
	return c.rootCredential()
}

func (c *cliContext) adminActorFromJSON(ctx context.Context, auth jsonAuth) (prismauth.Actor, []string, error) {
	var cred credential
	var err error
	if auth.AdminTokenFile != "" || auth.AdminToken != "" {
		cred, err = loadCredential(credentialRequest{File: auth.AdminTokenFile, Raw: auth.AdminToken, Name: "admin"})
	} else {
		cred, err = c.adminCredential()
	}
	if err != nil {
		return prismauth.Actor{}, nil, err
	}
	service, err := c.service(ctx)
	if err != nil {
		return prismauth.Actor{}, cred.Warnings, err
	}
	actor, err := service.AuthenticateToken(ctx, cred.Secret)
	if err != nil {
		return prismauth.Actor{}, cred.Warnings, err
	}
	return actor, cred.Warnings, nil
}

func (c *cliContext) adminCredentialFromJSON(auth jsonAuth) (credential, error) {
	if auth.AdminTokenFile != "" || auth.AdminToken != "" {
		return loadCredential(credentialRequest{File: auth.AdminTokenFile, Raw: auth.AdminToken, Name: "admin"})
	}
	return c.adminCredential()
}
