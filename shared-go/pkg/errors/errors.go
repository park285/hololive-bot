// Package errors: 공통 인프라 에러 타입을 정의합니다.
package errors

import (
	"errors"
	"fmt"
)

// 공통 센티널 에러
var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyExists     = errors.New("already exists")
	ErrInvalidInput      = errors.New("invalid input")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrTimeout           = errors.New("timeout")
	ErrRateLimited       = errors.New("rate limited")
	ErrServiceDown       = errors.New("service unavailable")
	ErrInternalServer    = errors.New("internal server error")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionExpired    = errors.New("session expired")
	ErrToolExecution     = errors.New("tool execution failed")
	ErrOAuthTokenExpired = errors.New("oauth token expired")
	ErrEncryption        = errors.New("encryption failed")
)

// RedisError: Redis/Valkey 관련 에러입니다.
type RedisError struct {
	Op  string // 실행한 작업 (예: "GET", "SET", "HGET")
	Key string // 관련 키 (있는 경우)
	Err error  // 원본 에러
}

func (e *RedisError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("redis %s [%s]: %v", e.Op, e.Key, e.Err)
	}
	return fmt.Sprintf("redis %s: %v", e.Op, e.Err)
}

func (e *RedisError) Unwrap() error {
	return e.Err
}

// NewRedisError: RedisError를 생성합니다.
func NewRedisError(op, key string, err error) *RedisError {
	return &RedisError{Op: op, Key: key, Err: err}
}

// DatabaseError: 데이터베이스 관련 에러입니다.
type DatabaseError struct {
	Op    string // 실행한 작업 (예: "query", "insert", "update")
	Table string // 관련 테이블 (있는 경우)
	Err   error  // 원본 에러
}

func (e *DatabaseError) Error() string {
	if e.Table != "" {
		return fmt.Sprintf("database %s [%s]: %v", e.Op, e.Table, e.Err)
	}
	return fmt.Sprintf("database %s: %v", e.Op, e.Err)
}

func (e *DatabaseError) Unwrap() error {
	return e.Err
}

// NewDatabaseError: DatabaseError를 생성합니다.
func NewDatabaseError(op, table string, err error) *DatabaseError {
	return &DatabaseError{Op: op, Table: table, Err: err}
}

// ExternalAPIError: 외부 API 호출 관련 에러입니다.
type ExternalAPIError struct {
	Service    string // 서비스 이름 (예: "gemini", "kakao")
	StatusCode int    // HTTP 상태 코드 (있는 경우)
	Err        error  // 원본 에러
}

func (e *ExternalAPIError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("external api %s [%d]: %v", e.Service, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("external api %s: %v", e.Service, e.Err)
}

func (e *ExternalAPIError) Unwrap() error {
	return e.Err
}

// NewExternalAPIError: ExternalAPIError를 생성합니다.
func NewExternalAPIError(service string, statusCode int, err error) *ExternalAPIError {
	return &ExternalAPIError{Service: service, StatusCode: statusCode, Err: err}
}

// APIError: 외부 API 호출 관련 에러입니다 (Message 필드 포함).
type APIError struct {
	API        string
	StatusCode int
	Message    string
	Err        error
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("api %s: %s", e.API, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("api %s: %v", e.API, e.Err)
	}
	return fmt.Sprintf("api %s: unknown error", e.API)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// NewAPIError: APIError를 생성합니다.
func NewAPIError(api string, statusCode int, message string, err error) *APIError {
	return &APIError{
		API:        api,
		StatusCode: statusCode,
		Message:    message,
		Err:        err,
	}
}

// ToolError: 도구 실행 에러를 컨텍스트와 함께 래핑합니다.
type ToolError struct {
	ToolName string
	Err      error
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("tool %s: %v", e.ToolName, e.Err)
}

func (e *ToolError) Unwrap() error {
	return e.Err
}

// NewToolError: ToolError를 생성합니다.
func NewToolError(toolName string, err error) *ToolError {
	return &ToolError{ToolName: toolName, Err: err}
}

// Is: 에러 타입 비교를 위한 헬퍼 함수입니다.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As: 에러 타입 변환을 위한 헬퍼 함수입니다.
func As(err error, target any) bool {
	return errors.As(err, target)
}
