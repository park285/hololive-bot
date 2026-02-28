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

func newError(code ErrorCode, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}
