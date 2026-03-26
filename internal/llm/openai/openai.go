package openai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"go.opentelemetry.io/otel/trace"
)

// Config holds OpenAI-specific configuration (Pure Data).
type Config struct {
	APIKey     string            `json:"api_key"     mod:"trim" validate:"required"`
	BaseURL    string            `json:"base_url"    mod:"trim,default=https://api.openai.com/v1" validate:"omitempty,url"`
	Project    string            `json:"project"     mod:"trim"`
	Timeout    time.Duration     `json:"timeout"     mod:"trim,default=30s"`
	HttpHeader map[string]string `json:"http_header" mod:"trim"`
}

// Provider implements both llm.Generator and llm.Embedder for OpenAI.
type Provider struct {
	client      *openai.Client
	logger      *slog.Logger
	tracer      trace.Tracer
	validator   *validator.Validate
	transformer *mold.Transformer
}

// New creates a new OpenAI provider instance with explicit dependency injection.
func New(ctx context.Context, l *slog.Logger, t trace.Tracer, v *validator.Validate,
	m *mold.Transformer, c *http.Client, cfg Config) (*Provider, error) {

	if err := m.Struct(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("openai %w: %s", llm.ErrCfgModError, err)
	}

	if err := v.StructCtx(ctx, cfg); err != nil {
		return nil, fmt.Errorf("openai %w: %s", llm.ErrCfgValError, err)
	}

	if cfg.APIKey == "" {
		return nil, llm.ErrMissingAPIKEY
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}

	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	if cfg.Project != "" {
		opts = append(opts, option.WithProject(cfg.Project))
	}

	if c != nil {
		opts = append(opts, option.WithHTTPClient(c))
	}

	if cfg.Timeout != 0 {
		opts = append(opts, option.WithRequestTimeout(cfg.Timeout))
	}

	if cfg.HttpHeader != nil {
		for k, v := range cfg.HttpHeader {
			opts = append(opts, option.WithHeaderAdd(k, v))
		}
	}

	client := openai.NewClient(opts...)
	return &Provider{
		client:      &client,
		logger:      l,
		tracer:      t,
		validator:   v,
		transformer: m,
	}, nil
}

// Generate produces content based on the structured request.
func (p *Provider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	l := logger.WithHook(p.logger,
		logger.SinceHook("time", time.Now()),
		func(ctx context.Context, r slog.Record) slog.Record {
			r.Add("trace_id", tid)
			r.Add("user_id", uid)
			return r
		})

	params := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(req.Model),
		Instructions: openai.String(req.SystemInstruction),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(req.Prompt),
		},
	}

	// Extract IDs from context via internal/obs
	if uid != uuid.Nil {
		params.SafetyIdentifier = openai.String(uid.String())
	}

	if req.Temperature != nil {
		params.Temperature = openai.Float(float64(*req.Temperature))
	}

	if req.TopP != nil {
		params.TopP = openai.Float(float64(*req.TopP))
	}

	if req.MaxTokens != nil {
		params.MaxOutputTokens = openai.Int(int64(*req.MaxTokens))
	}

	// Handle JSON Mode
	if req.Format == llm.ResponseFormatJsonSchema && req.JSONSchema.Schema != nil {
		params.Text = responses.ResponseTextConfigParam{
			Verbosity: responses.ResponseTextConfigVerbosityMedium,
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   req.JSONSchema.Name,
					Schema: req.JSONSchema.MustToOpenAI(),
					Strict: openai.Bool(true),
				},
			},
		}
	}

	// Propagate Metadata including TraceID via internal/obs
	params.Metadata = req.Meta
	if tid != "" && tid != obs.DefaultTraceIDFallback {
		if params.Metadata == nil {
			params.Metadata = map[string]string{}
		}
		params.Metadata["trace_id"] = tid
	}

	resp, err := p.client.Responses.New(ctx, params)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"openai generate error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("openai %w: %s", llm.ErrGenAPIError, err)
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"openai generate success",
		slog.String("model", resp.Model),
		slog.Int64("total_tokens", resp.Usage.TotalTokens))

	return &llm.GenerateResponse{
		Model: resp.Model,
		Text:  resp.OutputText(),
		Usage: llm.Usage{
			InputTokenCount:  int(resp.Usage.InputTokens),
			OutputTokenCount: int(resp.Usage.OutputTokens),
			TotalTokenCount:  int(resp.Usage.TotalTokens),
		},
		Raw:        resp,
		JsonSchema: req.JSONSchema,
	}, nil
}

// Embed generates vector embeddings for the provided input strings.
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

	params := openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(req.Model),
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: req.Input,
		},
		Dimensions:     openai.Int(int64(req.Dimentions)),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatBase64,
	}

	// Extract UserID from context via internal/obs
	if uid != uuid.Nil {
		params.User = openai.String(uid.String())
	}

	resp, err := p.client.Embeddings.New(ctx, params)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"openai embed error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("openai %w: %s", llm.ErrEmbedAPIError, err.Error())
	}

	n := len(resp.Data)
	d := len(resp.Data[0].Embedding)
	data := make([]float32, n*d)

	vectors := make([][]float32, n)
	for i := range vectors {
		vectors[i] = data[i*d : (i+1)*d]
		for j, f64 := range resp.Data[i].Embedding {
			vectors[i][j] = float32(f64)
		}
	}

	l.LogAttrs(ctx, slog.LevelInfo,
		"openai embed success",
		slog.String("model", resp.Model),
		slog.Int("input_count", n))

	return &llm.EmbedResponse{
		Model:   resp.Model,
		Vectors: vectors,
		Raw:     resp,
	}, nil
}

func (p *Provider) Close() error {
	return nil
}
