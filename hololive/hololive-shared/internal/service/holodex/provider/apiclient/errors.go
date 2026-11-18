package apiclient

import "fmt"

type APIError struct {
	Operation  string
	StatusCode int
	Err        error
}

func (e *APIError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("holodex: api: %s: status=%d", e.Operation, e.StatusCode)
	}

	return fmt.Sprintf("holodex: api: %s: status=%d: %v", e.Operation, e.StatusCode, e.Err)
}

func (e *APIError) Unwrap() error { return e.Err }

func NewAPIError(operation string, statusCode int) *APIError {
	return &APIError{
		Operation:  operation,
		StatusCode: statusCode,
	}
}
