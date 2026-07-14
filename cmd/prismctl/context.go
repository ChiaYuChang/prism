package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	prismauth "github.com/ChiaYuChang/prism/internal/auth"
	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultRootTokenFile  = "/run/secrets/prism_root_token"
	defaultAdminTokenFile = "/run/secrets/prism_admin_token"
)

type cliContext struct {
	v           *viper.Viper
	configPath  string
	output      string
	inputMode   string
	inputFile   string
	rootFile    string
	rootToken   string
	adminFile   string
	adminToken  string
	apiURL      string
	repository  repo.Repository
	closer      repo.Closer
	authService *prismauth.Service
}

type config struct {
	Postgres appconfig.PostgresConfig `mapstructure:"postgres"`
	Auth     authConfig               `mapstructure:"auth"`
}

type authConfig struct {
	HashAlgorithm string `mapstructure:"hash-algorithm" validate:"required"`
}

func newCLIContext() *cliContext {
	v := viper.New()
	v.SetDefault("postgres.host", "localhost")
	v.SetDefault("postgres.port", 5432)
	v.SetDefault("postgres.username", "postgres")
	v.SetDefault("postgres.password", "postgres")
	v.SetDefault("postgres.db", "prism")
	v.SetDefault("postgres.sslmode", "disable")
	v.SetDefault("auth.hash-algorithm", "sha256")
	v.SetEnvPrefix("PRISM_PRISMCTL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	return &cliContext{v: v}
}

func (c *cliContext) bindPersistentFlags(cmd *cobra.Command) {
	flags := cmd.PersistentFlags()
	flags.StringVarP(&c.configPath, "config", "c", "", "Path to config file")
	flags.StringVar(&c.output, "output", "text", "Output format: text or json")
	flags.StringVar(&c.inputMode, "input", "", "Input mode: json")
	flags.StringVar(&c.inputFile, "input-file", "", "Read JSON command envelope from file")
	flags.String("pg-host", "localhost", "Postgres host")
	flags.Int("pg-port", 5432, "Postgres port")
	flags.String("pg-username", "postgres", "Postgres username")
	flags.String("pg-password", "postgres", "Postgres password")
	flags.String("pg-password-file", "", "Path to Postgres password file")
	flags.String("pg-db", "prism", "Postgres database name")
	flags.String("pg-sslmode", "disable", "Postgres SSL mode")
	flags.String("hash-algorithm", "sha256", "Token hash algorithm")
	flags.StringVar(&c.rootFile, "root-token-file", "", "Path to root token file inside the container")
	flags.StringVar(&c.rootToken, "root-token", "", "Raw root token; prefer --root-token-file")
	flags.StringVar(&c.adminFile, "admin-token-file", "", "Path to admin token file inside the container")
	flags.StringVar(&c.adminToken, "admin-token", "", "Raw admin token; prefer --admin-token-file")
	flags.StringVar(&c.apiURL, "api-url", "http://localhost:8091/api/v1", "Prism admin API base URL")
	_ = c.v.BindPFlag("auth.hash-algorithm", flags.Lookup("hash-algorithm"))
	_ = c.v.BindPFlag("postgres.host", flags.Lookup("pg-host"))
	_ = c.v.BindPFlag("postgres.port", flags.Lookup("pg-port"))
	_ = c.v.BindPFlag("postgres.username", flags.Lookup("pg-username"))
	_ = c.v.BindPFlag("postgres.password", flags.Lookup("pg-password"))
	_ = c.v.BindPFlag("postgres.password-file", flags.Lookup("pg-password-file"))
	_ = c.v.BindPFlag("postgres.db", flags.Lookup("pg-db"))
	_ = c.v.BindPFlag("postgres.sslmode", flags.Lookup("pg-sslmode"))
}

func (c *cliContext) service(ctx context.Context) (*prismauth.Service, error) {
	if c.authService != nil {
		return c.authService, nil
	}
	cfg, err := c.loadConfig()
	if err != nil {
		return nil, err
	}
	repository, closer, err := pg.NewRepositoryBuilder(cfg.Postgres).NewRepository(ctx)
	if err != nil {
		return nil, err
	}
	c.repository = repository
	c.closer = closer
	hasher, err := authtoken.NewHasher(cfg.Auth.HashAlgorithm)
	if err != nil {
		return nil, err
	}
	service, err := prismauth.NewService(prismauth.ServiceParams{
		Tokens: repository.Tokens(),
		Hasher: hasher,
		TokenTypes: map[authtoken.Type]prismauth.TokenTypeConfig{
			authtoken.TypeAdmin:  {DefaultTTL: 720 * time.Hour, MaxTTL: 2160 * time.Hour},
			authtoken.TypeUser:   {DefaultTTL: 24 * time.Hour, MaxTTL: 168 * time.Hour},
			authtoken.TypeWorker: {DefaultTTL: 720 * time.Hour, MaxTTL: 2160 * time.Hour},
		},
	})
	if err != nil {
		return nil, err
	}
	c.authService = service
	return service, nil
}

func (c *cliContext) close() {
	if c.closer != nil {
		_ = c.closer.Close()
	}
}

func (c *cliContext) loadConfig() (config, error) {
	if c.configPath != "" {
		if err := appconfig.ReadConfigFile(c.v, c.configPath); err != nil {
			return config{}, err
		}
	}
	var cfg config
	if err := c.v.Unmarshal(&cfg); err != nil {
		return config{}, err
	}
	if err := cfg.Postgres.ResolveSecrets(); err != nil {
		return config{}, err
	}
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func readAll(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", args...)
}
