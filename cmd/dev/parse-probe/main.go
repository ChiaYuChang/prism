// parse-probe runs parser config against a local HTML file or stdin and prints
// the extraction result. Intended for iterating on selectors during development:
//
//	go run ./cmd/dev/parse-probe \
//	    -url https://www.dpp.org.tw/media/contents/11545 \
//	    -input testdata/fixtures/dpp/11545.html
//
// Use -all-parsers to ignore host routing and run every configured host's
// parser plus the generic fallback — useful for comparing extractors when
// adding a new site or diagnosing which one misbehaves.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/parser/config"
	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
	"github.com/ChiaYuChang/prism/internal/collector/parser/jsonld"
	parserllm "github.com/ChiaYuChang/prism/internal/collector/parser/llm"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/trace/noop"
	"gopkg.in/yaml.v3"
)

func main() {
	var (
		configPath = pflag.String("config", "internal/collector/parser/config/parsers.yaml", "path to parsers.yaml")
		urlFlag    = pflag.String("url", "", "article URL (required; determines default host routing)")
		inputPath  = pflag.String("input", "", "input HTML path (required; '-' for stdin)")
		allParsers = pflag.Bool("all-parsers", false, "ignore host routing; run every configured host's parser + generic fallback")
		format     = pflag.String("format", "plaintext", "output format: plaintext|json|yaml")
		promptFlag = pflag.String("prompt", "", "override path to the LLM fallback system-instruction file (defaults to fallback.prompt_file in parsers.yaml)")
	)
	pflag.Parse()

	if *urlFlag == "" || *inputPath == "" {
		fmt.Fprintln(os.Stderr, "both -url and -input are required")
		pflag.Usage()
		os.Exit(2)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		die("load config: %v", err)
	}
	data, err := readInput(*inputPath)
	if err != nil {
		die("read input: %v", err)
	}

	ctx := context.Background()
	logger := silentLogger()
	tracer := noop.NewTracerProvider().Tracer("parse-probe")

	var llmFactory config.LLMFactory
	if cfg.Fallback.Enable {
		if *promptFlag != "" {
			cfg.Fallback.PromptFile = *promptFlag
		}
		prompt, perr := config.LoadFallbackPrompt(cfg.Fallback)
		if perr != nil {
			die("load fallback prompt: %v", perr)
		}
		gen, gerr := llmfactory.NewGenerator(ctx, cfg.Fallback.LLM, logger)
		if gerr != nil {
			die("init fallback LLM generator: %v", gerr)
		}
		model := cfg.Fallback.LLM.Model
		llmFactory = func() (collector.Parser, error) {
			return parserllm.NewParser(gen, logger, model, prompt)
		}
	}

	results := make(map[string]Result)
	if *allParsers {
		for host, pCfg := range cfg.Parsers {
			if pCfg.Enabled != nil && !*pCfg.Enabled {
				continue
			}
			results[host] = run(ctx, buildParser(pCfg, logger), *urlFlag, data)
		}
		if llmFactory != nil {
			llmParser, err := llmFactory()
			if err != nil {
				die("build llm parser: %v", err)
			}
			results["__llm__"] = run(ctx, llmParser, *urlFlag, data)
		}
	} else {
		registry, err := config.BuildRegistry(cfg, logger, tracer, llmFactory)
		if err != nil {
			die("build registry: %v", err)
		}
		results["default"] = run(ctx, registry, *urlFlag, data)
	}

	if err := emit(os.Stdout, *format, results); err != nil {
		die("emit: %v", err)
	}
}

type Result struct {
	Article *collector.Article `json:"article,omitempty" yaml:"article,omitempty"`
	Error   string             `json:"error,omitempty"   yaml:"error,omitempty"`
}

func run(ctx context.Context, p collector.Parser, url, data string) Result {
	a, err := p.Parse(ctx, url, data)
	r := Result{Article: a}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

func buildParser(p config.ParserConfig, logger *slog.Logger) collector.Parser {
	h := html.New(p.HTML, p.DateLayouts)
	if !p.JSONLD {
		return h
	}
	j := jsonld.New()
	cp, err := parser.NewCompositeParser(logger, h, j)
	if err != nil {
		die("build composite parser: %v", err)
	}
	return cp
}

func loadConfig(path string) (config.Config, error) {
	var cfg config.Config
	body, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	return cfg, yaml.Unmarshal(body, &cfg)
}

func readInput(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func emit(w io.Writer, format string, results map[string]Result) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	case "yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer enc.Close()
		return enc.Encode(results)
	case "plaintext", "":
		return emitPlaintext(w, results)
	default:
		return fmt.Errorf("unknown format %q (want plaintext|json|yaml)", format)
	}
}

func emitPlaintext(w io.Writer, results map[string]Result) error {
	for name, r := range results {
		fmt.Fprintf(w, "=== %s ===\n", name)
		if r.Error != "" {
			fmt.Fprintf(w, "ERROR: %s\n\n", r.Error)
			continue
		}
		a := r.Article
		fmt.Fprintf(w, "URL:          %s\n", a.URL)
		fmt.Fprintf(w, "Title:        %s\n", a.Title)
		fmt.Fprintf(w, "Author:       %s\n", a.Author)
		fmt.Fprintf(w, "PublishedAt:  %s\n", a.PublishedAt.Format(time.RFC3339))
		fmt.Fprintf(w, "Content len:  %d bytes\n", len(a.Content))
		fmt.Fprintf(w, "Content head: %s\n\n", headRunes(a.Content, 200))
	}
	return nil
}

// headRunes returns up to n runes from s, appending "..." when truncated.
// Rune-safe so Chinese fixtures don't render as broken bytes.
func headRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func die(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}
