// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package errors

import (
	"fmt"
	"strings"
)

type APIError struct {
	Operation  string
	StatusCode int
	Message    string
	Err        error
}

func (e APIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "api error"
	}

	if e.Err == nil {
		return fmt.Sprintf("%s operation=%s status=%d", message, e.Operation, e.StatusCode)
	}

	return fmt.Sprintf("%s operation=%s status=%d: %v", message, e.Operation, e.StatusCode, e.Err)
}

func (e APIError) Unwrap() error { return e.Err }

func NewAPIError(message string, statusCode int, context map[string]any) *APIError {
	op := message

	if context != nil {
		if v, ok := context["operation"]; ok {
			if opStr, ok := v.(string); ok {
				op = opStr
			}
		}
	}

	return &APIError{
		Operation:  op,
		StatusCode: statusCode,
		Message:    message,
	}
}

type KeyRotationError struct {
	Operation  string
	StatusCode int
}

func (e KeyRotationError) Error() string {
	return fmt.Sprintf("key rotation exhausted operation=%s status=%d", e.Operation, e.StatusCode)
}

func NewKeyRotationError(message string, statusCode int, context map[string]any) *KeyRotationError {
	op := message

	if v, ok := context["url"]; ok {
		if urlStr, ok := v.(string); ok {
			op = urlStr
		}
	}

	return &KeyRotationError{
		Operation:  op,
		StatusCode: statusCode,
	}
}

type CacheError struct {
	Operation string
	Key       string
	Message   string
	Err       error
}

func (e CacheError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "cache error"
	}

	if e.Err == nil {
		return fmt.Sprintf("%s operation=%s key=%s", message, e.Operation, e.Key)
	}

	return fmt.Sprintf("%s operation=%s key=%s: %v", message, e.Operation, e.Key, e.Err)
}

func (e CacheError) Unwrap() error { return e.Err }

func NewCacheError(message, operation, key string, cause error) *CacheError {
	return &CacheError{
		Operation: operation,
		Key:       key,
		Message:   message,
		Err:       cause,
	}
}

type CircuitOpenError struct {
	RetryAfterMs int64
}

func (e CircuitOpenError) Error() string {
	return fmt.Sprintf("circuit breaker open retry_after_ms=%d", e.RetryAfterMs)
}

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}

	return fmt.Sprintf("validation error field=%s: %s", e.Field, e.Message)
}

func NewValidationError(message, field string, value any) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

type ServiceError struct {
	Service   string
	Operation string
	Message   string
	Err       error
}

func (e ServiceError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "service error"
	}

	if e.Err == nil {
		return fmt.Sprintf("%s service=%s operation=%s", message, e.Service, e.Operation)
	}

	return fmt.Sprintf("%s service=%s operation=%s: %v", message, e.Service, e.Operation, e.Err)
}

func (e ServiceError) Unwrap() error { return e.Err }

func NewServiceError(message, service, operation string, cause error) *ServiceError {
	return &ServiceError{
		Service:   service,
		Operation: operation,
		Message:   message,
		Err:       cause,
	}
}
