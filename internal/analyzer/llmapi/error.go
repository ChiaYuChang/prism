package llmapi

import "encoding/json"

// RetryError indicates the execution should stop and the task should be retried later.
type RetryError struct {
	Cause error
}

func (e *RetryError) Error() string {
	if e.Cause == nil {
		return "retry error"
	}
	return "retry error: " + e.Cause.Error()
}

func (e *RetryError) Unwrap() error {
	return e.Cause
}

// ReAttemptError indicates the LLM request completed but the semantic result is invalid
// and should be re-attempted.
type ReAttemptError struct {
	Cause error
	Hint  *RepairHint
}

func (e *ReAttemptError) Error() string {
	if e.Cause == nil {
		return "reattempt error"
	}
	return "reattempt error: " + e.Cause.Error()
}

func (e *ReAttemptError) Unwrap() error {
	return e.Cause
}

// RepairHint provides context to the next semantic attempt to fix the previous failure.
type RepairHint struct {
	PreviousOutput json.RawMessage
	Errors         []ValidationError
}

// ValidationError describes a structured failure in the semantic output.
type ValidationError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
