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

// Package dbx: PostgreSQL 연결 공통 모듈
// pgxpool 지원, DSN 생성(UDS/TCP), Retry, Ping/Close 헬퍼 제공
package dbx

import (
	"fmt"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

var queryExecModeNames = map[string]string{
	"cache_statement": "cache_statement",
	"cache_describe":  "cache_describe",
	"describe_exec":   "describe_exec",
	"exec":            "exec",
	"simple_protocol": "simple_protocol",
}

type Config struct {
	Host       string // TCP 호스트 (예: "localhost", "postgres")
	Port       int    // TCP 포트 (예: 5432)
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
	User       string
	Password   string //nolint:gosec // 설정 구조체 필드명이며 시크릿 값 자체를 로그/출력하지 않는다.
	Name       string // 데이터베이스 이름
	SSLMode    string // sslmode (기본: "require")
	// QueryExecMode: pgx default_query_exec_mode
	// 허용값: cache_statement, cache_describe, describe_exec, exec, simple_protocol
	QueryExecMode string
}

func (c Config) SafeDSN() string {
	masked := c
	if masked.Password != "" {
		masked.Password = "***"
	}
	return masked.DSN()
}

func (c Config) DSN() string {
	sslmode := c.SSLMode
	if sslmode == "" {
		sslmode = "require"
	}
	queryExecMode := normalizeQueryExecMode(c.QueryExecMode)
	queryExecModePart := ""
	if queryExecMode != "" {
		queryExecModePart = " default_query_exec_mode=" + queryExecMode
	}
	if c.SocketPath != "" {
		return fmt.Sprintf(
			"host=%s user=%s password=%s dbname=%s sslmode=%s%s",
			c.SocketPath, c.User, c.Password, c.Name, sslmode, queryExecModePart,
		)
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s%s",
		c.Host, c.Port, c.User, c.Password, c.Name, sslmode, queryExecModePart,
	)
}

func normalizeQueryExecMode(mode string) string {
	normalized, ok := queryExecModeNames[strings.ToLower(strings.TrimSpace(mode))]
	if !ok {
		return ""
	}
	return normalized
}

type PoolConfig struct {
	MinConns        int           // 최소 연결 수 (pgxpool용, 기본: 5)
	MaxConns        int           // 최대 연결 수 (기본: 20)
	MaxIdleConns    int           // 최대 유휴 연결 수 (pgxpool용, 0이면 MinConns로 fallback)
	ConnMaxLifetime time.Duration // 연결 최대 수명 (기본: 1시간)
	ConnMaxIdleTime time.Duration // 유휴 연결 최대 시간 (기본: 30분)
}

// 환경변수로 오버라이드 가능: DB_POOL_MIN_CONNS, DB_POOL_MAX_CONNS, DB_POOL_MAX_IDLE_CONNS
func DefaultPoolConfig() PoolConfig {
	minConns := clamp(sharedenv.Int("DB_POOL_MIN_CONNS", 5), 1, 100)
	maxConns := clamp(sharedenv.Int("DB_POOL_MAX_CONNS", 20), 1, 200)
	maxIdleConns := sharedenv.Int("DB_POOL_MAX_IDLE_CONNS", 0)

	return PoolConfig{
		MinConns:        minConns,
		MaxConns:        maxConns,
		MaxIdleConns:    maxIdleConns, // 0이면 MinConns 사용
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	}
}

func clamp(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

type RetryConfig struct {
	MaxAttempts int           // 최대 시도 횟수 (기본: 5)
	BaseDelay   time.Duration // 초기 대기 시간 (기본: 2초)
	MaxDelay    time.Duration // 최대 대기 시간 (기본: 30초)
	PingTimeout time.Duration // Ping 타임아웃 (기본: 5초)
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   2 * time.Second,
		MaxDelay:    30 * time.Second,
		PingTimeout: 5 * time.Second,
	}
}
