package llm

import "fmt"

// TransientError indicates a provider-level SDK or HTTP error that is temporary
// and safe to retry (e.g. rate limit, timeout, server unavailable).
type TransientError struct {
	Cause error
}

func (e *TransientError) Error() string {
	return fmt.Sprintf("transient LLM provider error: %v", e.Cause)
}

func (e *TransientError) Unwrap() error {
	return e.Cause
}
