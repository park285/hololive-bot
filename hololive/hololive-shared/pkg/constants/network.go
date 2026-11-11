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
	AdminRequest time.Duration
	BotCommand   time.Duration
	DatabasePing time.Duration
}{
	AdminRequest: 10 * time.Second,
	BotCommand:   10 * time.Second,
	DatabasePing: 5 * time.Second,
}

var DatabaseConfig = struct {
	MaxOpenConns int
	MaxIdleConns int
	// 불필요한 재연결과 TLS 왕복을 줄이도록 다른 스택 Go 서비스(TwentyQ·ChatBotGo)와 같은 1시간을 쓴다.
	ConnMaxLifetime time.Duration
	// ConnMaxLifetimeJitter·ConnMaxIdleTime은 shared-go v2.18.0이 숨은 기본값을 없애며 호출자 명시로 옮긴 값이다.
	// 지터는 수명/5 비율을 유지해 동시 만료를 분산하고, 유휴 30분은 그대로 둔다.
	ConnMaxLifetimeJitter time.Duration
	ConnMaxIdleTime       time.Duration
}{
	MaxOpenConns:          8,
	MaxIdleConns:          2,
	ConnMaxLifetime:       time.Hour,
	ConnMaxLifetimeJitter: 12 * time.Minute,
	ConnMaxIdleTime:       30 * time.Minute,
}

var QueryTimeout = struct {
	Default time.Duration
}{
	Default: 5 * time.Second,
}

var DatabaseDefaults = struct {
	Host     string
	Port     int
	User     string
	Database string
}{
	Host:     "postgres",
	Port:     5432,
	User:     "hololive_runtime",
	Database: "hololive", // hololive-kakao-bot-go 전용 DB
}
