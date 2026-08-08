package extraction_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/stageparams"
	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/stretchr/testify/require"
	"errors"

	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/google/uuid"
)

type fakeGenerator struct{}

func (f *fakeGenerator) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	return nil, nil
}

type fakeLoader struct{}

func (f *fakeLoader) Load(ctx context.Context, id uuid.UUID) (prompt.Loaded, error) {
	if id == uuid.MustParse("00000000-0000-0000-0000-000000000001") {
		return prompt.Loaded{
			ID:   id,
			Hash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Body: []byte(extraction.DefaultPromptTemplateText),
		}, nil
	}
	return prompt.Loaded{}, errors.New("prompt not found")
}

func TestFactory_ValidV1Parameters(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	body := json.RawMessage(`{"model":"fake-model", "prompt_id":"00000000-0000-0000-0000-000000000001"}`)
	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    body,
	}

	stage, err := extraction.New(context.Background(), deps, stored)
	require.NoError(t, err)
	require.NotNil(t, stage)
	require.Equal(t, "extraction", stage.Name())
}

func TestFactory_UnsupportedVersion(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 2, // unsupported
		Body:    json.RawMessage(`{}`),
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.ErrorIs(t, err, extraction.ErrUnsupportedParameterVersion)
}

func TestFactory_MalformedJSONFails(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    json.RawMessage(`{"model":"fake-model", "prompt_id": `), // malformed
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
}

func TestFactory_UnknownFieldFails(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    json.RawMessage(`{"model":"fake-model", "prompt_id":"00000000-0000-0000-0000-000000000001", "unknown_field": "123"}`),
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "unknown field"))
}

func TestFactory_InvalidV1Parameters(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    json.RawMessage(`{"model":"", "prompt_id":"00000000-0000-0000-0000-000000000001"}`), // invalid model
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.Contains(t, err.Error(), "model is required")
}

func TestFactory_PromptResolutionError(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    json.RawMessage(`{"model":"fake-model", "prompt_id":"00000000-0000-0000-0000-000000000002"}`),
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load prompt")
}

func TestFactory_MissingGenerator(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    nil, // missing
		PromptLoader: &fakeLoader{},
	}

	body := json.RawMessage(`{"model":"fake-model", "prompt_id":"00000000-0000-0000-0000-000000000001"}`)
	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    body,
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generator is required")
}

func TestFactory_TrailingJSONFails(t *testing.T) {
	deps := extraction.Dependencies{
		Generator:    &fakeGenerator{},
		PromptLoader: &fakeLoader{},
	}

	stored := stageparams.Stored{
		Stage:   "extraction",
		Version: 1,
		Body:    json.RawMessage(`{"model":"fake-model", "prompt_id":"00000000-0000-0000-0000-000000000001"}{"unexpected":"document"}`), // trailing json
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple JSON values")
}

func TestFactory_WrongStageName(t *testing.T) {
	deps := extraction.Dependencies{}
	stored := stageparams.Stored{
		Stage:   "canonicalization",
		Version: 1,
		Body:    json.RawMessage(`{}`),
	}

	_, err := extraction.New(context.Background(), deps, stored)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected stage 'extraction'")
}
