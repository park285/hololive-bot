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

package auth

import "fmt"

// ErrorCode: API 스펙에서 정의한 인증 오류 코드
type ErrorCode string

const (
	CodeInvalidInput       ErrorCode = "INVALID_INPUT"
	CodeEmailExists        ErrorCode = "EMAIL_EXISTS"
	CodeInvalidCredentials ErrorCode = "INVALID_CREDENTIALS" //nolint:gosec // G101: 인증 실패 코드 문자열일 뿐 credentials가 아님
	CodeAccountLocked      ErrorCode = "ACCOUNT_LOCKED"
	CodeRateLimited        ErrorCode = "RATE_LIMITED"
	CodeUnauthorized       ErrorCode = "UNAUTHORIZED"
	CodeInternal           ErrorCode = "INTERNAL_ERROR"
)

// Error: 서비스 레벨 에러 (HTTP 레이어에서 status/code로 매핑)
type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil && e.Message == "" {
		return fmt.Sprintf("auth error code=%s", e.Code)
	}
	if e.Err == nil {
		return fmt.Sprintf("auth error code=%s: %s", e.Code, e.Message)
	}
	if e.Message == "" {
		return fmt.Sprintf("auth error code=%s: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("auth error code=%s: %s: %v", e.Code, e.Message, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError: exported 생성자 (외부 모듈에서 사용 가능)
func NewError(code ErrorCode, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

func newError(code ErrorCode, message string, err error) *Error {
	return NewError(code, message, err)
}
