package collector

import (
	"fmt"
)

// stageName returns a human-readable identifier for a pipeline component.
// It prefers fmt.Stringer when the value implements it (stable, hand-picked
// label) and falls back to the Go type name (which still survives across
// refactors better than nothing).
func stageName(v any) string {
	if s, ok := v.(fmt.Stringer); ok {
		if name := s.String(); name != "" {
			return name
		}
	}
	return fmt.Sprintf("%T", v)
}

// PipelineStage identifies a stage in the F-M-T-P collector pipeline.
type PipelineStage string

const (
	PipelineStageFetch     PipelineStage = "fetch"
	PipelineStageMinify    PipelineStage = "minify"
	PipelineStageTransform PipelineStage = "transform"
	PipelineStageParse     PipelineStage = "parse"
)

// ParsePipelineStage converts a string to a PipelineStage, returning an error if
// the string does not match any known stage.
func ParsePipelineStage(s string) (PipelineStage, error) {
	stage := PipelineStage(s)
	if stage.IsValid() {
		return stage, nil
	}
	return "", fmt.Errorf("invalid pipeline stage: %q", s)
}

func (s PipelineStage) IsValid() bool {
	switch s {
	case PipelineStageFetch, PipelineStageMinify, PipelineStageTransform, PipelineStageParse:
		return true
	default:
		return false
	}
}

func (s PipelineStage) String() string {
	return string(s)
}

// StageError wraps a pipeline stage failure with the intermediate value
// that was flowing between stages when the failure occurred.
// Intermediate is the output of the previous stage = input to the failing stage.
// Empty for PipelineStageFetch (no upstream output exists).
//
// SubStage names the specific implementation that failed. For Transform it
// also carries the chain index (e.g. "[1] HTMLMinifier") so a failure in a
// chain of identically-typed transformers is still locatable. Empty when no
// stable identifier is available.
type StageError struct {
	Stage        PipelineStage
	SubStage     string
	Err          error
	Intermediate string
}

func (e *StageError) Error() string {
	if e.SubStage != "" {
		return string(e.Stage) + "[" + e.SubStage + "]: " + e.Err.Error()
	}
	return string(e.Stage) + ": " + e.Err.Error()
}

func (e *StageError) Unwrap() error { return e.Err }

// Is reports whether target is a *StageError for the same pipeline stage,
// letting callers write `errors.Is(err, &StageError{Stage: PipelineStageMinify})`
// as a stage query. Underlying-cause comparison should use errors.Is against
// the Err field directly (the Unwrap method already supports that).
func (e *StageError) Is(target error) bool {
	t, ok := target.(*StageError)
	if !ok {
		return false
	}
	return t.Stage == e.Stage
}
