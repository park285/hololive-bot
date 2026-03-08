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

package constants

import "time"

// AppTimeout: 앱 빌드/종료 타임아웃 설정입니다.
var AppTimeout = struct {
	Build    time.Duration
	Shutdown time.Duration
}{
	Build:    30 * time.Second,
	Shutdown: 10 * time.Second,
}

// ServerTimeout: HTTP 서버 타임아웃입니다.
var ServerTimeout = struct {
	ReadHeader     time.Duration
	Read           time.Duration
	Write          time.Duration
	Idle           time.Duration
	MaxHeaderBytes int
}{
	ReadHeader:     5 * time.Second,
	Read:           15 * time.Second,
	Write:          60 * time.Second,
	Idle:           60 * time.Second,
	MaxHeaderBytes: 1 << 20, // 1MiB
}

// ServerConfig: 서버 기본 설정입니다.
var ServerConfig = struct {
	TrustedProxies []string
	MaxBodyBytes   int64 // 요청 본문 최대 크기 (바이트)
}{
	TrustedProxies: []string{"127.0.0.1", "::1"},
	MaxBodyBytes:   1 << 20, // 1MiB
}

// CORSConfig: CORS 기본 설정입니다.
var CORSConfig = struct {
	AllowMethods []string
	AllowHeaders []string
}{
	AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
	AllowHeaders: []string{
		"Origin", "Content-Type", "Accept", "Authorization",
		// Client Hints 헤더 (실제 기기 정보 수집용)
		"Sec-CH-UA", "Sec-CH-UA-Mobile", "Sec-CH-UA-Platform",
		"Sec-CH-UA-Platform-Version", "Sec-CH-UA-Model",
		"Sec-CH-UA-Arch", "Sec-CH-UA-Bitness", "Sec-CH-UA-Full-Version-List",
	},
}

// RequestTimeout: HTTP 요청 및 서비스 타임아웃 설정
var RequestTimeout = struct {
	AdminRequest      time.Duration
	BotCommand        time.Duration
	BotAlarmCheck     time.Duration
	WebhookProcessing time.Duration
	AlarmService      time.Duration
	DatabasePing      time.Duration
}{
	AdminRequest:      10 * time.Second,
	BotCommand:        10 * time.Second,
	BotAlarmCheck:     2 * time.Minute,
	WebhookProcessing: 30 * time.Second,
	AlarmService:      10 * time.Second,
	DatabasePing:      5 * time.Second,
}

// LLMHTTPTimeout: LLM HTTP 클라이언트 타임아웃 설정
var LLMHTTPTimeout = struct {
	Request        time.Duration
	Dial           time.Duration
	TLSHandshake   time.Duration
	ResponseHeader time.Duration
	IdleConn       time.Duration
}{
	Request:        2 * time.Minute,
	Dial:           5 * time.Second,
	TLSHandshake:   5 * time.Second,
	ResponseHeader: 60 * time.Second, // GPT-5.4 요약 요청은 첫 헤더까지 수십 초가 걸릴 수 있다.
	IdleConn:       90 * time.Second,
}

// IrisConnection: Bot 시작 시 Iris 연결 준비 대기 설정입니다.
var IrisConnection = struct {
	ReadyTimeout  time.Duration
	RetryInterval time.Duration
	PingTimeout   time.Duration
}{
	ReadyTimeout:  10 * time.Minute,
	RetryInterval: 2 * time.Second,
	PingTimeout:   3 * time.Second,
}

// IrisWebhookDedupTTL: Iris -> Bot webhook 메시지 중복 처리 방지용 TTL 입니다.
// Iris 측 재시도(단기)에서 동일 메시지 ID를 스킵하기 위한 목적입니다.
var IrisWebhookDedupTTL = 60 * time.Second

// DatabaseConfig: 데이터베이스 연결 설정입니다.
var DatabaseConfig = struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}{
	MaxOpenConns:    25,
	MaxIdleConns:    5,
	ConnMaxLifetime: 5 * time.Minute,
}

// QueryTimeout: DB 쿼리 타임아웃 기본값입니다.
var QueryTimeout = struct {
	Default time.Duration
	Long    time.Duration
}{
	Default: 5 * time.Second,
	Long:    30 * time.Second,
}

// DatabaseDefaults: PostgreSQL 기본값이다. (env 미설정 시)
var DatabaseDefaults = struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}{
	Host:     "postgres",
	Port:     5432,
	User:     "hololive_runtime",
	Password: "",         // 반드시 환경변수로 설정 필요
	Database: "hololive", // hololive-kakao-bot-go 전용 DB
}
