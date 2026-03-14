package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/pkg/logger"

	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/ollama/ollama/api"
	"go.opentelemetry.io/otel/trace"
)

// Config holds Ollama-specific configuration (Pure Data).
type Config struct {
	BaseURL    string            `json:"base_url"    mod:"trim,default=http://localhost:11434" validate:"omitempty,url"`
	Timeout    time.Duration     `json:"timeout"     mod:"trim,default=5s"`
	Project    string            `json:"project"     mod:"trim"`
	HttpHeader map[string]string `json:"http_header" mod:"trim"`
}

// Provider implements both llm.Generator and llm.Embedder for Ollama.
type Provider struct {
	client      *api.Client
	logger      *slog.Logger
	tracer      trace.Tracer
	validator   *validator.Validate
	transformer *mold.Transformer
	header      http.Header
}

// New creates a new Ollama provider instance with explicit dependency injection.
func New(ctx context.Context, l *slog.Logger, t trace.Tracer, v *validator.Validate,
	m *mold.Transformer, c *http.Client, cfg Config) (*Provider, error) {

	if err := m.Struct(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("failed to scrub ollama config: %w", err)
	}

	if err := v.StructCtx(ctx, cfg); err != nil {
		return nil, fmt.Errorf("failed to validate ollama config: %w", err)
	}

	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ollama url: %w", err)
	}

	header := http.Header{}
	if cfg.HttpHeader != nil {
		for k, v := range cfg.HttpHeader {
			header.Set(k, v)
		}
	}

	client := api.NewClient(baseURL, c)
	return &Provider{
		client:      client,
		logger:      l,
		tracer:      t,
		validator:   v,
		transformer: m,
		header:      header,
	}, nil
}

// Generate produces content using the local Ollama model.
func (p *Provider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	// Dynamically enrich logger
	l := logger.WithHook(p.logger,
		logger.SinceHook("time", time.Now()),
		func(ctx context.Context, r slog.Record) slog.Record {
			r.Add("trace_id", tid)
			r.Add("user_id", uid)
			return r
		})

	format, err := json.Marshal(req.JSONSchema.Schema)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"failed to marshal json schema",
			slog.String("message", err.Error()))
		return nil, fmt.Errorf("failed to marshal json schema: %w", err)
	}

	cReq := &api.ChatRequest{
		Model: req.Model,
		Messages: []api.Message{
			{Role: "system", Content: req.SystemInstruction},
			{Role: "user", Content: req.Prompt},
		},
		Stream: new(bool),
		Format: format,
	}

	options := make(map[string]interface{})
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		options["top_p"] = *req.TopP
	}
	if req.TopK != nil {
		options["top_k"] = *req.TopK
	}
	if req.MaxTokens != nil {
		options["num_predict"] = *req.MaxTokens
	}
	cReq.Options = options

	resp := strings.Builder{}
	var raws []api.ChatResponse
	fn := func(r api.ChatResponse) error {
		_, _ = resp.WriteString(r.Message.Content)
		raws = append(raws, r)
		return nil
	}

	if err := p.client.Chat(ctx, cReq, fn); err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"ollama generate error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("ollama chat error: %w", err)
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"ollama generate success",
		slog.Int("resp_len", resp.Len()),
		slog.String("model", req.Model))

	return &llm.GenerateResponse{
		Model:      req.Model,
		Text:       resp.String(),
		JsonSchema: req.JSONSchema,
		Raw:        raws,
	}, nil
}

// Embed generates vector embeddings for the provided input strings using Ollama.
func (p *Provider) Embed(ctx context.Context, req *llm.EmbedRequest) (*llm.EmbedResponse, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	l := logger.WithHook(p.logger,
		logger.SinceHook("time", time.Now()),
		func(ctx context.Context, r slog.Record) slog.Record {
			r.Add("trace_id", tid)
			r.Add("user_id", uid)
			return r
		})

	eReq := &api.EmbedRequest{
		Model:      req.Model,
		Input:      req.Input,
		Dimensions: req.Dimentions,
	}

	resp, err := p.client.Embed(ctx, eReq)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"ollama embed error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("ollama embed error: %w", err)
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"ollama embed success",
		slog.Int("input_count", len(req.Input)),
		slog.String("model", resp.Model))

	return &llm.EmbedResponse{
		Model:   resp.Model,
		Vectors: resp.Embeddings,
		Raw:     resp,
	}, nil
}

func (p *Provider) Close() error {
	return nil
}
