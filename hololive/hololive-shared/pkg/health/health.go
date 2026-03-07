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

// Package health: 서비스 상태 정보
package health

import (
	"runtime"
	"sync"
	"time"
)

var (
	startTime time.Time
	version   = "dev"
	initOnce  sync.Once
)

// Init: 서비스 시작 시 호출 (버전 정보 설정)
func Init(v string) {
	initOnce.Do(func() {
		startTime = time.Now()
		if v != "" {
			version = v
		}
	})
}

// Response: /health 엔드포인트 표준 응답
type Response struct {
	Status     string `json:"status"`
	Version    string `json:"version"`
	Uptime     string `json:"uptime"`
	Goroutines int    `json:"goroutines"`
}

// Get: 현재 상태 반환
func Get() Response {
	return Response{
		Status:     "ok",
		Version:    version,
		Uptime:     formatDuration(time.Since(startTime)),
		Goroutines: runtime.NumGoroutine(),
	}
}

// GetVersion: 현재 버전 반환
func GetVersion() string {
	return version
}

// GetUptime: 현재 uptime 반환 (포맷팅된 문자열)
func GetUptime() string {
	return formatDuration(time.Since(startTime))
}

// formatDuration: Duration을 사람이 읽기 쉬운 형식으로 변환
func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
