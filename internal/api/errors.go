package api

import "fmt"

type ApiError struct {
	Code    string
	Message string
	Status  int // HTTP status code, 0 if not HTTP
}

func (e *ApiError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("api error: %s (status %d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("api error: %s: %s", e.Code, e.Message)
}

func MissingCredentials(provider string, envVars []string) *ApiError {
	return &ApiError{
		Code:    "missing_credentials",
		Message: fmt.Sprintf("missing %s credentials; export %s before calling the API", provider, envVars[0]),
	}
}

func HttpError(status int, body string) *ApiError {
	return &ApiError{
		Code:    "http_error",
		Message: body,
		Status:  status,
	}
}

func JsonError(err error) *ApiError {
	return &ApiError{
		Code:    "json_error",
		Message: err.Error(),
	}
}

func StreamError(msg string) *ApiError {
	return &ApiError{
		Code:    "stream_error",
		Message: msg,
	}
}

func RetriesExhausted(attempts int, lastErr error) *ApiError {
	return &ApiError{
		Code:    "retries_exhausted",
		Message: fmt.Sprintf("failed after %d attempts: %v", attempts, lastErr),
	}
}

func isRetryable(err error) bool {
	if apiErr, ok := err.(*ApiError); ok && apiErr.Status > 0 {
		return apiErr.Status == 429 || apiErr.Status == 500 || apiErr.Status == 502 || apiErr.Status == 503
	}
	return false
}
