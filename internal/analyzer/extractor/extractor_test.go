package extractor_test

import (
	"context"

	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer"
	"github.com/ChiaYuChang/prism/internal/analyzer/extractor"
	"github.com/ChiaYuChang/prism/internal/llm"
	llmmocks "github.com/ChiaYuChang/prism/internal/llm/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewExtractorValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	generator := llmmocks.NewMockGenerator(t)

	_, err := extractor.NewExtractor(nil, logger, tracer, "model", "prompt")
	require.ErrorIs(t, err, extractor.ErrParamMissing)

	_, err = extractor.NewExtractor(generator, nil, tracer, "model", "prompt")
	require.ErrorIs(t, err, extractor.ErrParamMissing)

	ext, err := extractor.NewExtractor(generator, logger, tracer, "model", "prompt")
	require.NoError(t, err)
	require.NotNil(t, ext)
}

func TestExtractTopic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	generator := llmmocks.NewMockGenerator(t)

	ext, err := extractor.NewExtractor(generator, logger, tracer, "gemini-2.0-flash", "sys-prompt")
	require.NoError(t, err)

	t.Run("nil input error", func(t *testing.T) {
		_, err := ext.ExtractTopic(context.Background(), nil)
		require.ErrorIs(t, err, extractor.ErrNilInput)
	})

	t.Run("successful extraction with ID assignment", func(t *testing.T) {
		rawJSON := `{
			"topic_name": "碳費爭議",
			"facts": ["立法院通過草案"],
			"common_ground": ["合規成本高"],
			"issues": [
				{"type": "major", "name": "費率合理性", "description": "300元是否合理", "pro_criteria": ["誘發減碳"], "con_criteria": ["衝擊競爭力"]},
				{"type": "minor", "name": "優惠門檻", "description": "自主減量計畫", "pro_criteria": ["鼓勵減量"], "con_criteria": ["寬鬆洗綠"]}
			]
		}`

		generator.EXPECT().Generate(mock.Anything, mock.MatchedBy(func(req *llm.GenerateRequest) bool {
			return req.Model == "gemini-2.0-flash" && req.Format == llm.ResponseFormatJsonSchema
		})).Return(&llm.GenerateResponse{
			Text:       rawJSON,
			JsonSchema: extractor.TopicExtractionResultJSONSchema,
		}, nil).Once()

		out, err := ext.ExtractTopic(context.Background(), &analyzer.TopicExtractionInput{
			Articles: []analyzer.ArticleInput{{Title: "T1", Content: "C1"}},
		})
		require.NoError(t, err)
		require.Equal(t, "碳費爭議", out.TopicName)
		require.Len(t, out.Issues, 2)
		require.Equal(t, "major_1", out.Issues[0].ID)
		require.Equal(t, "minor_1", out.Issues[1].ID)
	})
}
