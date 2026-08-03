package opencode

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	var createSessionCalled bool
	var promptRequest struct {
		Agent  string `json:"agent"`
		System string `json:"system"`
		Model  struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
		} `json:"model"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		username, password, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "prism", username)
		assert.Equal(t, "secret", password)
		switch r.URL.Path {
		case "/session":
			createSessionCalled = true
			assert.Equal(t, http.MethodPost, r.Method)
			_, _ = io.WriteString(w, `{
				"id":"session-1",
				"directory":"/workspace",
				"projectID":"project-1",
				"time":{},
				"title":"prism llm generation",
				"version":"test"
			}`)
		case "/session/session-1/message":
			assert.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&promptRequest))
			_, _ = io.WriteString(w, `{
				"info":{
					"id":"message-1",
					"cost":0,
					"mode":"build",
					"modelID":"claude-sonnet-4",
					"parentID":"",
					"path":{},
					"providerID":"anthropic",
					"role":"assistant",
					"sessionID":"session-1",
					"system":[],
					"time":{},
					"tokens":{"cache":{},"input":10,"output":3,"reasoning":2}
				},
				"parts":[
					{"id":"part-1","messageID":"message-1","sessionID":"session-1","type":"text","text":"hello"},
					{"id":"part-2","messageID":"message-1","sessionID":"session-1","type":"reasoning","text":"ignored"},
					{"id":"part-3","messageID":"message-1","sessionID":"session-1","type":"text","text":" world"}
				]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	provider, err := New(
		context.Background(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		noop.NewTracerProvider().Tracer("test"),
		validator.New(),
		mold.New(),
		server.Client(),
		Config{
			BaseURL:   server.URL,
			Agent:     "build",
			Directory: "/workspace",
			Username:  "prism",
			Password:  "secret",
			Timeout:   time.Second,
		},
	)
	require.NoError(t, err)

	resp, err := provider.Generate(context.Background(), &llm.GenerateRequest{
		Model:             "anthropic/claude-sonnet-4",
		SystemInstruction: "Be concise.",
		Prompt:            "Say hello.",
		Format:            llm.ResponseFormatText,
	})
	require.NoError(t, err)

	assert.True(t, createSessionCalled)
	assert.Equal(t, "build", promptRequest.Agent)
	assert.Equal(t, "Be concise.", promptRequest.System)
	assert.Equal(t, "anthropic", promptRequest.Model.ProviderID)
	assert.Equal(t, "claude-sonnet-4", promptRequest.Model.ModelID)
	require.Len(t, promptRequest.Parts, 1)
	assert.Equal(t, "text", promptRequest.Parts[0].Type)
	assert.Equal(t, "Say hello.", promptRequest.Parts[0].Text)
	assert.Equal(t, "hello world", resp.Text)
	assert.Equal(t, llm.TokenUsage{Input: 10, Output: 3, Total: 15, Reasoning: 2}, resp.Usage)
}

func TestParseModel_InvalidModel(t *testing.T) {
	t.Parallel()

	_, err := parseModel("claude-sonnet-4")
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrCfgValError)
	assert.ErrorContains(t, err, "provider/model")
}

func TestEmbedUnsupported(t *testing.T) {
	t.Parallel()

	provider := &Provider{}
	_, err := provider.Embed(context.Background(), &llm.EmbedRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrEmbedAPIError)
}
