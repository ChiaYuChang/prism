package extraction

import (
	"fmt"
	"strings"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
)

// Span represents a byte-offset slice of the original article.
// Start is inclusive, End is exclusive.
type Span struct {
	Start int
	End   int
	Text  string
}

// GroundEvidence resolves a semantically valid Evidence locator against the original article text.
// It returns a Span if exactly one verbatim match is found.
// If grounding fails, it returns a ValidationError explaining why.
func GroundEvidence(article string, evidence Evidence) (Span, *llmapi.ValidationError) {
	hasStart := evidence.StartWith != nil
	hasEnd := evidence.EndWith != nil
	hasQuote := evidence.Quote != nil

	if hasStart && hasEnd {
		startWith := *evidence.StartWith
		endWith := *evidence.EndWith

		starts := findAll(article, startWith)
		ends := findAll(article, endWith)

		if len(starts) == 0 {
			return Span{}, &llmapi.ValidationError{
				Code:    "start_locator_not_found",
				Message: "No exact occurrence of start_with exists in the article.",
			}
		}

		if len(ends) == 0 {
			return Span{}, &llmapi.ValidationError{
				Code:    "end_locator_not_found",
				Message: "No exact occurrence of end_with exists in the article.",
			}
		}

		var candidates []Span
		for _, s := range starts {
			for _, e := range ends {
				if e >= s {
					candidates = append(candidates, Span{
						Start: s,
						End:   e + len(endWith),
						Text:  article[s : e+len(endWith)],
					})
				}
			}
		}

		if len(candidates) == 0 {
			return Span{}, &llmapi.ValidationError{
				Code:    "invalid_locator_order",
				Message: "start_with and end_with found, but there are zero valid ordered pairs (end >= start).",
			}
		}

		if len(candidates) > 1 {
			return Span{}, &llmapi.ValidationError{
				Code:    "ambiguous_evidence_locator",
				Message: "The anchors resolve to more than one valid candidate span.",
			}
		}

		return candidates[0], nil
	}

	if hasQuote {
		quote := *evidence.Quote
		matches := findAll(article, quote)

		if len(matches) == 0 {
			return Span{}, &llmapi.ValidationError{
				Code:    "quote_not_found",
				Message: "No exact occurrence of quote exists in the article.",
			}
		}

		if len(matches) > 1 {
			return Span{}, &llmapi.ValidationError{
				Code:    "ambiguous_evidence_locator",
				Message: "The quote occurs more than once in the article.",
			}
		}

		s := matches[0]
		return Span{
			Start: s,
			End:   s + len(quote),
			Text:  quote,
		}, nil
	}

	// This should not be reachable if Step 5 semantic validation has already run,
	// but return a safe fallback if it does.
	return Span{}, nil
}

// findAll returns all byte-offset indices of pattern in text, including overlapping matches.
func findAll(text, pattern string) []int {
	var positions []int
	if pattern == "" {
		return positions
	}

	offset := 0
	for {
		idx := strings.Index(text[offset:], pattern)
		if idx == -1 {
			break
		}
		absIdx := offset + idx
		positions = append(positions, absIdx)
		offset = absIdx + 1 // Advance by 1 byte to allow overlapping occurrences
	}
	return positions
}

// ValidateGrounding validates the grounding of all statements in the Output.
// It accumulates grounding errors in statement order and assigns deterministic JSON paths.
func ValidateGrounding(article string, out *Output) []llmapi.ValidationError {
	var errs []llmapi.ValidationError
	if out == nil {
		return nil
	}

	for i, stmt := range out.Statements {
		_, err := GroundEvidence(article, stmt.Evidence)
		if err != nil {
			pathPrefix := fmt.Sprintf("statements[%d].evidence", i)
			switch err.Code {
			case "start_locator_not_found":
				err.Path = pathPrefix + ".start_with"
			case "end_locator_not_found":
				err.Path = pathPrefix + ".end_with"
			case "quote_not_found":
				err.Path = pathPrefix + ".quote"
			case "invalid_locator_order", "ambiguous_evidence_locator":
				err.Path = pathPrefix
			default:
				err.Path = pathPrefix
			}
			errs = append(errs, *err)
		}
	}
	return errs
}
