package contract

import "net/http"

// AppError는 Gin에 의존하지 않는 관리자 HTTP 오류와 비공개 원인을 보존합니다.
type AppError struct {
	Status int
	Body   ErrorResponse
	Cause  error
}

// Error는 내부 진단용 원인을 반환하며 외부 응답에는 Body만 사용해야 합니다.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}

	return e.Body.Error
}

// Unwrap은 원인 오류를 보존합니다.
func (e *AppError) Unwrap() error { return e.Cause }

// NewError는 HTTP 경계에서 보낼 안전한 상태와 메시지를 구성합니다.
func NewError(status int, message string) *AppError {
	return &AppError{Status: status, Body: ErrorResponse{Error: message}}
}

// Unauthorized는 관리자 세션 인증 실패를 나타냅니다.
func Unauthorized() *AppError { return NewError(http.StatusUnauthorized, "Unauthorized") }

// Forbidden은 확인한 접근 거부를 나타냅니다.
func Forbidden() *AppError { return NewError(http.StatusForbidden, "Forbidden") }

// BadGateway는 upstream 결과를 확인하지 못했음을 나타냅니다.
func BadGateway() *AppError { return NewError(http.StatusBadGateway, "Service unavailable") }

// StoreUnavailable은 세션 저장소 실패를 인증 성공으로 바꾸지 않습니다.
func StoreUnavailable() *AppError {
	return NewError(http.StatusServiceUnavailable, "Session store unavailable")
}

// BadRequest는 전송·효과 확인과 별개인 입력 거부를 나타냅니다.
func BadRequest(message string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Body: ErrorResponse{Error: message, Code: "BAD_REQUEST"}}
}

// Internal은 내부 원인을 보존하되 외부 메시지에는 포함하지 않습니다.
func Internal(err error) *AppError {
	return &AppError{Status: http.StatusInternalServerError, Body: ErrorResponse{Error: "An internal error occurred"}, Cause: err}
}
