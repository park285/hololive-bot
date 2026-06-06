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

	// AdminAllowedIPs: admin-api /api/auth/* 접근을 허용할 CIDR 목록.
	// admin-api는 Tailscale 직결(reverse-proxy 없음)이라 RemoteAddr 기준으로 판단한다.
	// 예: ADMIN_ALLOWED_IPS="100.100.1.0/24". 비어 있으면 전체 허용(개발 편의).
	AdminAllowedIPs []string
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
}

type ServicesConfig struct {
	LLMSchedulerHealthURL   string
	GameBotTwentyQHealthURL string
	GameBotTurtleHealthURL  string
}

type CORSConfig struct {
	AllowedOrigins      []string
	Enforce             bool
	MissingInProduction bool
}

type WebhookConfig struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
	MaxBodyBytes   int64
	DedupTTL       time.Duration
	DedupTimeout   time.Duration
	RequireHTTP2   bool
}

type WorkerPoolConfig struct {
	Workers   int
	QueueSize int
}

type WorkerProfileConfig struct {
	Version int
	Hash    string
}
