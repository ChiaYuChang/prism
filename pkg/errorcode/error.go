package errorcode

import (
	"fmt"
	"strings"
)

// Error is a composite error structure supporting multiple errors and warnings.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`

	// Errors acts as a container for multiple underlying errors (Multi-error).
	Errors []error `json:"errors,omitempty"`

	// Warnings records non-fatal alerts that do not halt execution.
	Warnings []Warning `json:"warnings,omitempty"`
}

// Error implements the standard Go error interface, concatenating all internal errors and warnings.
func (e *Error) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%d] %s", e.Code, e.Message))

	if len(e.Errors) > 0 {
		sb.WriteString(" | Sub-errors: ")
		errStrs := make([]string, len(e.Errors))
		for i, err := range e.Errors {
			errStrs[i] = err.Error()
		}
		sb.WriteString(strings.Join(errStrs, "; "))
	}

	if len(e.Warnings) > 0 {
		sb.WriteString(" | Warnings: ")
		warnStrs := make([]string, len(e.Warnings))
		for i, w := range e.Warnings {
			warnStrs[i] = w.String()
		}
		sb.WriteString(strings.Join(warnStrs, "; "))
	}

	return sb.String()
}

// New creates a new base PrismError with the specified code and message.
func New(code Code, msg string) *Error {
	return &Error{
		Code:    code,
		Message: msg,
	}
}

// AppendError adds an error to the container and returns the PrismError for chaining.
func (e *Error) AppendError(err error) *Error {
	if err != nil {
		e.Errors = append(e.Errors, err)
	}
	return e
}

// AddWarning adds a structured warning with a specific level to the error object.
func (e *Error) AddWarning(level WarningLevel, msg string) *Error {
	if msg != "" {
		e.Warnings = append(e.Warnings, Warning{
			Level:   level,
			Message: msg,
		})
	}
	return e
}

// HasErrors checks if any underlying errors are present in the container.
func (e *Error) HasErrors() bool {
	return len(e.Errors) > 0
}
