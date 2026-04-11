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

package config

import "time"

// ServerConfig: HTTP 서버 바인딩 포트 및 API 인증 설정
type ServerConfig struct {
	Port   int
	APIKey string // API 인증용 시크릿 키 (X-API-Key 헤더로 검증)
}

// HolodexConfig: Holodex API 키 및 호출 관련 설정
type HolodexConfig struct {
	BaseURL string
	APIKey  string
}

// YouTubeConfig: YouTube Data API 키 및 Quota 관리 설정
type YouTubeConfig struct {
	APIKey              string
	EnableQuotaBuilding bool
}

// IngestionConfig: ingestion 런타임 기능 토글 설정
type IngestionConfig struct {
	YouTubeEnabled                  bool
	PhotoSyncEnabled                bool
	CommunityShortsBigBangEnabled   bool
	CommunityShortsBigBangCutoverAt time.Time
}

// LoggingConfig: 애플리케이션 로그 설정
type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

// BotConfig: 봇의 기본 동작(명령어 접두사, 자기 자신 식별자) 설정
type BotConfig struct {
	Prefix        string
	SelfUser      string
	AdminEnabled  bool
	MentionPrefix string // 멘션 기반 명령어 접두사 (예: @카푸봇)
}

// ServicesConfig: 외부 Go 서비스 연결 설정 (goroutine 통합 모니터링용)
type ServicesConfig struct {
	LLMSchedulerHealthURL   string // llm-scheduler health URL
	GameBotTwentyQHealthURL string // game-bot-go twentyq health URL
	GameBotTurtleHealthURL  string // game-bot-go turtlesoup health URL
}

type ScraperPoll struct {
	Videos    time.Duration
	Shorts    time.Duration
	Community time.Duration
	Stats     time.Duration
	Live      time.Duration
}

func DefaultScraperWorkerCount() int {
	return 4
}

// ScraperConfig: YouTube 스크래퍼 프록시 설정 (SOCKS5)
type ScraperConfig struct {
	ProxyEnabled bool   // 프록시 사용 여부
	ProxyURL     string // SOCKS5 프록시 URL (예: socks5://user:pass@host:1080)
	WorkerCount  int
	Poll         ScraperPoll
}

func DefaultScraperPoll() ScraperPoll {
	return ScraperPoll{
		Videos:    15 * time.Minute,
		Shorts:    30 * time.Minute,
		Community: 30 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}
}

func (p ScraperPoll) EstimatedRequestsPerMinute() float64 {
	var rpm float64
	if p.Videos > 0 {
		rpm += 60.0 / p.Videos.Seconds()
	}
	if p.Shorts > 0 {
		rpm += 60.0 / p.Shorts.Seconds()
	}
	if p.Community > 0 {
		rpm += 60.0 / p.Community.Seconds()
	}
	if p.Stats > 0 {
		rpm += 60.0 / p.Stats.Seconds()
	}
	if p.Live > 0 {
		rpm += 60.0 / p.Live.Seconds()
	}
	return rpm
}

func (c ScraperConfig) PollOrDefault() ScraperPoll {
	poll := DefaultScraperPoll()

	if c.Poll.Videos > 0 {
		poll.Videos = c.Poll.Videos
	}
	if c.Poll.Shorts > 0 {
		poll.Shorts = c.Poll.Shorts
	}
	if c.Poll.Community > 0 {
		poll.Community = c.Poll.Community
	}
	if c.Poll.Stats > 0 {
		poll.Stats = c.Poll.Stats
	}
	if c.Poll.Live > 0 {
		poll.Live = c.Poll.Live
	}

	return poll
}

func (c ScraperConfig) WorkerCountOrDefault() int {
	if c.WorkerCount > 0 {
		return c.WorkerCount
	}

	return DefaultScraperWorkerCount()
}

// CORSConfig: CORS 허용 Origin 설정
type CORSConfig struct {
	AllowedOrigins      []string
	Enforce             bool
	MissingInProduction bool
}

// WebhookConfig: Iris 웹훅 워커 및 큐 설정
type WebhookConfig struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
	RequireHTTP2   bool
}

// ChzzkConfig: 치지직 Open API 설정 (Client 인증 방식)
type ChzzkConfig struct {
	ClientID     string
	ClientSecret string
}

// TwitchConfig: Twitch Helix API 설정 (Client Credentials 인증)
type TwitchConfig struct {
	ClientID     string
	ClientSecret string
}
