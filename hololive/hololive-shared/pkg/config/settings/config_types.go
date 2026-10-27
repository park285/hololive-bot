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

package settings

import "time"

type ServerConfig struct {
	Port           int
	APIKey         string // API 인증용 시크릿 키 (X-API-Key 헤더로 검증)
	HTTPTransports []string
	H3Addr         string
	H3CertFile     string
	H3KeyFile      string
	ShortLinkAddr  string

	// MetricsAddr: Prometheus scrape용 평문 HTTP/1.1 /metrics 전용 리스너 주소.
	// 운영 표면이 H3 전용이라 Prometheus가 직접 scrape하지 못해 분리한다(PR-P6-01 0단계).
	// 빈 값이면 비활성. compose에서 127.0.0.1(또는 Tailscale IP)로만 publish한다.
	MetricsAddr string

	// PprofAddr: net/http/pprof debug 리스너 주소. 빈 값이면 비활성.
	// 인증이 없으므로 compose에서 loopback(127.0.0.1) 또는 Tailscale IP로만 publish한다.
	PprofAddr string
}

type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

type BotConfig struct {
	Prefix                string
	SelfUser              string
	MentionPrefix         string // 멘션 기반 명령어 접두사 (예: @카푸봇)
	CalendarImageCacheDir string
	CalendarEntryCacheTTL time.Duration
	SeeMoreFold           bool // 기능·표시 항목 수에 따라 조회와 보고서를 머리 문단과 '전체보기'로 접는다. 기본 true, false는 운영 차단 스위치
	MarkdownReplies       bool
}

type CORSConfig struct {
	AllowedOrigins      []string
	Enforce             bool
	MissingInProduction bool
}
