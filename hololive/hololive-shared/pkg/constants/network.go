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

var AppTimeout = struct {
	Build    time.Duration
	Shutdown time.Duration
}{
	Build:    30 * time.Second,
	Shutdown: 10 * time.Second,
}

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

var IrisConnection = struct {
	ReadyTimeout  time.Duration
	RetryInterval time.Duration
	PingTimeout   time.Duration
}{
	ReadyTimeout:  10 * time.Minute,
	RetryInterval: 2 * time.Second,
	PingTimeout:   3 * time.Second,
}

var DatabaseConfig = struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// ConnMaxLifetimeJitter·ConnMaxIdleTime은 shared-go v2.18.0이 숨은 기본값을 없애며 호출자 명시로 옮긴 값이다.
	// 이전 실효값(수명/5 지터, 유휴 30분)을 그대로 둔다.
	ConnMaxLifetimeJitter time.Duration
	ConnMaxIdleTime       time.Duration
}{
	MaxOpenConns:          8,
	MaxIdleConns:          2,
	ConnMaxLifetime:       5 * time.Minute,
	ConnMaxLifetimeJitter: time.Minute,
	ConnMaxIdleTime:       30 * time.Minute,
}

var QueryTimeout = struct {
	Default time.Duration
	Long    time.Duration
}{
	Default: 5 * time.Second,
	Long:    30 * time.Second,
}

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
