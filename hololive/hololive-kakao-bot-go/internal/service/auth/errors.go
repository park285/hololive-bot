package auth

// 타입 및 상수는 hololive-shared/pkg/service/auth에서 정의.
// 패키지 이름 충돌 방지를 위해 type alias로 re-export.

import sharedauth "github.com/kapu/hololive-shared/pkg/service/auth"

// ErrorCode: API 스펙에서 정의한 인증 오류 코드
type ErrorCode = sharedauth.ErrorCode

const (
	CodeInvalidInput       = sharedauth.CodeInvalidInput
	CodeEmailExists        = sharedauth.CodeEmailExists
	CodeInvalidCredentials = sharedauth.CodeInvalidCredentials //nolint:gosec // G101: 인증 실패 코드 문자열일 뿐 credentials가 아님
	CodeAccountLocked      = sharedauth.CodeAccountLocked
	CodeRateLimited        = sharedauth.CodeRateLimited
	CodeUnauthorized       = sharedauth.CodeUnauthorized
	CodeInternal           = sharedauth.CodeInternal
)

// Error: 서비스 레벨 에러 (HTTP 레이어에서 status/code로 매핑)
type Error = sharedauth.Error

func newError(code ErrorCode, message string, err error) *Error {
	return sharedauth.NewError(code, message, err)
}
