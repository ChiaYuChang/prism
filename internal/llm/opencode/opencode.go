package opencode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	sdk "github.com/sst/opencode-sdk-go"
	"github.com/sst/opencode-sdk-go/option"
	"go.opentelemetry.io/otel/trace"
)

// Config holds opencode-specific configuration.
type Config struct {
	BaseURL      string            `json:"base_url"      mapstructure:"base_url"      yaml:"base_url"      mod:"trim" validate:"omitempty,url"`
	Agent        string            `json:"agent"         mapstructure:"agent"         yaml:"agent"         mod:"trim,default=general"`
	Directory    string            `json:"directory"     mapstructure:"directory"     yaml:"directory"     mod:"trim"`
	Username     string            `json:"username"      mapstructure:"username"      yaml:"username"      mod:"trim"`
	Password     string            `json:"password"      mapstructure:"password"      yaml:"password"      mod:"trim"`
	PasswordFile string            `json:"password_file" mapstructure:"password_file" yaml:"password_file" mod:"trim"`
	Timeout      time.Duration     `json:"timeout"       mapstructure:"timeout"       yaml:"timeout"       mod:"trim,default=30s"`
	HttpHeader   map[string]string `json:"http_header"   mapstructure:"http_header"   yaml:"http_header"   mod:"trim"`
}

// Decoder decodes raw provider config for opencode.
type Decoder struct{}

// Decode converts raw provider config into a typed opencode config.
func (Decoder) Decode(raw map[string]any) (llm.ProviderConfig, error) {
	var cfg Config
	if err := llm.DecodeProviderConfig(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Build constructs an opencode provider from config and shared dependencies.
func (cfg Config) Build(ctx context.Context, deps llm.BuildDeps, buildCfg llm.BuildConfig) (llm.Provider, error) {
	if secret, err := appconfig.LoadFromFile(cfg.PasswordFile); err != nil {
		return nil, err
	} else if secret != "" {
		cfg.Password = secret
	}
	cfg.Timeout = buildCfg.Timeout
	return New(ctx, deps.Logger, deps.Tracer, deps.Validator, deps.Transformer, deps.HTTPClient, cfg)
}

// Provider implements llm.Generator for an opencode server.
type Provider struct {
	client      *sdk.Client
	logger      *slog.Logger
	tracer      trace.Tracer
	validator   *validator.Validate
	transformer *mold.Transformer
	agent       string
	directory   string
}

// New creates a new opencode provider instance with explicit dependency injection.
func New(ctx context.Context, l *slog.Logger, t trace.Tracer, v *validator.Validate,
	m *mold.Transformer, c *http.Client, cfg Config) (*Provider, error) {

	if err := m.Struct(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("opencode %w: %s", llm.ErrCfgModError, err)
	}

	if err := v.StructCtx(ctx, cfg); err != nil {
		return nil, fmt.Errorf("opencode %w: %s", llm.ErrCfgValError, err)
	}

	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}

	opts := sdk.DefaultClientOptions()
	opts = append(opts, option.WithHTTPClient(c))
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Timeout != 0 {
		opts = append(opts, option.WithRequestTimeout(cfg.Timeout))
	}
	if cfg.Username != "" || cfg.Password != "" {
		token := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		opts = append(opts, option.WithHeader("Authorization", "Basic "+token))
	}
	for k, v := range cfg.HttpHeader {
		opts = append(opts, option.WithHeaderAdd(k, v))
	}

	client := sdk.NewClient(opts...)
	return &Provider{
		client:      client,
		logger:      l,
		tracer:      t,
		validator:   v,
		transformer: m,
		agent:       cfg.Agent,
		directory:   cfg.Directory,
	}, nil
}

// Generate sends the request to an opencode session and returns the text parts from the assistant response.
func (p *Provider) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	tid := obs.ExtractTraceID(ctx)
	uid := obs.ExtractUserID(ctx)

	l := logger.WithHook(p.logger,
		logger.SinceHook("time", time.Now()),
		logger.AttrHook("trace_id", tid),
		logger.AttrHook("user_id", uid.String()))

	model, err := parseModel(req.Model)
	if err != nil {
		return nil, err
	}

	session, err := p.client.Session.New(ctx,
		sdk.SessionNewParams{
			Directory: sdk.String(p.directory),
			Title:     sdk.String("prism llm generation"),
		})
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"opencode session create error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("opencode %w: %s", llm.ErrGenAPIError, err)
	}

	systemInstruction, err := buildSystemInstruction(req)
	if err != nil {
		return nil, err
	}

	params := sdk.SessionPromptParams{
		Agent:  sdk.String(p.agent),
		System: sdk.String(systemInstruction),
		Model:  sdk.F(model),
		Parts: sdk.F([]sdk.SessionPromptParamsPartUnion{
			sdk.TextPartInputParam{
				Type: sdk.F(sdk.TextPartInputTypeText),
				Text: sdk.String(req.Prompt),
			},
		}),
	}
	if p.directory != "" {
		params.Directory = sdk.String(p.directory)
	}

	resp, err := p.client.Session.Prompt(ctx, session.ID, params)
	if err != nil {
		l.LogAttrs(ctx, slog.LevelError,
			"opencode generate error",
			slog.String("message", err.Error()),
			slog.String("model", req.Model))
		return nil, fmt.Errorf("opencode %w: %s", llm.ErrGenAPIError, err)
	}

	text := collectText(resp.Parts)
	usage := llm.TokenUsage{
		Input:     int(resp.Info.Tokens.Input),
		Output:    int(resp.Info.Tokens.Output),
		Reasoning: int(resp.Info.Tokens.Reasoning),
	}
	usage.Total = usage.Input + usage.Output + usage.Reasoning

	l.LogAttrs(ctx, slog.LevelInfo,
		"opencode generate success",
		slog.String("model", req.Model),
		slog.Int("total_tokens", usage.Total))

	return &llm.GenerateResponse{
		Model:      req.Model,
		Text:       text,
		Usage:      usage,
		Raw:        resp,
		JsonSchema: req.JSONSchema,
	}, nil
}

// Embed is not supported by opencode's session API.
func (p *Provider) Embed(ctx context.Context, req *llm.EmbedRequest) (*llm.EmbedResponse, error) {
	return nil, fmt.Errorf("opencode %w: embeddings are not supported", llm.ErrEmbedAPIError)
}

func parseModel(model string) (sdk.SessionPromptParamsModel, error) {
	providerID, modelID, ok := strings.Cut(model, "/")
	if !ok || providerID == "" || modelID == "" {
		return sdk.SessionPromptParamsModel{}, fmt.Errorf(
			"opencode %w: model must use provider/model format", llm.ErrCfgValError)
	}
	return sdk.SessionPromptParamsModel{
		ProviderID: sdk.String(providerID),
		ModelID:    sdk.String(modelID),
	}, nil
}

func buildSystemInstruction(req *llm.GenerateRequest) (string, error) {
	systemInstruction := req.SystemInstruction
	if req.Format != llm.ResponseFormatJsonSchema || req.JSONSchema.Schema == nil {
		return systemInstruction, nil
	}

	schema, err := json.Marshal(req.JSONSchema.Schema)
	if err != nil {
		return "", fmt.Errorf("opencode marshal response schema: %w", err)
	}
	if systemInstruction != "" {
		systemInstruction += "\n\n"
	}
	systemInstruction += "Return only a JSON object matching this JSON Schema, with no markdown fences or commentary:\n" + string(schema)
	return systemInstruction, nil
}

func collectText(parts []sdk.Part) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type != sdk.PartTypeText || part.Text == "" {
			continue
		}
		_, _ = b.WriteString(part.Text)
	}
	return b.String()
}

func (p *Provider) Close() error {
	return nil
}
