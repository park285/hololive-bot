// Package errors: Hololive 봇 서비스 전체에서 사용되는 에러 타입들을 정의합니다.
// game-bot-go의 에러 패턴과 동일한 표준 Go 에러 스타일을 따른다.
package errors

import "fmt"

// APIError: 외부 API 호출 중 발생한 에러 (Holodex, YouTube 등)
type APIError struct {
	Operation  string // 수행 중이던 API 작업
	StatusCode int    // HTTP 상태 코드 (0이면 네트워크 오류)
	Err        error  // 원인 에러
}

func (e APIError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("api error operation=%s status=%d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("api error operation=%s status=%d: %v", e.Operation, e.StatusCode, e.Err)
}

func (e APIError) Unwrap() error { return e.Err }

// NewAPIError: API 에러를 생성합니다.
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

// KeyRotationError: 모든 API 키가 사용 불가능할 때 발생하는 에러
type KeyRotationError struct {
	Operation  string
	StatusCode int
}

func (e KeyRotationError) Error() string {
	return fmt.Sprintf("key rotation exhausted operation=%s status=%d", e.Operation, e.StatusCode)
}

// NewKeyRotationError: 키 로테이션 에러를 생성합니다.
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

// CacheError: 캐시 작업 중 발생한 에러
type CacheError struct {
	Operation string // get, set, delete 등
	Key       string // 캐시 키
	Err       error  // 원인 에러
}

func (e CacheError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("cache error operation=%s key=%s", e.Operation, e.Key)
	}
	return fmt.Sprintf("cache error operation=%s key=%s: %v", e.Operation, e.Key, e.Err)
}

func (e CacheError) Unwrap() error { return e.Err }

// NewCacheError: 캐시 에러를 생성합니다.
func NewCacheError(message, operation, key string, cause error) *CacheError {
	return &CacheError{
		Operation: operation,
		Key:       key,
		Err:       cause,
	}
}

// CircuitOpenError: 서킷 브레이커가 열려있을 때 발생하는 에러
type CircuitOpenError struct {
	RetryAfterMs int64
}

func (e CircuitOpenError) Error() string {
	return fmt.Sprintf("circuit breaker open retry_after_ms=%d", e.RetryAfterMs)
}

// ValidationError: 입력 검증 실패 에러
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

// NewValidationError: 검증 에러를 생성합니다.
func NewValidationError(message, field string, value any) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// ServiceError: 내부 서비스 로직 에러
type ServiceError struct {
	Service   string // 서비스 이름
	Operation string // 작업 이름
	Err       error  // 원인 에러
}

func (e ServiceError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("service error service=%s operation=%s", e.Service, e.Operation)
	}
	return fmt.Sprintf("service error service=%s operation=%s: %v", e.Service, e.Operation, e.Err)
}

func (e ServiceError) Unwrap() error { return e.Err }

// NewServiceError: 서비스 에러를 생성합니다.
func NewServiceError(message, service, operation string, cause error) *ServiceError {
	return &ServiceError{
		Service:   service,
		Operation: operation,
		Err:       cause,
	}
}
