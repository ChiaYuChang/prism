package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/collector/parser/config"
	parserllm "github.com/ChiaYuChang/prism/internal/collector/parser/llm"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/trace/noop"
)

const CommandName = "recover"

var ErrUsage = errors.New("invalid command usage")

type cliOptions struct {
	subcommand    string
	archiveURI    string
	parsersConfig string
	prompt        string
	since         time.Time
	until         time.Time
	limit         int
	traceID       string
	dryRun        bool
	purge         bool
	postgres      appconfig.PostgresConfig
}

func main() {
	opts, err := parseCLI(os.Args[1:], os.Stdout)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	logger, logFile, err := obs.InitLogger("logs/recover.log", slog.LevelDebug)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}

	arch, err := archiver.ParseURI(opts.archiveURI, logger)
	if err != nil {
		logger.Error("failed to initialize archiver", "uri", opts.archiveURI, "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	switch opts.subcommand {
	case "status":
		if err := runStatus(ctx, arch, opts); err != nil {
			logger.Error("status failed", "error", err)
			os.Exit(1)
		}
	case "list":
		if err := runList(ctx, arch, opts); err != nil {
			logger.Error("list failed", "error", err)
			os.Exit(1)
		}
	case "run":
		pipeline, closer, err := connectDB(ctx, opts.postgres, logger)
		if err != nil {
			logger.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer func() { _ = closer.Close() }()

		cfg, err := config.LoadConfig(opts.parsersConfig)
		if err != nil {
			logger.Error("failed to load parsers config", "path", opts.parsersConfig, "error", err)
			os.Exit(1)
		}

		var llmFactory config.LLMFactory
		if cfg.Fallback.Enable {
			if opts.prompt != "" {
				cfg.Fallback.PromptFile = opts.prompt
			}
			prompt, perr := config.LoadFallbackPrompt(cfg.Fallback)
			if perr != nil {
				logger.Error("failed to load fallback prompt",
					"path", cfg.Fallback.PromptFile, "error", perr)
				os.Exit(1)
			}
			gen, gerr := llmfactory.NewGenerator(ctx, cfg.Fallback.LLM, logger)
			if gerr != nil {
				logger.Error("failed to initialize fallback LLM generator",
					"provider", cfg.Fallback.LLM.Provider, "error", gerr)
				os.Exit(1)
			}
			model := cfg.Fallback.LLM.Model
			llmFactory = func() (collector.Parser, error) {
				return parserllm.NewParser(gen, logger, model, prompt)
			}
		}

		registry, err := config.BuildRegistry(cfg, logger, noop.NewTracerProvider().Tracer("recover"), llmFactory)
		if err != nil {
			logger.Error("failed to build parser registry", "error", err)
			os.Exit(1)
		}

		if err := runRecover(ctx, arch, pipeline, registry, logger, opts); err != nil {
			logger.Error("recover failed", "error", err)
			os.Exit(1)
		}
	case "clean":
		pipeline, closer, err := connectDB(ctx, opts.postgres, logger)
		if err != nil {
			logger.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer func() { _ = closer.Close() }()

		if err := runClean(ctx, arch, pipeline, logger, opts); err != nil {
			logger.Error("clean failed", "error", err)
			os.Exit(1)
		}
	}
}

func connectDB(ctx context.Context, pgCfg appconfig.PostgresConfig, logger *slog.Logger) (repo.Pipeline, repo.Closer, error) {
	repository, closer, err := pg.NewRepositoryBuilder(pgCfg).NewRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	logger.Info("connected to database", "host", pgCfg.Host, "db", pgCfg.DB)
	return repository.Pipeline(), closer, nil
}

func runStatus(ctx context.Context, arch archiver.Archiver, _ cliOptions) error {
	metas, err := arch.Scan(ctx, archiver.ScanOptions{})
	if err != nil {
		return err
	}

	var raw, minified, canonical, other int
	for _, m := range metas {
		switch m.PayloadKind {
		case archiver.PayloadKindRaw:
			raw++
		case archiver.PayloadKindMinified:
			minified++
		case archiver.PayloadKindCanonical:
			canonical++
		default:
			other++
		}
	}

	fmt.Printf("Archives: %d total\n", len(metas))
	fmt.Printf("  raw (minify failed): %d\n", raw)
	fmt.Printf("  minified:            %d\n", minified)
	fmt.Printf("  canonical:           %d\n", canonical)
	if other > 0 {
		fmt.Printf("  other:               %d\n", other)
	}
	return nil
}

func runList(ctx context.Context, arch archiver.Archiver, opts cliOptions) error {
	scanOpts := archiver.ScanOptions{
		Since:   opts.since,
		Until:   opts.until,
		Limit:   opts.limit,
		TraceID: opts.traceID,
	}
	metas, err := arch.Scan(ctx, scanOpts)
	if err != nil {
		return err
	}

	if len(metas) == 0 {
		fmt.Println("no archives found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TRACE_ID\tURL\tKIND\tSOURCE\tCREATED\tERROR")
	for _, m := range metas {
		errStr := m.Error
		if len(errStr) > 60 {
			errStr = errStr[:57] + "..."
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			m.TraceID,
			truncate(m.URL, 60),
			m.PayloadKind,
			m.SourceAbbr,
			m.CreatedAt.Format("2006-01-02"),
			errStr,
		)
	}
	return w.Flush()
}

func runClean(ctx context.Context, arch archiver.Archiver, pipeline repo.Pipeline, logger *slog.Logger, opts cliOptions) error {
	scanOpts := archiver.ScanOptions{
		Since:   opts.since,
		Until:   opts.until,
		Limit:   opts.limit,
		TraceID: opts.traceID,
	}
	metas, err := arch.Scan(ctx, scanOpts)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		fmt.Println("no archives to clean")
		return nil
	}

	var removed, kept int
	for _, m := range metas {
		if _, err := pipeline.GetContentByURL(ctx, m.URL); err != nil {
			kept++
			continue
		}

		if opts.dryRun {
			fmt.Printf("[dry-run] would remove trace_id=%s url=%s\n", m.TraceID, m.URL)
			removed++
			continue
		}

		if err := arch.Remove(ctx, m.TraceID); err != nil {
			logger.Error("failed to soft-delete archive", "trace_id", m.TraceID, "error", err)
			kept++
			continue
		}

		logger.Info("soft-deleted recovered archive", "trace_id", m.TraceID, "url", m.URL)
		removed++
	}

	fmt.Printf("\nClean complete: %d soft-deleted, %d kept (of %d total)\n", removed, kept, len(metas))

	if opts.purge && !opts.dryRun {
		return runPurge(ctx, arch, logger)
	}
	return nil
}

func runPurge(ctx context.Context, arch archiver.Archiver, logger *slog.Logger) error {
	local, ok := arch.(*archiver.LocalArchiver)
	if !ok {
		logger.Warn("--purge is only supported for local archives, skipping hard-delete")
		return nil
	}
	purged, err := local.PurgeAll(ctx)
	if err != nil {
		return fmt.Errorf("purge: %w", err)
	}
	fmt.Printf("Purged %d soft-deleted archives from disk\n", purged)
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	if len(args) == 0 {
		printUsage(output)
		return cliOptions{}, ErrUsage
	}

	subcmd := args[0]
	switch subcmd {
	case "status", "list", "run", "clean":
	case "-h", "--help", "help":
		printUsage(output)
		return cliOptions{}, pflag.ErrHelp
	default:
		printUsage(output)
		return cliOptions{}, fmt.Errorf("%w: unknown subcommand %q", ErrUsage, subcmd)
	}

	var opts cliOptions
	opts.subcommand = subcmd

	fs := pflag.NewFlagSet(CommandName+" "+subcmd, pflag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(output, "Usage: %s %s --archive <URI> [flags]\n\n", CommandName, subcmd)
		fs.PrintDefaults()
	}

	fs.StringVar(&opts.archiveURI, "archive", "", "archive URI (file:///path or bare path)")
	fs.StringVar(&opts.parsersConfig, "parsers-config", "internal/collector/parser/config/parsers.yaml", "path to parsers.yaml (used by run subcommand)")
	fs.StringVar(&opts.prompt, "prompt", "", "override path to the LLM fallback system-instruction file (defaults to fallback.prompt_file in parsers.yaml)")

	var sinceRaw, untilRaw string
	fs.StringVar(&sinceRaw, "since", "", "filter archives since date (YYYY-MM-DD)")
	fs.StringVar(&untilRaw, "until", "", "filter archives until date (YYYY-MM-DD)")
	fs.IntVar(&opts.limit, "limit", 0, "max archives to process (0 = all)")
	fs.StringVar(&opts.traceID, "trace-id", "", "filter by specific trace ID")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "preview without side effects (run/clean)")
	fs.BoolVar(&opts.purge, "purge", false, "hard-delete soft-deleted archives after clean")

	fs.StringVar(&opts.postgres.Host, "pg-host", "localhost", "Postgres host")
	fs.IntVar(&opts.postgres.Port, "pg-port", 5432, "Postgres port")
	fs.StringVar(&opts.postgres.Username, "pg-username", "postgres", "Postgres username")
	fs.StringVar(&opts.postgres.Password, "pg-password", "postgres", "Postgres password")
	fs.StringVar(&opts.postgres.DB, "pg-db", "prism", "Postgres database name")
	fs.StringVar(&opts.postgres.SSLMode, "pg-sslmode", "disable", "Postgres SSL mode")

	if err := fs.Parse(args[1:]); err != nil {
		return opts, err
	}

	if opts.archiveURI == "" {
		fs.Usage()
		return opts, fmt.Errorf("%w: --archive is required", ErrUsage)
	}

	if sinceRaw != "" {
		t, err := time.ParseInLocation("2006-01-02", sinceRaw, time.Local)
		if err != nil {
			return opts, fmt.Errorf("parse --since: %w", err)
		}
		opts.since = t
	}
	if untilRaw != "" {
		t, err := time.ParseInLocation("2006-01-02", untilRaw, time.Local)
		if err != nil {
			return opts, fmt.Errorf("parse --until: %w", err)
		}
		opts.until = t
	}

	return opts, nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, "Usage: %s <subcommand> --archive <URI> [flags]\n\n", CommandName)
	_, _ = fmt.Fprintln(w, "Subcommands:")
	_, _ = fmt.Fprintln(w, "  status    Show archive summary (no DB required)")
	_, _ = fmt.Fprintln(w, "  list      List archive entries (no DB required)")
	_, _ = fmt.Fprintln(w, "  run       Replay archived content through Minify→Transform→Parse→DB")
	_, _ = fmt.Fprintln(w, "  clean     Soft-delete archives whose content exists in DB")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintf(w, "  %s status --archive ./data/archives\n", CommandName)
	_, _ = fmt.Fprintf(w, "  %s list --archive ./data/archives --since 2026-04-01\n", CommandName)
	_, _ = fmt.Fprintf(w, "  %s run --archive ./data/archives --dry-run\n", CommandName)
	_, _ = fmt.Fprintf(w, "  %s run --archive ./data/archives --trace-id abc123\n", CommandName)
	_, _ = fmt.Fprintf(w, "  %s clean --archive ./data/archives --purge\n", CommandName)
}
