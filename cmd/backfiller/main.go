package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/discovery"
	backfiller "github.com/ChiaYuChang/prism/internal/discovery/backfiller/config"
	scout "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/google/uuid"
	"github.com/spf13/pflag"
)

const (
	DefaultScoutConfigPath      = "internal/discovery/scout/config/scouts.yaml"
	DefaultBackfillerConfigPath = "internal/discovery/backfiller/config/backfillers.yaml"
	CommandName                 = "backfiller"
	TracerName                  = "prism.backfiller"
)

var ErrUsage = errors.New("invalid command usage")

type cliOptions struct {
	source               string
	until                time.Time
	maxPages             int
	scoutConfigFile      string
	backfillerConfigFile string
	timeout              time.Duration
	postgres             appconfig.PostgresConfig
	messengerType        string
	natsCfg              appconfig.NatsConfig
	goChannelCfg         appconfig.GoChannelConfig
}

func main() {
	opts, err := parseCLI(os.Args[1:], os.Stdout)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	logger, logFile, err := obs.InitLogger("logs/backfiller.log", slog.LevelDebug)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer func() {
			_ = logFile.Close()
		}()
	}
	shutdownTracer := infra.InitAndSetTracer(TracerName)
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			logger.Error("failed to shutdown tracer", "error", err)
		}
	}()

	var messengerConfig appconfig.MessengerConfig
	switch opts.messengerType {
	case "nats":
		messengerConfig = &opts.natsCfg
	case "gochannel":
		messengerConfig = &opts.goChannelCfg
	default:
		logger.Error("unsupported messenger type", "type", opts.messengerType)
		os.Exit(1)
	}

	msgr, err := messengerConfig.NewMessenger(logger)
	if err != nil {
		logger.Error("failed to initialize messenger", "type", opts.messengerType, "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := msgr.Close(); err != nil {
			logger.Error("failed to close messenger", "error", err)
		}
	}()

	// load scout config and build scout repo
	scoutCfg, err := scout.ReadFile(opts.scoutConfigFile)
	if err != nil {
		logger.Error("failed to read scout config", slog.Any("error", err))
		os.Exit(1)
	}
	scoutRepo, err := scout.New(scoutCfg)
	if err != nil {
		logger.Error("failed to initialize scout repo", slog.Any("error", err))
		os.Exit(1)
	}
	if scoutRepo == nil {
		logger.Error("scout repo is nil")
		os.Exit(1)
	}

	// load backfill config and build backfill repo
	backfillerCfg, err := backfiller.ReadFile(opts.backfillerConfigFile)
	if err != nil {
		logger.Error("failed to read backfiller config", "path", opts.backfillerConfigFile, "error", err)
		os.Exit(1)
	}
	backfillerRepo, err := backfiller.New(backfillerCfg)
	if err != nil {
		logger.Error("failed to initialize backfiller repo", slog.Any("error", err))
		os.Exit(1)
	}
	if backfillerRepo == nil {
		logger.Error("backfiller repo is nil")
		os.Exit(1)
	}

	srcSpec, ok := backfillerRepo.Source(opts.source)
	if !ok {
		logger.Error("unknown backfill source", "source", opts.source)
		os.Exit(1)
	}

	if err := backfiller.ConfirmSourceAgainstScout(srcSpec, scoutRepo); err != nil {
		logger.Error("backfill source confirmation failed", "source", opts.source, "error", err)
		os.Exit(1)
	}

	logger.Info("backfiller source selected",
		"source", srcSpec.Name,
		"source_id", srcSpec.SourceID,
		"format", srcSpec.Format,
		"base_url", srcSpec.BaseURL,
	)

	tracer := infra.Tracer()
	var repositoryFactory repo.Factory = pg.NewFactory(opts.postgres)
	repository, repositoryCloser, err := repositoryFactory.NewRepository(context.Background())
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", opts.postgres.Host, "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := repositoryCloser.Close(); err != nil {
			logger.Error("failed to close repository resources", "error", err)
		}
	}()

	pageFetchPublisher, err := message.NewWatermillPageFetchPublisher(msgr)
	if err != nil {
		logger.Error("failed to build page fetch publisher", "error", err)
		os.Exit(1)
	}

	sink, err := discoverysink.NewPersistingCandidateSink(logger, tracer, repository.Scout(), pageFetchPublisher)
	if err != nil {
		logger.Error("failed to build candidate sink", "error", err)
		os.Exit(1)
	}
	backfiller, err := backfiller.BuildBackfiller(
		srcSpec, scoutRepo, logger, tracer, http.DefaultClient, sink)

	if err != nil {
		logger.Error("failed to build backfiller", "source", opts.source, "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}
	batchID, err := uuid.NewV7()
	if err != nil {
		logger.Error("failed to generate batch id", "error", err)
		os.Exit(1)
	}

	logger.Info(
		"backfiller started",
		"source_id", srcSpec.SourceID,
		"batch_id", batchID.String(),
		"base_url", srcSpec.BaseURL,
		"until", opts.until.Format("2006-01-02"))

	result, err := backfiller.Run(ctx, discovery.BackfillRequest{
		BatchID:  batchID,
		Until:    opts.until,
		MaxPages: opts.maxPages,
	})
	if err != nil {
		logger.Error("backfill failed", "source", opts.source, "error", err)
		os.Exit(1)
	}

	logger.Info("backfill completed",
		"source", opts.source,
		"source_id", srcSpec.SourceID,
		"batch_id", batchID.String(),
		"base_url", srcSpec.BaseURL,
		"until", opts.until.Format("2006-01-02"),
		"pages_visited", result.PagesVisited,
		"candidates_seen", result.CandidatesSeen,
		"candidates_processed", result.CandidatesProcessed,
	)
}

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	var opts cliOptions

	fs := pflag.NewFlagSet(CommandName, pflag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&opts.source, "source", "", "backfill source name, e.g. dpp, tpp, kmt")
	untilRaw := ""
	fs.StringVar(&untilRaw, "until", "", "stop when listing items become older than this date (YYYY-MM-DD)")
	fs.IntVar(&opts.maxPages, "max-pages", 0, "maximum number of listing pages to visit (0 means unlimited)")
	fs.StringVar(&opts.scoutConfigFile, "scout-config", DefaultScoutConfigPath, "path to scout config file")
	fs.StringVar(&opts.backfillerConfigFile, "backfill-config", DefaultBackfillerConfigPath, "path to backfiller config file")
	fs.DurationVar(&opts.timeout, "timeout", 0, "timeout for all backfill process (0 means unlimited)")
	fs.StringVar(&opts.postgres.Host, "pg-host", "localhost", "Postgres host")
	fs.IntVar(&opts.postgres.Port, "pg-port", 5432, "Postgres port")
	fs.StringVar(&opts.postgres.Username, "pg-username", "postgres", "Postgres username")
	fs.StringVar(&opts.postgres.Password, "pg-password", "postgres", "Postgres password")
	fs.StringVar(&opts.postgres.DB, "pg-db", "prism", "Postgres database name")
	fs.StringVar(&opts.postgres.SSLMode, "pg-sslmode", "disable", "Postgres SSL mode")
	fs.StringVar(&opts.messengerType, "messenger-type", "nats", "The messenger backend type (nats, gochannel)")
	fs.StringVar(&opts.natsCfg.URL, "nats-url", "nats://localhost:4222", "The URL for the NATS server")
	fs.StringVar(&opts.natsCfg.QueueGroup, "queue-group", "", "Queue group for NATS subscribers")
	fs.IntVar(&opts.natsCfg.SubscribersCount, "subscribers-count", 1, "How many subscriber goroutines to run")
	fs.DurationVar(&opts.natsCfg.AckWaitTimeout, "ack-wait-timeout", 30*time.Second, "Ack wait timeout for NATS subscriber")
	fs.Int64Var(&opts.goChannelCfg.ChannelBuffer, "channel-buffer", 100, "GoChannel output buffer size")
	fs.BoolVar(&opts.goChannelCfg.Persistent, "persistent", true, "Whether GoChannel should persist messages in memory")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s --source <name> --until <YYYY-MM-DD> [flags]\n\n", CommandName)
		_, _ = fmt.Fprintln(fs.Output(), "Examples:")
		_, _ = fmt.Fprintf(fs.Output(), "  %s --source dpp --until 2026-01-01\n", CommandName)
		_, _ = fmt.Fprintf(fs.Output(), "  %s --source kmt --until 2024-01-01 --max-pages 50\n\n", CommandName)
		_, _ = fmt.Fprintf(fs.Output(), "  %s --source kmt --until 2024-01-01 --timeout 10m\n\n", CommandName)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.source == "" || untilRaw == "" {
		fs.Usage()
		return opts, ErrUsage
	}

	until, err := time.ParseInLocation("2006-01-02", untilRaw, time.Local)
	if err != nil {
		return opts, fmt.Errorf("failed to parse --until: %w", err)
	}
	opts.until = until

	return opts, nil
}
