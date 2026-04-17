package extractor_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/extractor"
	"github.com/ChiaYuChang/prism/internal/llm"
	llmmocks "github.com/ChiaYuChang/prism/internal/llm/mocks"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestExtractor_Extract_Success(t *testing.T) {
	generator := llmmocks.NewMockGenerator(t)
	ext, err := extractor.NewExtractor(
		generator,
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		"test-model",
		"test-prompt",
	)
	require.NoError(t, err)

	input := &model.ExtractionInput{
		Title: "Test Title",
		Body:  "Test Body",
	}

	expectedOutput := model.ExtractionOutput{
		Title:   "Neutral Title",
		Summary: "Test Summary",
		Topics:  []string{"Topic 1"},
		Entities: []model.ExtractionEntity{
			{Canonical: "Entity 1", Surface: "E1", Type: "person"},
		},
		Phrases: []string{"Phrase 1"},
	}

	outputJSON, _ := json.Marshal(expectedOutput)

	generator.EXPECT().Generate(mock.Anything, mock.MatchedBy(func(req *llm.GenerateRequest) bool {
		return req.Model == "test-model" && req.SystemInstruction == "test-prompt"
	})).Return(&llm.GenerateResponse{
		Text:       string(outputJSON),
		JsonSchema: extractor.ExtractionResultJSONSchema,
	}, nil)

	got, err := ext.Extract(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, expectedOutput.Title, got.Title)
	require.Equal(t, expectedOutput.Summary, got.Summary)
	require.Equal(t, expectedOutput.Topics, got.Topics)
	require.Equal(t, expectedOutput.Entities, got.Entities)
	require.Equal(t, expectedOutput.Phrases, got.Phrases)
}

func TestExtractor_Extract_NilInput(t *testing.T) {
	generator := llmmocks.NewMockGenerator(t)
	ext, err := extractor.NewExtractor(
		generator,
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		"test-model",
		"test-prompt",
	)
	require.NoError(t, err)

	_, err = ext.Extract(context.Background(), nil)
	require.ErrorIs(t, err, extractor.ErrNilExtractionInput)
}
