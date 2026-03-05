// Package httputil: HTTP 클라이언트 공통 유틸리티
package httputil

import (
	"net/http"
	"time"
)

// NewClient: 지정 타임아웃의 HTTP 클라이언트 반환
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// DefaultClient: 30초 타임아웃 기본 클라이언트 반환
func DefaultClient() *http.Client {
	return NewClient(30 * time.Second)
}
