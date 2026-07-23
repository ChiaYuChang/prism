package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	prismauth "github.com/ChiaYuChang/prism/internal/auth"
	"github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const defaultRootTokenFile = "/run/secrets/prism_root_token"

var errAlreadyRendered = errors.New("response already rendered")

type rootCLI struct {
	v          *viper.Viper
	configPath string
	output     string
	rootFile   string
	rootToken  string
	service    *prismauth.Service
	closer     repo.Closer
}

type rootConfig struct {
	Postgres appconfig.PostgresConfig `mapstructure:"postgres"`
	Auth     struct {
		HashAlgorithm string `mapstructure:"hash-algorithm" validate:"required"`
	} `mapstructure:"auth"`
}

type credential struct {
	Secret   string
	Warnings []string
}

type tokenView struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"`
	Name          string     `json:"name"`
	Permissions   uint8      `json:"permissions"`
	HashAlgorithm string     `json:"hash_algorithm"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	RenewedAt     *time.Time `json:"renewed_at,omitempty"`
	RotatedAt     *time.Time `json:"rotated_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type tokenSecretView struct {
	tokenView
	Token string `json:"token"`
}

type response struct {
	OK       bool        `json:"ok"`
	Action   string      `json:"action,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
	Result   any         `json:"result,omitempty"`
	Error    *errorField `json:"error,omitempty"`
}

type errorField struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {
	cmd := newRootCommand()
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		if !errors.Is(err, errAlreadyRendered) {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	c := &rootCLI{v: viper.New()}
	c.v.SetDefault("postgres.host", "localhost")
	c.v.SetDefault("postgres.port", 5432)
	c.v.SetDefault("postgres.username", "postgres")
	c.v.SetDefault("postgres.password", "postgres")
	c.v.SetDefault("postgres.db", "prism")
	c.v.SetDefault("postgres.sslmode", "disable")
	c.v.SetDefault("auth.hash-algorithm", "sha256")
	c.v.SetEnvPrefix("PRISM_PRISMCTL_ROOT")
	c.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	c.v.AutomaticEnv()

	cmd := &cobra.Command{
		Use:          "prismctl-root",
		Short:        "Prism root break-glass control",
		SilenceUsage: true,
	}
	c.bindFlags(cmd.PersistentFlags())
	cmd.AddCommand(c.initCommand(), c.checkCommand(), c.adminCreateCommand(), c.revokeAllCommand())
	return cmd
}

func (c *rootCLI) bindFlags(fs *pflag.FlagSet) {
	fs.StringVarP(&c.configPath, "config", "c", "", "Path to config file")
	fs.StringVar(&c.output, "output", "text", "Output format: text or json")
	fs.StringVar(&c.rootFile, "root-token-file", "", "Path to root token file")
	fs.StringVar(&c.rootToken, "root-token", "", "Raw root token; prefer --root-token-file")
	fs.String("pg-host", "localhost", "Postgres host")
	fs.Int("pg-port", 5432, "Postgres port")
	fs.String("pg-username", "postgres", "Postgres username")
	fs.String("pg-password", "postgres", "Postgres password")
	fs.String("pg-password-file", "", "Path to Postgres password file")
	fs.String("pg-db", "prism", "Postgres database name")
	fs.String("pg-sslmode", "disable", "Postgres SSL mode")
	fs.String("hash-algorithm", "sha256", "Token hash algorithm")
	_ = c.v.BindPFlag("auth.hash-algorithm", fs.Lookup("hash-algorithm"))
	_ = c.v.BindPFlag("postgres.host", fs.Lookup("pg-host"))
	_ = c.v.BindPFlag("postgres.port", fs.Lookup("pg-port"))
	_ = c.v.BindPFlag("postgres.username", fs.Lookup("pg-username"))
	_ = c.v.BindPFlag("postgres.password", fs.Lookup("pg-password"))
	_ = c.v.BindPFlag("postgres.password-file", fs.Lookup("pg-password-file"))
	_ = c.v.BindPFlag("postgres.db", fs.Lookup("pg-db"))
	_ = c.v.BindPFlag("postgres.sslmode", fs.Lookup("pg-sslmode"))
}

func (c *rootCLI) initCommand() *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Initialize root token", RunE: func(cmd *cobra.Command, _ []string) error {
		cred, err := c.credential()
		if err != nil {
			return err
		}
		service, err := c.authService(cmd.Context())
		if err != nil {
			return err
		}
		defer c.close()
		tok, err := service.InitRoot(cmd.Context(), cred.Secret)
		if err != nil {
			return c.renderError("init", err, cred.Warnings)
		}
		return c.render("init", tokenView{ID: tok.ID.String(), Type: tok.Type, Name: tok.Name, Permissions: tok.Permissions, HashAlgorithm: tok.HashAlgorithm, CreatedAt: tok.CreatedAt, ExpiresAt: tok.ExpiresAt}, cred.Warnings)
	}}
}

func (c *rootCLI) checkCommand() *cobra.Command {
	return &cobra.Command{Use: "check", Short: "Check root token", RunE: func(cmd *cobra.Command, _ []string) error {
		cred, err := c.credential()
		if err != nil {
			return err
		}
		service, err := c.authService(cmd.Context())
		if err != nil {
			return err
		}
		defer c.close()
		if err := service.CheckRoot(cmd.Context(), cred.Secret); err != nil {
			return c.renderError("check", err, cred.Warnings)
		}
		return c.render("check", map[string]bool{"valid": true}, cred.Warnings)
	}}
}

func (c *rootCLI) adminCreateCommand() *cobra.Command {
	var name, expiresAt string
	cmd := &cobra.Command{Use: "admin-create", Short: "Create an admin token with root token", RunE: func(cmd *cobra.Command, _ []string) error {
		cred, err := c.credential()
		if err != nil {
			return err
		}
		expires, err := parseOptionalTime(expiresAt)
		if err != nil {
			return err
		}
		service, err := c.authService(cmd.Context())
		if err != nil {
			return err
		}
		defer c.close()
		res, err := service.CreateAdminWithRoot(cmd.Context(), cred.Secret, name, expires)
		if err != nil {
			return c.renderError("admin_create", err, cred.Warnings)
		}
		return c.render("admin_create", tokenSecretView{tokenView: tokenView{ID: res.Token.ID.String(), Type: res.Token.Type, Name: res.Token.Name, Permissions: res.Token.Permissions, HashAlgorithm: res.Token.HashAlgorithm, CreatedAt: res.Token.CreatedAt, ExpiresAt: res.Token.ExpiresAt}, Token: res.Raw}, cred.Warnings)
	}}
	cmd.Flags().StringVar(&name, "name", "initial-admin", "Admin token name")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "Token expiry RFC3339 timestamp")
	return cmd
}

func (c *rootCLI) revokeAllCommand() *cobra.Command {
	return &cobra.Command{Use: "tokens-revoke-all", Short: "Revoke all non-root tokens", RunE: func(cmd *cobra.Command, _ []string) error {
		cred, err := c.credential()
		if err != nil {
			return err
		}
		service, err := c.authService(cmd.Context())
		if err != nil {
			return err
		}
		defer c.close()
		count, err := service.RevokeAllWithRoot(cmd.Context(), cred.Secret)
		if err != nil {
			return c.renderError("tokens_revoke_all", err, cred.Warnings)
		}
		return c.render("tokens_revoke_all", map[string]int64{"revoked": count}, cred.Warnings)
	}}
}

func (c *rootCLI) authService(ctx context.Context) (*prismauth.Service, error) {
	if c.service != nil {
		return c.service, nil
	}
	var cfg rootConfig
	if c.configPath != "" {
		if err := appconfig.ReadConfigFile(c.v, c.configPath); err != nil {
			return nil, err
		}
	}
	if err := c.v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.Postgres.ResolveSecrets(); err != nil {
		return nil, err
	}
	if err := validator.New().Struct(cfg); err != nil {
		return nil, err
	}
	repository, closer, err := pg.NewRepositoryBuilder(cfg.Postgres).NewRepository(ctx)
	if err != nil {
		return nil, err
	}
	hasher, err := token.NewHasher(cfg.Auth.HashAlgorithm)
	if err != nil {
		_ = closer.Close()
		return nil, err
	}
	service, err := prismauth.NewService(prismauth.ServiceParams{
		Tokens: repository.Tokens(),
		Hasher: hasher,
		TokenTypes: map[token.Type]prismauth.TokenTypeConfig{
			token.TypeAdmin: {DefaultTTL: 720 * time.Hour, MaxTTL: 2160 * time.Hour},
		},
	})
	if err != nil {
		_ = closer.Close()
		return nil, err
	}
	c.closer = closer
	c.service = service
	return service, nil
}

func (c *rootCLI) close() {
	if c.closer != nil {
		_ = c.closer.Close()
		c.closer = nil
	}
}

func (c *rootCLI) credential() (credential, error) {
	path := c.rootFile
	if path == "" {
		path = strings.TrimSpace(os.Getenv("PRISM_ROOT_TOKEN_FILE"))
	}
	if path == "" {
		path = defaultRootTokenFile
	}
	if path != "" {
		if secret, err := readAll(path); err == nil {
			return credential{Secret: secret}, nil
		} else if c.rootToken == "" && os.Getenv("PRISM_ROOT_TOKEN") == "" {
			return credential{}, fmt.Errorf("read root token file %q: %w", path, err)
		}
	}
	if strings.TrimSpace(c.rootToken) != "" {
		return credential{Secret: strings.TrimSpace(c.rootToken), Warnings: []string{"reading root token from raw flag is less safe than token file"}}, nil
	}
	if raw := strings.TrimSpace(os.Getenv("PRISM_ROOT_TOKEN")); raw != "" {
		return credential{Secret: raw, Warnings: []string{"reading root token from PRISM_ROOT_TOKEN is less safe than token file"}}, nil
	}
	return credential{}, errors.New("root token not provided")
}

func (c *rootCLI) render(action string, result any, warnings []string) error {
	if c.output == "json" {
		return json.NewEncoder(os.Stdout).Encode(response{OK: true, Action: action, Warnings: warnings, Result: result})
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	_, _ = fmt.Fprintln(os.Stdout, string(b))
	return nil
}

func (c *rootCLI) renderError(action string, err error, warnings []string) error {
	if c.output == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(response{OK: false, Action: action, Warnings: warnings, Error: &errorField{Code: "error", Message: err.Error()}})
		return errAlreadyRendered
	}
	return err
}

func readAll(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
