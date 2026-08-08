package extraction_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

const validArticleTitle = `政院稱電價暫無調漲規畫，在野黨要求公開評估`

const validArticleContent = `行政院發言人林明華今天表示，政府目前沒有調漲電價的規畫，相關政策仍會依能源價格與供電情勢滾動檢討。
林明華說：「如果國際燃料價格持續上升，我們會重新評估電價方案。」
在野黨立委陳志強批評，政府沒有完整公開電價評估資料，並要求行政院在一週內公布相關文件。
陳志強表示：「資訊不透明只會增加民眾的不信任。」`

func validInput() extraction.Input {
	return extraction.Input{
		Title:   validArticleTitle,
		Content: validArticleContent,
	}
}

func strptr(s string) *string { return &s }

func validOutput() extraction.Output {
	return extraction.Output{
		Summary: "行政院表示目前沒有調漲電價的規畫，但會依能源與供電情勢持續檢討；在野黨立委則要求政府公開電價評估資料。",
		Statements: []extraction.Statement{
			{
				Statement: "政府目前沒有調漲電價的規畫。",
				Type:      extraction.StatementTypeFactual,
				Tags:      []extraction.StatementTag{},
				Sentiment: extraction.SentimentNeutral,
				Source: extraction.Source{
					Name: strptr("林明華"),
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeIndirect,
				},
				Importance: extraction.ImportancePrimary,
				Evidence: extraction.Evidence{
					Summary:   "行政院發言人表示目前沒有調漲電價的規畫。",
					StartWith: strptr("行政院發言人林明華今天表示"),
					EndWith:   strptr("相關政策仍會依能源價格與供電情勢滾動檢討。"),
					Quote:     nil,
				},
			},
			{
				Statement: "如果國際燃料價格持續上升，政府會重新評估電價方案。",
				Type:      extraction.StatementTypeFactual,
				Tags: []extraction.StatementTag{
					extraction.StatementTagPrediction,
				},
				Sentiment: extraction.SentimentNeutral,
				Source: extraction.Source{
					Name: strptr("林明華"),
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeDirect,
				},
				Importance: extraction.ImportanceSupporting,
				Evidence: extraction.Evidence{
					Summary:   "林明華表示，國際燃料價格若持續上升，政府會重新評估電價。",
					StartWith: nil,
					EndWith:   nil,
					Quote:     strptr("如果國際燃料價格持續上升，我們會重新評估電價方案。"),
				},
			},
			{
				Statement: "行政院應在一週內公布相關文件。",
				Type:      extraction.StatementTypeOpinion,
				Tags: []extraction.StatementTag{
					extraction.StatementTagProposal,
				},
				Sentiment: extraction.SentimentNeutral,
				Source: extraction.Source{
					Name: strptr("陳志強"),
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeIndirect,
				},
				Importance: extraction.ImportanceSupporting,
				Evidence: extraction.Evidence{
					Summary:   "陳志強要求行政院在一週內公布電價評估相關文件。",
					StartWith: strptr("在野黨立委陳志強批評"),
					EndWith:   strptr("並要求行政院在一週內公布相關文件。"),
					Quote:     nil,
				},
			},
		},
		Entities: []extraction.Entity{
			{Name: "行政院", Type: extraction.EntityTypePublicSector},
			{Name: "林明華", Type: extraction.EntityTypePerson},
			{Name: "陳志強", Type: extraction.EntityTypePerson},
			{Name: "電價方案", Type: extraction.EntityTypePolicy},
		},
	}
}

// ---------------------------------------------------------
// Fake LLM Framework
// ---------------------------------------------------------

type FakeResult struct {
	Response *llm.GenerateResponse
	Err      error
}

type FakeLLM struct {
	Results []FakeResult
	Calls   []*llm.GenerateRequest
	next    int
}

func (f *FakeLLM) Generate(ctx context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	f.Calls = append(f.Calls, req)

	if f.next >= len(f.Results) {
		return nil, errors.New("unexpected extra LLM call")
	}

	result := f.Results[f.next]
	f.next++
	return result.Response, result.Err
}

// Helpers to build FakeResults

func JSONResult(out extraction.Output) FakeResult {
	b, _ := json.Marshal(out)
	return RawResult(string(b))
}

func RawResult(text string) FakeResult {
	return FakeResult{
		Response: &llm.GenerateResponse{
			Text:       text,
			JsonSchema: extraction.Schema(),
		},
		Err: nil,
	}
}

func ErrorResult(err error) FakeResult {
	return FakeResult{
		Response: nil,
		Err:      err,
	}
}

// ---------------------------------------------------------
// Scenarios Data setup
// ---------------------------------------------------------

func invalidSourceOutput() extraction.Output {
	out := validOutput()
	out.Statements[0].Source = extraction.Source{
		Name: strptr("林明華"),
		Type: extraction.SourceTypeNone,
		Mode: extraction.SourceModeIndirect,
	}
	return out
}

func missingStartLocatorOutput() extraction.Output {
	out := validOutput()
	out.Statements[0].Evidence.StartWith = strptr("不存在於原文中的行政院發言人")
	return out
}

func ambiguousInput() extraction.Input {
	return extraction.Input{
		Title: "政府回應政策爭議",
		Content: `行政院表示，政策仍在檢討。
其他官員稍後提出補充說明。
行政院表示，政策仍在檢討。`,
	}
}

func ambiguousOutput() extraction.Output {
	return extraction.Output{
		Summary: "行政院表示政策仍在檢討。",
		Statements: []extraction.Statement{
			{
				Statement:  "政策仍在檢討。",
				Type:       extraction.StatementTypeFactual,
				Tags:       []extraction.StatementTag{},
				Sentiment:  extraction.SentimentNeutral,
				Importance: extraction.ImportancePrimary,
				Source: extraction.Source{
					Name: strptr("行政院"),
					Type: extraction.SourceTypeOrganization,
					Mode: extraction.SourceModeIndirect,
				},
				Evidence: extraction.Evidence{
					Summary:   "行政院表示政策仍在檢討。",
					StartWith: strptr("行政院表示"),
					EndWith:   strptr("政策仍在檢討。"),
					Quote:     nil,
				},
			},
		},
		Entities: []extraction.Entity{
			{Name: "行政院", Type: extraction.EntityTypePublicSector},
		},
	}
}

func repairedAmbiguousOutput() extraction.Output {
	out := ambiguousOutput()
	out.Statements[0].Evidence = extraction.Evidence{
		Summary:   "後段再次記載行政院表示政策仍在檢討。",
		StartWith: strptr("其他官員稍後提出補充說明。\n行政院表示"),
		EndWith:   strptr("政策仍在檢討。"),
		Quote:     nil,
	}
	return out
}

var fakeTransientError = &llm.TransientError{
	Cause: errors.New("fake provider unavailable"),
}

var errFakeFatal = errors.New("invalid model configuration")

func setupRunner(fake *FakeLLM, maxAttempt int) *llmapi.Runner[extraction.Input, any, extraction.Output] {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := otel.Tracer("test")

	deps := extraction.Dependencies{
		Generator:    fake,
		PromptLoader: &fakeLoader{},
	}
	params := extraction.V1Parameters{
		Model:    "fake-model",
		PromptID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
	}

	stage, err := extraction.NewV1(context.Background(), deps, params)
	if err != nil {
		panic(err)
	}

	runner, err := llmapi.NewRunner[extraction.Input, any, extraction.Output](tracer, logger, maxAttempt, stage)
	if err != nil {
		panic(err)
	}
	return runner
}

// ---------------------------------------------------------
// Test Cases
// ---------------------------------------------------------

// 1. Valid first attempt
func TestStage_ValidFirstAttempt(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(validOutput()),
		},
	}
	runner := setupRunner(fake, 3)

	out, err := runner.Do(context.Background(), validInput())
	require.NoError(t, err)
	require.Equal(t, 1, fake.next)
	require.Equal(t, validOutput(), out)

	req := fake.Calls[0]
	require.Equal(t, "article_extraction_result", req.JSONSchema.Name)
	require.Contains(t, req.Prompt, validArticleContent)
}

// 2. Semantic validation fails, second attempt repairs it
func TestStage_SemanticValidationFails_Repaired(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(invalidSourceOutput()),
			JSONResult(validOutput()),
		},
	}
	runner := setupRunner(fake, 3)

	out, err := runner.Do(context.Background(), validInput())
	require.NoError(t, err)
	require.Equal(t, 2, fake.next)
	require.Equal(t, validOutput(), out)

	secondReq := fake.Calls[1]
	require.Contains(t, secondReq.Prompt, "invalid_source_combination")
	require.Contains(t, secondReq.Prompt, validArticleContent)
}

// 3. Grounding failure, second attempt repairs it
func TestStage_GroundingFailure_Repaired(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(missingStartLocatorOutput()),
			JSONResult(validOutput()),
		},
	}
	runner := setupRunner(fake, 3)

	out, err := runner.Do(context.Background(), validInput())
	require.NoError(t, err)
	require.Equal(t, 2, fake.next)
	require.Equal(t, validOutput(), out)

	secondReq := fake.Calls[1]
	require.Contains(t, secondReq.Prompt, "start_locator_not_found")
	require.Contains(t, secondReq.Prompt, "statements[0].evidence.start_with")
	require.Contains(t, secondReq.Prompt, validArticleContent)
}

// 4. Ambiguous grounding, second attempt repairs it
func TestStage_AmbiguousGrounding_Repaired(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(ambiguousOutput()),
			JSONResult(repairedAmbiguousOutput()),
		},
	}
	runner := setupRunner(fake, 3)

	out, err := runner.Do(context.Background(), ambiguousInput())
	require.NoError(t, err)
	require.Equal(t, 2, fake.next)
	require.Equal(t, repairedAmbiguousOutput(), out)

	secondReq := fake.Calls[1]
	require.Contains(t, secondReq.Prompt, "ambiguous_evidence_locator")
}

// 5. RetryError immediately aborts
func TestStage_RetryErrorAborts(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			ErrorResult(fakeTransientError),
		},
	}
	runner := setupRunner(fake, 3)

	_, err := runner.Do(context.Background(), validInput())
	require.Error(t, err)
	require.Equal(t, 1, fake.next)

	var retryErr *llmapi.RetryError
	require.True(t, errors.As(err, &retryErr))
}

// 6. Fatal provider error immediately aborts
func TestStage_FatalProviderErrorAborts(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			ErrorResult(errFakeFatal),
		},
	}
	runner := setupRunner(fake, 3)

	_, err := runner.Do(context.Background(), validInput())
	require.Error(t, err)
	require.Equal(t, 1, fake.next)

	var retryErr *llmapi.RetryError
	require.False(t, errors.As(err, &retryErr))

	var reAttemptErr *llmapi.ReAttemptError
	require.False(t, errors.As(err, &reAttemptErr))
}

// 7. Empty article fails before LLM
func TestStage_EmptyArticleFailsBeforeLLM(t *testing.T) {
	fake := &FakeLLM{} // no calls expected, panics if called
	runner := setupRunner(fake, 3)

	_, err := runner.Do(context.Background(), extraction.Input{Title: "T", Content: ""})
	require.ErrorIs(t, err, extraction.ErrEmptyArticleContent)
	require.Equal(t, 0, fake.next)

	_, err = runner.Do(context.Background(), extraction.Input{Title: "T", Content: "  \n\t "})
	require.ErrorIs(t, err, extraction.ErrEmptyArticleContent)
	require.Equal(t, 0, fake.next)
}

// 8. Decode failure is fatal
func TestStage_DecodeFailureIsFatal(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			RawResult(`{"summary":"這是一個不完整的 JSON"`),
		},
	}
	runner := setupRunner(fake, 3)

	_, err := runner.Do(context.Background(), validInput())
	require.Error(t, err)
	require.Equal(t, 1, fake.next)

	var reAttemptErr *llmapi.ReAttemptError
	require.False(t, errors.As(err, &reAttemptErr), "Decode error should not be ReAttemptError")
}

// 9. Semantic-invalid Output is never committed
func TestStage_SemanticInvalidNeverCommitted(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(invalidSourceOutput()),
		},
	}
	runner := setupRunner(fake, 1) // Only 1 attempt allowed

	out, err := runner.Do(context.Background(), validInput())
	require.Error(t, err)
	require.Contains(t, err.Error(), "max semantic attempts exhausted")
	require.Equal(t, 1, fake.next)

	// ensure zero value returned
	require.Empty(t, out.Summary)
}

// 10. Grounding-invalid Output is never committed
func TestStage_GroundingInvalidNeverCommitted(t *testing.T) {
	fake := &FakeLLM{
		Results: []FakeResult{
			JSONResult(missingStartLocatorOutput()),
		},
	}
	runner := setupRunner(fake, 1)

	out, err := runner.Do(context.Background(), validInput())
	require.Error(t, err)
	require.Contains(t, err.Error(), "max semantic attempts exhausted")
	require.Equal(t, 1, fake.next)

	require.Empty(t, out.Summary)
}
