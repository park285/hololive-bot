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

package holodexprovider

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

// NewAPIError는 context 파라미터를 레거시 호환을 위해 유지합니다(현재는 operation만 읽음).
func NewAPIError(message string, statusCode int, context map[string]any) *APIError {
	op := message
	if v, ok := context["operation"]; ok {
		if opStr, ok := v.(string); ok {
			op = opStr
		}
	}
	return &APIError{
		Operation:  op,
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

// NewKeyRotationError는 context 파라미터를 레거시 호환을 위해 유지합니다(현재는 url만 operation으로 사용).
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
