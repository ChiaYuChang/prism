package extraction_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer/stages/extraction"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T {
	return &v
}

func TestValidator_ValidAttributedSourcePasses(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("王明"),
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeDirect,
				},
				Evidence: extraction.Evidence{
					StartWith: ptr("start"),
					EndWith:   ptr("end"),
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Empty(t, errs)
}

func TestValidator_ValidUnattributedSourcePasses(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: nil,
					Type: extraction.SourceTypeNone,
					Mode: extraction.SourceModeUnattributed,
				},
				Evidence: extraction.Evidence{
					StartWith: ptr("start"),
					EndWith:   ptr("end"),
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Empty(t, errs)
}

func TestValidator_UnattributedSourceWithConcreteTypeRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: nil,
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeUnattributed,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 2) // Also gets missing_evidence_locator
	require.Equal(t, "statements[0].source", errs[0].Path)
	require.Equal(t, "invalid_source_combination", errs[0].Code)
}

func TestValidator_UnattributedSourceWithNameRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("王明"),
					Type: extraction.SourceTypeNone,
					Mode: extraction.SourceModeUnattributed,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Equal(t, "statements[0].source", errs[0].Path)
	require.Equal(t, "invalid_source_combination", errs[0].Code)
}

func TestValidator_AttributedSourceWithTypeNoneRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("行政院"),
					Type: extraction.SourceTypeNone,
					Mode: extraction.SourceModeIndirect,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Equal(t, "statements[0].source", errs[0].Path)
	require.Equal(t, "invalid_source_combination", errs[0].Code)
}

func TestValidator_AttributedSourceWithoutNameRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: nil,
					Type: extraction.SourceTypeOrganization,
					Mode: extraction.SourceModeIndirect,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Equal(t, "statements[0].source", errs[0].Path)
	require.Equal(t, "invalid_source_combination", errs[0].Code)
}

func TestValidator_AttributedSourceWithBlankNameRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("   "),
					Type: extraction.SourceTypeOrganization,
					Mode: extraction.SourceModeIndirect,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Equal(t, "statements[0].source", errs[0].Path)
	require.Equal(t, "invalid_source_combination", errs[0].Code)
}

func TestValidator_AnchorEvidenceFormPasses(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("Valid"),
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeDirect,
				},
				Evidence: extraction.Evidence{
					StartWith: ptr("經濟部表示"),
					EndWith:   ptr("目前沒有調漲規畫"),
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Empty(t, errs)
}

func TestValidator_QuoteFallbackFormPasses(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("Valid"),
					Type: extraction.SourceTypePerson,
					Mode: extraction.SourceModeDirect,
				},
				Evidence: extraction.Evidence{
					StartWith: nil,
					EndWith:   nil,
					Quote:     ptr("經濟部表示，目前沒有調漲規畫。"),
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Empty(t, errs)
}

func TestValidator_EvidenceWithNoLocatorRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: nil,
					EndWith:   nil,
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 1)
	require.Equal(t, "statements[0].evidence", errs[0].Path)
	require.Equal(t, "missing_evidence_locator", errs[0].Code)
}

func TestValidator_StartOnlyEvidenceRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: ptr("start"),
					EndWith:   nil,
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 1)
	require.Equal(t, "statements[0].evidence", errs[0].Path)
	require.Equal(t, "partial_evidence_locator", errs[0].Code)
}

func TestValidator_EndOnlyEvidenceRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: nil,
					EndWith:   ptr("end"),
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 1)
	require.Equal(t, "statements[0].evidence", errs[0].Path)
	require.Equal(t, "partial_evidence_locator", errs[0].Code)
}

func TestValidator_AnchorsPlusQuoteRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: ptr("start"),
					EndWith:   ptr("end"),
					Quote:     ptr("quote"),
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 1)
	require.Equal(t, "statements[0].evidence", errs[0].Path)
	require.Equal(t, "conflicting_evidence_locator", errs[0].Code)
}

func TestValidator_BlankLocatorRejected(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: ptr(" "),
					EndWith:   ptr("有效文字"),
					Quote:     nil,
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 1)
	require.Equal(t, "statements[0].evidence.start_with", errs[0].Path)
	require.Equal(t, "empty_evidence_locator", errs[0].Code)

	out.Statements[0].Evidence = extraction.Evidence{
		StartWith: nil,
		EndWith:   nil,
		Quote:     ptr("   \t\n"),
	}
	errs = extraction.Validate(out)
	require.Len(t, errs, 1)
	require.Equal(t, "statements[0].evidence.quote", errs[0].Path)
	require.Equal(t, "empty_evidence_locator", errs[0].Code)
}

func TestValidator_PrecedenceStructuralVersusContent(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: ptr("   "),
					EndWith:   nil,
					Quote:     ptr("real quote"),
				},
			},
		},
	}
	errs := extraction.Validate(out)
	require.Len(t, errs, 2)
	
	hasEmpty := false
	hasConflicting := false
	for _, e := range errs {
		if e.Code == "empty_evidence_locator" && e.Path == "statements[0].evidence.start_with" {
			hasEmpty = true
		}
		if e.Code == "conflicting_evidence_locator" && e.Path == "statements[0].evidence" {
			hasConflicting = true
		}
	}
	
	require.True(t, hasEmpty, "Should have empty_evidence_locator on start_with")
	require.True(t, hasConflicting, "Should have conflicting_evidence_locator")
}

func TestValidator_AccumulatesMultipleErrorsInDeterministicOrder(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("Name"),
					Type: extraction.SourceTypeNone,
					Mode: extraction.SourceModeUnattributed,
				},
				Evidence: extraction.Evidence{
					StartWith: ptr("s"),
					EndWith:   ptr("e"),
					Quote:     nil,
				},
			},
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: nil,
					EndWith:   nil,
					Quote:     nil,
				},
			},
			{
				Source: extraction.Source{Name: ptr("a"), Type: extraction.SourceTypePerson, Mode: extraction.SourceModeDirect},
				Evidence: extraction.Evidence{
					StartWith: ptr("s"),
					EndWith:   nil,
					Quote:     nil,
				},
			},
		},
	}
	
	errs := extraction.Validate(out)
	require.Len(t, errs, 3)
	
	require.Equal(t, "statements[0].source", errs[0].Path)
	require.Equal(t, "invalid_source_combination", errs[0].Code)
	
	require.Equal(t, "statements[1].evidence", errs[1].Path)
	require.Equal(t, "missing_evidence_locator", errs[1].Code)
	
	require.Equal(t, "statements[2].evidence", errs[2].Path)
	require.Equal(t, "partial_evidence_locator", errs[2].Code)
}

func TestValidator_ValidationDoesNotMutateOutput(t *testing.T) {
	out := &extraction.Output{
		Statements: []extraction.Statement{
			{
				Source: extraction.Source{
					Name: ptr("   "),
					Type: extraction.SourceTypeOrganization,
					Mode: extraction.SourceModeIndirect,
				},
				Evidence: extraction.Evidence{
					StartWith: ptr(" start "),
					EndWith:   nil,
					Quote:     ptr(" quote "),
				},
			},
		},
	}
	
	_ = extraction.Validate(out)
	
	// Ensure fields haven't been trimmed or changed
	require.Equal(t, "   ", *out.Statements[0].Source.Name)
	require.Equal(t, " start ", *out.Statements[0].Evidence.StartWith)
	require.Nil(t, out.Statements[0].Evidence.EndWith)
	require.Equal(t, " quote ", *out.Statements[0].Evidence.Quote)
}
