package scorer_test

import (
	"context"

	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer"
	"github.com/ChiaYuChang/prism/internal/analyzer/scorer"
	"github.com/ChiaYuChang/prism/internal/llm"
	llmmocks "github.com/ChiaYuChang/prism/internal/llm/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewScorerValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	generator := llmmocks.NewMockGenerator(t)

	_, err := scorer.NewScorer(nil, logger, tracer, "model", "prompt")
	require.ErrorIs(t, err, scorer.ErrParamMissing)

	s, err := scorer.NewScorer(generator, logger, tracer, "model", "prompt")
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestScoreStance(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	generator := llmmocks.NewMockGenerator(t)

	s, err := scorer.NewScorer(generator, logger, tracer, "gemini-2.0-flash", "sys-prompt")
	require.NoError(t, err)

	t.Run("nil input error", func(t *testing.T) {
		_, err := s.ScoreStance(context.Background(), nil)
		require.ErrorIs(t, err, scorer.ErrNilInput)
	})

	t.Run("successful stance scoring", func(t *testing.T) {
		rawJSON := `{
			"scores": [
				{
					"issue_id": "major_1",
					"score": 2,
					"reasoning": "工總質疑費率過高衝擊競爭力",
					"references": ["每噸300元費率對產業將造成沉重負擔"]
				}
			]
		}`

		generator.EXPECT().Generate(mock.Anything, mock.MatchedBy(func(req *llm.GenerateRequest) bool {
			return req.Model == "gemini-2.0-flash" && req.Format == llm.ResponseFormatJsonSchema
		})).Return(&llm.GenerateResponse{
			Text:       rawJSON,
			JsonSchema: scorer.StanceScoringResultJSONSchema,
		}, nil).Once()

		out, err := s.ScoreStance(context.Background(), &analyzer.StanceScoringInput{
			TopicName: "碳費爭議",
			Issues:    []analyzer.Issue{{ID: "major_1", Type: "major", Name: "費率合理性"}},
			Article:   analyzer.ArticleInput{Title: "Title", Content: "Content"},
		})
		require.NoError(t, err)
		require.NotNil(t, out)
		require.Len(t, out.Scores, 1)
		require.Equal(t, "major_1", out.Scores[0].IssueID)
		require.Equal(t, 2, out.Scores[0].Score)
	})
}
