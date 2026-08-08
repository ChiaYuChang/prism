package extraction_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/stretchr/testify/require"
)

func TestGroundEvidence_UniqueQuoteResolvesSuccessfully(t *testing.T) {
	article := "這是一段測試文章。經濟部表示，目前沒有調漲電價的規畫。這是結尾。"
	quote := "經濟部表示，目前沒有調漲電價的規畫。"

	ev := extraction.Evidence{Quote: &quote}
	span, err := extraction.GroundEvidence(article, ev)
	require.Nil(t, err)

	require.Equal(t, quote, span.Text)
	require.Equal(t, quote, article[span.Start:span.End])
}

func TestGroundEvidence_QuoteDoesNotExist(t *testing.T) {
	article := "這是一段測試文章。經濟部表示，目前沒有調漲電價的規畫。這是結尾。"
	quote := "經濟部表示，目前沒有調降電價的規畫。"

	ev := extraction.Evidence{Quote: &quote}
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "quote_not_found", err.Code)
}

func TestGroundEvidence_RepeatedQuoteRejected(t *testing.T) {
	article := "這是一段測試文章。重複的話。中間隔一些字。重複的話。"
	quote := "重複的話。"

	ev := extraction.Evidence{Quote: &quote}
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "ambiguous_evidence_locator", err.Code)
}

func TestGroundEvidence_SimilarButNonVerbatimTextRejected(t *testing.T) {
	article := "目前沒有調漲電價的規畫"
	quote := "目前沒有調漲電價的規劃" // 規畫 vs 規劃

	ev := extraction.Evidence{Quote: &quote}
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "quote_not_found", err.Code)
}

func TestGroundEvidence_WhitespaceDifferencesNotNormalized(t *testing.T) {
	article := "行政院表示，目前沒有規畫。"
	quote := "行政院表示， 目前沒有規畫。" // Extra space after comma

	ev := extraction.Evidence{Quote: &quote}
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "quote_not_found", err.Code)
}

func TestGroundEvidence_UniqueAnchorResolvesSuccessfully(t *testing.T) {
	article := "開頭。我說：今天天氣很好，對吧。結尾。"
	start := "我說："
	end := "對吧。"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	span, err := extraction.GroundEvidence(article, ev)
	require.Nil(t, err)
	require.Equal(t, "我說：今天天氣很好，對吧。", span.Text)
	require.Equal(t, span.Text, article[span.Start:span.End])
}

func TestGroundEvidence_StartLocatorNotFound(t *testing.T) {
	article := "開頭。我說：今天天氣很好，對吧。結尾。"
	start := "他說："
	end := "對吧。"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "start_locator_not_found", err.Code)
}

func TestGroundEvidence_EndLocatorNotFound(t *testing.T) {
	article := "開頭。我說：今天天氣很好，對吧。結尾。"
	start := "我說："
	end := "對吧？"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "end_locator_not_found", err.Code)
}

func TestGroundEvidence_InvalidLocatorOrder(t *testing.T) {
	article := "先結束。再開始。"
	start := "開始"
	end := "結束"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "invalid_locator_order", err.Code)
}

func TestGroundEvidence_AmbiguousAnchorEvidence(t *testing.T) {
	article := "開頭。我說：天氣好，對吧。另外我說：心情好，對吧。結尾。"
	start := "我說："
	end := "對吧。"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	// starts = 2, ends = 2. Valid combinations (end >= start) will be > 1.
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "ambiguous_evidence_locator", err.Code)
}

func TestGroundEvidence_AmbiguousAnchorEvidence_TwoStartsOneEnd(t *testing.T) {
	article := "開始1...開始2...結束"
	start := "開始"
	end := "結束"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	// The single '結束' appears after BOTH '開始' occurrences.
	// So both (開始1, 結束) and (開始2, 結束) are valid spans.
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "ambiguous_evidence_locator", err.Code)
}

func TestGroundEvidence_OverlappingOccurrencesAreDiscovered(t *testing.T) {
	article := "ABABA" // Occurrences of "ABA": at index 0, and at index 2.
	quote := "ABA"

	ev := extraction.Evidence{Quote: &quote}

	// Because we discover ABA at both 0 and 2, the quote count is 2 -> ambiguous.
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "ambiguous_evidence_locator", err.Code)
}

func TestGroundEvidence_EndIsImmediatelyAfterStart(t *testing.T) {
	// If e >= s is used, what if end_with starts exactly at s or partially overlaps?
	// Realistically, if start_with and end_with overlap entirely, the rule is e >= s.
	// We'll test an exact boundary case where end starts exactly at the end of start, which is normal.
	article := "ABCDEF"
	start := "ABC"
	end := "DEF"

	ev := extraction.Evidence{
		StartWith: &start,
		EndWith:   &end,
	}

	span, err := extraction.GroundEvidence(article, ev)
	require.Nil(t, err)
	require.Equal(t, "ABCDEF", span.Text)
}

func TestGroundEvidence_RepeatedStartUniqueSpan(t *testing.T) {
	// Repeated start anchor but unique span
	// To make it unique: the second start must occur AFTER the end.

	start := "START"
	end := "END"
	article2 := "START then END and then START again"
	ev := extraction.Evidence{StartWith: &start, EndWith: &end}
	span, err := extraction.GroundEvidence(article2, ev)
	require.Nil(t, err)
	require.Equal(t, "START then END", span.Text)
}

func TestGroundEvidence_RepeatedEndCreatesMultipleValidSpans(t *testing.T) {
	article := "START ... END ... END"
	start := "START"
	end := "END"
	ev := extraction.Evidence{StartWith: &start, EndWith: &end}
	_, err := extraction.GroundEvidence(article, ev)
	require.NotNil(t, err)
	require.Equal(t, "ambiguous_evidence_locator", err.Code)
}

func TestGroundEvidence_ChineseUTF8Offsets(t *testing.T) {
	article := "行政院今天宣布了一項政策，這項政策將會影響深遠。"
	start := "行政院今天宣布"
	end := "影響深遠。"
	ev := extraction.Evidence{StartWith: &start, EndWith: &end}
	span, err := extraction.GroundEvidence(article, ev)
	require.Nil(t, err)
	require.Equal(t, article, span.Text) // The span covers the whole article
	require.Equal(t, article[span.Start:span.End], span.Text)
}

func TestGroundEvidence_DoesNotMutateEvidence(t *testing.T) {
	article := "行政院今天宣布"
	start := "行政院"
	end := "宣布"
	ev := extraction.Evidence{StartWith: &start, EndWith: &end}

	// Copy to compare later
	evCopy := extraction.Evidence{
		StartWith: ptr(*ev.StartWith),
		EndWith:   ptr(*ev.EndWith),
		Quote:     nil,
	}

	_, _ = extraction.GroundEvidence(article, ev)

	require.Equal(t, evCopy.StartWith, ev.StartWith)
	require.Equal(t, evCopy.EndWith, ev.EndWith)
	require.Equal(t, evCopy.Quote, ev.Quote)
}

func TestValidateGrounding_AccumulatesErrors(t *testing.T) {
	article := "行政院宣布，今天天氣很好。"
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Evidence: extraction.Evidence{
					StartWith: ptr("行政院"),
					EndWith:   ptr("今天天氣很好。"),
				}, // Valid
			},
			{
				Evidence: extraction.Evidence{
					StartWith: ptr("沒有的開頭"),
					EndWith:   ptr("今天天氣很好。"),
				}, // start_locator_not_found -> start_with
			},
			{
				Evidence: extraction.Evidence{
					StartWith: nil,
					EndWith:   nil,
					Quote:     ptr("沒有的引言"),
				}, // quote_not_found -> quote
			},
			{
				Evidence: extraction.Evidence{
					StartWith: ptr("今天天氣很好。"),
					EndWith:   ptr("行政院宣布"),
				}, // invalid_locator_order -> evidence
			},
		},
	}

	errs := extraction.ValidateGrounding(article, out)
	require.Len(t, errs, 3)

	require.Equal(t, "statements[1].evidence.start_with", errs[0].Path)
	require.Equal(t, "start_locator_not_found", errs[0].Code)

	require.Equal(t, "statements[2].evidence.quote", errs[1].Path)
	require.Equal(t, "quote_not_found", errs[1].Code)

	require.Equal(t, "statements[3].evidence", errs[2].Path)
	require.Equal(t, "invalid_locator_order", errs[2].Code)
}
