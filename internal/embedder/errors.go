package embedder

import "fmt"

// ErrorType classifies embedding failures so callers can react appropriately.
type ErrorType string

const (
	// ErrBusy means too many concurrent requests — caller should retry with backoff.
	ErrBusy ErrorType = "BUSY"
	// ErrUnavailable means Ollama is unreachable — caller should retry after delay.
	ErrUnavailable ErrorType = "UNAVAILABLE"
	// ErrBadQuery means the query can't be embedded (empty, too short, etc.) — don't retry.
	ErrBadQuery ErrorType = "BAD_QUERY"
)

// EmbedError is a typed error for embedding failures.
type EmbedError struct {
	Type    ErrorType
	Message string
	Cause   error
}

func (e *EmbedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

func (e *EmbedError) Unwrap() error {
	return e.Cause
}

// NewBusyError creates a BUSY error (semaphore full, context deadline).
func NewBusyError(msg string) *EmbedError {
	return &EmbedError{Type: ErrBusy, Message: msg}
}

// NewUnavailableError creates an UNAVAILABLE error (connection refused, timeout).
func NewUnavailableError(msg string, cause error) *EmbedError {
	return &EmbedError{Type: ErrUnavailable, Message: msg, Cause: cause}
}

// NewBadQueryError creates a BAD_QUERY error (empty text, unembeddable).
func NewBadQueryError(msg string) *EmbedError {
	return &EmbedError{Type: ErrBadQuery, Message: msg}
}

// IsBusy returns true if the error is a BUSY error.
func IsBusy(err error) bool {
	if e, ok := err.(*EmbedError); ok {
		return e.Type == ErrBusy
	}
	return false
}

// IsUnavailable returns true if the error is an UNAVAILABLE error.
func IsUnavailable(err error) bool {
	if e, ok := err.(*EmbedError); ok {
		return e.Type == ErrUnavailable
	}
	return false
}

// IsBadQuery returns true if the error is a BAD_QUERY error.
func IsBadQuery(err error) bool {
	if e, ok := err.(*EmbedError); ok {
		return e.Type == ErrBadQuery
	}
	return false
}
