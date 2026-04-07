package api

import (
	"fmt"
	"time"
)

// ApiError represents an error from the API layer.
// It mirrors the Rust ApiError enum as a struct with a kind discriminator.
type ApiError struct {
	Kind    ApiErrorKind
	Message string
	Status  int // HTTP status code, 0 if not HTTP

	// For Api errors with structured response
	ErrorType string
	Body      string
	Retryable bool

	// For RetriesExhausted
	Attempts  int
	LastError error

	// For BackoffOverflow
	Attempt   int
	BaseDelay time.Duration

	// Underlying error
	Cause error
}

type ApiErrorKind int

const (
	ErrMissingCredentials ApiErrorKind = iota
	ErrExpiredOAuthToken
	ErrAuth
	ErrInvalidApiKeyEnv
	ErrHttp
	ErrIo
	ErrJson
	ErrApi
	ErrRetriesExhausted
	ErrInvalidSseFrame
	ErrBackoffOverflow
)

func (e *ApiError) Error() string {
	switch e.Kind {
	case ErrMissingCredentials:
		return e.Message
	case ErrExpiredOAuthToken:
		return "saved OAuth token is expired and no refresh token is available"
	case ErrAuth:
		return fmt.Sprintf("auth error: %s", e.Message)
	case ErrInvalidApiKeyEnv:
		return fmt.Sprintf("failed to read credential environment variable: %s", e.Message)
	case ErrHttp:
		return fmt.Sprintf("http error: %s", e.Message)
	case ErrIo:
		return fmt.Sprintf("io error: %s", e.Message)
	case ErrJson:
		return fmt.Sprintf("json error: %s", e.Message)
	case ErrApi:
		if e.ErrorType != "" && e.Message != "" {
			return fmt.Sprintf("api returned %d (%s): %s", e.Status, e.ErrorType, e.Message)
		}
		return fmt.Sprintf("api returned %d: %s", e.Status, e.Body)
	case ErrRetriesExhausted:
		return fmt.Sprintf("api failed after %d attempts: %v", e.Attempts, e.LastError)
	case ErrInvalidSseFrame:
		return fmt.Sprintf("invalid sse frame: %s", e.Message)
	case ErrBackoffOverflow:
		return fmt.Sprintf("retry backoff overflowed on attempt %d with base delay %v", e.Attempt, e.BaseDelay)
	default:
		return e.Message
	}
}

func (e *ApiError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	if e.LastError != nil {
		return e.LastError
	}
	return nil
}

// IsRetryable returns whether this error can be retried.
func (e *ApiError) IsRetryable() bool {
	switch e.Kind {
	case ErrHttp:
		return e.Status == 0 || e.Status == 429 || e.Status >= 500
	case ErrApi:
		return e.Retryable
	case ErrRetriesExhausted:
		if apiErr, ok := e.LastError.(*ApiError); ok {
			return apiErr.IsRetryable()
		}
		return false
	default:
		return false
	}
}

// NewMissingCredentials creates an error for missing API credentials.
func NewMissingCredentials(provider string, envVars []string) *ApiError {
	return &ApiError{
		Kind: ErrMissingCredentials,
		Message: fmt.Sprintf("missing %s credentials; export %s before calling the %s API",
			provider, envVars[0], provider),
	}
}

// NewExpiredOAuthToken creates an error for expired OAuth tokens.
func NewExpiredOAuthToken() *ApiError {
	return &ApiError{Kind: ErrExpiredOAuthToken}
}

// NewAuthError creates an authentication error.
func NewAuthError(msg string) *ApiError {
	return &ApiError{Kind: ErrAuth, Message: msg}
}

// NewHttpError wraps an HTTP-level error.
func NewHttpError(err error) *ApiError {
	return &ApiError{Kind: ErrHttp, Message: err.Error(), Cause: err}
}

// NewIoError wraps an I/O error.
func NewIoError(err error) *ApiError {
	return &ApiError{Kind: ErrIo, Message: err.Error(), Cause: err}
}

// NewJsonError wraps a JSON error.
func NewJsonError(err error) *ApiError {
	return &ApiError{Kind: ErrJson, Message: err.Error(), Cause: err}
}

// NewApiStatusError creates an error from an HTTP status response.
func NewApiStatusError(statusCode int, body string, errorType, message string) *ApiError {
	retryable := statusCode == 429 || statusCode >= 500
	return &ApiError{
		Kind:      ErrApi,
		Status:    statusCode,
		ErrorType: errorType,
		Message:   message,
		Body:      body,
		Retryable: retryable,
	}
}

// NewRetriesExhausted creates an error after exhausting all retry attempts.
func NewRetriesExhausted(attempts int, lastErr error) *ApiError {
	return &ApiError{
		Kind:      ErrRetriesExhausted,
		Attempts:  attempts,
		LastError: lastErr,
	}
}

// NewInvalidSseFrame creates an error for invalid SSE data.
func NewInvalidSseFrame(msg string) *ApiError {
	return &ApiError{Kind: ErrInvalidSseFrame, Message: msg}
}

// NewBackoffOverflow creates an error when retry backoff overflows.
func NewBackoffOverflow(attempt int, baseDelay time.Duration) *ApiError {
	return &ApiError{
		Kind:      ErrBackoffOverflow,
		Attempt:   attempt,
		BaseDelay: baseDelay,
	}
}

// Backwards-compatible constructors

func MissingCredentials(provider string, envVars []string) *ApiError {
	return NewMissingCredentials(provider, envVars)
}

func HttpError(status int, body string) *ApiError {
	return &ApiError{
		Kind:   ErrApi,
		Status: status,
		Body:   body,
		Retryable: status == 429 || status >= 500,
	}
}

func JsonError(err error) *ApiError {
	return NewJsonError(err)
}

func StreamError(msg string) *ApiError {
	return NewInvalidSseFrame(msg)
}

func RetriesExhausted(attempts int, lastErr error) *ApiError {
	return NewRetriesExhausted(attempts, lastErr)
}

func isRetryable(err error) bool {
	if apiErr, ok := err.(*ApiError); ok {
		return apiErr.IsRetryable()
	}
	return false
}
