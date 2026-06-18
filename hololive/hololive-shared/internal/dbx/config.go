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

package dbx

import (
	"strconv"
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
	Host        string // TCP 호스트 (예: "localhost", "postgres")
	Port        int    // TCP 포트 (예: 5432)
	SocketPath  string // UDS 경로 (비어있으면 TCP 사용)
	User        string
	Password    string
	Name        string // 데이터베이스 이름
	SSLMode     string // sslmode (기본: "verify-full")
	SSLRootCert string
	// QueryExecMode: pgx default_query_exec_mode
	// 허용값: cache_statement, cache_describe, describe_exec, exec, simple_protocol
	QueryExecMode string
}

func (c *Config) SafeDSN() string {
	if c == nil {
		return ""
	}
	masked := *c
	if masked.Password != "" {
		masked.Password = "***"
	}
	return masked.DSN()
}

func (c *Config) DSN() string {
	if c == nil {
		return ""
	}
	sslmode := c.SSLMode
	if sslmode == "" {
		sslmode = "verify-full"
	}
	sslRootCert := strings.TrimSpace(c.SSLRootCert)
	if sslRootCert == "" {
		sslRootCert = strings.TrimSpace(sharedenv.String("POSTGRES_SSLROOTCERT", ""))
	}
	queryExecMode := normalizeQueryExecMode(c.QueryExecMode)

	parts := make([]string, 0, 8)
	if c.SocketPath != "" {
		parts = append(parts, libpqKeywordValue("host", c.SocketPath))
	} else {
		parts = append(parts,
			libpqKeywordValue("host", c.Host),
			"port="+strconv.Itoa(c.Port),
		)
	}
	parts = append(parts,
		libpqKeywordValue("user", c.User),
		libpqKeywordValue("password", c.Password),
		libpqKeywordValue("dbname", c.Name),
		libpqKeywordValue("sslmode", sslmode),
	)
	if sslRootCert != "" {
		parts = append(parts, libpqKeywordValue("sslrootcert", sslRootCert))
	}
	if queryExecMode != "" {
		parts = append(parts, libpqKeywordValue("default_query_exec_mode", queryExecMode))
	}
	return strings.Join(parts, " ")
}

func libpqKeywordValue(key, value string) string {
	return key + "=" + libpqQuote(value)
}

func libpqQuote(value string) string {
	var builder strings.Builder
	builder.Grow(len(value) + 2)
	builder.WriteByte('\'')
	for _, char := range value {
		if char == '\\' || char == '\'' {
			builder.WriteByte('\\')
		}
		builder.WriteRune(char)
	}
	builder.WriteByte('\'')
	return builder.String()
}

func normalizeQueryExecMode(mode string) string {
	normalized, ok := queryExecModeNames[strings.ToLower(strings.TrimSpace(mode))]
	if !ok {
		return ""
	}
	return normalized
}

type PoolConfig struct {
	MinConns              int           // 최소 연결 수 (pgxpool용, 기본: 5)
	MaxConns              int           // 최대 연결 수 (기본: 20)
	ConnMaxLifetime       time.Duration // 연결 최대 수명 (기본: 1시간)
	ConnMaxLifetimeJitter time.Duration // 수명 만료 분산 폭 (0이면 ConnMaxLifetime/5)
	ConnMaxIdleTime       time.Duration // 유휴 연결 최대 시간 (기본: 30분)
}

// 환경변수로 오버라이드 가능: DB_POOL_MIN_CONNS, DB_POOL_MAX_CONNS
func DefaultPoolConfig() PoolConfig {
	minConns := clamp(sharedenv.Int("DB_POOL_MIN_CONNS", 5), 1, 100)
	maxConns := clamp(sharedenv.Int("DB_POOL_MAX_CONNS", 20), 1, 200)

	return PoolConfig{
		MinConns:        minConns,
		MaxConns:        maxConns,
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
