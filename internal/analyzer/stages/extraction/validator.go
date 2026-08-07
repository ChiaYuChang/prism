package extraction

import (
	"fmt"
	"strings"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
)

// Validate checks the semantic consistency of the Output contract.
// It returns all detected validation errors. It does not validate against the source article.
func Validate(out *Output) []llmapi.ValidationError {
	if out == nil {
		return nil
	}

	var errs []llmapi.ValidationError

	for i, stmt := range out.Statements {
		pathPrefix := fmt.Sprintf("statements[%d]", i)
		
		// 1. Validate Source
		sourceErrs := validateSource(stmt.Source, pathPrefix+".source")
		errs = append(errs, sourceErrs...)

		// 2. Validate Evidence
		evidenceErrs := validateEvidence(stmt.Evidence, pathPrefix+".evidence")
		errs = append(errs, evidenceErrs...)
	}

	return errs
}

func validateSource(src Source, path string) []llmapi.ValidationError {
	var errs []llmapi.ValidationError

	isBlank := func(s *string) bool {
		return s == nil || strings.TrimSpace(*s) == ""
	}

	switch src.Mode {
	case SourceModeUnattributed:
		if src.Name != nil || src.Type != SourceTypeNone {
			errs = append(errs, llmapi.ValidationError{
				Path:    path,
				Code:    "invalid_source_combination",
				Message: "unattributed source must have type=none and name=null",
			})
		}
	case SourceModeDirect, SourceModeIndirect:
		if isBlank(src.Name) || src.Type == SourceTypeNone {
			errs = append(errs, llmapi.ValidationError{
				Path:    path,
				Code:    "invalid_source_combination",
				Message: "attributed source must have a non-blank name and type != none",
			})
		}
	}
	
	return errs
}

func validateEvidence(ev Evidence, path string) []llmapi.ValidationError {
	var errs []llmapi.ValidationError

	hasStart := ev.StartWith != nil
	hasEnd := ev.EndWith != nil
	hasQuote := ev.Quote != nil

	isBlank := func(s *string) bool {
		return s != nil && strings.TrimSpace(*s) == ""
	}

	// 1. Check for empty string values
	if isBlank(ev.StartWith) {
		errs = append(errs, llmapi.ValidationError{
			Path:    path + ".start_with",
			Code:    "empty_evidence_locator",
			Message: "evidence locator cannot be empty or whitespace only",
		})
	}
	if isBlank(ev.EndWith) {
		errs = append(errs, llmapi.ValidationError{
			Path:    path + ".end_with",
			Code:    "empty_evidence_locator",
			Message: "evidence locator cannot be empty or whitespace only",
		})
	}
	if isBlank(ev.Quote) {
		errs = append(errs, llmapi.ValidationError{
			Path:    path + ".quote",
			Code:    "empty_evidence_locator",
			Message: "evidence locator cannot be empty or whitespace only",
		})
	}

	// 2. Check shape combinations
	switch {
	case hasQuote && (hasStart || hasEnd):
		errs = append(errs, llmapi.ValidationError{
			Path:    path,
			Code:    "conflicting_evidence_locator",
			Message: "cannot provide both anchors (start_with/end_with) and quote",
		})
	case hasStart != hasEnd:
		errs = append(errs, llmapi.ValidationError{
			Path:    path,
			Code:    "partial_evidence_locator",
			Message: "must provide both start_with and end_with for anchor mode",
		})
	case !hasStart && !hasEnd && !hasQuote:
		errs = append(errs, llmapi.ValidationError{
			Path:    path,
			Code:    "missing_evidence_locator",
			Message: "must provide either (start_with and end_with) or quote",
		})
	}

	return errs
}
