package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	backfillconfig "github.com/ChiaYuChang/prism/internal/discovery/backfiller/config"
	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
)

const (
	defaultScoutConfigPath      = "internal/discovery/scout/config/scouts.yaml"
	defaultBackfillerConfigPath = "internal/discovery/backfiller/config/backfillers.yaml"
	commandName                 = "backfiller"
)

func main() {
	var (
		source         string
		untilRaw       string
		maxPages       int
		scoutConfig    string
		backfillConfig string
		timeout        time.Duration
	)

	fs := pflag.NewFlagSet("backfiller", pflag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.StringVar(&source, "source", "", "backfill source name, e.g. dpp, tpp, kmt")
	fs.StringVar(&untilRaw, "until", "", "stop when listing items become older than this date (YYYY-MM-DD)")
	fs.IntVar(&maxPages, "max-pages", 0, "maximum number of listing pages to visit (0 means unlimited)")
	fs.StringVar(&scoutConfig, "scout-config", defaultScoutConfigPath, "path to scout config file")
	fs.StringVar(&backfillConfig, "backfill-config", defaultBackfillerConfigPath, "path to backfiller config file")
	fs.DurationVar(&timeout, "timeout", 0, "timeout for all backfill process (0 means unlimited)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s --source <name> --until <YYYY-MM-DD> [flags]\n\n", commandName)
		_, _ = fmt.Fprintln(fs.Output(), "Examples:")
		_, _ = fmt.Fprintf(fs.Output(), "  %s --source dpp --until 2026-01-01\n", commandName)
		_, _ = fmt.Fprintf(fs.Output(), "  %s --source kmt --until 2024-01-01 --max-pages 50\n\n", commandName)
		_, _ = fmt.Fprintf(fs.Output(), "  %s --source kmt --until 2024-01-01 --timeout 10m\n\n", commandName)
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if source == "" || untilRaw == "" {
		fs.Usage()
		os.Exit(2)
	}

	until, err := time.ParseInLocation("2006-01-02", untilRaw, time.Local)
	if err != nil {
		slog.Error("failed to parse --until", "value", untilRaw, "error", err)
		os.Exit(1)
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
	scoutCfg, err := scoutconfig.ReadFile(scoutConfig)
	if err != nil {
		logger.Error("failed to read scout config", slog.Any("error", err))
		os.Exit(1)
	}
	scoutRepo, err := scoutconfig.New(scoutCfg)
	if err != nil {
		logger.Error("failed to initialize scout repo", slog.Any("error", err))
		os.Exit(1)
	}

	backfillCfg, err := backfillconfig.ReadFile(backfillConfig)
	if err != nil {
		logger.Error("failed to read backfiller config", "path", backfillConfig, "error", err)
		os.Exit(1)
	}
	backfillRepo, err := backfillconfig.New(backfillCfg)
	if err != nil {
		logger.Error("failed to initialize backfiller repo", slog.Any("error", err))
		os.Exit(1)
	}
	spec, ok := backfillRepo.Source(source)
	if !ok {
		logger.Error("unknown backfill source", "source", source)
		os.Exit(1)
	}

	sink := backfiller.SinkFunc(func(_ context.Context, sourceURL string, candidates []model.Candidates) error {
		logger.Info("backfiller discovered candidates",
			"source_url", sourceURL,
			"count", len(candidates),
		)
		return nil
	})
	backfiller, err := backfillconfig.BuildBackfiller(spec, scoutRepo, logger, otel.Tracer("prism.backfiller"), http.DefaultClient, sink)
	if err != nil {
		logger.Error("failed to build backfiller", "source", source, "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	result, err := backfiller.Run(ctx, discovery.BackfillRequest{
		Until:    until,
		MaxPages: maxPages,
	})
	if err != nil {
		logger.Error("backfill failed", "source", source, "error", err)
		os.Exit(1)
	}

	logger.Info("backfill completed",
		"source", source,
		"until", until.Format("2006-01-02"),
		"pages_visited", result.PagesVisited,
		"candidates_seen", result.CandidatesSeen,
		"candidates_processed", result.CandidatesProcessed,
	)
}
