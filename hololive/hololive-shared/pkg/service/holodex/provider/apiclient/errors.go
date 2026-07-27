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

package apiclient

import "fmt"

// (기존 hololive-shared/pkg/errors.APIError 의존 제거를 위한 로컬 타입)
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

type KeyRotationError struct {
	Operation  string
	StatusCode int
	Err        error
}

func (e *KeyRotationError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("holodex: key rotation exhausted: %s: status=%d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("holodex: key rotation exhausted: %s: status=%d: %v", e.Operation, e.StatusCode, e.Err)
}

func (e *KeyRotationError) Unwrap() error { return e.Err }

func NewKeyRotationError(operation string, statusCode int) *KeyRotationError {
	return &KeyRotationError{
		Operation:  operation,
		StatusCode: statusCode,
	}
}
