package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/ChiaYuChang/prism/pkg/utils"

	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genai"
)

// Config holds Gemini-specific configuration (Pure Data).
type Config struct {
	APIKey     string            `json:"api_key"     mod:"trim"                                                   validate:"required"`
	BaseURL    string            `json:"base_url"    mod:"trim,default=https://generativelanguage.googleapis.com" validate:"omitempty,url"`
	APIVer     string            `json:"api_ver"     mod:"trim,default=v1beta"`
	Project    string            `json:"project"     mod:"trim"`
	Timeout    time.Duration     `json:"timeout"     mod:"trim,default=30s"`
	HttpHeader map[string]string `json:"http_header" mod:"trim"`
}

// Provider implements both llm.Generator and llm.Embedder for Google Gemini.
type Provider struct {
	client      *genai.Client
	logger      *slog.Logger
	tracer      trace.Tracer
	validator   *validator.Validate
	transformer *mold.Transformer
}

// New creates a new Gemini provider instance with explicit dependency injection.
func New(ctx context.Context, l *slog.Logger, t trace.Tracer, v *validator.Validate,
	m *mold.Transformer, c *http.Client, cfg Config) (*Provider, error) {

	if err := m.Struct(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("gemini %w: %s", llm.ErrCfgModError, err)
	}

	if err := v.StructCtx(ctx, cfg); err != nil {
		return nil, fmt.Errorf("gemini %w: %s", llm.ErrCfgValError, err)
	}

	header := http.Header{}
	if cfg.HttpHeader != nil {
		for k, v := range cfg.HttpHeader {
			header.Set(k, v)
		}
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
		Project: cfg.Project,
		HTTPOptions: genai.HTTPOptions{
			BaseURL:    cfg.BaseURL,
			APIVersion: cfg.APIVer,
			Timeout:    utils.Ptr(cfg.Timeout),
			Headers:    header,
		},
		HTTPClient: c,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini %w: %s", llm.ErrCliCreateError, err)
	}

	return &Provider{
		client:      client,
		logger:      l,
		tracer:      t,
		validator:   v,
		transformer: m,
	}, nil
}

func (p *Provider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	l := logger.WithHook(p.logger,
		logger.SinceHook("time", time.Now()),
		logger.AttrHook("trace_id", tid),
		logger.AttrHook("user_id", uid.String()))

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: req.SystemInstruction}},
		},
	}

	// Apply sampling params
	if req.Temperature != nil {
		config.Temperature = utils.Ptr(float32(*req.Temperature))
	}
	if req.TopP != nil {
		config.TopP = utils.Ptr(float32(*req.TopP))
	}
	if req.TopK != nil {
		config.TopK = utils.Ptr(float32(*req.TopK))
	}
	if req.MaxTokens != nil {
		config.MaxOutputTokens = int32(*req.MaxTokens)
	}

	// Handle JSON Mode
	if req.Format == llm.ResponseFormatJsonSchema && req.JSONSchema.Schema != nil {
		config.ResponseMIMEType = "application/json"
		config.ResponseJsonSchema, _ = req.JSONSchema.ToGemini()
	}

	// Propagate Metadata & Tracking from Context via internal/obs
	config.Labels = req.Meta
	if config.Labels == nil {
		config.Labels = map[string]string{}
	}
	if tid != "" && tid != obs.DefaultTraceIDFallback {
		config.Labels["trace_id"] = tid
	}
	if uid != uuid.Nil {
		config.Labels["user_id"] = uid.String()
	}

	resp, err := p.client.Models.GenerateContent(ctx, req.Model, genai.Text(req.Prompt), config)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"gemini generate error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("gemini %w: %s", llm.ErrGenAPIError, err.Error())
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"gemini generate success",
		slog.String("model", resp.ModelVersion),
		slog.Int("total_tokens", int(resp.UsageMetadata.TotalTokenCount)))

	return &llm.GenerateResponse{
		Model: fmt.Sprintf("%s:%s", req.Model, resp.ModelVersion),
		Text:  resp.Text(),
		Usage: llm.Usage{
			InputTokenCount:  int(resp.UsageMetadata.PromptTokenCount),
			OutputTokenCount: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokenCount:  int(resp.UsageMetadata.TotalTokenCount),
		},
		JsonSchema: req.JSONSchema,
		Raw:        resp,
	}, nil
}

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

	contents := make([]*genai.Content, 0, len(req.Input))
	for _, text := range req.Input {
		contents = append(contents, &genai.Content{
			Parts: []*genai.Part{{Text: text}},
		})
	}

	config := &genai.EmbedContentConfig{}
	if req.Dimentions > 0 {
		config.OutputDimensionality = utils.Ptr(int32(req.Dimentions))
	}

	resp, err := p.client.Models.EmbedContent(ctx, req.Model, contents, config)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"gemini embed error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("gemini %w: %s", llm.ErrEmbedAPIError, err.Error())
	}

	n := len(resp.Embeddings)
	d := len(resp.Embeddings[0].Values)
	data := make([]float32, n*d)

	vectors := make([][]float32, n)
	for i := range vectors {
		vectors[i] = data[i*d : (i+1)*d]
		copy(vectors[i], resp.Embeddings[i].Values)
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"gemini embed success",
		slog.String("model", req.Model),
		slog.Int("input_count", n))

	return &llm.EmbedResponse{
		Model:   req.Model,
		Vectors: vectors,
		Raw:     resp,
	}, nil
}

func (p *Provider) BatchEmbed(ctx context.Context, req *llm.BatchJobRequest, model string, input ...string) (*llm.BatchJobResponse, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	l := logger.WithHook(p.logger,
		logger.SinceHook("time", time.Now()),
		func(ctx context.Context, r slog.Record) slog.Record {
			r.Add("trace_id", tid)
			r.Add("user_id", uid)
			return r
		})

	contents := make([]*genai.Content, 0, len(input))
	for _, text := range input {
		contents = append(contents, &genai.Content{
			Parts: []*genai.Part{{Text: text}},
		})
	}

	resp, err := p.client.Batches.CreateEmbeddings(
		ctx, utils.Ptr(model),
		&genai.EmbeddingsBatchJobSource{
			InlinedRequests: &genai.EmbedContentBatch{
				Contents: contents,
				Config:   nil,
			},
		},
		&genai.CreateEmbeddingsBatchJobConfig{},
	)

	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"gemini batch embed error",
			slog.String("message", err.Error()),
			slog.String("model", model))
		return nil, fmt.Errorf("gemini %w: %s", llm.ErrEmbedBatchAPIError, err.Error())
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"gemini batch embed created",
		slog.String("model", model),
		slog.String("job_name", resp.Name))

	return &llm.BatchJobResponse{
		Name:        resp.Name,
		DisplayName: resp.DisplayName,
		State:       string(resp.State),
		OutFileName: resp.Dest.FileName,
		Raw:         resp,
	}, nil
}

func (p *Provider) Close() error {
	return nil
}
