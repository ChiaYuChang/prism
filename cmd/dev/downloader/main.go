package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/fetcher"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	bfconfig "github.com/ChiaYuChang/prism/internal/discovery/backfiller/config"
	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/google/uuid"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/trace/noop"
)

var headers = map[string]string{
	"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
	"Accept-Language": "zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7",
}

// mirrorSaver saves content using its URL path structure to facilitate
// offline simulation with a simple file server.
type mirrorSaver struct {
	baseDir string
	logger  *slog.Logger
}

func (s *mirrorSaver) SaveURL(ctx context.Context, rawURL string, data string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	// Use host name as the root directory for this source.
	hostDir := filepath.Join(s.baseDir, u.Host)

	// Map URL path to file path.
	relPath := strings.TrimPrefix(u.Path, "/")
	if relPath == "" {
		relPath = "index.html"
	}

	// Incorporate query parameters into filename if present
	if u.RawQuery != "" {
		querySafe := strings.NewReplacer("?", "_", "&", "_", "=", "_", "/", "_").Replace(u.RawQuery)
		relPath = relPath + "_" + querySafe
	}
	fullPath := filepath.Join(hostDir, relPath)

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(data), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	s.logger.Info("mirrored file saved", "url", rawURL, "file", fullPath)
	return nil
}

// failingMinifier triggers raw archive save by returning an error.
type failingMinifier struct{}

func (f *failingMinifier) Transform(ctx context.Context, raw string) (string, error) {
	return "", errors.New("deliberate failure to trigger raw HTML archive")
}

type noopParser struct{}

func (p *noopParser) Parse(ctx context.Context, url string, data string) (*collector.Article, error) {
	return &collector.Article{}, nil
}

// bridgeSink connects discovery to downloader.
type bridgeSink struct {
	dispatcher *collector.Dispatcher
	mSaver     *mirrorSaver
	logger     *slog.Logger
	httpClient *http.Client
}

func (s *bridgeSink) Handle(ctx context.Context, req discoverysink.CandidateSinkRequest) error {
	// 1. First, download and mirror the directory page itself if not already done.
	if err := s.downloadAndMirror(ctx, req.SourceURL); err != nil {
		s.logger.Error("failed to mirror directory page", "url", req.SourceURL, "error", err)
	}

	// 2. Download each candidate.
	for _, c := range req.Candidates {
		s.logger.Info("downloading candidate", "url", c.URL)
		_, err := s.dispatcher.Dispatch(ctx, "", c.URL)
		if err != nil {
			var stageErr *collector.StageError
			if errors.As(err, &stageErr) && stageErr.Stage == collector.PipelineStageMinify {
				// We expect this! Now we save it via MirrorSaver.
				if err := s.mSaver.SaveURL(ctx, c.URL, stageErr.Intermediate); err != nil {
					s.logger.Error("failed to mirror candidate", "url", c.URL, "error", err)
				}
			} else {
				s.logger.Error("unexpected dispatch error", "url", c.URL, "error", err)
			}
		}
	}
	return nil
}

func (s *bridgeSink) downloadAndMirror(ctx context.Context, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return s.mSaver.SaveURL(ctx, rawURL, string(body))
}

func main() {
	var (
		sourceName   string
		customName   string
		maxPages     int
		configPath   string
		bfConfigPath string
		outputDir    string
		startURL     string
		step         int
		first        int
	)

	pflag.StringVarP(&sourceName, "source", "s", "dpp", "Source name in scouts.yaml")
	pflag.StringVar(&customName, "name", "", "Custom name for output directory (defaults to source)")
	pflag.IntVarP(&maxPages, "max-pages", "n", 1, "Maximum directory pages to crawl")
	pflag.StringVarP(&configPath, "config", "c", "internal/discovery/scout/config/scouts.yaml", "Path to scouts.yaml")
	pflag.StringVar(&bfConfigPath, "backfill-config", "internal/discovery/backfiller/config/backfillers.yaml", "Path to backfillers.yaml")
	pflag.StringVarP(&outputDir, "output", "o", "testdata/fixtures", "Base directory for mirrored HTML")
	pflag.StringVar(&startURL, "start-url", "", "Override start URL (supports Go template {{.Value}})")
	pflag.IntVar(&step, "step", 0, "Pager step (0 = use config)")
	pflag.IntVar(&first, "first", -1, "Pager first index (-1 = use config)")
	pflag.Parse()

	if customName == "" {
		customName = sourceName
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	tracer := noop.NewTracerProvider().Tracer("downloader")
	ctx := context.Background()

	// 1. Load Scout Config
	scoutCfgRaw, err := scoutconfig.ReadFile(configPath)
	if err != nil {
		logger.Error("failed to read scouts.yaml", "path", configPath, "error", err)
		os.Exit(1)
	}
	scoutRepo, _ := scoutconfig.New(scoutCfgRaw)

	// 2. Load Backfiller Config
	bfCfgRaw, err := bfconfig.ReadFile(bfConfigPath)
	if err != nil {
		logger.Error("failed to read backfillers.yaml", "path", bfConfigPath, "error", err)
		os.Exit(1)
	}
	bfRepo, _ := bfconfig.New(bfCfgRaw)

	// 3. Build Scout
	httpClient := &http.Client{Timeout: 15 * time.Second}
	scout, err := scoutconfig.BuildScoutByName(scoutRepo, sourceName, logger, tracer, httpClient)
	if err != nil {
		logger.Error("failed to build scout", "source", sourceName, "error", err)
		os.Exit(1)
	}

	// 4. Setup Mirror Storage
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		logger.Error("failed to create output dir", "dir", outputDir, "error", err)
		os.Exit(1)
	}
	mSaver := &mirrorSaver{baseDir: outputDir, logger: logger}

	// 5. Setup Dispatcher. The failingMinifier deliberately errors so the
	// caller archives the raw fetched HTML via MirrorSaver — the whole point
	// of this dev tool.
	f := fetcher.NewHTTPFetcher(httpClient)
	reg := collector.NewPipelineRegistry(collector.Pipeline{
		Fetcher:      f,
		Minifier:     &failingMinifier{},
		Transformers: []collector.Transformer{transformer.NewNoOpTransformer()},
		Parser:       &noopParser{},
	})
	d, err := collector.NewDispatcher(logger, tracer, reg)
	if err != nil {
		logger.Error("failed to init dispatcher", "error", err)
		os.Exit(1)
	}

	// 6. Setup Pager from Config
	sourceCfg, ok := bfRepo.Source(sourceName)
	if !ok {
		logger.Error("source not found in backfiller config", "source", sourceName)
		os.Exit(1)
	}

	pagerCfg := backfiller.IndexPagerConfig{
		BaseURL:     sourceCfg.BaseURL,
		URLTemplate: sourceCfg.Pager.URLTemplate,
		First:       sourceCfg.Pager.First,
		Step:        sourceCfg.Pager.Step,
		Params:      sourceCfg.Pager.Params,
	}

	// Apply CLI overrides if provided
	if startURL != "" {
		pagerCfg.URLTemplate = startURL
	}
	if step > 0 {
		pagerCfg.Step = step
	}
	if first >= 0 {
		pagerCfg.First = first
	}

	pager, err := backfiller.NewIndexPager(logger, tracer, pagerCfg)
	if err != nil {
		logger.Error("failed to init pager", "error", err)
		os.Exit(1)
	}

	// 7. Run!
	sink := &bridgeSink{dispatcher: d, mSaver: mSaver, logger: logger, httpClient: httpClient}
	bf, _ := backfiller.New(logger, tracer, scout, pager, sink, sourceName, 30*time.Second)

	logger.Info("starting mirror downloader", "source", sourceName, "max_pages", maxPages, "pattern", pagerCfg.URLTemplate)
	batchID, _ := uuid.NewV7()
	_, err = bf.Run(ctx, discovery.BackfillRequest{
		BatchID:  batchID,
		Until:    time.Now().AddDate(-10, 0, 0),
		MaxPages: maxPages,
	})
	if err != nil {
		logger.Error("backfill failed", "error", err)
		os.Exit(1)
	}

	logger.Info("mirroring complete", "dir", outputDir)
}
